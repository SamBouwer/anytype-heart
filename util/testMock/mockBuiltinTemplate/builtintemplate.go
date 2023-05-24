//go:generate mockgen -package mockBuiltinTemplate -destination builtintemplate_mock.go github.com/anyproto/anytype-heart/util/builtintemplate BuiltinTemplate
package mockBuiltinTemplate

import (
	"context"
	"github.com/anyproto/anytype-heart/app/testapp"
	"github.com/anyproto/anytype-heart/util/builtintemplate"
	"github.com/golang/mock/gomock"
)

func RegisterMockBuiltinTemplate(ctrl *gomock.Controller, ta *testapp.TestApp) *MockBuiltinTemplate {
	ms := NewMockBuiltinTemplate(ctrl)
	ms.EXPECT().Name().AnyTimes().Return(builtintemplate.CName)
	ms.EXPECT().Init(gomock.Any()).AnyTimes()
	ms.EXPECT().Run(context.Background()).AnyTimes()
	ms.EXPECT().Close().AnyTimes()
	ta.Register(ms)
	return ms
}
