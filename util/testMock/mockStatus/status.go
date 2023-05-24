//go:generate mockgen -package mockStatus -destination status_mock.go github.com/anyproto/anytype-heart/core/status Service
package mockStatus

import (
	"context"
	"github.com/anyproto/anytype-heart/app/testapp"
	"github.com/anyproto/anytype-heart/core/status"
	"github.com/golang/mock/gomock"
)

func RegisterMockStatus(ctrl *gomock.Controller, ta *testapp.TestApp) *MockService {
	ms := NewMockService(ctrl)
	ms.EXPECT().Name().AnyTimes().Return(status.CName)
	ms.EXPECT().Init(gomock.Any()).AnyTimes()
	ms.EXPECT().Run(context.Background()).AnyTimes()
	ms.EXPECT().Close().AnyTimes()
	ta.Register(ms)
	return ms
}
