package objectstore

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/anyproto/anytype-heart/core/relation/relationutils"

	"github.com/anyproto/anytype-heart/pkg/lib/datastore/noctxds"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	ds "github.com/ipfs/go-datastore"
	"github.com/ipfs/go-datastore/query"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/metrics"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	cafePb "github.com/anyproto/anytype-heart/pkg/lib/cafe/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/core/smartblock"
	"github.com/anyproto/anytype-heart/pkg/lib/database"
	"github.com/anyproto/anytype-heart/pkg/lib/datastore"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/addr"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/ftsearch"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
	"github.com/anyproto/anytype-heart/pkg/lib/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/pkg/lib/schema"
	"github.com/anyproto/anytype-heart/util/pbtypes"
	"github.com/anyproto/anytype-heart/util/slice"
)

var log = logging.Logger("anytype-localstore")

const CName = "objectstore"

var (
	// ObjectInfo is stored in db key pattern:
	pagesPrefix        = "pages"
	pagesDetailsBase   = ds.NewKey("/" + pagesPrefix + "/details")
	pendingDetailsBase = ds.NewKey("/" + pagesPrefix + "/pending")
	pagesRelationsBase = ds.NewKey("/" + pagesPrefix + "/relations")     // store the list of full relation model for /objectId
	setRelationsBase   = ds.NewKey("/" + pagesPrefix + "/set/relations") // store the list of full relation model for /setId

	pagesSnippetBase       = ds.NewKey("/" + pagesPrefix + "/snippet")
	pagesInboundLinksBase  = ds.NewKey("/" + pagesPrefix + "/inbound")
	pagesOutboundLinksBase = ds.NewKey("/" + pagesPrefix + "/outbound")
	indexQueueBase         = ds.NewKey("/" + pagesPrefix + "/index")
	bundledChecksums       = ds.NewKey("/" + pagesPrefix + "/checksum")
	indexedHeadsState      = ds.NewKey("/" + pagesPrefix + "/headsstate")

	cafePrefix   = "cafe"
	accountState = ds.NewKey("/" + cafePrefix + "/accountstate")

	workspacesPrefix = "workspaces"
	currentWorkspace = ds.NewKey("/" + workspacesPrefix + "/current")
	workspaceMapBase = ds.NewKey("/" + workspacesPrefix + "/workspacemap")

	threadCreateQueuePrefix = "threadcreatequeue"
	threadCreateQueueBase   = ds.NewKey("/" + threadCreateQueuePrefix)

	relationsPrefix = "relations"
	// /relations/options/<relOptionId>: option model
	relationsOptionsBase = ds.NewKey("/" + relationsPrefix + "/options")
	// /relations/relations/<relKey>: relation model
	relationsBase = ds.NewKey("/" + relationsPrefix + "/relations")

	// /relations/objtype_relkey_objid/<objType>/<relKey>/<objId>
	indexObjectTypeRelationObjectId = localstore.Index{
		Prefix: relationsPrefix,
		Name:   "objtype_relkey_objid",
		Keys: func(val interface{}) []localstore.IndexKeyParts {
			if v, ok := val.(*relationObjectType); ok {
				var indexes []localstore.IndexKeyParts
				for _, rk := range v.relationKeys {
					for _, ot := range v.objectTypes {
						otCompact, err := objTypeCompactEncode(ot)
						if err != nil {
							log.Errorf("objtype_relkey_objid index construction error(ot '%s'): %s", ot, err.Error())
							continue
						}

						indexes = append(indexes, localstore.IndexKeyParts([]string{otCompact, rk}))
					}
				}
				return indexes
			}
			return nil
		},
		Unique:             false,
		SplitIndexKeyParts: true,
	}

	// /relations/objtype_relkey_setid/<objType>/<relKey>/<setObjId>
	indexObjectTypeRelationSetId = localstore.Index{
		Prefix: relationsPrefix,
		Name:   "objtype_relkey_setid",
		Keys: func(val interface{}) []localstore.IndexKeyParts {
			if v, ok := val.(*relationObjectType); ok {
				var indexes []localstore.IndexKeyParts
				for _, rk := range v.relationKeys {
					for _, ot := range v.objectTypes {
						otCompact, err := objTypeCompactEncode(ot)
						if err != nil {
							log.Errorf("objtype_relkey_setid index construction error('%s'): %s", ot, err.Error())
							continue
						}

						indexes = append(indexes, localstore.IndexKeyParts([]string{otCompact, rk}))
					}
				}
				return indexes
			}
			return nil
		},
		Unique:             false,
		SplitIndexKeyParts: true,
	}

	// /relations/relkey_optid/<relKey>/<optId>/<objId>
	indexRelationOptionObject = localstore.Index{
		Prefix: pagesPrefix,
		Name:   "relkey_optid",
		Keys: func(val interface{}) []localstore.IndexKeyParts {
			if v, ok := val.(*model.Relation); ok {
				var indexes []localstore.IndexKeyParts
				if v.Format != model.RelationFormat_tag && v.Format != model.RelationFormat_status {
					return nil
				}
				if len(v.SelectDict) == 0 {
					return nil
				}

				for _, opt := range v.SelectDict {
					// todo: migration?

					indexes = append(indexes, localstore.IndexKeyParts([]string{v.Key, opt.Id}))
				}
				return indexes
			}
			return nil
		},
		Unique:             false,
		SplitIndexKeyParts: true,
	}

	// /relations/relkey/<relKey>/<objId>
	indexRelationObject = localstore.Index{
		Prefix: pagesPrefix,
		Name:   "relkey",
		Keys: func(val interface{}) []localstore.IndexKeyParts {
			if v, ok := val.(*model.Relation); ok {
				return []localstore.IndexKeyParts{[]string{v.Key}}
			}
			return nil
		},
		Unique: false,
	}

	// /pages/type/<objType>/<objId>
	indexObjectTypeObject = localstore.Index{
		Prefix: pagesPrefix,
		Name:   "type",
		Keys: func(val interface{}) []localstore.IndexKeyParts {
			if v, ok := val.(*model.ObjectDetails); ok {
				var indexes []localstore.IndexKeyParts
				types := pbtypes.GetStringList(v.Details, bundle.RelationKeyType.String())

				for _, ot := range types {
					otCompact, err := objTypeCompactEncode(ot)
					if err != nil {
						log.Errorf("type index construction error('%s'): %s", ot, err.Error())
						continue
					}
					indexes = append(indexes, localstore.IndexKeyParts([]string{otCompact}))
				}
				return indexes
			}
			return nil
		},
		Unique: false,
		Hash:   false,
	}

	_ ObjectStore = (*dsObjectStore)(nil)
)

func New() ObjectStore {
	return &dsObjectStore{}
}

func NewWithLocalstore(ds noctxds.DSTxnBatching) ObjectStore {
	return &dsObjectStore{
		ds: ds,
	}
}

type SourceIdEncodedDetails interface {
	GetDetailsFromIdBasedSource(id string) (*types.Struct, error)
}

func (ls *dsObjectStore) Init(a *app.App) (err error) {
	ls.dsIface = a.MustComponent(datastore.CName).(datastore.Datastore)
	s := a.Component("source")
	if s != nil {
		ls.sourceService = a.MustComponent("source").(SourceIdEncodedDetails)
	}
	fts := a.Component(ftsearch.CName)
	if fts == nil {
		log.Warnf("init objectstore without fulltext")
	} else {
		ls.fts = fts.(ftsearch.FTSearch)
	}
	return nil
}

func (ls *dsObjectStore) Name() (name string) {
	return CName
}

