package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/ipfs/go-cid"
	cbornode "github.com/ipfs/go-ipld-cbor"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	mh "github.com/multiformats/go-multihash"
	"github.com/textileio/go-threads/cbor"
	"github.com/textileio/go-threads/core/net"
	"github.com/textileio/go-threads/core/thread"

	"github.com/anyproto/anytype-heart/metrics"
	"github.com/anyproto/anytype-heart/pkg/lib/core/smartblock"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/pkg/lib/vclock"
)

type ProfileThreadEncryptionKeys struct {
	ServiceKey []byte
	ReadKey    []byte
}

func init() {
	cbornode.RegisterCborType(ProfileThreadEncryptionKeys{})
}

// ShouldCreateSnapshot informs if you need to make a snapshot based on deterministic alg
// temporally always returns true
func (block smartBlock) ShouldCreateSnapshot(state vclock.VClock) bool {
	if strings.HasSuffix(state.Hash(), "0") {
		return true
	}

	// return false
	// todo: return false when changes will be implemented
	return true
}

type SmartBlockContentChange struct {
	state vclock.VClock
	// to be discussed
}

type SmartBlockMeta struct {
	ObjectTypes   []string
	RelationLinks []*model.RelationLink
	Details       *types.Struct
}

type SmartBlockMetaChange struct {
	SmartBlockMeta
	state vclock.VClock
}

func (meta *SmartBlockMetaChange) State() vclock.VClock {
	return meta.state
}

func (meta *SmartBlockContentChange) State() vclock.VClock {
	return meta.state
}

type SmartBlockChange struct {
	Content *SmartBlockContentChange
	Meta    *SmartBlockMetaChange
}

type SmartBlock interface {
	ID() string
	Type() smartblock.SmartBlockType
	Creator() (string, error)

	GetLogs() ([]SmartblockLog, error)
	GetRecord(ctx context.Context, recordID string) (*SmartblockRecordEnvelope, error)
	PushRecord(payload proto.Marshaler) (id string, err error)

	SubscribeForRecords(ch chan SmartblockRecordEnvelope) (cancel func(), err error)
	// SubscribeClientEvents provide a way to subscribe for the client-side events e.g. carriage position change
	SubscribeClientEvents(event chan<- proto.Message) (cancelFunc func(), err error)
	// PublishClientEvent gives a way to push the new client-side event e.g. carriage position change
	// notice that you will also get this event in SubscribeForEvents
	PublishClientEvent(event proto.Message) error
}

type smartBlock struct {
	thread thread.Info
	node   *Anytype
}

func NewSmartBlock(thread thread.Info, node Service) SmartBlock {
	return &smartBlock{
		thread: thread,
		node:   node.(*Anytype),
	}
}

func (block *smartBlock) Creator() (string, error) {
	return "", fmt.Errorf("to be implemented")
}

func (block *smartBlock) GetThread() thread.Info {
	return block.thread
}

func (block *smartBlock) Type() smartblock.SmartBlockType {
	t, err := smartblock.SmartBlockTypeFromThreadID(block.thread.ID)
	if err != nil {
		// shouldn't happen as we init the smartblock with an existing thread
		log.Errorf("smartblock has incorrect id(%s), failed to decode type: %s", block.thread.ID.String(), err.Error())
		return 0
	}

	return t
}

func (block *smartBlock) ID() string {
	return block.thread.ID.String()
}

func (block *smartBlock) GetLastSnapshot() (SmartBlockSnapshot, error) {
	versions, err := block.GetSnapshots(vclock.Undef, 1, false)
	if err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		return nil, ErrBlockSnapshotNotFound
	}

	return versions[0], nil
}

func (block *smartBlock) GetChangesBetween(since vclock.VClock, until vclock.VClock) ([]SmartBlockChange, error) {
	return nil, fmt.Errorf("not implemented")
}

func (block *smartBlock) GetSnapshotBefore(state vclock.VClock) (SmartBlockSnapshot, error) {
	versions, err := block.GetSnapshots(state, 1, false)
	if err != nil {
		return nil, err
	}

	if len(versions) == 0 {
		return nil, ErrBlockSnapshotNotFound
	}

	return versions[0], nil
}

