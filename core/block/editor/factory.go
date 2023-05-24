package editor

import (
	"fmt"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/core/block/editor/bookmark"
	"github.com/anyproto/anytype-heart/core/block/editor/file"
	"github.com/anyproto/anytype-heart/core/block/editor/smartblock"
	"github.com/anyproto/anytype-heart/core/block/source"
	"github.com/anyproto/anytype-heart/core/event"
	relation2 "github.com/anyproto/anytype-heart/core/relation"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
)

type ObjectFactory struct {
	anytype              core.Service
	bookmarkBlockService bookmark.BlockService
	bookmarkService      bookmark.BookmarkService
	detailsModifier      DetailsModifier
	fileBlockService     file.BlockService
	objectStore          objectstore.ObjectStore
	relationService      relation2.Service
	sourceService        source.Service
	accountMigrator      AccountMigrator
	sendEvent            func(e *pb.Event)

	app *app.App
}

func NewObjectFactory() *ObjectFactory {
	return &ObjectFactory{}
}

func (f *ObjectFactory) Init(a *app.App) (err error) {
	f.anytype = app.MustComponent[core.Service](a)
	f.bookmarkBlockService = app.MustComponent[bookmark.BlockService](a)
	f.bookmarkService = app.MustComponent[bookmark.BookmarkService](a)
	f.detailsModifier = app.MustComponent[DetailsModifier](a)
	f.fileBlockService = app.MustComponent[file.BlockService](a)
	f.objectStore = app.MustComponent[objectstore.ObjectStore](a)
	f.relationService = app.MustComponent[relation2.Service](a)
	f.sourceService = app.MustComponent[source.Service](a)
	f.accountMigrator = app.MustComponent[AccountMigrator](a)
	f.sendEvent = app.MustComponent[event.Sender](a).Send

	f.app = a
	return nil
}

const CName = "objectFactory"

func (f *ObjectFactory) Name() (name string) {
	return CName
}

func (f *ObjectFactory) InitObject(id string, initCtx *smartblock.InitContext) (sb smartblock.SmartBlock, err error) {
	sc, err := f.sourceService.NewSource(id, false)
	if err != nil {
		return
	}

	sb = f.New(sc.Type())
	sb.Lock()
	defer sb.Unlock()
	if initCtx == nil {
		initCtx = &smartblock.InitContext{}
	}
	initCtx.App = f.app
	initCtx.Source = sc
	err = sb.Init(initCtx)
	return
}

func (f *ObjectFactory) New(sbType model.SmartBlockType) smartblock.SmartBlock {
	switch sbType {
	case model.SmartBlockType_Page, model.SmartBlockType_Date:
		return NewPage(
			f.objectStore,
			f.anytype,
			f.fileBlockService,
			f.bookmarkBlockService,
			f.bookmarkService,
			f.relationService,
		)
	case model.SmartBlockType_Archive:
		return NewArchive(
			f.detailsModifier,
			f.objectStore,
		)
	case model.SmartBlockType_Home:
		return NewDashboard(
			f.detailsModifier,
			f.objectStore,
			f.anytype,
		)
	case model.SmartBlockType_Set:
		return NewSet(
			f.anytype,
			f.objectStore,
			f.relationService,
		)
	case model.SmartBlockType_ProfilePage, model.SmartBlockType_AnytypeProfile:
		return NewProfile(
			f.objectStore,
			f.anytype,
			f.fileBlockService,
			f.bookmarkBlockService,
			f.bookmarkService,
			f.sendEvent,
		)
	case model.SmartBlockType_STObjectType,
		model.SmartBlockType_BundledObjectType:
		return NewObjectType(
			f.anytype,
			f.objectStore,
			f.relationService,
		)
	case model.SmartBlockType_BundledRelation:
		return NewSet(
			f.anytype,
			f.objectStore,
			f.relationService,
		)
	case model.SmartBlockType_SubObject:
		panic("subobject not supported via factory")
	case model.SmartBlockType_File:
		return NewFiles()
	case model.SmartBlockType_MarketplaceType:
		return NewMarketplaceType(
			f.anytype,
			f.objectStore,
			f.relationService,
		)
	case model.SmartBlockType_MarketplaceRelation:
		return NewMarketplaceRelation(
			f.anytype,
			f.objectStore,
			f.relationService,
		)
	case model.SmartBlockType_MarketplaceTemplate:
		return NewMarketplaceTemplate(
			f.anytype,
			f.objectStore,
			f.relationService,
		)
	case model.SmartBlockType_Template:
		return NewTemplate(
			f.objectStore,
			f.anytype,
			f.fileBlockService,
			f.bookmarkBlockService,
			f.bookmarkService,
			f.relationService,
		)
	case model.SmartBlockType_BundledTemplate:
		return NewTemplate(
			f.objectStore,
			f.anytype,
			f.fileBlockService,
			f.bookmarkBlockService,
			f.bookmarkService,
			f.relationService,
		)
	case model.SmartBlockType_Breadcrumbs:
		return NewBreadcrumbs()
	case model.SmartBlockType_Workspace:
		return NewWorkspace(
			f.objectStore,
			f.anytype,
			f.relationService,
			f.sourceService,
			f.detailsModifier,
			f.fileBlockService,
		)
	case model.SmartBlockType_AccountOld:
		return NewThreadDB(f.accountMigrator)
	case model.SmartBlockType_Widget:
		return NewWidgetObject()
	default:
		panic(fmt.Errorf("unexpected smartblock type: %v", sbType))
	}
}
