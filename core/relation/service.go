package relation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/gogo/protobuf/types"
	"github.com/ipfs/go-datastore/query"

	"github.com/anytypeio/go-anytype-middleware/app"
	"github.com/anytypeio/go-anytype-middleware/core/relation/relationutils"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/bundle"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/database"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/localstore/addr"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/localstore/objectstore"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/logging"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/pb/model"
	"github.com/anytypeio/go-anytype-middleware/util/pbtypes"
	"github.com/anytypeio/go-anytype-middleware/util/uri"
)

const CName = "relation"

var (
	ErrNotFound = errors.New("relation not found")
	log         = logging.Logger("anytype-relations")
)

func New() Service {
	return new(service)
}

type Service interface {
	FetchKeys(keys ...string) (relations relationutils.Relations, err error)
	FetchKey(key string, opts ...FetchOption) (relation *relationutils.Relation, err error)
	ListAll(opts ...FetchOption) (relations relationutils.Relations, err error)

	FetchLinks(links pbtypes.RelationLinks) (relations relationutils.Relations, err error)
	CreateBulkMigration() BulkMigration
	MigrateRelations(relations []*model.Relation) error
	MigrateObjectTypes(relations []*model.ObjectType) error
	ValidateFormat(key string, v *types.Value) error
	app.Component
}

type BulkMigration interface {
	AddRelations(relations []*model.Relation)
	AddObjectTypes(objectType []*model.ObjectType)
	Commit() error
}

type subObjectCreator interface {
	CreateSubObjectInWorkspace(details *types.Struct, workspaceId string) (id string, newDetails *types.Struct, err error)
	CreateSubObjectsInWorkspace(details []*types.Struct) (ids []string, objects []*types.Struct, err error)
}

var errSubobjectAlreadyExists = fmt.Errorf("subobject already exists in the collection")

type bulkMigration struct {
	cache     map[string]struct{}
	s         subObjectCreator
	relations []*types.Struct
	options   []*types.Struct
	types     []*types.Struct
	mu        sync.Mutex
}

func (b *bulkMigration) AddRelations(relations []*model.Relation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, rel := range relations {
		if rel == nil || rel.GetName() == "" {
			continue
		}
		for _, opt := range rel.SelectDict {
			if _, exists := b.cache[opt.Id]; exists {
				continue
			}
			// hack for missing relation name on snippet
			if rel.Key == bundle.RelationKeySnippet.String() {
				rel.Name = "Snippet"
			}
			opt.RelationKey = rel.Key
			b.options = append(b.options, (&relationutils.Option{RelationOption: opt}).ToStruct())
			b.cache[opt.Id] = struct{}{}
		}
		if _, exists := b.cache[addr.RelationKeyToIdPrefix+rel.Key]; exists {
			continue
		}
		b.relations = append(b.relations, (&relationutils.Relation{Relation: rel}).ToStruct())
		b.cache[addr.RelationKeyToIdPrefix+rel.Key] = struct{}{}
	}
}

func (b *bulkMigration) AddObjectTypes(objectTypes []*model.ObjectType) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ot := range objectTypes {
		tk, err := bundle.TypeKeyFromUrl(ot.Url)
		if err != nil {
			log.Errorf("failed to parse type key %s: %v", ot.Url, err)
			continue
		}
		if _, exists := b.cache[addr.ObjectTypeKeyToIdPrefix+tk.String()]; exists {
			continue
		}
		b.types = append(b.types, (&relationutils.ObjectType{ObjectType: ot}).ToStruct())
		b.cache[addr.ObjectTypeKeyToIdPrefix+tk.String()] = struct{}{}
	}
}

