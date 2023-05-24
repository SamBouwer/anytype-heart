package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gogo/protobuf/types"
	cid "github.com/ipfs/go-cid"
	cbornode "github.com/ipfs/go-ipld-cbor"
	"github.com/textileio/go-threads/cbor"
	"github.com/textileio/go-threads/core/thread"
	"github.com/textileio/go-threads/crypto"

	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/storage"
	"github.com/anyproto/anytype-heart/pkg/lib/vclock"
)

type SmartBlockSnapshot interface {
	State() vclock.VClock
	Creator() (string, error)
	CreatedDate() *time.Time
	ReceivedDate() *time.Time
	Blocks() ([]*model.Block, error)
	Meta() (*SmartBlockMeta, error)
	PublicWebURL() (string, error)
}

var ErrFailedToDecodeSnapshot = fmt.Errorf("failed to decode pb block snapshot")

type smartBlockSnapshot struct {
	blocks  []*model.Block
	details *types.Struct
	state   vclock.VClock

	threadID thread.ID
	recordID cid.Cid
	eventID  cid.Cid
	key      crypto.DecryptionKey
	creator  string
	date     *types.Timestamp
	node     *Anytype
}

func (snapshot smartBlockSnapshot) State() vclock.VClock {
	return snapshot.state
}

func (snapshot smartBlockSnapshot) Creator() (string, error) {
	return snapshot.creator, nil
}

func (snapshot smartBlockSnapshot) CreatedDate() *time.Time {
	return nil
}

func (snapshot smartBlockSnapshot) ReceivedDate() *time.Time {
	return nil
}

func (snapshot smartBlockSnapshot) Blocks() ([]*model.Block, error) {
	// todo: blocks lazy loading
	return snapshot.blocks, nil
}

func (snapshot smartBlockSnapshot) Meta() (*SmartBlockMeta, error) {
	return &SmartBlockMeta{Details: snapshot.details}, nil
}

func (snapshot smartBlockSnapshot) PublicWebURL() (string, error) {
	return "", fmt.Errorf("not implemented")
	/*ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	ipfs := snapshot.node.threadService.Threads()
	if snapshot.eventID == cid.Undef {
		// todo: extract from recordID?
		return "", fmt.Errorf("eventID is empty")
	}

	event, err := cbor.GetEvent(ctx, ipfs, snapshot.eventID)
	if err != nil {
		return "", fmt.Errorf("failed to get snapshot event: %w", err)
	}

	header, err := event.GetHeader(ctx, ipfs, snapshot.key)
	if err != nil {
		return "", fmt.Errorf("failed to get snapshot event header: %w", err)
	}

	bodyKey, err := header.Key()
	if err != nil {
		return "", fmt.Errorf("failed to get body decryption key: %w", err)
	}

	bodyKeyBin, err := bodyKey.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("failed to get marshal decryption key: %w", err)
	}

	return fmt.Sprintf(
		snapshot.node.opts.WebGatewayBaseUrl+snapshot.node.opts.WebGatewaySnapshotUri,
		snapshot.threadID.String(),
		event.BodyID().String(),
		base64.RawURLEncoding.EncodeToString(bodyKeyBin),
	), nil*/
}

type SnapshotWithMetadata struct {
	storage.SmartBlockSnapshot
	Creator  string
	RecordID cid.Cid
	EventID  cid.Cid
}

// deprecated, used only for migrating the data from version 0 to 1
func (a *Anytype) snapshotTraverseFromCid(ctx context.Context, thrd thread.Info, li thread.LogInfo, before vclock.VClock, limit int) ([]SnapshotWithMetadata, error) {
	var snapshots []SnapshotWithMetadata
	// todo: filter by record type
	var m = make(map[cid.Cid]struct{})

	rid := li.Head.ID
	if !rid.Defined() {
		return []SnapshotWithMetadata{}, nil
	}

	for {
		if _, exists := m[rid]; exists {
			break
		}

		m[rid] = struct{}{}
		rec, err := a.threadService.Threads().GetRecord(ctx, thrd.ID, rid)
		if err != nil {
			return nil, err
		}

		event, err := cbor.EventFromRecord(ctx, a.threadService.Threads(), rec)
		if err != nil {
			return nil, err
		}

		node, err := event.GetBody(context.TODO(), a.threadService.Threads(), thrd.Key.Read())
		if err != nil {
			return nil, fmt.Errorf("failed to get record body: %w", err)
		}
		m := new(SignedPbPayload)
		err = cbornode.DecodeInto(node.RawData(), m)
		if err != nil {
			return nil, fmt.Errorf("%s: cbor decode error: %w", ErrFailedToDecodeSnapshot.Error(), err)
		}

		if m.Ver > 0 {
			// looks like we've got the migrated data, we need to return explicit error here, because it is forbidden to work with already migrated log
			return nil, fmt.Errorf("%s: cbor node version is higher than 0 (%d)", ErrFailedToDecodeSnapshot.Error(), m.Ver)
		}

		err = m.Verify()
		if err != nil {
			return nil, err
		}

		var snapshot = storage.SmartBlockSnapshot{}
		err = m.Unmarshal(&snapshot)
		if err != nil {
			return nil, fmt.Errorf("%s: pb decode error: %w", ErrFailedToDecodeSnapshot.Error(), err)
		}

		if !before.IsNil() && vclock.NewFromMap(snapshot.State).Compare(before, vclock.Ancestor) {
			rid = rec.PrevID()
			if !rid.Defined() {
				break
			}
			continue
		}

		snapshots = append(snapshots, SnapshotWithMetadata{
			SmartBlockSnapshot: snapshot,
			Creator:            m.AccAddr,
			RecordID:           rec.Cid(),
			EventID:            event.Cid(),
		})
		if len(snapshots) == limit {
			break
		}

		if !rec.PrevID().Defined() {
			break
		}

		rid = rec.PrevID()

		if !rid.Defined() {
			break
		}
	}

	return snapshots, nil
}

func (a *Anytype) snapshotTraverseLogs(ctx context.Context, thrdId thread.ID, before vclock.VClock, limit int) ([]SnapshotWithMetadata, error) {
	var allSnapshots []SnapshotWithMetadata
	thrd, err := a.threadService.Threads().GetThread(context.Background(), thrdId)
	if err != nil {
		return nil, err
	}

	for _, thrdLog := range thrd.Logs {
		snapshots, err := a.snapshotTraverseFromCid(ctx, thrd, thrdLog, before, limit)

		if err != nil {
			if strings.HasPrefix(err.Error(), ErrFailedToDecodeSnapshot.Error()) {
				return nil, ErrFailedToDecodeSnapshot
			}
			continue
		}

		allSnapshots = append(allSnapshots, snapshots...)
	}

	sort.Slice(allSnapshots, func(i, j int) bool {
		// sort from the newest to the oldest snapshot
		stateI := vclock.NewFromMap(allSnapshots[i].State)
		stateJ := vclock.NewFromMap(allSnapshots[j].State)
		anc := stateI.Compare(stateJ, vclock.Ancestor)
		if anc {
			return true
		}

		if stateI.Compare(stateJ, vclock.Descendant) {
			return false
		}

		// in case of concurrent changes choose the hash with greater hash first
		return stateI.Hash() > stateJ.Hash()
	})

	if len(allSnapshots) < limit {
		limit = len(allSnapshots)
	}

	return allSnapshots[0:limit], nil
}
