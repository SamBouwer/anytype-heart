package source

import (
	"context"
	"strings"
	"time"

	"github.com/gogo/protobuf/types"

	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/core/files"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/filestore"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/util/pbtypes"
)

var getFileTimeout = 60 * time.Second

func NewFile(a core.Service, fileStore filestore.FileStore, fileService files.Service, id string) (s Source) {
	return &file{
		id:          id,
		a:           a,
		fileStore:   fileStore,
		fileService: fileService,
	}
}

type file struct {
	id          string
	a           core.Service
	fileStore   filestore.FileStore
	fileService files.Service
}

func (f *file) ReadOnly() bool {
	return true
}

func (f *file) Id() string {
	return f.id
}

func (f *file) Type() model.SmartBlockType {
	return model.SmartBlockType_File
}

func (f *file) getDetailsForFileOrImage(ctx context.Context, id string) (p *types.Struct, isImage bool, err error) {
	file, err := f.fileService.FileByHash(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if strings.HasPrefix(file.Info().Media, "image") {
		i, err := f.fileService.ImageByHash(ctx, id)
		if err != nil {
			return nil, false, err
		}
		d, err := i.Details(ctx)
		if err != nil {
			return nil, false, err
		}
		return d, true, nil
	}

	d, err := file.Details(ctx)
	if err != nil {
		return nil, false, err
	}
	return d, false, nil
}

func (f *file) ReadDoc(ctx context.Context, receiver ChangeReceiver, empty bool) (doc state.Doc, err error) {
	s := state.NewDoc(f.id, nil).(*state.State)

	ctx, cancel := context.WithTimeout(ctx, getFileTimeout)
	defer cancel()

	d, _, err := f.getDetailsForFileOrImage(ctx, f.id)
	if err != nil {
		return nil, err
	}
	if d.GetFields() != nil {
		d.Fields[bundle.RelationKeyWorkspaceId.String()] = pbtypes.String(f.a.PredefinedBlocks().Account)
	}

	s.SetDetails(d)

	s.SetObjectTypes(pbtypes.GetStringList(d, bundle.RelationKeyType.String()))
	return s, nil
}

func (f *file) ReadMeta(ctx context.Context, _ ChangeReceiver) (doc state.Doc, err error) {
	s := &state.State{}

	ctx, cancel := context.WithTimeout(context.Background(), getFileTimeout)
	defer cancel()

	d, _, err := f.getDetailsForFileOrImage(ctx, f.id)
	if err != nil {
		return nil, err
	}
	if d.GetFields() != nil {
		d.Fields[bundle.RelationKeyWorkspaceId.String()] = pbtypes.String(f.a.PredefinedBlocks().Account)
	}

	s.SetDetails(d)
	s.SetLocalDetail(bundle.RelationKeyId.String(), pbtypes.String(f.id))
	s.SetObjectTypes(pbtypes.GetStringList(d, bundle.RelationKeyType.String()))
	return s, nil
}

func (f *file) PushChange(params PushChangeParams) (id string, err error) {
	return "", nil
}

func (f *file) ListIds() ([]string, error) {
	return f.fileStore.ListTargets()
}

func (f *file) Close() (err error) {
	return
}

func (f *file) Heads() []string {
	return []string{f.id}
}

func (f *file) GetFileKeysSnapshot() []*pb.ChangeFileKeys {
	return nil
}
