package builtintemplate

import (
	"context"
	"testing"

	"github.com/anyproto/anytype-heart/app/testapp"
	"github.com/anyproto/anytype-heart/core/block/source"
	"github.com/anyproto/anytype-heart/util/testMock/mockSource"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func Test_registerBuiltin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s := mockSource.NewMockService(ctrl)
	s.EXPECT().Name().Return(source.CName).AnyTimes()
	s.EXPECT().Init(gomock.Any()).AnyTimes()
	s.EXPECT().NewStaticSource(gomock.Any(), gomock.Any(), gomock.Any(), nil).AnyTimes()
	s.EXPECT().RegisterStaticSource(gomock.Any(), gomock.Any()).AnyTimes()

	a := testapp.New().With(s).With(New())
	require.NoError(t, a.Start(context.Background()))
	defer a.Close()
}
