package importer

import (
	"strings"

	"github.com/globalsign/mgo/bson"
	"github.com/gogo/protobuf/types"
	"go.uber.org/zap"

	"github.com/anyproto/anytype-heart/core/block"
	editor "github.com/anyproto/anytype-heart/core/block/editor/smartblock"
	"github.com/anyproto/anytype-heart/core/block/import/converter"
	"github.com/anyproto/anytype-heart/core/block/simple"
	"github.com/anyproto/anytype-heart/core/session"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/database"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/filestore"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/util/pbtypes"
)

type relationIDFormat struct {
	ID     string
	Format model.RelationFormat
}

type relations []relationIDFormat

type RelationService struct {
	core             core.Service
	service          *block.Service
	objCreator       objectCreator
	createdRelations map[string]relations // need this field to avoid creation of the same relations
	store            filestore.FileStore
}

// NewRelationCreator constructor for RelationService
func NewRelationCreator(service *block.Service,
	objCreator objectCreator,
	store filestore.FileStore,
	core core.Service) RelationCreator {
	return &RelationService{
		service:          service,
		objCreator:       objCreator,
		core:             core,
		createdRelations: make(map[string]relations, 0),
		store:            store,
	}
}

func (rc *RelationService) CreateRelations(ctx *session.Context,
	snapshot *model.SmartBlockSnapshotBase,
	pageID string,
	relations []*converter.Relation) ([]string, map[string]*model.Block, error) {
	notExistedRelations := make([]*converter.Relation, 0)
	existedRelations := make(map[string]*converter.Relation, 0)
	for _, r := range relations {
		if strings.EqualFold(r.Name, bundle.RelationKeyName.String()) {
			continue
		}
		if rel, ok := rc.createdRelations[r.Name]; ok {
			var exist bool
			for _, v := range rel {
				if v.Format == r.Format {
					existedRelations[v.ID] = r
					exist = true
					break
				}
			}
			if exist {
				continue
			}
		}
		records, _, err := rc.core.ObjectStore().Query(nil, database.Query{
			Filters: []*model.BlockContentDataviewFilter{
				{
					Condition:   model.BlockContentDataviewFilter_Equal,
					RelationKey: bundle.RelationKeyName.String(),
					Value:       pbtypes.String(r.Name),
					Format:      r.Format,
				},
			},
			Limit: 1,
		})
		if err == nil && len(records) > 0 {
			id := pbtypes.GetString(records[0].Details, bundle.RelationKeyRelationKey.String())
			existedRelations[id] = r
			continue
		}
		notExistedRelations = append(notExistedRelations, r)
	}

	filesToDelete, oldRelationBlockToNewUpdate, failedRelations, err := rc.update(ctx, snapshot, existedRelations, pageID)
	if err != nil {
		return nil, nil, err
	}
	notExistedRelations = append(notExistedRelations, failedRelations...)
	createfilesToDelete, oldRelationBlockToNewCreate, err := rc.create(ctx, snapshot, notExistedRelations, pageID)
	if err != nil {
		return nil, nil, err
	}
	filesToDelete = append(filesToDelete, createfilesToDelete...)
	totalNumberOfRelationBlocks := len(oldRelationBlockToNewCreate) + len(oldRelationBlockToNewUpdate)
	oldRelationBlockToNewTotal := make(map[string]*model.Block, totalNumberOfRelationBlocks)
	if len(oldRelationBlockToNewUpdate) == 0 {
		for k, b := range oldRelationBlockToNewUpdate {
			oldRelationBlockToNewTotal[k] = b
		}
	}
	if len(oldRelationBlockToNewCreate) == 0 {
		for k, b := range oldRelationBlockToNewCreate {
			oldRelationBlockToNewTotal[k] = b
		}
	}
	return filesToDelete, oldRelationBlockToNewTotal, nil
}