type ObjectStore interface {
	app.ComponentRunnable
	localstore.Indexable
	database.Reader

	// CreateObject create or overwrite an existing object. Should be used if
	CreateObject(id string, details *types.Struct, links []string, snippet string) error
	// UpdateObjectDetails updates existing object or create if not missing. Should be used in order to amend existing indexes based on prev/new value
	// set discardLocalDetailsChanges to true in case the caller doesn't have local details in the State
	UpdateObjectDetails(id string, details *types.Struct, discardLocalDetailsChanges bool) error
	UpdateObjectLinks(id string, links []string) error
	UpdateObjectSnippet(id string, snippet string) error
	UpdatePendingLocalDetails(id string, details *types.Struct) error
	GetPendingLocalDetails(id string) (*model.ObjectDetails, error)

	DeleteObject(id string) error
	DeleteDetails(id string) error

	GetWithLinksInfoByID(id string) (*model.ObjectInfoWithLinks, error)
	GetOutboundLinksById(id string) ([]string, error)
	GetInboundLinksById(id string) ([]string, error)

	GetWithOutboundLinksInfoById(id string) (*model.ObjectInfoWithOutboundLinks, error)
	GetDetails(id string) (*model.ObjectDetails, error)
	GetAggregatedOptions(relationKey string) (options []*model.RelationOption, err error)

	HasIDs(ids ...string) (exists []string, err error)
	GetByIDs(ids ...string) ([]*model.ObjectInfo, error)
	List() ([]*model.ObjectInfo, error)
	ListIds() ([]string, error)

	QueryObjectInfo(q database.Query, objectTypes []smartblock.SmartBlockType) (results []*model.ObjectInfo, total int, err error)
	QueryObjectIds(q database.Query, objectTypes []smartblock.SmartBlockType) (ids []string, total int, err error)

	AddToIndexQueue(id string) error
	IndexForEach(f func(id string, tm time.Time) error) error
	FTSearch() ftsearch.FTSearch

	// EraseIndexes erase all indexes for objectstore.. All objects needs to be reindexed
	EraseIndexes() error

	// GetChecksums Used to get information about localstore state and decide do we need to reindex some objects
	GetChecksums() (checksums *model.ObjectStoreChecksums, err error)
	// SaveChecksums Used to save checksums and force reindex counter
	SaveChecksums(checksums *model.ObjectStoreChecksums) (err error)

	GetLastIndexedHeadsHash(id string) (headsHash string, err error)
	SaveLastIndexedHeadsHash(id string, headsHash string) (err error)

	GetAccountState() (state *cafePb.AccountState, err error)
	SaveAccountState(state *cafePb.AccountState) (err error)

	GetCurrentWorkspaceId() (string, error)
	SetCurrentWorkspaceId(threadId string) (err error)
	RemoveCurrentWorkspaceId() (err error)

	AddThreadQueueEntry(entry *model.ThreadCreateQueueEntry) (err error)
	RemoveThreadQueueEntry(threadId string) (err error)
	GetAllQueueEntries() ([]*model.ThreadCreateQueueEntry, error)

	AddThreadToWorkspace(threadId, workspaceId string) error
	RemoveThreadForWorkspace(threadId, workspaceId string) error
	GetThreadQueueState() (map[string]map[string]struct{}, map[string]map[string]struct{}, error)
}

type relationObjectType struct {
	relationKeys []string
	objectTypes  []string
}

var ErrNotAnObject = fmt.Errorf("not an object")

var filterNotSystemObjects = &filterSmartblockTypes{
	smartBlockTypes: []smartblock.SmartBlockType{
		smartblock.SmartBlockTypeArchive,
		smartblock.SmartBlockTypeHome,
	},
	not: true,
}

type filterSmartblockTypes struct {
	smartBlockTypes []smartblock.SmartBlockType
	not             bool
}

func (m *filterSmartblockTypes) Filter(e query.Entry) bool {
	keyParts := strings.Split(e.Key, "/")
	id := keyParts[len(keyParts)-1]

	t, err := smartblock.SmartBlockTypeFromID(id)
	if err != nil {
		log.Errorf("failed to detect smartblock type for %s: %s", id, err.Error())
		return false
	}

	for _, ot := range m.smartBlockTypes {
		if t == ot {
			return !m.not
		}
	}
	return m.not
}

type dsObjectStore struct {
	// underlying storage
	ds            noctxds.DSTxnBatching
	dsIface       datastore.Datastore
	sourceService SourceIdEncodedDetails

	fts ftsearch.FTSearch

	// serializing page updates
	l sync.Mutex

	onChangeCallback func(record database.Record)

	subscriptions    []database.Subscription
	depSubscriptions []database.Subscription
}

func (m *dsObjectStore) AddThreadToWorkspace(threadId, workspaceId string) error {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()
	workspaceKey := workspaceMapBase.Child(ds.NewKey(workspaceId)).Child(ds.NewKey(threadId))
	if err := txn.Put(workspaceKey, nil); err != nil {
		return fmt.Errorf("failed to put into ds: %w", err)
	}
	return txn.Commit()
}

func (m *dsObjectStore) RemoveThreadForWorkspace(threadId, workspaceId string) error {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()
	workspaceKey := workspaceMapBase.Child(ds.NewKey(workspaceId)).Child(ds.NewKey(threadId))
	if err := txn.Delete(workspaceKey); err != nil {
		return fmt.Errorf("failed to put into ds: %w", err)
	}
	return txn.Commit()
}

func (m *dsObjectStore) GetThreadQueueState() (map[string]map[string]struct{}, map[string]map[string]struct{}, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	res, err := txn.Query(query.Query{
		Prefix:   workspaceMapBase.String(),
		KeysOnly: true,
	})
	if err != nil {
		return nil, nil, err
	}

	threadWorkspaces := make(map[string]map[string]struct{})
	workspaceThreads := make(map[string]map[string]struct{})
	for entry := range res.Next() {
		split := strings.Split(entry.Key, "/")
		if len(split) < 2 {
			continue
		}
		workspaceId := split[len(split)-2]
		threadId := split[len(split)-1]
		threadKV, exists := threadWorkspaces[threadId]
		if !exists {
			threadKV = make(map[string]struct{})
			threadWorkspaces[threadId] = threadKV
		}
		workspaceKV, exists := workspaceThreads[workspaceId]
		if !exists {
			workspaceKV = make(map[string]struct{})
			workspaceThreads[workspaceId] = workspaceKV
		}
		threadKV[workspaceId] = struct{}{}
		workspaceKV[threadId] = struct{}{}
	}
	return workspaceThreads, threadWorkspaces, nil
}

func (m *dsObjectStore) AddThreadQueueEntry(entry *model.ThreadCreateQueueEntry) (err error) {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	b, err := entry.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal entry into binary: %w", err)
	}

	key := threadCreateQueueBase.Child(ds.NewKey(entry.ThreadId))
	if err := txn.Put(key, b); err != nil {
		return fmt.Errorf("failed to put into ds: %w", err)
	}

	return txn.Commit()
}

func (m *dsObjectStore) RemoveThreadQueueEntry(threadId string) (err error) {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	key := threadCreateQueueBase.Child(ds.NewKey(threadId))
	if err = txn.Delete(key); err != nil {
		return fmt.Errorf("failed to delete entry from ds: %w", err)
	}

	return txn.Commit()
}