func (b *bulkMigration) Commit() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.relations) > 0 {
		ids, _, err1 := b.s.CreateSubObjectsInWorkspace(b.relations)
		if len(ids) == 0 && (err1 == nil || err1.Error() != errSubobjectAlreadyExists.Error()) {
			log.Errorf("relations migration done %d/%d: %v", len(ids), len(b.relations), err1)
		}

		if err1 != nil && err1.Error() != errSubobjectAlreadyExists.Error() {
			return err1
		}
	}
	if len(b.options) > 0 {
		ids, _, err1 := b.s.CreateSubObjectsInWorkspace(b.options)
		if len(ids) == 0 && (err1 == nil || err1.Error() != errSubobjectAlreadyExists.Error()) {
			log.Errorf("options migration done %d/%d: %v", len(ids), len(b.relations), err1)
		}

		if err1 != nil && err1.Error() != errSubobjectAlreadyExists.Error() {
			return err1
		}
	}
	if len(b.types) > 0 {
		ids, _, err1 := b.s.CreateSubObjectsInWorkspace(b.types)
		if len(ids) == 0 && (err1 == nil || err1.Error() != errSubobjectAlreadyExists.Error()) {
			log.Errorf("types migration done %d/%d: %v", len(ids), len(b.relations), err1)
		}
		if err1 != nil && err1.Error() != errSubobjectAlreadyExists.Error() {
			return err1
		}
	}
	b.options = nil
	b.relations = nil
	b.types = nil

	return nil
}

type service struct {
	objectStore     objectstore.ObjectStore
	relationCreator subObjectCreator
	existingIds     map[string]struct{}
}

func (s *service) MigrateRelations(relations []*model.Relation) error {
	b := s.CreateBulkMigration()
	b.AddRelations(relations)
	return b.Commit()
}

func (s *service) MigrateObjectTypes(types []*model.ObjectType) error {
	b := s.CreateBulkMigration()
	b.AddObjectTypes(types)
	return b.Commit()
}

func (s *service) preloadSubobjects() {
	s.existingIds = make(map[string]struct{})
	ids, err := s.existingSubobjects()
	if err != nil {
		log.Errorf("failed to preload subobjects: %v", err)
	}
	for _, id := range ids {
		s.existingIds[id] = struct{}{}
	}
}

func (s *service) Init(a *app.App) (err error) {
	s.objectStore = a.MustComponent(objectstore.CName).(objectstore.ObjectStore)
	s.relationCreator = a.MustComponent("objectCreator").(subObjectCreator)
	return
}

func (s *service) Run(context.Context) error {
	s.preloadSubobjects()
	return nil
}

func (s *service) CreateBulkMigration() BulkMigration {
	m := s.existingIds
	if m == nil {
		m = make(map[string]struct{})
	}
	return &bulkMigration{cache: m, s: s.relationCreator}
}

func (s *service) Name() (name string) {
	return CName
}

func (s *service) FetchLinks(links pbtypes.RelationLinks) (relations relationutils.Relations, err error) {
	keys := make([]string, 0, len(links))
	for _, l := range links {
		keys = append(keys, l.Key)
	}
	return s.fetchKeys(keys...)
}

func (s *service) FetchKeys(keys ...string) (relations relationutils.Relations, err error) {
	return s.fetchKeys(keys...)
}

func (s *service) fetchKeys(keys ...string) (relations []*relationutils.Relation, err error) {
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, addr.RelationKeyToIdPrefix+key)
	}
	records, err := s.objectStore.QueryById(ids)
	if err != nil {
		return
	}

	for _, rec := range records {
		if pbtypes.GetString(rec.Details, bundle.RelationKeyType.String()) != bundle.TypeKeyRelation.URL() {
			continue
		}
		relations = append(relations, relationutils.RelationFromStruct(rec.Details))
	}
	return
}

func (s *service) ListAll(opts ...FetchOption) (relations relationutils.Relations, err error) {
	return s.listAll(opts...)
}

