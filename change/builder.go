package change

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
	"github.com/anyproto/anytype-heart/util/slice"
)

var (
	ErrEmpty = errors.New("logs empty")
)

var log = logging.Logger("anytype-mw-change-builder")

const (
	virtualChangeBasePrefix    = "_virtual:"
	virtualChangeBaseSeparator = "+"
)

func BuildTreeBefore(ctx context.Context, s core.SmartBlock, beforeLogId string, includeBeforeId bool) (t *Tree, err error) {
	sb := &stateBuilder{beforeId: beforeLogId, includeBeforeId: includeBeforeId}
	err = sb.Build(ctx, s)
	return sb.tree, err
}

func BuildTree(ctx context.Context, s core.SmartBlock) (t *Tree, logHeads map[string]*Change, err error) {
	sb := new(stateBuilder)
	err = sb.Build(ctx, s)
	return sb.tree, sb.logHeads, err
}

func BuildMetaTree(ctx context.Context, s core.SmartBlock) (t *Tree, logHeads map[string]*Change, err error) {
	sb := &stateBuilder{onlyMeta: true}
	err = sb.Build(ctx, s)
	return sb.tree, sb.logHeads, err
}

type stateBuilder struct {
	smartblockId    string
	cache           map[string]*Change
	logHeads        map[string]*Change
	tree            *Tree
	smartblock      core.SmartBlock
	qt              time.Duration
	qr              int64
	onlyMeta        bool
	beforeId        string
	includeBeforeId bool
}

func (sb *stateBuilder) Build(ctx context.Context, s core.SmartBlock) (err error) {
	sb.smartblockId = s.ID()
	st := time.Now()
	sb.smartblock = s
	logs, err := sb.getLogs(ctx)
	if err != nil {
		return err
	}
	heads, err := sb.getActualHeads(ctx, logs)
	if err != nil {
		return fmt.Errorf("getActualHeads error: %v", err)
	}

	breakpoint, err := sb.findBreakpoint(ctx, heads)
	if err != nil {
		return fmt.Errorf("findBreakpoint error: %v", err)
	}
	if err = sb.buildTree(ctx, heads, breakpoint); err != nil {
		return fmt.Errorf("buildTree error: %v", err)
	}
	log.Infof("tree build: len: %d; scanned: %d; dur: %v (lib %v)", sb.tree.Len(), len(sb.cache), time.Since(st), sb.qt)
	sb.cache = nil
	return
}

func (sb *stateBuilder) getLogs(ctx context.Context) (logs []core.SmartblockLog, err error) {
	sb.cache = make(map[string]*Change)
	if sb.beforeId != "" {
		before, e := sb.loadChange(ctx, sb.beforeId)
		if e != nil {
			return nil, e
		}
		if sb.includeBeforeId {
			return []core.SmartblockLog{
				{Head: sb.beforeId},
			}, nil
		}
		for _, pid := range before.PreviousIds {
			logs = append(logs, core.SmartblockLog{Head: pid})
		}
		return
	}
	logs, err = sb.smartblock.GetLogs()
	if err != nil {
		return nil, fmt.Errorf("GetLogs error: %w", err)
	}
	log.Debugf("build tree: logs: %v", logs)
	sb.logHeads = make(map[string]*Change)
	if len(logs) == 0 || len(logs) == 1 && len(logs[0].Head) <= 1 {
		return nil, ErrEmpty
	}
	var nonEmptyLogs = logs[:0]
	for _, l := range logs {
		if len(l.Head) == 0 {
			continue
		}
		if ch, err := sb.loadChange(ctx, l.Head); err != nil {
			log.Errorf("loading head %s of the log %s failed: %v", l.Head, l.ID, err)
		} else {
			sb.logHeads[l.ID] = ch
		}
		nonEmptyLogs = append(nonEmptyLogs, l)
	}
	return nonEmptyLogs, nil
}