// Create read relations link from snaphot and create according relations in anytype,
// also set details for according relation in object for files it loads them in ipfs
func (rc *RelationService) create(ctx *session.Context,
	snapshot *model.SmartBlockSnapshotBase,
	relations []*converter.Relation,
	pageID string) ([]string, map[string]*model.Block, error) {
	var (
		err                   error
		filesToDelete         = make([]string, 0)
		oldRelationBlockToNew = make(map[string]*model.Block, 0)
		createRequest         = make([]*types.Struct, 0)
		existedRelationsIDs   = make([]string, 0)
		setDetailsRequest     = make([]*pb.RpcObjectSetDetailsDetail, 0)
	)

	for _, r := range relations {
		detail := &types.Struct{
			Fields: map[string]*types.Value{
				bundle.RelationKeyName.String():           pbtypes.String(r.Name),
				bundle.RelationKeyRelationFormat.String(): pbtypes.Int64(int64(r.Format)),
				bundle.RelationKeyType.String():           pbtypes.String(bundle.TypeKeyRelation.URL()),
				bundle.RelationKeyLayout.String():         pbtypes.Float64(float64(model.ObjectType_relation)),
			},
		}
		createRequest = append(createRequest, detail)
	}
	var objects []*types.Struct
	if _, objects, err = rc.objCreator.CreateSubObjectsInWorkspace(createRequest); err != nil {
		log.Errorf("create relation %s", err)
	}

	ids := make([]string, 0, len(existedRelationsIDs)+len(objects))
	ids = append(ids, existedRelationsIDs...)

	for _, s := range objects {
		name := pbtypes.GetString(s, bundle.RelationKeyName.String())
		id := pbtypes.GetString(s, bundle.RelationKeyRelationKey.String())
		format := model.RelationFormat(pbtypes.GetFloat64(s, bundle.RelationKeyRelationFormat.String()))
		rc.createdRelations[name] = append(rc.createdRelations[name], relationIDFormat{
			ID:     id,
			Format: format,
		})
		ids = append(ids, id)
	}

	if err = rc.service.AddExtraRelations(ctx, pageID, ids); err != nil {
		log.Errorf("add extra relation %s", err)
	}

	for _, r := range relations {
		var relationID string
		if cr, ok := rc.createdRelations[r.Name]; ok {
			for _, rel := range cr {
				if rel.Format == r.Format {
					relationID = rel.ID
				}
			}
		}
		if relationID == "" {
			continue
		}
		if snapshot.Details != nil && snapshot.Details.Fields != nil {
			if snapshot.Details.Fields[r.Name].GetListValue() != nil && r.Format != model.RelationFormat_object {
				rc.handleListValue(ctx, snapshot, r, relationID)
			}
			if r.Format == model.RelationFormat_file {
				filesToDelete = append(filesToDelete, rc.handleFileRelation(ctx, snapshot, r.Name)...)
			}
		}
		setDetailsRequest = append(setDetailsRequest, &pb.RpcObjectSetDetailsDetail{
			Key:   relationID,
			Value: snapshot.Details.Fields[r.Name],
		})
		if r.BlockID != "" {
			original, new := rc.linkRelationsBlocks(snapshot, r.BlockID, relationID)
			if original != nil && new != nil {
				oldRelationBlockToNew[original.GetId()] = new
			}
		}
	}

	err = rc.service.SetDetails(ctx, pb.RpcObjectSetDetailsRequest{
		ContextId: pageID,
		Details:   setDetailsRequest,
	})
	if err != nil {
		log.Errorf("set details %s", err)
	}

	if ftd, err := rc.handleCoverRelation(ctx, snapshot, pageID); err != nil {
		log.Errorf("failed to upload cover image %s", err)
	} else {
		filesToDelete = append(filesToDelete, ftd...)
	}

	return filesToDelete, oldRelationBlockToNew, nil
}

