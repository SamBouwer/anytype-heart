package core

import (
	"github.com/anyproto/anytype-heart/pkg/lib/files"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
)

type ServiceOption func(*ServiceOptions) error
type ServiceOptions struct {
	SnapshotMarshalerFunc func(blocks []*model.Block, details *types.Struct, relations []*model.Relation, objectTypes []string, fileKeys []*files.FileKeys) proto.Marshaler
	NewSmartblockChan     chan string
}