func (m *dsObjectStore) GetAllQueueEntries() ([]*model.ThreadCreateQueueEntry, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	res, err := txn.Query(query.Query{
		Prefix: threadCreateQueueBase.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("error query txn in datastore: %w", err)
	}
	var models []*model.ThreadCreateQueueEntry
	for entry := range res.Next() {
		var qEntry model.ThreadCreateQueueEntry
		err = proto.Unmarshal(entry.Value, &qEntry)
		if err != nil {
			log.Errorf("failed to unmarshal thread create entry")
			continue
		}
		models = append(models, &qEntry)
	}

	return models, nil
}

func (m *dsObjectStore) GetCurrentWorkspaceId() (string, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return "", fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	val, err := txn.Get(currentWorkspace)
	if err != nil {
		return "", err
	}
	return string(val), nil
}

func (m *dsObjectStore) SetCurrentWorkspaceId(threadId string) (err error) {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	if err := txn.Put(currentWorkspace, []byte(threadId)); err != nil {
		return fmt.Errorf("failed to put into ds: %w", err)
	}

	return txn.Commit()
}

func (m *dsObjectStore) RemoveCurrentWorkspaceId() (err error) {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	if err := txn.Delete(currentWorkspace); err != nil {
		return fmt.Errorf("failed to delete from ds: %w", err)
	}

	return txn.Commit()
}

func (m *dsObjectStore) GetAccountState() (cfg *cafePb.AccountState, err error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	var state cafePb.AccountState
	if val, err := txn.Get(accountState); err != nil {
		return nil, err
	} else if err := proto.Unmarshal(val, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

func (m *dsObjectStore) SaveAccountState(state *cafePb.AccountState) (err error) {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	b, err := state.Marshal()
	if err != nil {
		return err
	}

	if err := txn.Put(accountState, b); err != nil {
		return fmt.Errorf("failed to put into ds: %w", err)
	}

	return txn.Commit()
}

func (m *dsObjectStore) EraseIndexes() (err error) {
	for _, idx := range m.Indexes() {
		err = localstore.EraseIndex(idx, m.ds)
		if err != nil {
			return
		}
	}
	err = m.eraseStoredRelations()
	if err != nil {
		log.Errorf("eraseStoredRelations failed: %s", err.Error())
	}

	err = m.eraseLinks()
	if err != nil {
		log.Errorf("eraseLinks failed: %s", err.Error())
	}

	return
}

func (m *dsObjectStore) eraseStoredRelations() (err error) {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return err
	}

	defer txn.Discard()
	res, err := localstore.GetKeys(txn, setRelationsBase.String(), 0)
	if err != nil {
		return err
	}

	keys, err := localstore.ExtractKeysFromResults(res)
	if err != nil {
		return err
	}

	for _, key := range keys {
		err = txn.Delete(ds.NewKey(key))
		if err != nil {
			log.Errorf("eraseStoredRelations: failed to delete key %s: %s", key, err.Error())
		}
	}
	return txn.Commit()
}

func (m *dsObjectStore) eraseLinks() (err error) {
	n, err := removeByPrefix(m.ds, pagesOutboundLinksBase.String())
	if err != nil {
		return err
	}

	log.Infof("eraseLinks: removed %d outbound links", n)
	n, err = removeByPrefix(m.ds, pagesInboundLinksBase.String())
	if err != nil {
		return err
	}

	log.Infof("eraseLinks: removed %d inbound links", n)

	return nil
}

func (m *dsObjectStore) Run(context.Context) (err error) {
	lds, err := m.dsIface.LocalstoreDS()
	m.ds = noctxds.New(lds)
	return
}

func (m *dsObjectStore) Close() (err error) {
	return nil
}

func (m *dsObjectStore) AggregateObjectIdsForOptionAndRelation(relationKey, optId string) (objectsIds []string, err error) {
	txn, err := m.ds.NewTransaction(true)
	defer txn.Discard()

	res, err := localstore.GetKeysByIndexParts(txn, pagesPrefix, indexRelationOptionObject.Name, []string{relationKey, optId}, "/", false, 0)
	if err != nil {
		return nil, err
	}

	return localstore.GetLeavesFromResults(res)
}

func (m *dsObjectStore) AggregateObjectIdsByOptionForRelation(relationKey string) (objectsByOptionId map[string][]string, err error) {
	txn, err := m.ds.NewTransaction(true)
	defer txn.Discard()

	res, err := localstore.GetKeysByIndexParts(txn, pagesPrefix, indexRelationOptionObject.Name, []string{relationKey}, "/", false, 0)
	if err != nil {
		return nil, err
	}

	keys, err := localstore.ExtractKeysFromResults(res)
	if err != nil {
		return nil, err
	}

	objectsByOptionId = make(map[string][]string)

	for _, key := range keys {
		optionId, err := localstore.CarveKeyParts(key, -2, -1)
		if err != nil {
			return nil, err
		}
		objId, err := localstore.CarveKeyParts(key, -1, 0)
		if err != nil {
			return nil, err
		}

		if _, exists := objectsByOptionId[optionId]; !exists {
			objectsByOptionId[optionId] = []string{}
		}

		objectsByOptionId[optionId] = append(objectsByOptionId[optionId], objId)
	}
	return
}

// GetAggregatedOptions returns aggregated options for specific relation. Options have a specific scope
func (m *dsObjectStore) GetAggregatedOptions(relationKey string) (options []*model.RelationOption, err error) {
	// todo: add workspace
	records, _, err := m.Query(nil, database.Query{
		Filters: []*model.BlockContentDataviewFilter{
			{
				Condition:   model.BlockContentDataviewFilter_Equal,
				RelationKey: bundle.RelationKeyRelationKey.String(),
				Value:       pbtypes.String(relationKey),
			},
			{
				Condition:   model.BlockContentDataviewFilter_Equal,
				RelationKey: bundle.RelationKeyType.String(),
				Value:       pbtypes.String(bundle.TypeKeyRelationOption.URL()),
			},
		},
	})

	for _, rec := range records {
		options = append(options, relationutils.OptionFromStruct(rec.Details).RelationOption)
	}
	return
}

func (m *dsObjectStore) objectTypeFilter(ots ...string) query.Filter {
	var filter filterSmartblockTypes
	for _, otUrl := range ots {
		if ot, err := bundle.GetTypeByUrl(otUrl); err == nil {
			for _, sbt := range ot.Types {
				filter.smartBlockTypes = append(filter.smartBlockTypes, smartblock.SmartBlockType(sbt))
			}
			continue
		}
		if sbt, err := smartblock.SmartBlockTypeFromID(otUrl); err == nil {
			filter.smartBlockTypes = append(filter.smartBlockTypes, sbt)
		}
	}
	return &filter
}

func (m *dsObjectStore) QueryAndSubscribeForChanges(schema schema.Schema, q database.Query, sub database.Subscription) (records []database.Record, close func(), total int, err error) {
	m.l.Lock()
	defer m.l.Unlock()

	records, total, err = m.Query(schema, q)

	var ids []string
	for _, record := range records {
		ids = append(ids, pbtypes.GetString(record.Details, bundle.RelationKeyId.String()))
	}

	sub.Subscribe(ids)
	m.addSubscriptionIfNotExists(sub)
	close = func() {
		m.closeAndRemoveSubscription(sub)
	}

	return
}

// unsafe, use under mutex
func (m *dsObjectStore) addSubscriptionIfNotExists(sub database.Subscription) (existed bool) {
	for _, s := range m.subscriptions {
		if s == sub {
			return true
		}
	}

	m.subscriptions = append(m.subscriptions, sub)
	return false
}

func (m *dsObjectStore) closeAndRemoveSubscription(sub database.Subscription) {
	m.l.Lock()
	defer m.l.Unlock()
	sub.Close()

	for i, s := range m.subscriptions {
		if s == sub {
			m.subscriptions = append(m.subscriptions[:i], m.subscriptions[i+1:]...)
			break
		}
	}
}

func (m *dsObjectStore) QueryByIdAndSubscribeForChanges(ids []string, sub database.Subscription) (records []database.Record, close func(), err error) {
	m.l.Lock()
	defer m.l.Unlock()

	if sub == nil {
		err = fmt.Errorf("subscription func is nil")
		return
	}
	sub.Subscribe(ids)
	records, err = m.QueryById(ids)
	if err != nil {
		// can mean only the datastore is already closed, so we can resign and return
		log.Errorf("QueryByIdAndSubscribeForChanges failed to query ids: %v", err)
		return nil, nil, err
	}

	close = func() {
		m.closeAndRemoveSubscription(sub)
	}

	m.addSubscriptionIfNotExists(sub)

	return
}

func (m *dsObjectStore) Query(sch schema.Schema, q database.Query) (records []database.Record, total int, err error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	dsq, err := q.DSQuery(sch)
	if err != nil {
		return
	}
	dsq.Offset = 0
	dsq.Limit = 0
	dsq.Prefix = pagesDetailsBase.String() + "/"
	if !q.WithSystemObjects {
		dsq.Filters = append([]query.Filter{filterNotSystemObjects}, dsq.Filters...)
	}

	if len(q.ObjectTypeFilter) > 0 {
		dsq.Filters = append([]query.Filter{m.objectTypeFilter(q.ObjectTypeFilter...)}, dsq.Filters...)
	}

	if q.FullText != "" {
		if dsq, err = m.makeFTSQuery(q.FullText, dsq); err != nil {
			return
		}
	}
	for _, f := range dsq.Filters {
		log.Debugf("query filter: %+v", f)
	}
	res, err := txn.Query(dsq)
	if err != nil {
		return nil, 0, fmt.Errorf("error when querying ds: %w", err)
	}

	var (
		results []database.Record
		offset  = q.Offset
	)

	// We use own limit/offset implementation in order to find out
	// total number of records matching specified filters. Query
	// returns this number for handy pagination on clients.
	for rec := range res.Next() {
		total++

		if offset > 0 {
			offset--
			continue
		}

		if q.Limit > 0 && len(results) >= q.Limit {
			continue
		}

		key := ds.NewKey(rec.Key)
		keyList := key.List()
		id := keyList[len(keyList)-1]

		var details *model.ObjectDetails
		details, err = unmarshalDetails(id, rec.Value)
		if err != nil {
			total--
			log.Errorf("failed to unmarshal: %s", err.Error())
			continue
		}
		results = append(results, database.Record{Details: details.Details})
	}

	return results, total, nil
}

func (m *dsObjectStore) QueryRaw(dsq query.Query) (records []database.Record, err error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()
	dsq.Prefix = pagesDetailsBase.String() + "/"

	res, err := txn.Query(dsq)
	if err != nil {
		return nil, fmt.Errorf("error when querying ds: %w", err)
	}

	for rec := range res.Next() {
		key := ds.NewKey(rec.Key)
		keyList := key.List()
		id := keyList[len(keyList)-1]

		var details *model.ObjectDetails
		details, err = unmarshalDetails(id, rec.Value)
		if err != nil {
			log.Errorf("failed to unmarshal: %s", err.Error())
			continue
		}
		records = append(records, database.Record{Details: details.Details})
	}
	return
}

func unmarshalDetails(id string, rawValue []byte) (*model.ObjectDetails, error) {
	var details model.ObjectDetails
	if err := proto.Unmarshal(rawValue, &details); err != nil {
		return nil, err
	}

	if details.Details == nil || details.Details.Fields == nil {
		details.Details = &types.Struct{Fields: map[string]*types.Value{}}
	} else {
		pb.StructDeleteEmptyFields(details.Details)
	}
	details.Details.Fields[database.RecordIDField] = pb.ToValue(id)
	return &details, nil
}

func (m *dsObjectStore) SubscribeForAll(callback func(rec database.Record)) {
	m.l.Lock()
	m.onChangeCallback = callback
	m.l.Unlock()
}

func (m *dsObjectStore) QueryObjectInfo(q database.Query, objectTypes []smartblock.SmartBlockType) (results []*model.ObjectInfo, total int, err error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	dsq, err := q.DSQuery(nil)
	if err != nil {
		return
	}
	dsq.Offset = 0
	dsq.Limit = 0
	dsq.Prefix = pagesDetailsBase.String() + "/"
	if len(objectTypes) > 0 {
		dsq.Filters = append([]query.Filter{&filterSmartblockTypes{smartBlockTypes: objectTypes}}, dsq.Filters...)
	}
	if q.FullText != "" {
		if dsq, err = m.makeFTSQuery(q.FullText, dsq); err != nil {
			return
		}
	}
	res, err := txn.Query(dsq)
	if err != nil {
		return nil, 0, fmt.Errorf("error when querying ds: %w", err)
	}

	var (
		offset = q.Offset
	)

	// We use own limit/offset implementation in order to find out
	// total number of records matching specified filters. Query
	// returns this number for handy pagination on clients.
	for rec := range res.Next() {
		if rec.Error != nil {
			return nil, 0, rec.Error
		}
		total++

		if offset > 0 {
			offset--
			continue
		}

		if q.Limit > 0 && len(results) >= q.Limit {
			continue
		}

		key := ds.NewKey(rec.Key)
		keyList := key.List()
		id := keyList[len(keyList)-1]
		oi, err := m.getObjectInfo(txn, id)
		if err != nil {
			// probably details are not yet indexed, let's skip it
			log.Errorf("QueryObjectInfo getObjectInfo error: %s", err.Error())
			total--
			continue
		}
		results = append(results, oi)
	}
	return results, total, nil
}

func (m *dsObjectStore) QueryObjectIds(q database.Query, objectTypes []smartblock.SmartBlockType) (ids []string, total int, err error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	dsq, err := q.DSQuery(nil)
	if err != nil {
		return
	}
	dsq.Offset = 0
	dsq.Limit = 0
	dsq.Prefix = pagesDetailsBase.String() + "/"
	if len(objectTypes) > 0 {
		dsq.Filters = append([]query.Filter{&filterSmartblockTypes{smartBlockTypes: objectTypes}}, dsq.Filters...)
	}
	if q.FullText != "" {
		if dsq, err = m.makeFTSQuery(q.FullText, dsq); err != nil {
			return
		}
	}
	res, err := txn.Query(dsq)
	if err != nil {
		return nil, 0, fmt.Errorf("error when querying ds: %w", err)
	}

	var (
		offset = q.Offset
	)

	// We use own limit/offset implementation in order to find out
	// total number of records matching specified filters. Query
	// returns this number for handy pagination on clients.
	for rec := range res.Next() {
		if rec.Error != nil {
			return nil, 0, rec.Error
		}
		total++

		if offset > 0 {
			offset--
			continue
		}

		if q.Limit > 0 && len(ids) >= q.Limit {
			continue
		}

		key := ds.NewKey(rec.Key)
		keyList := key.List()
		id := keyList[len(keyList)-1]
		ids = append(ids, id)
	}
	return ids, total, nil
}

func (m *dsObjectStore) QueryById(ids []string) (records []database.Record, err error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	for _, id := range ids {
		if sbt, err := smartblock.SmartBlockTypeFromID(id); err == nil {
			if indexDetails, _ := sbt.Indexable(); !indexDetails && m.sourceService != nil {
				details, err := m.sourceService.GetDetailsFromIdBasedSource(id)
				if err != nil {
					log.Errorf("QueryByIds failed to GetDetailsFromIdBasedSource id: %s", id)
					continue
				}
				details.Fields[database.RecordIDField] = pb.ToValue(id)
				records = append(records, database.Record{Details: details})
				continue
			}
		}
		v, err := txn.Get(pagesDetailsBase.ChildString(id))
		if err != nil {
			log.Infof("QueryByIds failed to find id: %s", id)
			continue
		}

		var details *model.ObjectDetails
		details, err = unmarshalDetails(id, v)
		if err != nil {
			log.Errorf("QueryByIds failed to unmarshal id: %s", id)
			continue
		}
		records = append(records, database.Record{Details: details.Details})
	}

	return
}

func (m *dsObjectStore) GetRelationById(id string) (*model.Relation, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	s, err := m.GetDetails(id)
	if err != nil {
		return nil, err
	}

	rel := relationutils.RelationFromStruct(s.GetDetails())
	return rel.Relation, nil
}

// GetRelationByKey is deprecated, should be used from relationService
func (m *dsObjectStore) GetRelationByKey(key string) (*model.Relation, error) {
	// todo: should pass workspace
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

	f, err := database.NewFilters(q, nil, m, nil)
	if err != nil {
		return nil, err
	}
	records, err := m.QueryRaw(query.Query{
		Filters: []query.Filter{f},
	})
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, ds.ErrNotFound
	}

	rel := relationutils.RelationFromStruct(records[0].Details)

	return rel.Relation, nil
}

// ListRelations retrieves all available relations and sort them in this order:
// 1. extraRelations aggregated from object of specific type (scope objectsOfTheSameType)
// 2. relations aggregated from sets of specific type  (scope setsOfTheSameType)
// 3. user-defined relations aggregated from all objects (scope library)
// 4. the rest of bundled relations (scope library)
func (m *dsObjectStore) ListRelations(objType string) ([]*model.Relation, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	if objType == "" {
		rels, err := m.listRelations(txn, 0)
		if err != nil {
			return nil, err
		}
		// todo: omit when we will have everything in index
		relsKeys2 := bundle.ListRelationsKeys()
		for _, relKey := range relsKeys2 {
			if pbtypes.HasRelation(rels, relKey.String()) {
				continue
			}

			rel := bundle.MustGetRelation(relKey)
			rel.Scope = model.Relation_library
			rels = append(rels, rel)
		}
		return rels, nil
	}

	rels, err := m.AggregateRelationsFromObjectsOfType(objType)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate relations from objects: %w", err)
	}

	rels2, err := m.AggregateRelationsFromSetsOfType(objType)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate relations from sets: %w", err)
	}

	for i, rel := range rels2 {
		if pbtypes.HasRelation(rels, rel.Key) {
			continue
		}
		rels = append(rels, rels2[i])
	}

	relsKeys, err := m.listRelationsKeys(txn)
	if err != nil {
		return nil, fmt.Errorf("failed to list relations from store index: %w", err)
	}

	// todo: omit when we will have everything in index
	for _, relKey := range relsKeys {
		if pbtypes.HasRelation(rels, relKey) {
			continue
		}
		rel, err := getRelation(txn, relKey)
		if err != nil {
			log.Errorf("relation found in index but failed to retrieve from store")
			continue
		}
		rel.Scope = model.Relation_library
		rels = append(rels, rel)
	}

	relsKeys2 := bundle.ListRelationsKeys()
	for _, relKey := range relsKeys2 {
		if pbtypes.HasRelation(rels, relKey.String()) {
			continue
		}

		rel := bundle.MustGetRelation(relKey)
		rel.Scope = model.Relation_library
		rels = append(rels, rel)
	}

	return rels, nil
}