func (s *service) listAll(opts ...FetchOption) (relations relationutils.Relations, err error) {
	filters := []*model.BlockContentDataviewFilter{
		{
			RelationKey: bundle.RelationKeyType.String(),
			Condition:   model.BlockContentDataviewFilter_Equal,
			Value:       pbtypes.String(bundle.TypeKeyRelation.URL()),
		},
	}
	o := &fetchOptions{}
	for _, apply := range opts {
		apply(o)
	}
	if o.workspaceId != nil {
		filters = append(filters, &model.BlockContentDataviewFilter{
			RelationKey: bundle.RelationKeyWorkspaceId.String(),
			Condition:   model.BlockContentDataviewFilter_Equal,
			Value:       pbtypes.String(*o.workspaceId),
		})
	}

	relations2, _, err := s.objectStore.Query(nil, database.Query{
		Filters: filters,
	})
	if err != nil {
		return
	}

	for _, rec := range relations2 {
		relations = append(relations, relationutils.RelationFromStruct(rec.Details))
	}
	return
}

type fetchOptions struct {
	workspaceId *string
}

type FetchOption func(options *fetchOptions)

func WithWorkspaceId(id string) FetchOption {
	return func(options *fetchOptions) {
		options.workspaceId = &id
	}
}

func (s *service) existingSubobjects() (ids []string, err error) {
	q := database.Query{
		Filters: []*model.BlockContentDataviewFilter{
			{
				Condition:   model.BlockContentDataviewFilter_In,
				RelationKey: bundle.RelationKeyType.String(),
				Value:       pbtypes.StringList([]string{bundle.TypeKeyRelation.URL(), bundle.TypeKeyRelationOption.URL(), bundle.TypeKeyObjectType.URL()}),
			},
		},
	}
	f, err := database.NewFilters(q, nil, s.objectStore, nil)
	if err != nil {
		return
	}
	records, err := s.objectStore.QueryRaw(query.Query{
		Filters: []query.Filter{f},
	})
	if err != nil {
		return nil, err
	}
	ids = make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, pbtypes.GetString(record.Details, bundle.RelationKeyId.String()))
	}
	return
}

func (s *service) FetchKey(key string, opts ...FetchOption) (relation *relationutils.Relation, err error) {
	return s.fetchKey(key, opts...)
}

func (s *service) fetchKey(key string, opts ...FetchOption) (relation *relationutils.Relation, err error) {
	o := &fetchOptions{}
	for _, apply := range opts {
		apply(o)
	}
	q := database.Query{
		Filters: []*model.BlockContentDataviewFilter{
			{
				Condition:   model.BlockContentDataviewFilter_Equal,
				RelationKey: bundle.RelationKeyRelationKey.String(),
				Value:       pbtypes.String(key),
			},
			{
				Condition:   model.BlockContentDataviewFilter_Equal,
				RelationKey: bundle.RelationKeyType.String(),
				Value:       pbtypes.String(bundle.TypeKeyRelation.URL()),
			},
		},
	}
	if o.workspaceId != nil {
		q.Filters = append(q.Filters, &model.BlockContentDataviewFilter{
			Condition:   model.BlockContentDataviewFilter_Equal,
			RelationKey: bundle.RelationKeyWorkspaceId.String(),
			Value:       pbtypes.String(*o.workspaceId),
		})
	}
	f, err := database.NewFilters(q, nil, s.objectStore, nil)
	if err != nil {
		return
	}
	records, err := s.objectStore.QueryRaw(query.Query{
		Filters: []query.Filter{f},
	})
	for _, rec := range records {
		return relationutils.RelationFromStruct(rec.Details), nil
	}
	return nil, ErrNotFound
}

