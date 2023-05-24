package converter

import (
	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/gogo/protobuf/types"
)

type Converter interface {
	Convert(model.SmartBlockType) (result []byte)
	SetKnownDocs(docs map[string]*types.Struct) Converter
	FileHashes() []string
	ImageHashes() []string
	Ext() string
}

type MultiConverter interface {
	Converter
	Add(state *state.State) error
}
