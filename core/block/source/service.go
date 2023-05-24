package source

import (
	"fmt"
	"sync"

	"github.com/gogo/protobuf/types"
	"github.com/textileio/go-threads/core/thread"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/core/status"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/core/smartblock"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/addr"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
)

const CName = "source"

func New() Service {
	return &service{}
}

type Service interface {
	NewSource(id string, listenToOwnChanges bool) (s Source, err error)
	RegisterStaticSource(id string, new func() Source)
	NewStaticSource(id string, sbType model.SmartBlockType, doc *state.State, pushChange func(p PushChangeParams) (string, error)) SourceWithType
	RemoveStaticSource(id string)

	GetDetailsFromIdBasedSource(id string) (*types.Struct, error)
	SourceTypeBySbType(blockType smartblock.SmartBlockType) (SourceType, error)
	app.Component
}

type service struct {
	anytype       core.Service
	statusService status.Service

	staticIds map[string]func() Source
	mu        sync.Mutex
}

func (s *service) Init(a *app.App) (err error) {
	s.staticIds = make(map[string]func() Source)
	s.anytype = a.MustComponent(core.CName).(core.Service)
	s.statusService = a.MustComponent(status.CName).(status.Service)
	return
}

func (s *service) Name() (name string) {
	return CName
}

func (s *service) NewSource(id string, listenToOwnChanges bool) (source Source, err error) {
	if id == addr.AnytypeProfileId {
		return NewAnytypeProfile(s.anytype, id), nil
	}
	st, err := smartblock.SmartBlockTypeFromID(id)
	switch st {
	case smartblock.SmartBlockTypeFile:
		return NewFiles(s.anytype, id), nil
	case smartblock.SmartBlockTypeDate:
		return NewDate(s.anytype, id), nil
	case smartblock.SmartBlockTypeBundledObjectType:
		return NewBundledObjectType(s.anytype, id), nil
	case smartblock.SmartBlockTypeBundledRelation:
		return NewBundledRelation(s.anytype, id), nil
	case smartblock.SmartBlockTypeBreadcrumbs:
		return NewVirtual(s.anytype, st.ToProto()), nil
	case smartblock.SmartBlockTypeWorkspaceOld:
		return nil, fmt.Errorf("threadDB-based workspaces are deprecated")
	case smartblock.SmartBlockTypeAccountOld:
		return NewThreadDB(s.anytype, id), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if newStatic := s.staticIds[id]; newStatic != nil {
		return newStatic(), nil
	}

	tid, err := thread.Decode(id)
	if err != nil {
		err = fmt.Errorf("can't restore thread ID %s: %w", id, err)
		return
	}

	return newSource(s.anytype, s.statusService, tid, listenToOwnChanges)
}

func (s *service) GetDetailsFromIdBasedSource(id string) (*types.Struct, error) {
	ss, err := s.NewSource(id, false)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	if v, ok := ss.(SourceIdEndodedDetails); ok {
		return v.DetailsFromId()
	}
	_ = ss.Close()
	return nil, fmt.Errorf("id unsupported")
}

func (s *service) RegisterStaticSource(id string, new func() Source) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticIds[id] = new
}

func (s *service) RemoveStaticSource(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.staticIds, id)
}