func (m *dsObjectStore) ListRelationsKeys() ([]string, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	return m.listRelationsKeys(txn)
}

func (m *dsObjectStore) AggregateRelationsFromObjectsOfType(objType string) ([]*model.Relation, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	var rels []*model.Relation
	objTypeCompact, err := objTypeCompactEncode(objType)
	if err != nil {
		return nil, fmt.Errorf("failed to encode object type '%s': %s", objType, err.Error())
	}
	res, err := localstore.GetKeysByIndexParts(txn, indexObjectTypeRelationObjectId.Prefix, indexObjectTypeRelationObjectId.Name, []string{objTypeCompact}, "/", false, 0)
	if err != nil {
		return nil, err
	}

	relKeys, err := localstore.GetKeyPartFromResults(res, -2, -1, true)
	if err != nil {
		return nil, err
	}

	for _, relKey := range relKeys {
		rel, err := getRelation(txn, relKey)
		if err != nil {
			log.Errorf("relation '%s' found in the index but failed to retreive: %s", relKey, err.Error())
			continue
		}

		rel.Scope = model.Relation_objectsOfTheSameType
		rels = append(rels, rel)
	}

	return rels, nil
}

func (m *dsObjectStore) AggregateRelationsFromSetsOfType(objType string) ([]*model.Relation, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	var rels []*model.Relation
	objTypeCompact, err := objTypeCompactEncode(objType)
	if err != nil {
		return nil, err
	}
	res, err := localstore.GetKeysByIndexParts(txn, indexObjectTypeRelationSetId.Prefix, indexObjectTypeRelationSetId.Name, []string{objTypeCompact}, "/", false, 0)
	if err != nil {
		return nil, err
	}

	relKeys, err := localstore.GetKeyPartFromResults(res, -2, -1, true)
	if err != nil {
		return nil, err
	}

	for _, relKey := range relKeys {
		rel, err := getRelation(txn, relKey)
		if err != nil {
			log.Errorf("relation '%s' found in the index but failed to retreive: %s", relKey, err.Error())
			continue
		}

		rel.Scope = model.Relation_setOfTheSameType
		rels = append(rels, rel)
	}

	return rels, nil
}