/*func (block *smartBlock) GetSnapshotMeta(id string) (Sm, error) {
	event, err := block.getSnapshotSnapshotEvent(id)
	if err != nil {
		return nil, err
	}

	service, err := event.GetBody(context.TODO(), *block.Service.t, block.thread.ReadKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get record body: %w", err)
	}
	m := new(threadSnapshot)
	err = cbornode.DecodeInto(service.RawData(), m)
	if err != nil {
		return nil, fmt.Errorf("incorrect record type: %w", err)
	}

	model, err := m.()
	if err != nil {
		return nil, fmt.Errorf("failed to decode pb block version: %w", err)
	}

	time, err := block.getSnapshotTime(event)
	if err != nil {
		return nil, fmt.Errorf("failed to decode pb block version: %w", err)
	}

	// todo: how to get creator peer id?
	version := &smartBlockSnapshotMeta{model: model, date: time, creator: "<todo>"}

	return version, nil
}*/

func (block *smartBlock) GetSnapshots(offset vclock.VClock, limit int, metaOnly bool) (snapshots []smartBlockSnapshot, err error) {
	snapshotsPB, err := block.node.snapshotTraverseLogs(context.TODO(), block.thread.ID, offset, limit)
	if err != nil {
		return
	}

	for _, snapshot := range snapshotsPB {
		snapshots = append(snapshots, smartBlockSnapshot{

			blocks:  snapshot.Blocks,
			details: snapshot.Details,
			state:   vclock.NewFromMap(snapshot.State),
			creator: snapshot.Creator,

			threadID: block.thread.ID,
			recordID: snapshot.RecordID,
			eventID:  snapshot.EventID,
			key:      block.thread.Key.Read(),

			node: block.node,
		})
	}

	return
}

func (block *smartBlock) PushRecord(payload proto.Marshaler) (id string, err error) {
	metrics.ChangeCreatedCounter.Inc()
	payloadB, err := payload.Marshal()
	if err != nil {
		return "", err
	}

	acc, err := block.node.wallet.GetAccountPrivkey()
	if err != nil {
		return "", fmt.Errorf("failed to get account key: %v", err)
	}

	signedPayload, err := newSignedPayload(payloadB, acc)
	if err != nil {
		return "", err
	}

	body, err := cbornode.WrapObject(signedPayload, mh.SHA2_256, -1)
	if err != nil {
		return "", err
	}

	pk, err := block.node.wallet.GetDevicePrivkey()
	if err != nil {
		return "", err
	}

	rec, err := block.node.threadService.Threads().CreateRecord(context.TODO(), block.thread.ID, body, net.WithLogPrivateKey(pk))
	if err != nil {
		log.Errorf("failed to create record: %w", err)
		return "", err
	}

	log.Debugf("SmartBlock.PushRecord: blockId = %s", block.ID())
	return rec.Value().Cid().String(), nil
}

func (block *smartBlock) SubscribeForRecords(ch chan SmartblockRecordEnvelope) (cancel func(), err error) {
	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())

	// todo: this is not effective, need to make a single subscribe point for all subscribed threads
	threadsCh, err := block.node.threadService.Threads().Subscribe(ctx, net.WithSubFilter(block.thread.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe: %s", err.Error())
	}

	go func() {
		block.node.lock.Lock()
		shutdownCh := block.node.shutdownStartsCh
		block.node.lock.Unlock()
		defer close(ch)
		for {
			select {
			case val, ok := <-threadsCh:
				if !ok {
					return
				}

				metrics.ChangeReceivedCounter.Inc()
				rec, err := block.decodeRecord(ctx, val.Value(), false)
				if err != nil {
					log.Errorf("failed to decode thread record: %s", err.Error())
					continue
				}
				select {
				case ch <- SmartblockRecordEnvelope{
					SmartblockRecord: rec.SmartblockRecord,
					AccountID:        rec.AccountID,
					LogID:            val.LogID().String(),
				}: // everything is ok
				case <-ctx.Done():
					// no need to cancel, continue to read the rest msgs from the channel
					continue
				case <-shutdownCh:
					// cancel first, then we should read ok == false from the threadsCh
					cancel()
				}
			case <-ctx.Done():
				continue
			case <-shutdownCh:
				cancel()
			}
		}
	}()

	return cancel, nil
}