func (s *service) ValidateFormat(key string, v *types.Value) error {
	r, err := s.FetchKey(key)
	if err != nil {
		return err
	}
	if _, isNull := v.Kind.(*types.Value_NullValue); isNull {
		// allow null value for any field
		return nil
	}

	switch r.Format {
	case model.RelationFormat_longtext, model.RelationFormat_shorttext:
		if _, ok := v.Kind.(*types.Value_StringValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of string", v.Kind)
		}
		return nil
	case model.RelationFormat_number:
		if _, ok := v.Kind.(*types.Value_NumberValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of number", v.Kind)
		}
		return nil
	case model.RelationFormat_status:
		if _, ok := v.Kind.(*types.Value_StringValue); ok {

		} else if _, ok := v.Kind.(*types.Value_ListValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of list", v.Kind)
		}

		vals := pbtypes.GetStringListValue(v)
		if len(vals) > 1 {
			return fmt.Errorf("status should not contain more than one value")
		}
		return s.validateOptions(r, vals)

	case model.RelationFormat_tag:
		if _, ok := v.Kind.(*types.Value_ListValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of list", v.Kind)
		}

		vals := pbtypes.GetStringListValue(v)
		if r.MaxCount > 0 && len(vals) > int(r.MaxCount) {
			return fmt.Errorf("maxCount exceeded")
		}

		return s.validateOptions(r, vals)
	case model.RelationFormat_date:
		if _, ok := v.Kind.(*types.Value_NumberValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of number", v.Kind)
		}

		return nil
	case model.RelationFormat_file, model.RelationFormat_object:
		switch s := v.Kind.(type) {
		case *types.Value_StringValue:
			return nil
		case *types.Value_ListValue:
			if r.MaxCount > 0 && len(s.ListValue.Values) > int(r.MaxCount) {
				return fmt.Errorf("relation %s(%s) has maxCount exceeded", r.Key, r.Format.String())
			}

			for i, lv := range s.ListValue.Values {
				if optId, ok := lv.Kind.(*types.Value_StringValue); !ok {
					return fmt.Errorf("incorrect list item value at index %d: %T instead of string", i, lv.Kind)
				} else if optId.StringValue == "" {
					return fmt.Errorf("empty option at index %d", i)
				}
			}
			return nil
		default:
			return fmt.Errorf("incorrect type: %T instead of list/string", v.Kind)
		}
	case model.RelationFormat_checkbox:
		if _, ok := v.Kind.(*types.Value_BoolValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of bool", v.Kind)
		}

		return nil
	case model.RelationFormat_url:
		if _, ok := v.Kind.(*types.Value_StringValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of string", v.Kind)
		}

		s := strings.TrimSpace(v.GetStringValue())
		if s != "" {
			err := uri.ValidateURI(strings.TrimSpace(v.GetStringValue()))
			if err != nil {
				return fmt.Errorf("failed to parse URL: %s", err.Error())
			}
		}
		// todo: should we allow schemas other than http/https?
		// if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		//	return fmt.Errorf("url scheme %s not supported", u.Scheme)
		// }
		return nil
	case model.RelationFormat_email:
		if _, ok := v.Kind.(*types.Value_StringValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of string", v.Kind)
		}
		// todo: revise regexp and reimplement
		/*valid := uri.ValidateEmail(v.GetStringValue())
		if !valid {
			return fmt.Errorf("failed to validate email")
		}*/
		return nil
	case model.RelationFormat_phone:
		if _, ok := v.Kind.(*types.Value_StringValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of string", v.Kind)
		}

		// todo: revise regexp and reimplement
		/*valid := uri.ValidatePhone(v.GetStringValue())
		if !valid {
			return fmt.Errorf("failed to validate phone")
		}*/
		return nil
	case model.RelationFormat_emoji:
		if _, ok := v.Kind.(*types.Value_StringValue); !ok {
			return fmt.Errorf("incorrect type: %T instead of string", v.Kind)
		}

		// check if the symbol is emoji
		return nil
	default:
		return fmt.Errorf("unsupported rel format: %s", r.Format.String())
	}
}

func (s *service) validateOptions(rel *relationutils.Relation, v []string) error {
	// TODO:
	return nil
}

func (s *service) Close() (err error) {
	return nil
}
