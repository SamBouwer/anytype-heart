package change

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/core/smartblock"
)

func NewTestSmartBlock() *TestSmartblock {
	return &TestSmartblock{
		changes: make(map[string]*core.SmartblockRecordEnvelope),
	}
}

type TestSmartblock struct {
	logs    []core.SmartblockLog
	changes map[string]*core.SmartblockRecordEnvelope
}

func (s *TestSmartblock) AddChanges(logId string, chs ...*Change) *TestSmartblock {
	var id string
	for _, ch := range chs {
		pl, _ := ch.Change.Marshal()
		s.changes[ch.Id] = &core.SmartblockRecordEnvelope{
			SmartblockRecord: core.SmartblockRecord{
				ID:      ch.Id,
				Payload: pl,
			},
			LogID: logId,
		}
		id = ch.Id
	}
	for i, l := range s.logs {
		if l.ID == logId {
			s.logs[i].Head = id
			return s
		}
	}
	s.logs = append(s.logs, core.SmartblockLog{
		ID:   logId,
		Head: id,
	})
	return s
}

func (s *TestSmartblock) ID() string {
	return "id"
}

func (s *TestSmartblock) Type() smartblock.SmartBlockType {
	return smartblock.SmartBlockTypePage
}

func (s *TestSmartblock) Creator() (string, error) {
	return "", nil
}

func (s *TestSmartblock) GetLogs() ([]core.SmartblockLog, error) {
	return s.logs, nil
}

func (s *TestSmartblock) GetRecord(ctx context.Context, recordID string) (*core.SmartblockRecordEnvelope, error) {
	if data, ok := s.changes[recordID]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("record %v not found", recordID)
}

func (s *TestSmartblock) PushRecord(payload proto.Marshaler) (id string, err error) {
	panic("implement me")
}

func (s *TestSmartblock) SubscribeForRecords(ch chan core.SmartblockRecordEnvelope) (cancel func(), err error) {
	panic("implement me")
}

func (s *TestSmartblock) SubscribeClientEvents(event chan<- proto.Message) (cancelFunc func(), err error) {
	panic("implement me")
}

func (s *TestSmartblock) PublishClientEvent(event proto.Message) error {
	panic("implement me")
}