func (block *smartBlock) SubscribeClientEvents(events chan<- proto.Message) (cancelFunc func(), err error) {
	// todo: to be implemented
	return func() { close(events) }, nil
}

func (block *smartBlock) PublishClientEvent(event proto.Message) error {
	// todo: to be implemented
	return fmt.Errorf("not implemented")
}

func (block *smartBlock) GetLogs() ([]SmartblockLog, error) {
	thrd, err := block.node.ThreadService().Threads().GetThread(context.Background(), block.thread.ID)
	if err != nil {
		return nil, err
	}

	var logs = make([]SmartblockLog, 0, len(thrd.Logs))
	for _, l := range thrd.Logs {
		var head string
		if l.Head.ID.Defined() {
			head = l.Head.ID.String()
		}

		logs = append(logs, SmartblockLog{
			ID:          l.ID.String(),
			Head:        head,
			HeadCounter: l.Head.Counter,
		})
	}

	return logs, nil
}

func (block *smartBlock) decodeRecord(
	ctx context.Context,
	rec net.Record,
	decodeLogID bool,
) (*SmartblockRecordEnvelope, error) {
	event, err := cbor.EventFromRecord(ctx, block.node.threadService.Threads(), rec)
	if err != nil {
		return nil, err
	}

	node, err := event.GetBody(context.TODO(), block.node.threadService.Threads(), block.thread.Key.Read())
	if err != nil {
		return nil, fmt.Errorf("failed to get record body: %w", err)
	}

	var m SignedPbPayload
	if err = cbornode.DecodeInto(node.RawData(), &m); err != nil {
		return nil, fmt.Errorf("incorrect record type: %w", err)
	}

	if err = m.Verify(); err != nil {
		return nil, err
	}

	var prevID string
	if rec.PrevID().Defined() {
		prevID = rec.PrevID().String()
	}

	var logID string
	if decodeLogID {
		if pk, err := crypto.UnmarshalPublicKey(rec.PubKey()); err != nil {
			return nil, fmt.Errorf("failed to decode record's public key: %w", err)
		} else if pid, err := peer.IDFromPublicKey(pk); err != nil {
			return nil, fmt.Errorf("failed to restore ID from public key: %w", err)
		} else {
			logID = pid.String()
		}
	}

	return &SmartblockRecordEnvelope{
		SmartblockRecord: SmartblockRecord{
			ID:      rec.Cid().String(),
			PrevID:  prevID,
			Payload: m.Data,
		},
		AccountID: m.AccAddr,
		LogID:     logID,
	}, nil
}

func (block *smartBlock) GetRecord(ctx context.Context, recordID string) (*SmartblockRecordEnvelope, error) {
	rid, err := cid.Decode(recordID)
	if err != nil {
		return nil, err
	}

	cid, err := cid.Parse(rid)
	if err != nil {
		return nil, err
	}

	b, err := block.node.ipfs.HasBlock(cid)
	if err != nil {
		return nil, err
	}
	skipMissing, ok := ctx.Value(ThreadLoadSkipMissingRecords).(bool)

	if ok && skipMissing && !b {
		return nil, fmt.Errorf("record not found locally")
	}

	ctxProgress, _ := ctx.Value(ThreadLoadProgressContextKey).(*ThreadLoadProgress)
	if ctxProgress != nil {
		if !b {
			ctxProgress.IncrementMissingRecord()
		}
	}
	start := time.Now()
	rec, err := block.node.threadService.Threads().GetRecord(ctx, block.thread.ID, rid)
	if err != nil {
		if ctxProgress != nil {
			ctxProgress.IncrementFailedRecords()
		}
		return nil, err
	}
	if ctxProgress != nil {
		ctxProgress.IncrementLoadedRecords(time.Since(start).Seconds())
	}

	return block.decodeRecord(ctx, rec, true)
}
