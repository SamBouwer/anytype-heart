package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/globalsign/mgo/bson"
	"github.com/gogo/protobuf/types"
	"github.com/gosimple/slug"

	"github.com/anytypeio/go-anytype-middleware/app"
	"github.com/anytypeio/go-anytype-middleware/core/anytype/config"
	"github.com/anytypeio/go-anytype-middleware/core/block"
	sb "github.com/anytypeio/go-anytype-middleware/core/block/editor/smartblock"
	"github.com/anytypeio/go-anytype-middleware/core/block/process"
	"github.com/anytypeio/go-anytype-middleware/core/converter"
	"github.com/anytypeio/go-anytype-middleware/core/converter/dot"
	"github.com/anytypeio/go-anytype-middleware/core/converter/graphjson"
	"github.com/anytypeio/go-anytype-middleware/core/converter/md"
	"github.com/anytypeio/go-anytype-middleware/core/converter/pbc"
	"github.com/anytypeio/go-anytype-middleware/core/converter/pbjson"
	"github.com/anytypeio/go-anytype-middleware/pb"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/bundle"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/core"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/core/smartblock"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/database"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/localstore/addr"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/logging"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/pb/model"
	"github.com/anytypeio/go-anytype-middleware/util/pbtypes"
	"github.com/anytypeio/go-anytype-middleware/util/text"
)

const CName = "export"

const (
	profileFile  = "profile"
	tempFileName = "temp"
)

var log = logging.Logger("anytype-mw-export")

func New() Export {
	return new(export)
}

type Export interface {
	Export(req pb.RpcObjectListExportRequest) (path string, succeed int, err error)
	app.Component
}

type pathProvider interface {
	GetBlockStorePath() string
}

type export struct {
	bs           *block.Service
	a            core.Service
	pathProvider pathProvider
}

func (e *export) Init(a *app.App) (err error) {
	e.bs = a.MustComponent(block.CName).(*block.Service)
	e.a = a.MustComponent(core.CName).(core.Service)
	e.pathProvider = app.MustComponent[pathProvider](a)
	return
}

func (e *export) Name() (name string) {
	return CName
}

func (e *export) Export(req pb.RpcObjectListExportRequest) (path string, succeed int, err error) {
	queue := e.bs.Process().NewQueue(pb.ModelProcess{
		Id:    bson.NewObjectId().Hex(),
		Type:  pb.ModelProcess_Export,
		State: 0,
	}, 4)
	queue.SetMessage("prepare")

	if err = queue.Start(); err != nil {
		return
	}
	defer queue.Stop(err)

	docs, err := e.docsForExport(req.ObjectIds, req.IncludeNested, req.IncludeArchived)
	if err != nil {
		return
	}

	var wr writer
	if req.Zip {
		if wr, err = newZipWriter(req.Path, tempFileName); err != nil {
			return
		}
	} else {
		if wr, err = newDirWriter(req.Path, req.IncludeFiles); err != nil {
			return
		}
	}

	defer wr.Close()

	queue.SetMessage("export docs")
	if req.Format == pb.RpcObjectListExport_DOT || req.Format == pb.RpcObjectListExport_SVG {
		var format = dot.ExportFormatDOT
		if req.Format == pb.RpcObjectListExport_SVG {
			format = dot.ExportFormatSVG
		}
		mc := dot.NewMultiConverter(format)
		mc.SetKnownDocs(docs)
		var werr error
		if succeed, werr = e.writeMultiDoc(mc, wr, docs, queue); werr != nil {
			log.Warnf("can't export docs: %v", werr)
		}
	} else if req.Format == pb.RpcObjectListExport_GRAPH_JSON {
		mc := graphjson.NewMultiConverter()
		mc.SetKnownDocs(docs)
		var werr error
		if succeed, werr = e.writeMultiDoc(mc, wr, docs, queue); werr != nil {
			log.Warnf("can't export docs: %v", werr)
		}
	} else {
		if req.Format == pb.RpcObjectListExport_Protobuf {
			if len(req.ObjectIds) == 0 {
				if err = e.createProfileFile(wr); err != nil {
					log.Errorf("failed to create profile file: %s", err.Error())
				}
			}
			if req.IncludeConfig {
				wErr := e.writeConfig(wr)
				if wErr != nil {
					log.Errorf("failed to create profile file: %s", wErr.Error())
				}
			}
		}
		for docId := range docs {
			did := docId
			if err = queue.Wait(func() {
				log.With("threadId", did).Debugf("write doc")
				if werr := e.writeDoc(req.Format, wr, docs, queue, did, req.IncludeFiles); werr != nil {
					log.With("threadId", did).Warnf("can't export doc: %v", werr)
				} else {
					succeed++
				}
			}); err != nil {
				succeed = 0
				return
			}
		}
	}
	queue.SetMessage("export files")
	if err = queue.Finalize(); err != nil {
		succeed = 0
		return
	}
	zipName := getZipName(req.Path)
	err = os.Rename(wr.Path(), zipName)
	if err != nil {
		return
	}
	return zipName, succeed, nil
}

