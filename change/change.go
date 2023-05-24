package change

import (
	"encoding/json"

	"github.com/gogo/protobuf/jsonpb"

	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
)

func NewChangeFromRecord(record core.SmartblockRecordEnvelope) (*Change, error) {
	var ch = &pb.Change{}
	err := record.Unmarshal(ch)
	return &Change{
		Id:      record.ID,
		Account: record.AccountID,
		Device:  record.LogID,
		Change:  ch,
	}, err
}

type Change struct {
	Id          string
	Account     string
	Device      string
	Next        []*Change
	detailsOnly bool
	*pb.Change
}

func (ch *Change) GetLastSnapshotId() string {
	if ch.Snapshot != nil {
		return ch.Id
	}
	return ch.LastSnapshotId
}

func (ch *Change) HasMeta() bool {
	if ch.Snapshot != nil {
		return true
	}
	for _, ct := range ch.Content {
		switch ct.Value.(type) {
		case *pb.ChangeContentValueOfDetailsSet:
			return true
		case *pb.ChangeContentValueOfDetailsUnset:
			return true
		case *pb.ChangeContentValueOfRelationAdd:
			return true
		case *pb.ChangeContentValueOfRelationRemove:
			return true
		case *pb.ChangeContentValueOfOldRelationUpdate:
			return true
		case *pb.ChangeContentValueOfOldRelationRemove:
			return true
		case *pb.ChangeContentValueOfOldRelationAdd:
			return true
		case *pb.ChangeContentValueOfObjectTypeAdd:
			return true
		case *pb.ChangeContentValueOfObjectTypeRemove:
			return true
		case *pb.ChangeContentValueOfStoreKeySet:
			return true
		case *pb.ChangeContentValueOfStoreKeyUnset:
			return true
		case *pb.ChangeContentValueOfBlockUpdate:
			// todo: find a better solution to store dataview relations
			for _, ev := range ct.Value.(*pb.ChangeContentValueOfBlockUpdate).BlockUpdate.Events {
				switch ev.Value.(type) {
				case *pb.EventMessageValueOfBlockDataviewRelationSet:
					return true
				case *pb.EventMessageValueOfBlockDataviewRelationDelete:
					return true
				}
			}
		}
	}
	return false
}

func (ch *Change) MarshalJSON() ([]byte, error) {
	pbjson := ""
	if ch.Change != nil {
		var err error
		ml := &jsonpb.Marshaler{}
		pbjson, err = ml.MarshalToString(ch.Change)
		if err != nil {
			return nil, err
		}
	}
	var data = map[string]string{
		"Id":      ch.Id,
		"Account": ch.Account,
		"Device":  ch.Device,
		"Change":  pbjson,
	}
	return json.Marshal(data)
}

func (ch *Change) UnmarshalJSON(data []byte) (err error) {
	var dataMap = make(map[string]string)
	if err = json.Unmarshal(data, &dataMap); err != nil {
		return
	}
	if chs, ok := dataMap["Change"]; ok {
		ch.Change = &pb.Change{}
		if err = jsonpb.UnmarshalString(chs, ch.Change); err != nil {
			return
		}
	}
	ch.Id = dataMap["Id"]
	ch.Account = dataMap["Account"]
	ch.Device = dataMap["Device"]
	return
}
