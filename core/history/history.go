package history

import (
	"context"
	"fmt"
	"time"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/change"
	"github.com/anyproto/anytype-heart/core/block"
	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/core/relation"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/core/smartblock"
	"github.com/anyproto/anytype-heart/pkg/lib/database"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/util/pbtypes"
	"github.com/anyproto/anytype-heart/util/slice"
)

const CName = "history"

const versionGroupInterval = time.Minute * 5

func New() History {
	return new(history)
}

type History interface {
	Show(pageId, versionId string) (bs *model.ObjectView, ver *pb.RpcHistoryVersion, err error)
	Versions(pageId, lastVersionId string, limit int) (resp []*pb.RpcHistoryVersion, err error)
	SetVersion(pageId, versionId string) (err error)
	app.Component
}

type BlockService interface {
	ResetToState(pageId string, s *state.State) (err error)
}

type history struct {
	a               core.Service
	blockService    BlockService
	objectStore     objectstore.ObjectStore
	relationService relation.Service
}

func (h *history) Init(a *app.App) (err error) {
	h.a = a.MustComponent(core.CName).(core.Service)
	h.blockService = a.MustComponent(block.CName).(BlockService)
	h.objectStore = a.MustComponent(objectstore.CName).(objectstore.ObjectStore)
	h.relationService = a.MustComponent(relation.CName).(relation.Service)
	return
}

func (h *history) Name() (name string) {
	return CName
}

func (h *history) Show(pageId, versionId string) (bs *model.ObjectView, ver *pb.RpcHistoryVersion, err error) {
	s, ver, err := h.buildState(pageId, versionId)
	if err != nil {
		return
	}
	metaD, _ := h.objectStore.QueryById(s.DepSmartIds(true, true, false, true, false))
	details := make([]*model.ObjectViewDetailsSet, 0, len(metaD))
	var uniqueObjTypes []string
	sbType, err := smartblock.SmartBlockTypeFromID(pageId)
	if err != nil {
		return nil, nil, fmt.Errorf("incorrect sb type: %w", err)
	}
	metaD = append(metaD, database.Record{Details: s.CombinedDetails()})
	uniqueObjTypes = s.ObjectTypes()
	for _, m := range metaD {
		details = append(details, &model.ObjectViewDetailsSet{
			Id:      pbtypes.GetString(m.Details, bundle.RelationKeyId.String()),
			Details: m.Details,
		})

		if ot := pbtypes.GetString(m.Details, bundle.RelationKeyType.String()); ot != "" {
			if slice.FindPos(uniqueObjTypes, ot) == -1 {
				uniqueObjTypes = append(uniqueObjTypes, ot)
			}
		}
	}

	rels, _ := h.relationService.FetchLinks(s.PickRelationLinks())
	return &model.ObjectView{
		RootId:        pageId,
		Type:          model.SmartBlockType(sbType),
		Blocks:        s.Blocks(),
		Details:       details,
		RelationLinks: rels.RelationLinks(),
	}, ver, nil
}

func (h *history) Versions(pageId, lastVersionId string, limit int) (resp []*pb.RpcHistoryVersion, err error) {
	if limit <= 0 {
		limit = 100
	}
	profileId, profileName, err := h.getProfileInfo()
	if err != nil {
		return
	}
	var includeLastId = true

	reverse := func(vers []*pb.RpcHistoryVersion) []*pb.RpcHistoryVersion {
		for i, j := 0, len(vers)-1; i < j; i, j = i+1, j-1 {
			vers[i], vers[j] = vers[j], vers[i]
		}
		return vers
	}

	for len(resp) < limit {
		tree, _, e := h.buildTree(pageId, lastVersionId, includeLastId)
		if e != nil {
			return nil, e
		}
		var data []*pb.RpcHistoryVersion

		tree.Iterate(tree.RootId(), func(c *change.Change) (isContinue bool) {
			data = append(data, &pb.RpcHistoryVersion{
				Id:          c.Id,
				PreviousIds: c.PreviousIds,
				AuthorId:    profileId,
				AuthorName:  profileName,
				Time:        c.Timestamp,
			})
			return true
		})
		if len(data[0].PreviousIds) == 0 {
			if h.isEmpty(tree.Get(data[0].Id)) {
				data = data[1:]
			}
			resp = append(data, resp...)
			break
		} else {
			resp = append(data, resp...)
			lastVersionId = tree.RootId()
			includeLastId = false
		}

		if len(data) == 0 {
			break
		}

	}

	resp = reverse(resp)

	var groupId int64
	var nextVersionTimestamp int64

	for i := 0; i < len(resp); i++ {
		if nextVersionTimestamp-resp[i].Time > int64(versionGroupInterval.Seconds()) {
			groupId++
		}
		nextVersionTimestamp = resp[i].Time
		resp[i].GroupId = groupId
	}

	return
}

func (h *history) isEmpty(c *change.Change) bool {
	if c.Snapshot != nil && c.Snapshot.Data != nil {
		if c.Snapshot.Data.Details != nil && c.Snapshot.Data.Details.Fields != nil && len(c.Snapshot.Data.Details.Fields) > 0 {
			return false
		}
		for _, b := range c.Snapshot.Data.Blocks {
			if b.GetSmartblock() != nil && b.GetLayout() != nil {
				return false
			}
		}
		return true
	}
	return false
}

func (h *history) SetVersion(pageId, versionId string) (err error) {
	s, _, err := h.buildState(pageId, versionId)
	if err != nil {
		return
	}
	return h.blockService.ResetToState(pageId, s)
}

func (h *history) buildTree(pageId, versionId string, includeLastId bool) (tree *change.Tree, blockType smartblock.SmartBlockType, err error) {
	sb, err := h.a.GetBlock(pageId)
	if err != nil {
		err = fmt.Errorf("history: anytype.GetBlock error: %v", err)
		return
	}
	if versionId != "" {
		if tree, err = change.BuildTreeBefore(context.TODO(), sb, versionId, includeLastId); err != nil {
			return
		}
	} else {
		if tree, _, err = change.BuildTree(context.TODO(), sb); err != nil {
			return
		}
	}
	return tree, sb.Type(), nil
}

func (h *history) buildState(pageId, versionId string) (s *state.State, ver *pb.RpcHistoryVersion, err error) {
	tree, _, err := h.buildTree(pageId, versionId, true)
	if err != nil {
		return
	}
	root := tree.Root()
	if root == nil || root.GetSnapshot() == nil {
		return nil, nil, fmt.Errorf("root missing or not a snapshot")
	}
	s = state.NewDocFromSnapshot(pageId, root.GetSnapshot()).(*state.State)
	s.SetChangeId(root.Id)
	st, _, err := change.BuildStateSimpleCRDT(s, tree)
	if err != nil {
		return
	}
	if _, _, err = state.ApplyStateFast(st); err != nil {
		return
	}
	s.BlocksInit(s)
	if ch := tree.Get(versionId); ch != nil {
		profileId, profileName, e := h.getProfileInfo()
		if e != nil {
			err = e
			return
		}
		ver = &pb.RpcHistoryVersion{
			Id:          ch.Id,
			PreviousIds: ch.PreviousIds,
			AuthorId:    profileId,
			AuthorName:  profileName,
			Time:        ch.Timestamp,
		}
	}
	return
}

func (h *history) getProfileInfo() (profileId, profileName string, err error) {
	profileId = h.a.ProfileID()
	lp, err := h.a.LocalProfile()
	if err != nil {
		return
	}
	profileName = lp.Name
	return
}
