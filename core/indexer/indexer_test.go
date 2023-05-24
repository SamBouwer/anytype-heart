package indexer_test

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"testing"

	"github.com/gogo/protobuf/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/app/testapp"
	"github.com/anyproto/anytype-heart/core/anytype/config"
	"github.com/anyproto/anytype-heart/core/block/doc"
	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/core/block/source"
	"github.com/anyproto/anytype-heart/core/indexer"
	"github.com/anyproto/anytype-heart/core/recordsbatcher"
	"github.com/anyproto/anytype-heart/core/relation"
	"github.com/anyproto/anytype-heart/core/relation/relationutils"
	"github.com/anyproto/anytype-heart/core/wallet"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/datastore/clientds"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/addr"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/ftsearch"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/util/pbtypes"
	"github.com/anyproto/anytype-heart/util/testMock"
	"github.com/anyproto/anytype-heart/util/testMock/mockBuiltinTemplate"
	"github.com/anyproto/anytype-heart/util/testMock/mockDoc"
	"github.com/anyproto/anytype-heart/util/testMock/mockRelation"
	"github.com/anyproto/anytype-heart/util/testMock/mockStatus"
)

func TestNewIndexer(t *testing.T) {
	t.Run("open/close", func(t *testing.T) {
		fx := newFixture(t)
		// should add all bundled relations to full text index
		defer fx.Close()
		defer fx.tearDown()

	})
}

func newFixture(t *testing.T) *fixture {
	ta := testapp.New()
	rb := recordsbatcher.New()

	fx := &fixture{
		ctrl: gomock.NewController(t),
		ta:   ta,
		rb:   rb,
	}

	fx.anytype = testMock.RegisterMockAnytype(fx.ctrl, ta)
	fx.docService = mockDoc.NewMockService(fx.ctrl)
	fx.docService.EXPECT().Name().AnyTimes().Return(doc.CName)
	fx.docService.EXPECT().Init(gomock.Any())
	fx.docService.EXPECT().Run(context.Background())
	fx.anytype.EXPECT().PredefinedBlocks().Times(2)
	fx.docService.EXPECT().Close().AnyTimes()
	fx.objectStore = testMock.RegisterMockObjectStore(fx.ctrl, ta)

	fx.docService.EXPECT().GetDocInfo(gomock.Any(), gomock.Any()).Return(doc.DocInfo{State: state.NewDoc("", nil).(*state.State)}, nil).AnyTimes()
	fx.docService.EXPECT().OnWholeChange(gomock.Any())
	fx.objectStore.EXPECT().GetDetails(addr.AnytypeProfileId)
	fx.objectStore.EXPECT().AddToIndexQueue(addr.AnytypeProfileId)

	for _, rk := range bundle.ListRelationsKeys() {
		fx.objectStore.EXPECT().GetDetails(addr.BundledRelationURLPrefix + rk.String())
		fx.objectStore.EXPECT().AddToIndexQueue(addr.BundledRelationURLPrefix + rk.String())
	}
	for _, ok := range bundle.ListTypesKeys() {
		fx.objectStore.EXPECT().GetDetails(ok.BundledURL())
		fx.objectStore.EXPECT().AddToIndexQueue(ok.BundledURL())
	}
	fx.anytype.EXPECT().ProfileID().AnyTimes()
	fx.objectStore.EXPECT().GetDetails("_anytype_profile")
	fx.objectStore.EXPECT().AddToIndexQueue("_anytype_profile")
	fx.anytype.EXPECT().ThreadsIds().AnyTimes()
	fx.objectStore.EXPECT().FTSearch().Return(nil).AnyTimes()
	fx.objectStore.EXPECT().IndexForEach(gomock.Any()).Times(1)
	fx.objectStore.EXPECT().UpdateObjectLinks(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	fx.objectStore.EXPECT().CreateObject(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	fx.anytype.EXPECT().ObjectStore().Return(fx.objectStore).AnyTimes()
	fx.objectStore.EXPECT().SaveChecksums(&model.ObjectStoreChecksums{
		BundledObjectTypes:               bundle.TypeChecksum,
		BundledRelations:                 bundle.RelationChecksum,
		BundledLayouts:                   "",
		ObjectsForceReindexCounter:       indexer.ForceThreadsObjectsReindexCounter,
		FilesForceReindexCounter:         indexer.ForceFilesReindexCounter,
		IdxRebuildCounter:                indexer.ForceIdxRebuildCounter,
		FulltextRebuild:                  indexer.ForceFulltextIndexCounter,
		BundledObjects:                   indexer.ForceBundledObjectsReindexCounter,
		FilestoreKeysForceReindexCounter: indexer.ForceFilestoreKeysReindexCounter,
	}).Times(1)

	fx.Indexer = indexer.New()

	rootPath, err := ioutil.TempDir(os.TempDir(), "anytype_*")
	require.NoError(t, err)
	cfg := config.DefaultConfig
	cfg.NewAccount = true
	ta.With(&cfg).With(wallet.NewWithRepoPathAndKeys(rootPath, nil, nil)).
		With(clientds.New()).
		With(ftsearch.New()).
		With(fx.rb).
		With(fx.Indexer).
		With(fx.docService).
		With(source.New())
	mockStatus.RegisterMockStatus(fx.ctrl, ta)
	mockBuiltinTemplate.RegisterMockBuiltinTemplate(fx.ctrl, ta).EXPECT().Hash().AnyTimes()
	rs := mockRelation.RegisterMockRelation(fx.ctrl, ta)
	rs.EXPECT().MigrateObjectTypes(gomock.Any()).Times(1)
	rs.EXPECT().MigrateRelations(gomock.Any()).Times(1)

	require.NoError(t, ta.Start(context.Background()))
	return fx
}

type fixture struct {
	indexer.Indexer
	ctrl        *gomock.Controller
	anytype     *testMock.MockService
	objectStore *testMock.MockObjectStore
	docService  *mockDoc.MockService
	ch          chan core.SmartblockRecordWithThreadID
	rb          recordsbatcher.RecordsBatcher
	ta          *testapp.TestApp
}

func (fx *fixture) tearDown() {
	fx.rb.(io.Closer).Close()
	fx.ta.Close()
	fx.ctrl.Finish()
}

type rs struct{}

func (b *rs) FetchKeys(keys ...string) (relations relationutils.Relations, err error) {
	return
}

func (b *rs) FetchKey(key string, opts ...relation.FetchOption) (relation *relationutils.Relation, err error) {
	return
}

func (b *rs) FetchLinks(links pbtypes.RelationLinks) (relations relationutils.Relations, err error) {
	return
}

func (b *rs) MigrateOldRelations(relations []*model.Relation) (err error) {
	return
}

func (b *rs) ValidateFormat(key string, v *types.Value) error {
	return nil
}

func (b *rs) Init(_ *app.App) (err error) {
	return
}

func (b *rs) Name() (name string) {
	return "relation"
}