func (e *export) docsForExport(reqIds []string, includeNested bool, includeArchived bool) (docs map[string]*types.Struct, err error) {
	if len(reqIds) == 0 {
		return e.getAllObjects(includeArchived)
	}

	if len(reqIds) > 0 {
		return e.getObjectsByIDs(reqIds, includeNested)
	}
	return
}

func (e *export) getObjectsByIDs(reqIds []string, includeNested bool) (map[string]*types.Struct, error) {
	var res []*model.ObjectInfo
	docs := make(map[string]*types.Struct)
	res, _, err := e.a.ObjectStore().QueryObjectInfo(database.Query{
		Filters: []*model.BlockContentDataviewFilter{
			{
				RelationKey: bundle.RelationKeyId.String(),
				Condition:   model.BlockContentDataviewFilter_In,
				Value:       pbtypes.StringList(reqIds),
			},
			{
				RelationKey: bundle.RelationKeyIsArchived.String(),
				Condition:   model.BlockContentDataviewFilter_Equal,
				Value:       pbtypes.Bool(false),
			},
			{
				RelationKey: bundle.RelationKeyIsDeleted.String(),
				Condition:   model.BlockContentDataviewFilter_Equal,
				Value:       pbtypes.Bool(false),
			},
		},
	}, nil)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(res))
	for _, r := range res {
		docs[r.Id] = r.Details
		ids = append(ids, r.Id)
	}
	if includeNested {
		for _, id := range ids {
			e.getNested(id, docs)
		}
	}
	return docs, err
}

func (e *export) getAllObjects(includeArchived bool) (map[string]*types.Struct, error) {
	var res []*model.ObjectInfo
	if includeArchived {
		archivedObjects, err := e.getArchivedObjects()
		if err != nil {
			return nil, err
		}
		res = append(res, archivedObjects...)
	}
	objectDetails, err := e.getExistedObjects()
	if err != nil {
		return nil, err
	}
	for _, re := range res {
		if !e.objectValid(re.Id, re) {
			continue
		}
		objectDetails[re.Id] = re.Details
	}
	return objectDetails, nil
}

func (e *export) getNested(id string, docs map[string]*types.Struct) {
	links, err := e.a.ObjectStore().GetOutboundLinksById(id)
	if err != nil {
		log.Errorf("export failed to get outbound links for id: %s", err.Error())
		return
	}
	for _, link := range links {
		if _, exists := docs[link]; !exists {
			sbt, sbtErr := smartblock.SmartBlockTypeFromID(link)
			if sbtErr != nil {
				log.Errorf("failed to get smartblocktype of id %s", link)
				continue
			}
			if sbt != smartblock.SmartBlockTypePage && sbt != smartblock.SmartBlockTypeSet {
				continue
			}
			rec, qErr := e.a.ObjectStore().QueryById(links)
			if qErr != nil {
				log.Errorf("failed to query object with id %s, err: %s", link, err.Error())
				continue
			}
			if len(rec) > 0 {
				docs[link] = rec[0].Details
				e.getNested(link, docs)
			}
		}
	}
}

func (e *export) getExistedObjects() (map[string]*types.Struct, error) {
	res, err := e.a.ObjectStore().List()
	if err != nil {
		return nil, err
	}
	objectDetails := make(map[string]*types.Struct, len(res))
	for _, r := range res {
		if !e.objectValid(r.Id, r) {
			continue
		}
		objectDetails[r.Id] = r.Details

	}
	if err != nil {
		return nil, err
	}
	return objectDetails, nil
}