func (m *dsObjectStore) DeleteDetails(id string) error {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	// todo: remove all indexes with this object
	for _, k := range []ds.Key{
		pagesSnippetBase.ChildString(id),
		pagesDetailsBase.ChildString(id),
		setRelationsBase.ChildString(id),
	} {
		if err = txn.Delete(k); err != nil {
			return err
		}
	}

	return txn.Commit()
}

// DeleteObject removes all details, leaving only id and isDeleted
func (m *dsObjectStore) DeleteObject(id string) error {
	// do not completely remove object details, so we can distinguish links to deleted and not-yet-loaded objects
	err := m.UpdateObjectDetails(id, &types.Struct{
		Fields: map[string]*types.Value{
			bundle.RelationKeyId.String():        pbtypes.String(id),
			bundle.RelationKeyIsDeleted.String(): pbtypes.Bool(true), // maybe we can store the date instead?
		},
	}, false)
	if err != nil {
		return fmt.Errorf("failed to overwrite details and relations: %w", err)
	}
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	// todo: remove all indexes with this object
	for _, k := range []ds.Key{
		pagesSnippetBase.ChildString(id),
		indexQueueBase.ChildString(id),
		setRelationsBase.ChildString(id),
		indexedHeadsState.ChildString(id),
	} {
		if err = txn.Delete(k); err != nil {
			return err
		}
	}

	_, err = removeByPrefixInTx(txn, pagesInboundLinksBase.String()+"/"+id+"/")
	if err != nil {
		return err
	}

	_, err = removeByPrefixInTx(txn, pagesOutboundLinksBase.String()+"/"+id+"/")
	if err != nil {
		return err
	}

	if m.fts != nil {
		_ = m.removeFromIndexQueue(id)

		if err := m.fts.Delete(id); err != nil {
			return err
		}
	}
	return txn.Commit()
}

// RemoveRelationFromCache removes cached relation data
func (m *dsObjectStore) RemoveRelationFromCache(key string) error {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	for _, k := range []ds.Key{
		relationsBase.ChildString(key),
	} {
		if err = txn.Delete(k); err != nil {
			return err
		}
	}

	return txn.Commit()
}

func (m *dsObjectStore) GetWithLinksInfoByID(id string) (*model.ObjectInfoWithLinks, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	pages, err := m.getObjectsInfo(txn, []string{id})
	if err != nil {
		return nil, err
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("page not found")
	}
	page := pages[0]

	inboundIds, err := findInboundLinks(txn, id)
	if err != nil {
		return nil, err
	}

	outboundsIds, err := findOutboundLinks(txn, id)
	if err != nil {
		return nil, err
	}

	inbound, err := m.getObjectsInfo(txn, inboundIds)
	if err != nil {
		return nil, err
	}

	outbound, err := m.getObjectsInfo(txn, outboundsIds)
	if err != nil {
		return nil, err
	}

	return &model.ObjectInfoWithLinks{
		Id:   id,
		Info: page,
		Links: &model.ObjectLinksInfo{
			Inbound:  inbound,
			Outbound: outbound,
		},
	}, nil
}

func (m *dsObjectStore) GetOutboundLinksById(id string) ([]string, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	return findOutboundLinks(txn, id)
}

func (m *dsObjectStore) GetInboundLinksById(id string) ([]string, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	return findInboundLinks(txn, id)
}

func (m *dsObjectStore) GetWithOutboundLinksInfoById(id string) (*model.ObjectInfoWithOutboundLinks, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	pages, err := m.getObjectsInfo(txn, []string{id})
	if err != nil {
		return nil, err
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("page not found")
	}
	page := pages[0]

	outboundsIds, err := findOutboundLinks(txn, id)
	if err != nil {
		return nil, err
	}

	outbound, err := m.getObjectsInfo(txn, outboundsIds)
	if err != nil {
		return nil, err
	}

	return &model.ObjectInfoWithOutboundLinks{
		Info:          page,
		OutboundLinks: outbound,
	}, nil
}

func (m *dsObjectStore) GetDetails(id string) (*model.ObjectDetails, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	return getObjectDetails(txn, id)
}

func (m *dsObjectStore) List() ([]*model.ObjectInfo, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	ids, err := findByPrefix(txn, pagesDetailsBase.String()+"/", 0)
	if err != nil {
		return nil, err
	}

	return m.getObjectsInfo(txn, ids)
}

func (m *dsObjectStore) HasIDs(ids ...string) (exists []string, err error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()
	for _, id := range ids {
		if exist, err := hasObjectId(txn, id); err != nil {
			return nil, err
		} else if exist {
			exists = append(exists, id)
		}
	}
	return exists, nil
}

func (m *dsObjectStore) GetByIDs(ids ...string) ([]*model.ObjectInfo, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	return m.getObjectsInfo(txn, ids)
}

