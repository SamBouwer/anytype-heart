package editor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/anyproto/anytype-heart/core/block/editor/smartblock/smarttest"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/util/pbtypes"
)

func TestDataview_SetDetails(t *testing.T) {
	var event *pb.Event
	p := NewProfile(nil, nil, nil, nil, nil, func(e *pb.Event) {
		event = e
	})
	p.SmartBlock = smarttest.New("1")

	err := p.SetDetails(nil, []*pb.RpcObjectSetDetailsDetail{
		{
			Key:   "key",
			Value: pbtypes.String("value"),
		},
	}, false)
	require.NoError(t, err)
	require.NotNil(t, event)
}