func (rc *RelationService) update(ctx *session.Context,
	snapshot *model.SmartBlockSnapshotBase,
	relations map[string]*converter.Relation,
	pageID string) ([]string, map[string]*model.Block, []*converter.Relation, error) {
	var (
		err                   error
		filesToDelete         = make([]string, 0)
		oldRelationBlockToNew = make(map[string]*model.Block, 0)
		failedRelations       = make([]*converter.Relation, 0)
	)

	// to get failed relations and fallback them to create function
	for key, r := range relations {
		if err = rc.service.AddExtraRelations(ctx, pageID, []string{key}); err != nil {
			log.Errorf("add extra relation %s", err)
			failedRelations = append(failedRelations, r)
			continue
		}
		if snapshot.Details != nil && snapshot.Details.Fields != nil {
			if snapshot.Details.Fields[r.Name].GetListValue() != nil && r.Format != model.RelationFormat_object {
				rc.handleListValue(ctx, snapshot, r, key)
			}
			if r.Format == model.RelationFormat_file {
				filesToDelete = append(filesToDelete, rc.handleFileRelation(ctx, snapshot, r.Name)...)
			}
		}
		setDetailsRequest := make([]*pb.RpcObjectSetDetailsDetail, 0)
		setDetailsRequest = append(setDetailsRequest, &pb.RpcObjectSetDetailsDetail{
			Key:   key,
			Value: snapshot.Details.Fields[r.Name],
		})
		if r.BlockID != "" {
			original, new := rc.linkRelationsBlocks(snapshot, r.BlockID, key)
			if original != nil && new != nil {
				oldRelationBlockToNew[original.GetId()] = new
			}
		}
		err = rc.service.SetDetails(ctx, pb.RpcObjectSetDetailsRequest{
			ContextId: pageID,
			Details:   setDetailsRequest,
		})
		if err != nil {
			log.Errorf("set details %s", err)
			failedRelations = append(failedRelations, r)
		}
	}

	if ftd, err := rc.handleCoverRelation(ctx, snapshot, pageID); err != nil {
		log.Errorf("failed to upload cover image %s", err)
	} else {
		filesToDelete = append(filesToDelete, ftd...)
	}

	return filesToDelete, oldRelationBlockToNew, failedRelations, nil

}

func (rc *RelationService) ReplaceRelationBlock(ctx *session.Context,
	oldRelationBlocksToNew map[string]*model.Block,
	pageID string) {
	if sbErr := rc.service.Do(pageID, func(sb editor.SmartBlock) error {
		s := sb.NewStateCtx(ctx)
		if err := s.Iterate(func(b simple.Block) (isContinue bool) {
			if b.Model().GetRelation() == nil {
				return true
			}
			bl, ok := oldRelationBlocksToNew[b.Model().GetId()]
			if !ok {
				return true
			}
			simpleBlock := simple.New(bl)
			s.Add(simpleBlock)
			if err := s.InsertTo(b.Model().GetId(), model.Block_Replace, simpleBlock.Model().GetId()); err != nil {
				log.With(zap.String("object id", pageID)).Errorf("failed to insert: %w", err)
			}
			return true
		}); err != nil {
			return err
		}
		if err := sb.Apply(s); err != nil {
			log.With(zap.String("object id", pageID)).Errorf("failed to apply state: %w", err)
			return err
		}
		return nil
	}); sbErr != nil {
		log.With(zap.String("object id", pageID)).Errorf("failed to replace relation block: %w", sbErr)
	}
}

func (rc *RelationService) handleCoverRelation(ctx *session.Context,
	snapshot *model.SmartBlockSnapshotBase,
	pageID string) ([]string, error) {
	filesToDelete := rc.handleFileRelation(ctx, snapshot, bundle.RelationKeyCoverId.String())
	details := make([]*pb.RpcObjectSetDetailsDetail, 0)
	details = append(details, &pb.RpcObjectSetDetailsDetail{
		Key:   bundle.RelationKeyCoverId.String(),
		Value: snapshot.Details.Fields[bundle.RelationKeyCoverId.String()],
	})
	err := rc.service.SetDetails(ctx, pb.RpcObjectSetDetailsRequest{
		ContextId: pageID,
		Details:   details,
	})

	return filesToDelete, err
}