func (m *dsObjectStore) CreateObject(id string, details *types.Struct, links []string, snippet string) error {
	m.l.Lock()
	defer m.l.Unlock()
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	// init an empty state to skip nil checks later
	before := model.ObjectInfo{
		Details: &types.Struct{Fields: map[string]*types.Value{}},
	}

	err = m.updateObjectDetails(txn, id, before, details)
	if err != nil {
		return err
	}

	err = m.updateObjectLinksAndSnippet(txn, id, links, snippet)
	if err != nil {
		return err
	}
	return txn.Commit()
}

func (m *dsObjectStore) UpdateObjectLinks(id string, links []string) error {
	m.l.Lock()
	defer m.l.Unlock()
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	err = m.updateObjectLinks(txn, id, links)
	if err != nil {
		return err
	}
	return txn.Commit()
}

func (m *dsObjectStore) UpdateObjectSnippet(id string, snippet string) error {
	m.l.Lock()
	defer m.l.Unlock()
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	if val, err := txn.Get(pagesSnippetBase.ChildString(id)); err == ds.ErrNotFound || string(val) != snippet {
		if err := m.updateSnippet(txn, id, snippet); err != nil {
			return err
		}
	}
	return txn.Commit()
}

func (m *dsObjectStore) GetPendingLocalDetails(id string) (*model.ObjectDetails, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	return m.getPendingLocalDetails(txn, id)
}

func (m *dsObjectStore) UpdatePendingLocalDetails(id string, details *types.Struct) error {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()
	key := pendingDetailsBase.ChildString(id)

	if details == nil {
		err = txn.Delete(key)
		if err != nil {
			return err
		}
		return txn.Commit()
	}

	b, err := proto.Marshal(&model.ObjectDetails{Details: details})
	if err != nil {
		return err
	}
	err = txn.Put(key, b)
	if err != nil {
		return err
	}

	return txn.Commit()
}

func (m *dsObjectStore) UpdateObjectDetails(id string, details *types.Struct, discardLocalDetailsChanges bool) error {
	m.l.Lock()
	defer m.l.Unlock()
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()
	var (
		before model.ObjectInfo
	)

	if details != nil {
		exInfo, err := m.getObjectInfo(txn, id)
		if err != nil {
			log.Debugf("UpdateObject failed to get ex state for object %s: %s", id, err.Error())
		}

		if exInfo != nil {
			before = *exInfo
		} else {
			// init an empty state to skip nil checks later
			before = model.ObjectInfo{
				Details: &types.Struct{Fields: map[string]*types.Value{}},
			}
		}

		if discardLocalDetailsChanges && details != nil {
			injectedDetails := pbtypes.StructFilterKeys(before.Details, bundle.LocalRelationsKeys)
			for k, v := range injectedDetails.Fields {
				details.Fields[k] = pbtypes.CopyVal(v)
			}
		}
	}

	err = m.updateObjectDetails(txn, id, before, details)
	if err != nil {
		return err
	}
	err = txn.Commit()
	if err != nil {
		return err
	}

	return nil
}

// GetLastIndexedHeadsHash return empty hash without error if record was not found
func (m *dsObjectStore) GetLastIndexedHeadsHash(id string) (headsHash string, err error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return "", fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	if val, err := txn.Get(indexedHeadsState.ChildString(id)); err != nil && err != ds.ErrNotFound {
		return "", fmt.Errorf("failed to get heads hash: %w", err)
	} else if val == nil {
		return "", nil
	} else {
		return string(val), nil
	}
}

func (m *dsObjectStore) SaveLastIndexedHeadsHash(id string, headsHash string) (err error) {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	if err := txn.Put(indexedHeadsState.ChildString(id), []byte(headsHash)); err != nil {
		return fmt.Errorf("failed to put into ds: %w", err)
	}

	return txn.Commit()
}

func (m *dsObjectStore) GetChecksums() (checksums *model.ObjectStoreChecksums, err error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	var objChecksum model.ObjectStoreChecksums
	if val, err := txn.Get(bundledChecksums); err != nil && err != ds.ErrNotFound {
		return nil, fmt.Errorf("failed to get details: %w", err)
	} else if err := proto.Unmarshal(val, &objChecksum); err != nil {
		return nil, err
	}

	return &objChecksum, nil
}

func (m *dsObjectStore) SaveChecksums(checksums *model.ObjectStoreChecksums) (err error) {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	b, err := checksums.Marshal()
	if err != nil {
		return err
	}

	if err := txn.Put(bundledChecksums, b); err != nil {
		return fmt.Errorf("failed to put into ds: %w", err)
	}

	return txn.Commit()
}