func (sb *stateBuilder) buildTree(ctx context.Context, heads []string, breakpoint string) (err error) {
	ch, err := sb.loadChange(ctx, breakpoint)
	if err != nil {
		return
	}
	if sb.onlyMeta {
		sb.tree = NewMetaTree(ctx)
	} else {
		sb.tree = NewTree(ctx)
	}
	defer func() {
		if err != nil {
			return
		}
		// check if ctx is canceled
		// in this case the tree is not complete and should not be used
		select {
		case <-ctx.Done():
			err = ctx.Err()
		default:
		}
	}()
	sb.tree.AddFast(ch)
	var changes = make([]*Change, 0, len(heads)*2)
	var uniqMap = map[string]struct{}{breakpoint: {}}
	for _, id := range heads {
		changes, err = sb.loadChangesFor(ctx, id, uniqMap, changes)
		if err != nil {
			return
		}
	}
	if sb.onlyMeta {
		var filteredChanges = changes[:0]
		for _, ch := range changes {
			if ch.HasMeta() {
				filteredChanges = append(filteredChanges, ch)
			}
		}
		changes = filteredChanges
	}
	sb.tree.AddFast(changes...)
	return
}

func (sb *stateBuilder) loadChangesFor(ctx context.Context, id string, uniqMap map[string]struct{}, buf []*Change) ([]*Change, error) {
	if _, exists := uniqMap[id]; exists {
		return buf, nil
	}
	ch, err := sb.loadChange(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, prev := range ch.GetPreviousIds() {
		if buf, err = sb.loadChangesFor(ctx, prev, uniqMap, buf); err != nil {
			return nil, err
		}
	}
	uniqMap[id] = struct{}{}
	return append(buf, ch), nil
}

func (sb *stateBuilder) findBreakpoint(ctx context.Context, heads []string) (breakpoint string, err error) {
	var (
		ch          *Change
		snapshotIds []string
	)
	for _, head := range heads {
		if ch, err = sb.loadChange(ctx, head); err != nil {
			return
		}
		shId := ch.GetLastSnapshotId()
		if slice.FindPos(snapshotIds, shId) == -1 {
			snapshotIds = append(snapshotIds, shId)
		}
	}
	return sb.findCommonSnapshot(ctx, snapshotIds)
}

func (sb *stateBuilder) findCommonSnapshot(ctx context.Context, snapshotIds []string) (snapshotId string, err error) {
	// sb.smartblock can be nil in this func
	if len(snapshotIds) == 1 {
		return snapshotIds[0], nil
	} else if len(snapshotIds) == 0 {
		return "", fmt.Errorf("snapshots not found")
	}
	findCommon := func(s1, s2 string) (s string, err error) {
		// fast cases
		if s1 == s2 {
			return s1, nil
		}
		ch1, err := sb.loadChange(ctx, s1)
		if err != nil {
			return "", err
		}
		if ch1.LastSnapshotId == s2 {
			return s2, nil
		}
		ch2, err := sb.loadChange(ctx, s2)
		if err != nil {
			return "", err
		}
		if ch2.LastSnapshotId == s1 {
			return s1, nil
		}
		if ch1.LastSnapshotId == ch2.LastSnapshotId && ch1.LastSnapshotId != "" {
			return ch1.LastSnapshotId, nil
		}
		// traverse
		var t1 = make([]string, 0, 5)
		var t2 = make([]string, 0, 5)
		t1 = append(t1, ch1.Id, ch1.LastSnapshotId)
		t2 = append(t2, ch2.Id, ch2.LastSnapshotId)
		for {
			lid1 := t1[len(t1)-1]
			if lid1 != "" {
				l1, e := sb.loadChange(ctx, lid1)
				if e != nil {
					return "", e
				}
				if l1.LastSnapshotId != "" {
					if slice.FindPos(t2, l1.LastSnapshotId) != -1 {
						return l1.LastSnapshotId, nil
					}
				}
				t1 = append(t1, l1.LastSnapshotId)
			}
			lid2 := t2[len(t2)-1]
			if lid2 != "" {
				l2, e := sb.loadChange(ctx, t2[len(t2)-1])
				if e != nil {
					return "", e
				}
				if l2.LastSnapshotId != "" {
					if slice.FindPos(t1, l2.LastSnapshotId) != -1 {
						return l2.LastSnapshotId, nil
					}
				}
				t2 = append(t2, l2.LastSnapshotId)
			}
			if lid1 == "" && lid2 == "" {
				break
			}
		}

		log.Warnf("changes build tree: possible versions split")

		// prefer not first snapshot
		if len(ch1.PreviousIds) == 0 && len(ch2.PreviousIds) > 0 {
			log.Warnf("changes build tree: prefer %s(%d prevIds) over %s(%d prevIds)", s2, len(ch2.PreviousIds), s1, len(ch1.PreviousIds))
			return s2, nil
		} else if len(ch1.PreviousIds) > 0 && len(ch2.PreviousIds) == 0 {
			log.Warnf("changes build tree: prefer %s(%d prevIds) over %s(%d prevIds)", s1, len(ch1.PreviousIds), s2, len(ch2.PreviousIds))
			return s1, nil
		}

		isEmptySnapshot := func(ch *Change) bool {
			// todo: ignore root & header blocks
			if ch.Snapshot == nil || ch.Snapshot.Data == nil || len(ch.Snapshot.Data.Blocks) <= 1 {
				return true
			}

			return false
		}

		// prefer not empty snapshot
		if isEmptySnapshot(ch1) && !isEmptySnapshot(ch2) {
			log.Warnf("changes build tree: prefer %s(not empty) over %s(empty)", s2, s1)
			return s2, nil
		} else if isEmptySnapshot(ch2) && !isEmptySnapshot(ch1) {
			log.Warnf("changes build tree: prefer %s(not empty) over %s(empty)", s1, s2)
			return s1, nil
		}

		var p1, p2 string
		// unexpected behavior - lets merge branches using the virtual change mechanism
		if s1 < s2 {
			p1, p2 = s1, s2
		} else {
			p1, p2 = s2, s1
		}

		log.With("thread", sb.smartblockId).Errorf("changes build tree: made base snapshot for logs %s and %s: conflicting snapshots %s+%s", ch1.Device, ch2.Device, p1, p2)
		baseId := sb.makeVirtualSnapshotId(p1, p2)

		if len(ch2.PreviousIds) != 0 || len(ch2.PreviousIds) != 0 {
			if len(ch2.PreviousIds) == 1 && len(ch2.PreviousIds) == 1 && ch1.PreviousIds[0] == baseId && ch2.PreviousIds[0] == baseId {
				// already patched
				return baseId, nil
			} else {
				return "", fmt.Errorf("failed to create virtual base change: has invalid PreviousIds")
			}
		}

		ch1.PreviousIds = []string{baseId}
		ch2.PreviousIds = []string{baseId}
		return baseId, nil
	}

	for len(snapshotIds) > 1 {
		l := len(snapshotIds)
		shId, e := findCommon(snapshotIds[l-2], snapshotIds[l-1])
		if e != nil {
			return "", e
		}
		snapshotIds[l-2] = shId
		snapshotIds = snapshotIds[:l-1]
	}
	return snapshotIds[0], nil
}

func (sb *stateBuilder) getActualHeads(ctx context.Context, logs []core.SmartblockLog) (heads []string, err error) {
	sort.Slice(logs, func(i, j int) bool {
		return logs[i].ID < logs[j].ID
	})
	var knownHeads []string
	var validLogs = logs[:0]
	for _, l := range logs {
		if slice.FindPos(knownHeads, l.Head) != -1 { // do not scan known heads
			continue
		}
		sh, err := sb.getNearSnapshot(ctx, l.Head)
		if err != nil {
			log.Warnf("can't get near snapshot: %v; ignore", err)
			continue
		}
		if sh.Snapshot.LogHeads != nil {
			for _, headId := range sh.Snapshot.LogHeads {
				knownHeads = append(knownHeads, headId)
			}
		}
		validLogs = append(validLogs, l)
	}
	for _, l := range validLogs {
		if slice.FindPos(knownHeads, l.Head) != -1 { // do not scan known heads
			continue
		} else {
			heads = append(heads, l.Head)
		}
	}
	if len(heads) == 0 {
		return nil, fmt.Errorf("no usable logs in head")
	}
	return
}

func (sb *stateBuilder) getNearSnapshot(ctx context.Context, id string) (sh *Change, err error) {
	ch, err := sb.loadChange(ctx, id)
	if err != nil {
		return
	}
	if ch.Snapshot != nil {
		return ch, nil
	}
	sch, err := sb.loadChange(ctx, ch.LastSnapshotId)
	if err != nil {
		return
	}
	if sch.Snapshot == nil {
		return nil, fmt.Errorf("snapshot %s is empty", ch.LastSnapshotId)
	}
	return sch, nil
}

func (sb *stateBuilder) makeVirtualSnapshotId(s1, s2 string) string {
	return virtualChangeBasePrefix + base64.RawStdEncoding.EncodeToString([]byte(s1+virtualChangeBaseSeparator+s2))
}

func (sb *stateBuilder) makeChangeFromVirtualId(ctx context.Context, id string) (*Change, error) {
	dataB, err := base64.RawStdEncoding.DecodeString(id[len(virtualChangeBasePrefix):])
	if err != nil {
		return nil, fmt.Errorf("invalid virtual id format: %s", err.Error())
	}

	ids := strings.Split(string(dataB), virtualChangeBaseSeparator)
	if len(ids) != 2 {
		return nil, fmt.Errorf("invalid virtual id format: %v", id)
	}

	ch1, err := sb.loadChange(context.Background(), ids[0])
	if err != nil {
		return nil, err
	}
	ch2, err := sb.loadChange(ctx, ids[1])
	if err != nil {
		return nil, err
	}
	return &Change{
		Id:      id,
		Account: ch1.Account,
		Device:  ch1.Device,
		Next:    []*Change{ch1, ch2},
		Change:  &pb.Change{Snapshot: ch1.Snapshot},
	}, nil

}

func (sb *stateBuilder) loadChange(ctx context.Context, id string) (ch *Change, err error) {
	if ch, ok := sb.cache[id]; ok {
		return ch, nil
	}
	if strings.HasPrefix(id, virtualChangeBasePrefix) {
		ch, err = sb.makeChangeFromVirtualId(ctx, id)
		if err != nil {
			return nil, err
		}
		sb.cache[id] = ch
		return
	}
	if sb.smartblock == nil {
		return nil, fmt.Errorf("no smarblock in builder")
	}
	st := time.Now()

	sr, err := sb.smartblock.GetRecord(ctx, id)
	s := time.Since(st)
	if err != nil {
		log.With("thread", sb.smartblock.ID()).
			Errorf("failed to loadChange %s after %.2fs. Total %.2f(%d records were loaded)", id, s.Seconds(), sb.qt.Seconds(), sb.qr)
		return
	}
	sb.qt += s
	sb.qr++
	if s.Seconds() > 0.1 {
		// this means we got this record through bitswap, so lets log some details
		lgs, _ := sb.smartblock.GetLogs()
		var sbLog *core.SmartblockLog
		for _, lg := range lgs {
			if lg.ID == sr.LogID {
				sbLog = &lg
				break
			}
		}
		var (
			logHead    string
			logCounter int64
		)

		if sbLog != nil {
			logHead = sbLog.Head
			logCounter = sbLog.HeadCounter
		}

		log.With("thread", sb.smartblock.ID()).
			With("logid", sr.LogID).
			With("logHead", logHead).
			With("logCounter", logCounter).
			Errorf("long loadChange %.2fs for %s. Total %.2f(%d records)", s.Seconds(), id, sb.qt.Seconds(), sb.qr)
	}
	chp := new(pb.Change)
	if err3 := sr.Unmarshal(chp); err3 != nil {
		// skip this error for the future compatibility
		log.With("thread", sb.smartblock.ID()).
			With("logid", sr.LogID).
			With("change", id).Errorf("failed to unmarshal change: %s; continue", err3.Error())
		if chp == nil || chp.PreviousIds == nil {
			// no way we can continue when we don't have some minimal information
			return nil, err3
		}
	}
	ch = &Change{
		Id:      id,
		Account: sr.AccountID,
		Device:  sr.LogID,
		Change:  chp,
	}

	if sb.onlyMeta {
		ch.PreviousIds = ch.PreviousMetaIds
	}
	sb.cache[id] = ch
	return
}
