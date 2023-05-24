package pbc

import (
	"github.com/gogo/protobuf/types"

	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/core/converter"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
)

var log = logging.Logger("pb-converter")

func NewConverter(s state.Doc) converter.Converter {
	return &pbc{s}
}

type pbc struct {
	s state.Doc
}

func (p *pbc) Convert(sbType model.SmartBlockType) []byte {
	st := p.s.NewState()
	snapshot := &pb.ChangeSnapshot{
		Data: &model.SmartBlockSnapshotBase{
			Blocks:        st.BlocksToSave(),
			Details:       st.CombinedDetails(),
			ObjectTypes:   st.ObjectTypes(),
			Collections:   st.Store(),
			RelationLinks: st.PickRelationLinks(),
		},
	}
	for _, fk := range p.s.GetAndUnsetFileKeys() {
		snapshot.FileKeys = append(snapshot.FileKeys, &pb.ChangeFileKeys{Hash: fk.Hash, Keys: fk.Keys})
	}

	mo := &pb.SnapshotWithType{
		SbType:   sbType,
		Snapshot: snapshot,
	}
	result, err := mo.Marshal()
	if err != nil {
		log.Errorf("failed to marshal object: %s", err.Error())
	}
	return result
}

func (p *pbc) Ext() string {
	return ".pb"
}

func (p *pbc) SetKnownDocs(map[string]*types.Struct) converter.Converter {
	return p
}

func (p *pbc) FileHashes() []string {
	return nil
}

func (p *pbc) ImageHashes() []string {
	return nil
}
