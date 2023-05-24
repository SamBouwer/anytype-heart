//go:generate mockgen -package testMock -destination anytype_mock.go github.com/anyproto/anytype-heart/pkg/lib/core Service,SmartBlock,SmartBlockSnapshot,File,Image
//go:generate mockgen -package testMock -destination objectstore_mock.go github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore ObjectStore
//go:generate mockgen -package testMock -destination history_mock.go github.com/anyproto/anytype-heart/core/block/undo History
package testMock

import (
	"context"
	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/app/testapp"
	"github.com/anyproto/anytype-heart/core/kanban"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore"
	"github.com/anyproto/anytype-heart/util/testMock/mockKanban"
	"github.com/golang/mock/gomock"
)

type App interface {
	Register(component app.Component) *app.App
}

func RegisterMockAnytype(ctrl *gomock.Controller, ta *testapp.TestApp) *MockService {
	ms := NewMockService(ctrl)
	ms.EXPECT().Name().AnyTimes().Return(core.CName)
	ms.EXPECT().Init(gomock.Any()).AnyTimes()
	ms.EXPECT().Run(context.Background()).AnyTimes()
	ms.EXPECT().Close().AnyTimes()
	ms.EXPECT().Account().AnyTimes().Return("account")
	ms.EXPECT().ProfileID().AnyTimes().Return("profileId")
	ta.Register(ms)
	return ms
}

func RegisterMockObjectStore(ctrl *gomock.Controller, ta App) *MockObjectStore {
	ms := NewMockObjectStore(ctrl)
	ms.EXPECT().Name().AnyTimes().Return(objectstore.CName)
	ms.EXPECT().Init(gomock.Any()).AnyTimes()
	ms.EXPECT().Run(context.Background()).AnyTimes()
	ms.EXPECT().Close().AnyTimes()
	ta.Register(ms)
	return ms
}

func RegisterMockKanban(ctrl *gomock.Controller, ta App) *mockKanban.MockService {
	ms := mockKanban.NewMockService(ctrl)
	ms.EXPECT().Name().AnyTimes().Return(kanban.CName)
	ms.EXPECT().Init(gomock.Any()).AnyTimes()
	ta.Register(ms)
	return ms
}

func GetMockAnytype(ta *testapp.TestApp) *MockService {
	return ta.MustComponent(core.CName).(*MockService)
}
