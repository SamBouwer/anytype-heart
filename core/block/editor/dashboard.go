package editor

import (
	"github.com/gogo/protobuf/types"

	"github.com/anyproto/anytype-heart/core/block/editor/basic"
	"github.com/anyproto/anytype-heart/core/block/editor/collection"
	"github.com/anyproto/anytype-heart/core/block/editor/smartblock"
	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/core/block/editor/template"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/database"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/pkg/lib/threads"
	"github.com/anyproto/anytype-heart/util/pbtypes"
	"github.com/anyproto/anytype-heart/util/slice"
)

type Dashboard struct {
	smartblock.SmartBlock
	basic.AllOperations
	collection.Collection

	DetailsModifier DetailsModifier
	objectStore     objectstore.ObjectStore
	anytype         core.Service
}

func NewDashboard(
	detailsModifier DetailsModifier,
	objectStore objectstore.ObjectStore,
	anytype core.Service,
) *Dashboard {
	sb := smartblock.New()
	return &Dashboard{
		SmartBlock:      sb,
		AllOperations:   basic.NewBasic(sb),
		Collection:      collection.NewCollection(sb),
		DetailsModifier: detailsModifier,
		objectStore:     objectStore,
		anytype:         anytype,
	}
}

func (p *Dashboard) Init(ctx *smartblock.InitContext) (err error) {
	if err = p.SmartBlock.Init(ctx); err != nil {
		return
	}
	p.DisableLayouts()
	return p.init(ctx.State)
}

func (p *Dashboard) init(s *state.State) (err error) {
	state.CleanupLayouts(s)
	p.AddHook(p.updateObjects, smartblock.HookAfterApply)
	if err = smartblock.ObjectApplyTemplate(p, s,
		template.WithObjectTypesAndLayout([]string{bundle.TypeKeyDashboard.URL()}, model.ObjectType_dashboard),
		template.WithEmpty,
		template.WithDetailName("Home"),
		template.WithDetailIconEmoji("🏠"),
		template.WithNoRootLink(p.anytype.PredefinedBlocks().Archive),
		template.WithRequiredRelations(),
		template.WithNoDuplicateLinks(),
	); err != nil {
		return
	}

	log.Infof("create default structure for dashboard: %v", s.RootId())
	return
}

func (p *Dashboard) updateObjects(_ smartblock.ApplyInfo) (err error) {
	favoritedIds, err := p.GetIds()
	if err != nil {
		return
	}

	p.anytype.ThreadsService().ThreadQueue().UpdatePriority(favoritedIds, threads.HighPriority)

	records, _, err := p.objectStore.Query(nil, database.Query{
		Filters: []*model.BlockContentDataviewFilter{
			{
				RelationKey: bundle.RelationKeyIsFavorite.String(),
				Condition:   model.BlockContentDataviewFilter_Equal,
				Value:       pbtypes.Bool(true),
			},
		},
	})
	if err != nil {
		return
	}
	var storeFavoritedIds = make([]string, 0, len(records))
	for _, rec := range records {
		storeFavoritedIds = append(storeFavoritedIds, pbtypes.GetString(rec.Details, bundle.RelationKeyId.String()))
	}

	removedIds, addedIds := slice.DifferenceRemovedAdded(storeFavoritedIds, favoritedIds)
	for _, removedId := range removedIds {
		go func(id string) {
			if err := p.DetailsModifier.ModifyLocalDetails(id, func(current *types.Struct) (*types.Struct, error) {
				if current == nil || current.Fields == nil {
					current = &types.Struct{
						Fields: map[string]*types.Value{},
					}
				}
				current.Fields[bundle.RelationKeyIsFavorite.String()] = pbtypes.Bool(false)
				return current, nil
			}); err != nil {
				log.Errorf("favorite: can't set detail to object: %v", err)
			}
		}(removedId)
	}
	for _, addedId := range addedIds {
		go func(id string) {
			if err := p.DetailsModifier.ModifyLocalDetails(id, func(current *types.Struct) (*types.Struct, error) {
				if current == nil || current.Fields == nil {
					current = &types.Struct{
						Fields: map[string]*types.Value{},
					}
				}
				current.Fields[bundle.RelationKeyIsFavorite.String()] = pbtypes.Bool(true)
				return current, nil
			}); err != nil {
				log.Errorf("favorite: can't set detail to object: %v", err)
			}
		}(addedId)
	}
	return
}