func (m *dsObjectStore) updateObjectLinks(txn noctxds.Txn, id string, links []string) error {
	exLinks, _ := findOutboundLinks(txn, id)
	var addedLinks, removedLinks []string

	removedLinks, addedLinks = slice.DifferenceRemovedAdded(exLinks, links)
	if len(addedLinks) > 0 {
		for _, k := range pageLinkKeys(id, nil, addedLinks) {
			if err := txn.Put(k, nil); err != nil {
				return err
			}
		}
	}

	if len(removedLinks) > 0 {
		for _, k := range pageLinkKeys(id, nil, removedLinks) {
			if err := txn.Delete(k); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *dsObjectStore) updateObjectLinksAndSnippet(txn noctxds.Txn, id string, links []string, snippet string) error {
	err := m.updateObjectLinks(txn, id, links)
	if err != nil {
		return err
	}

	if val, err := txn.Get(pagesSnippetBase.ChildString(id)); err == ds.ErrNotFound || string(val) != snippet {
		if err := m.updateSnippet(txn, id, snippet); err != nil {
			return err
		}
	}

	return nil
}

func (m *dsObjectStore) updateObjectDetails(txn noctxds.Txn, id string, before model.ObjectInfo, details *types.Struct) error {
	if details != nil {
		if err := m.updateDetails(txn, id, &model.ObjectDetails{Details: before.Details}, &model.ObjectDetails{Details: details}); err != nil {
			return err
		}
	}

	return nil
}

// should be called under the mutex
func (m *dsObjectStore) sendUpdatesToSubscriptions(id string, details *types.Struct) {
	detCopy := pbtypes.CopyStruct(details)
	detCopy.Fields[database.RecordIDField] = pb.ToValue(id)
	if m.onChangeCallback != nil {
		m.onChangeCallback(database.Record{
			Details: detCopy,
		})
	}
	for i := range m.subscriptions {
		go func(sub database.Subscription) {
			_ = sub.Publish(id, detCopy)
		}(m.subscriptions[i])
	}
}

func (m *dsObjectStore) AddToIndexQueue(id string) error {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()
	var buf [8]byte
	size := binary.PutVarint(buf[:], time.Now().Unix())
	if err = txn.Put(indexQueueBase.ChildString(id), buf[:size]); err != nil {
		return err
	}
	return txn.Commit()
}

func (m *dsObjectStore) removeFromIndexQueue(id string) error {
	txn, err := m.ds.NewTransaction(false)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	if err := txn.Delete(indexQueueBase.ChildString(id)); err != nil {
		return fmt.Errorf("failed to remove id from full text index queue: %s", err.Error())
	}

	return txn.Commit()
}

func (m *dsObjectStore) IndexForEach(f func(id string, tm time.Time) error) error {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()
	res, err := txn.Query(query.Query{Prefix: indexQueueBase.String()})
	if err != nil {
		return fmt.Errorf("error query txn in datastore: %w", err)
	}
	for entry := range res.Next() {
		id := extractIdFromKey(entry.Key)
		ts, _ := binary.Varint(entry.Value)
		indexErr := f(id, time.Unix(ts, 0))
		if indexErr != nil {
			log.Warnf("can't index '%s'(ts %d): %v", id, ts, indexErr)
			// in case indexation is has failed it's better to remove this document from the index
			// so we will not stuck with this object forever
		}

		err = m.removeFromIndexQueue(id)
		if err != nil {
			// if we have the error here we have nothing to do but retry later
			log.Errorf("failed to remove %s(ts %d) from index, will redo the fulltext index: %v", id, ts, err)
		}
	}

	err = res.Close()
	if err != nil {
		return err
	}

	return nil
}

func (m *dsObjectStore) ListIds() ([]string, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	return findByPrefix(txn, pagesDetailsBase.String()+"/", 0)
}

func (m *dsObjectStore) updateSnippet(txn noctxds.Txn, id string, snippet string) error {
	snippetKey := pagesSnippetBase.ChildString(id)
	return txn.Put(snippetKey, []byte(snippet))
}

func (m *dsObjectStore) updateDetails(txn noctxds.Txn, id string, oldDetails *model.ObjectDetails, newDetails *model.ObjectDetails) error {
	t, err := smartblock.SmartBlockTypeFromID(id)
	if err != nil {
		log.Errorf("updateDetails: failed to detect smartblock type for %s: %s", id, err.Error())
		return fmt.Errorf("updateDetails: failed to detect smartblock type for %s: %s", id, err.Error())
	} else if indexdetails, _ := t.Indexable(); !indexdetails {
		log.Errorf("updateDetails: trying to index non-indexable sb %s(%d): %s", id, t, string(debug.Stack()))
		return fmt.Errorf("updateDetails: trying to index non-indexable sb %s(%d)", id, t)
	}

	metrics.ObjectDetailsUpdatedCounter.Inc()
	detailsKey := pagesDetailsBase.ChildString(id)

	if newDetails.GetDetails().GetFields() == nil {
		return fmt.Errorf("newDetails is nil")
	}

	newDetails.Details.Fields[bundle.RelationKeyId.String()] = pbtypes.String(id) // always ensure we have id set
	b, err := proto.Marshal(newDetails)
	if err != nil {
		return err
	}
	err = txn.Put(detailsKey, b)
	if err != nil {
		return err
	}

	if oldDetails.GetDetails().Equal(newDetails.GetDetails()) {
		return nil
	}

	diff := pbtypes.StructDiff(oldDetails.GetDetails(), newDetails.GetDetails())
	log.Debugf("updateDetails %s: diff %s", id, pbtypes.Sprint(diff))
	err = localstore.UpdateIndexesWithTxn(m, txn, oldDetails, newDetails, id)
	if err != nil {
		return err
	}

	if newDetails != nil && newDetails.Details.Fields != nil {
		m.sendUpdatesToSubscriptions(id, newDetails.Details)
	}

	return nil
}

func (m *dsObjectStore) Prefix() string {
	return pagesPrefix
}

func (m *dsObjectStore) Indexes() []localstore.Index {
	return []localstore.Index{indexObjectTypeRelationObjectId, indexObjectTypeRelationSetId, indexRelationOptionObject, indexRelationObject, indexObjectTypeObject}
}

func (m *dsObjectStore) FTSearch() ftsearch.FTSearch {
	return m.fts
}

func (m *dsObjectStore) makeFTSQuery(text string, dsq query.Query) (query.Query, error) {
	if m.fts == nil {
		return dsq, fmt.Errorf("fullText search not configured")
	}
	ids, err := m.fts.Search(text)
	if err != nil {
		return dsq, err
	}
	idsQuery := newIdsFilter(ids)
	dsq.Filters = append([]query.Filter{idsQuery}, dsq.Filters...)
	dsq.Orders = append([]query.Order{idsQuery}, dsq.Orders...)
	return dsq, nil
}

func (m *dsObjectStore) listIdsOfType(txn noctxds.Txn, ot string) ([]string, error) {
	res, err := localstore.GetKeysByIndexParts(txn, pagesPrefix, indexObjectTypeObject.Name, []string{ot}, "", false, 0)
	if err != nil {
		return nil, err
	}

	return localstore.GetLeavesFromResults(res)
}

func (m *dsObjectStore) listRelationsKeys(txn noctxds.Txn) ([]string, error) {
	txn, err := m.ds.NewTransaction(true)
	if err != nil {
		return nil, fmt.Errorf("error creating txn in datastore: %w", err)
	}
	defer txn.Discard()

	return findByPrefix(txn, relationsBase.String()+"/", 0)
}

func getRelation(txn noctxds.Txn, key string) (*model.Relation, error) {
	br, err := bundle.GetRelation(bundle.RelationKey(key))
	if br != nil {
		return br, nil
	}

	res, err := txn.Get(relationsBase.ChildString(key))
	if err != nil {
		return nil, err
	}

	var rel model.Relation
	if err = proto.Unmarshal(res, &rel); err != nil {
		return nil, fmt.Errorf("failed to unmarshal relation: %s", err.Error())
	}

	return &rel, nil
}

func (m *dsObjectStore) listRelations(txn noctxds.Txn, limit int) ([]*model.Relation, error) {
	var rels []*model.Relation

	res, err := txn.Query(query.Query{
		Prefix:   relationsBase.String(),
		Limit:    limit,
		KeysOnly: false,
	})
	if err != nil {
		return nil, err
	}

	for r := range res.Next() {
		var rel model.Relation
		if err = proto.Unmarshal(r.Value, &rel); err != nil {
			log.Errorf("listRelations failed to unmarshal relation: %s", err.Error())
			continue
		}
		rels = append(rels, &rel)
	}

	return rels, nil
}

/* internal */
// getObjectDetails returns empty(not nil) details when not found in the DS
func getObjectDetails(txn noctxds.Txn, id string) (*model.ObjectDetails, error) {
	val, err := txn.Get(pagesDetailsBase.ChildString(id))
	if err != nil {
		if err != ds.ErrNotFound {
			return nil, fmt.Errorf("failed to get relations: %w", err)
		}
		// return empty details in case not found
		return &model.ObjectDetails{
			Details: &types.Struct{Fields: map[string]*types.Value{}},
		}, nil
	}

	details, err := unmarshalDetails(id, val)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal details: %w", err)
	}

	for k, v := range details.GetDetails().GetFields() {
		// todo: remove null cleanup(should be done when receiving from client)
		if _, isNull := v.GetKind().(*types.Value_NullValue); v == nil || isNull {
			delete(details.Details.Fields, k)
		}
	}
	return details, nil
}

func (m *dsObjectStore) getPendingLocalDetails(txn noctxds.Txn, id string) (*model.ObjectDetails, error) {
	val, err := txn.Get(pendingDetailsBase.ChildString(id))
	if err != nil {
		return nil, err
	}
	return unmarshalDetails(id, val)
}

func hasObjectId(txn noctxds.Txn, id string) (bool, error) {
	if exists, err := txn.Has(pagesDetailsBase.ChildString(id)); err != nil {
		return false, fmt.Errorf("failed to get details: %w", err)
	} else {
		return exists, nil
	}
}

// getObjectRelations returns the list of relations last time indexed for the object
func getObjectRelations(txn noctxds.Txn, id string) ([]*model.Relation, error) {
	var relations model.Relations
	if val, err := txn.Get(pagesRelationsBase.ChildString(id)); err != nil {
		if err != ds.ErrNotFound {
			return nil, fmt.Errorf("failed to get relations: %w", err)
		}
	} else if err := proto.Unmarshal(val, &relations); err != nil {
		return nil, fmt.Errorf("failed to unmarshal relations: %w", err)
	}

	return relations.GetRelations(), nil
}

func (m *dsObjectStore) getObjectInfo(txn noctxds.Txn, id string) (*model.ObjectInfo, error) {
	sbt, err := smartblock.SmartBlockTypeFromID(id)
	if err != nil {
		log.With("thread", id).Errorf("failed to extract smartblock type %s", id)
		return nil, ErrNotAnObject
	}
	if sbt == smartblock.SmartBlockTypeArchive {
		return nil, ErrNotAnObject
	}

	var details *types.Struct
	if indexDetails, _ := sbt.Indexable(); !indexDetails {
		if m.sourceService != nil {
			details, err = m.sourceService.GetDetailsFromIdBasedSource(id)
			if err != nil {
				return nil, err
			}
		}
	} else {
		detailsWrapped, err := getObjectDetails(txn, id)
		if err != nil {
			return nil, err
		}
		details = detailsWrapped.GetDetails()
	}

	relations, err := getObjectRelations(txn, id)
	if err != nil {
		return nil, err
	}

	var snippet string
	if val, err := txn.Get(pagesSnippetBase.ChildString(id)); err != nil && err != ds.ErrNotFound {
		return nil, fmt.Errorf("failed to get snippet: %w", err)
	} else {
		snippet = string(val)
	}

	// omit decoding page state
	hasInbound, err := hasInboundLinks(txn, id)
	if err != nil {
		return nil, err
	}

	return &model.ObjectInfo{
		Id:              id,
		ObjectType:      sbt.ToProto(),
		Details:         details,
		Relations:       relations,
		Snippet:         snippet,
		HasInboundLinks: hasInbound,
	}, nil
}

func (m *dsObjectStore) getObjectsInfo(txn noctxds.Txn, ids []string) ([]*model.ObjectInfo, error) {
	var objects []*model.ObjectInfo
	for _, id := range ids {
		info, err := m.getObjectInfo(txn, id)
		if err != nil {
			if strings.HasSuffix(err.Error(), "key not found") || err == ErrNotAnObject {
				continue
			}
			return nil, err
		}
		if f := info.GetDetails().GetFields(); f != nil {
			// skip deleted objects
			if v := f[bundle.RelationKeyIsDeleted.String()]; v != nil && v.GetBoolValue() {
				continue
			}
		}
		objects = append(objects, info)
	}

	return objects, nil
}

func hasInboundLinks(txn noctxds.Txn, id string) (bool, error) {
	inboundResults, err := txn.Query(query.Query{
		Prefix:   pagesInboundLinksBase.String() + "/" + id + "/",
		Limit:    1, // we only need to know if there is at least 1 inbound link
		KeysOnly: true,
	})
	if err != nil {
		return false, err
	}

	// max is 1
	inboundLinks, err := localstore.CountAllKeysFromResults(inboundResults)
	return inboundLinks > 0, err
}

// Find to which IDs specified one has outbound links.
func findOutboundLinks(txn noctxds.Txn, id string) ([]string, error) {
	return findByPrefix(txn, pagesOutboundLinksBase.String()+"/"+id+"/", 0)
}

// Find from which IDs specified one has inbound links.
func findInboundLinks(txn noctxds.Txn, id string) ([]string, error) {
	return findByPrefix(txn, pagesInboundLinksBase.String()+"/"+id+"/", 0)
}

func findByPrefix(txn noctxds.Txn, prefix string, limit int) ([]string, error) {
	results, err := txn.Query(query.Query{
		Prefix:   prefix,
		Limit:    limit,
		KeysOnly: true,
	})
	if err != nil {
		return nil, err
	}

	return localstore.GetLeavesFromResults(results)
}

// removeByPrefix query prefix and then remove keys in multiple TXs
func removeByPrefix(d noctxds.DSTxnBatching, prefix string) (int, error) {
	results, err := d.Query(query.Query{
		Prefix:   prefix,
		KeysOnly: true,
	})
	if err != nil {
		return 0, err
	}
	var keys []ds.Key
	for res := range results.Next() {
		keys = append(keys, ds.NewKey(res.Key))
	}
	b, err := d.Batch()
	if err != nil {
		return 0, err
	}
	var removed int
	for _, key := range keys {
		err := b.Delete(key)
		if err != nil {
			return removed, err
		}
		removed++
	}

	return removed, b.Commit()
}

func removeByPrefixInTx(txn noctxds.Txn, prefix string) (int, error) {
	results, err := txn.Query(query.Query{
		Prefix:   prefix,
		KeysOnly: true,
	})
	if err != nil {
		return 0, err
	}

	var removed int
	for res := range results.Next() {
		err := txn.Delete(ds.NewKey(res.Key))
		if err != nil {
			_ = results.Close()
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func pageLinkKeys(id string, in []string, out []string) []ds.Key {
	var keys = make([]ds.Key, 0, len(in)+len(out))

	// links incoming into specified node id
	for _, from := range in {
		keys = append(keys, inboundLinkKey(from, id), outgoingLinkKey(from, id))
	}

	// links outgoing from specified node id
	for _, to := range out {
		keys = append(keys, outgoingLinkKey(id, to), inboundLinkKey(id, to))
	}

	return keys
}

func outgoingLinkKey(from, to string) ds.Key {
	return pagesOutboundLinksBase.ChildString(from).ChildString(to)
}

func inboundLinkKey(from, to string) ds.Key {
	return pagesInboundLinksBase.ChildString(to).ChildString(from)
}

func newIdsFilter(ids []string) idsFilter {
	f := make(idsFilter)
	for i, id := range ids {
		f[id] = i
	}
	return f
}

type idsFilter map[string]int

func (f idsFilter) Filter(e query.Entry) bool {
	_, ok := f[extractIdFromKey(e.Key)]
	return ok
}

func (f idsFilter) Compare(a, b query.Entry) int {
	aIndex := f[extractIdFromKey(a.Key)]
	bIndex := f[extractIdFromKey(b.Key)]
	if aIndex == bIndex {
		return 0
	} else if aIndex < bIndex {
		return -1
	} else {
		return 1
	}
}

func extractIdFromKey(key string) (id string) {
	i := strings.LastIndexByte(key, '/')
	if i == -1 || len(key)-1 == i {
		return
	}
	return key[i+1:]
}

// temp func until we move to the proper ids
func objTypeCompactEncode(objType string) (string, error) {
	if strings.HasPrefix(objType, addr.BundledObjectTypeURLPrefix) {
		return objType, nil
	}
	if strings.HasPrefix(objType, addr.ObjectTypeKeyToIdPrefix) {
		return objType, nil
	}
	if strings.HasPrefix(objType, "ba") {
		return objType, nil
	}
	return "", fmt.Errorf("invalid objType")
}

func GetObjectType(store ObjectStore, url string) (*model.ObjectType, error) {
	objectType := &model.ObjectType{}
	if strings.HasPrefix(url, addr.BundledObjectTypeURLPrefix) {
		var err error
		objectType, err = bundle.GetTypeByUrl(url)
		if err != nil {
			if err == bundle.ErrNotFound {
				return nil, fmt.Errorf("unknown object type")
			}
			return nil, err
		}
		return objectType, nil
	}

	ois, err := store.GetByIDs(url)
	if err != nil {
		return nil, err
	}
	if len(ois) == 0 {
		return nil, fmt.Errorf("object type not found in the index")
	}

	details := ois[0].Details
	//relationKeys := ois[0].RelationKeys
	for _, relId := range pbtypes.GetStringList(details, bundle.RelationKeyRecommendedRelations.String()) {
		rk, err := pbtypes.RelationIdToKey(relId)
		if err != nil {
			log.Errorf("GetObjectType failed to get relation key from id: %s (%s)", err.Error(), relId)
			continue
		}

		rel, err := store.GetRelationByKey(rk)
		if err != nil {
			log.Errorf("GetObjectType failed to get relation key from id: %s (%s)", err.Error(), relId)
			continue
		}

		objectType.RelationLinks = append(objectType.RelationLinks, (&relationutils.Relation{rel}).RelationLink())
	}

	objectType.Url = url
	if details != nil && details.Fields != nil {
		if v, ok := details.Fields[bundle.RelationKeyName.String()]; ok {
			objectType.Name = v.GetStringValue()
		}
		if v, ok := details.Fields[bundle.RelationKeyRecommendedLayout.String()]; ok {
			objectType.Layout = model.ObjectTypeLayout(int(v.GetNumberValue()))
		}
		if v, ok := details.Fields[bundle.RelationKeyIconEmoji.String()]; ok {
			objectType.IconEmoji = v.GetStringValue()
		}
	}

	objectType.IsArchived = pbtypes.GetBool(details, bundle.RelationKeyIsArchived.String())
	// we use Page for all custom object types
	objectType.Types = []model.SmartBlockType{model.SmartBlockType_Page}
	return objectType, err
}

func GetObjectTypes(store ObjectStore, urls []string) (ots []*model.ObjectType, err error) {
	ots = make([]*model.ObjectType, 0, len(urls))
	for _, url := range urls {
		ot, e := GetObjectType(store, url)
		if e != nil {
			err = e
		} else {
			ots = append(ots, ot)
		}
	}
	return
}