func (rc *RelationService) handleListValue(ctx *session.Context,
	snapshot *model.SmartBlockSnapshotBase,
	r *converter.Relation,
	relationID string) {
	var (
		optionIDs      = make([]string, 0)
		id             string
		err            error
		existedOptions = make(map[string]string, 0)
	)
	options, err := rc.service.Anytype().ObjectStore().GetAggregatedOptions(relationID)
	if err != nil {
		log.Errorf("get relations options %s", err)
	}
	for _, ro := range options {
		existedOptions[ro.Text] = ro.Id
	}
	for _, tag := range r.SelectDict {
		if optionID, ok := existedOptions[tag.Text]; ok {
			optionIDs = append(optionIDs, optionID)
			continue
		}
		if id, _, err = rc.objCreator.CreateSubObjectInWorkspace(&types.Struct{
			Fields: map[string]*types.Value{
				bundle.RelationKeyName.String():                pbtypes.String(tag.Text),
				bundle.RelationKeyRelationKey.String():         pbtypes.String(relationID),
				bundle.RelationKeyType.String():                pbtypes.String(bundle.TypeKeyRelationOption.URL()),
				bundle.RelationKeyLayout.String():              pbtypes.Float64(float64(model.ObjectType_relationOption)),
				bundle.RelationKeyRelationOptionColor.String(): pbtypes.String(tag.Color),
			},
		}, rc.core.PredefinedBlocks().Account); err != nil {
			log.Errorf("add extra relation %s", err)
		}
		optionIDs = append(optionIDs, id)
	}
	snapshot.Details.Fields[r.Name] = pbtypes.StringList(optionIDs)
}

func (rc *RelationService) handleFileRelation(ctx *session.Context,
	snapshot *model.SmartBlockSnapshotBase,
	name string) []string {
	var allFiles []string
	if files := snapshot.Details.Fields[name].GetListValue(); files != nil {
		for _, f := range files.Values {
			allFiles = append(allFiles, f.GetStringValue())
		}
	}

	if files := snapshot.Details.Fields[name].GetStringValue(); files != "" {
		allFiles = append(allFiles, files)
	}

	allFilesHashes := make([]string, 0)

	filesToDelete := make([]string, 0, len(allFiles))
	for _, f := range allFiles {
		if f == "" {
			continue
		}
		if _, err := rc.store.GetByHash(f); err == nil {
			allFilesHashes = append(allFilesHashes, f)
			continue
		}

		req := pb.RpcFileUploadRequest{LocalPath: f}

		if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") {
			req.Url = f
			req.LocalPath = ""
		}

		hash, err := rc.service.UploadFile(req)
		if err != nil {
			log.Errorf("file uploading %s", err)
		} else {
			f = hash
		}

		filesToDelete = append(filesToDelete, f)
		allFilesHashes = append(allFilesHashes, f)
	}

	if snapshot.Details.Fields[name].GetListValue() != nil {
		snapshot.Details.Fields[name] = pbtypes.StringList(allFilesHashes)
	}

	if snapshot.Details.Fields[name].GetStringValue() != "" && len(allFilesHashes) != 0 {
		snapshot.Details.Fields[name] = pbtypes.String(allFilesHashes[0])
	}

	return filesToDelete
}

func (rc *RelationService) linkRelationsBlocks(snapshot *model.SmartBlockSnapshotBase,
	oldID, newID string) (*model.Block, *model.Block) {
	for _, b := range snapshot.Blocks {
		if rel, ok := b.Content.(*model.BlockContentOfRelation); ok && rel.Relation.GetKey() == oldID {
			return b, &model.Block{
				Id: bson.NewObjectId().Hex(),
				Content: &model.BlockContentOfRelation{
					Relation: &model.BlockContentRelation{
						Key: newID,
					},
				}}
		}
	}
	return nil, nil
}