func (e *export) getArchivedObjects() ([]*model.ObjectInfo, error) {
	archivedObjects, _, err := e.a.ObjectStore().QueryObjectInfo(database.Query{
		Filters: []*model.BlockContentDataviewFilter{{
			RelationKey: bundle.RelationKeyIsArchived.String(),
			Condition:   model.BlockContentDataviewFilter_Equal,
			Value:       pbtypes.Bool(true),
		},
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to QueryObjectIds: %v", err)
	}
	return archivedObjects, nil
}

func (e *export) getDeletedObjects() ([]*model.ObjectInfo, error) {
	deletedObjects, _, err := e.a.ObjectStore().QueryObjectInfo(database.Query{
		Filters: []*model.BlockContentDataviewFilter{{
			RelationKey: bundle.RelationKeyIsDeleted.String(),
			Condition:   model.BlockContentDataviewFilter_Equal,
			Value:       pbtypes.Bool(true),
		},
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to QueryObjectIds: %v", err)
	}
	return deletedObjects, nil
}

func (e *export) writeMultiDoc(mw converter.MultiConverter, wr writer, docs map[string]*types.Struct, queue process.Queue) (succeed int, err error) {
	for did := range docs {
		if err = queue.Wait(func() {
			log.With("threadId", did).Debugf("write doc")
			werr := e.bs.Do(did, func(b sb.SmartBlock) error {
				return mw.Add(b.NewState().Copy())
			})
			if err != nil {
				log.With("threadId", did).Warnf("can't export doc: %v", werr)
			} else {
				succeed++
			}

		}); err != nil {
			return
		}
	}

	if err = wr.WriteFile("export"+mw.Ext(), bytes.NewReader(mw.Convert(0))); err != nil {
		return 0, err
	}

	for _, fh := range mw.FileHashes() {
		fileHash := fh
		if err = queue.Add(func() {
			if werr := e.saveFile(wr, fileHash); werr != nil {
				log.With("hash", fileHash).Warnf("can't save file: %v", werr)
			}
		}); err != nil {
			return
		}
	}
	for _, fh := range mw.ImageHashes() {
		fileHash := fh
		if err = queue.Add(func() {
			if werr := e.saveImage(wr, fileHash); werr != nil {
				log.With("hash", fileHash).Warnf("can't save image: %v", werr)
			}
		}); err != nil {
			return
		}
	}

	err = nil
	return
}

func (e *export) writeDoc(format pb.RpcObjectListExportFormat, wr writer, docInfo map[string]*types.Struct, queue process.Queue, docId string, exportFiles bool) (err error) {
	return e.bs.Do(docId, func(b sb.SmartBlock) error {
		if pbtypes.GetBool(b.CombinedDetails(), bundle.RelationKeyIsDeleted.String()) {
			return nil
		}
		var conv converter.Converter
		switch format {
		case pb.RpcObjectListExport_Markdown:
			conv = md.NewMDConverter(e.a, b.NewState(), wr.Namer())
		case pb.RpcObjectListExport_Protobuf:
			conv = pbc.NewConverter(b)
		case pb.RpcObjectListExport_JSON:
			conv = pbjson.NewConverter(b)
		}
		conv.SetKnownDocs(docInfo)
		result := conv.Convert(b.Type())
		filename := docId + conv.Ext()
		if format == pb.RpcObjectListExport_Markdown {
			s := b.NewState()
			name := pbtypes.GetString(s.Details(), bundle.RelationKeyName.String())
			if name == "" {
				name = s.Snippet()
			}
			filename = wr.Namer().Get("", docId, name, conv.Ext())
		}
		if docId == e.a.PredefinedBlocks().Home {
			filename = "index" + conv.Ext()
		}
		if err = wr.WriteFile(filename, bytes.NewReader(result)); err != nil {
			return err
		}
		if !exportFiles {
			return nil
		}
		for _, fh := range conv.FileHashes() {
			fileHash := fh
			if err = queue.Add(func() {
				if werr := e.saveFile(wr, fileHash); werr != nil {
					log.With("hash", fileHash).Warnf("can't save file: %v", werr)
				}
			}); err != nil {
				return err
			}
		}
		for _, fh := range conv.ImageHashes() {
			fileHash := fh
			if err = queue.Add(func() {
				if werr := e.saveImage(wr, fileHash); werr != nil {
					log.With("hash", fileHash).Warnf("can't save image: %v", werr)
				}
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (e *export) saveFile(wr writer, hash string) (err error) {
	file, err := e.a.FileByHash(context.TODO(), hash)
	if err != nil {
		return
	}
	origName := file.Meta().Name
	filename := wr.Namer().Get("files", hash, filepath.Base(origName), filepath.Ext(origName))
	rd, err := file.Reader()
	if err != nil {
		return
	}
	return wr.WriteFile(filename, rd)
}

func (e *export) saveImage(wr writer, hash string) (err error) {
	file, err := e.a.ImageByHash(context.TODO(), hash)
	if err != nil {
		return
	}
	orig, err := file.GetOriginalFile(context.TODO())
	if err != nil {
		return
	}
	origName := orig.Meta().Name
	filename := wr.Namer().Get("files", hash, filepath.Base(origName), filepath.Ext(origName))
	rd, err := orig.Reader()
	if err != nil {
		return
	}
	return wr.WriteFile(filename, rd)
}

func (e *export) createProfileFile(wr writer) error {
	localProfile, err := e.a.LocalProfile()
	if err != nil {
		return err
	}
	profile := &pb.Profile{
		Name:      localProfile.Name,
		Avatar:    localProfile.IconImage,
		Address:   localProfile.AccountAddr,
		ProfileId: e.a.ProfileID(), // save profile id to restore user profile during import
	}
	data, err := profile.Marshal()
	if err != nil {
		return err
	}
	err = wr.WriteFile(profileFile, bytes.NewReader(data))
	if err != nil {
		return err
	}
	return fmt.Errorf("account predefined block not found")
}

func (e *export) writeConfig(wr writer) error {
	cfg := struct {
		LegacyFileStorePath string
	}{
		LegacyFileStorePath: e.pathProvider.GetBlockStorePath(),
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	err = wr.WriteFile(config.ConfigFileName, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create config file: %v", err)
	}
	return nil
}

func (e *export) objectValid(id string, r *model.ObjectInfo) bool {
	if r.Id == addr.AnytypeProfileId {
		return false
	}
	if !validType(smartblock.SmartBlockType(r.ObjectType)) {
		return false
	}
	if sourceObject := pbtypes.GetString(r.Details, bundle.RelationKeySourceObject.String()); sourceObject != "" {
		if deleted := pbtypes.GetBool(r.Details, bundle.RelationKeyIsDeleted.String()); deleted {
			return true
		}
		return false
	}
	if strings.HasPrefix(id, addr.BundledObjectTypeURLPrefix) || strings.HasPrefix(id, addr.BundledRelationURLPrefix) {
		if deleted := pbtypes.GetBool(r.Details, bundle.RelationKeyIsDeleted.String()); deleted {
			return true
		}
		return false
	}
	return true
}

func newNamer() *namer {
	return &namer{
		names: make(map[string]string),
	}
}

type namer struct {
	// id -> name and name -> id
	names map[string]string
	mu    sync.Mutex
}

func (fn *namer) Get(path, hash, title, ext string) (name string) {
	const fileLenLimit = 48
	fn.mu.Lock()
	defer fn.mu.Unlock()
	var ok bool
	if name, ok = fn.names[hash]; ok {
		return name
	}
	title = slug.Make(strings.TrimSuffix(title, ext))
	name = text.Truncate(title, fileLenLimit)
	name = strings.TrimSuffix(name, text.TruncateEllipsis)
	var (
		i = 0
		b = 36
	)
	gname := filepath.Join(path, name+ext)
	for {
		if _, ok = fn.names[gname]; !ok {
			fn.names[hash] = gname
			fn.names[gname] = hash
			return gname
		}
		i++
		n := int64(i * b)
		gname = filepath.Join(path, name+"_"+strconv.FormatInt(rand.Int63n(n), b)+ext)
	}
}

func validType(sbType smartblock.SmartBlockType) bool {
	return sbType == smartblock.SmartBlockTypeHome ||
		sbType == smartblock.SmartBlockTypeProfilePage ||
		sbType == smartblock.SmartBlockTypePage ||
		sbType == smartblock.SmartBlockTypeSubObject ||
		sbType == smartblock.SmartBlockTypeTemplate ||
		sbType == smartblock.SmartBlockTypeDate ||
		sbType == smartblock.SmartBlockTypeObjectType ||
		sbType == smartblock.SmartBlockTypeSet ||
		sbType == smartblock.SmartBlockTypeWorkspace
}
