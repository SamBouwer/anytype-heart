package editor

import (
	"github.com/anyproto/anytype-heart/core/block/editor/smartblock"
	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/core/block/editor/template"
	"github.com/anyproto/anytype-heart/core/block/simple"
	"github.com/anyproto/anytype-heart/core/relation/relationutils"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
)

var log = logging.Logger("anytype-mw-editor")

func NewBreadcrumbs() *Breadcrumbs {
	return &Breadcrumbs{
		SmartBlock: smartblock.New(),
	}
}

type Breadcrumbs struct {
	smartblock.SmartBlock
}

func (p *Breadcrumbs) Init(ctx *smartblock.InitContext) (err error) {
	if err = p.SmartBlock.Init(ctx); err != nil {
		return
	}
	p.SmartBlock.DisableLayouts()
	return smartblock.ObjectApplyTemplate(p, ctx.State, template.WithEmpty, template.WithNoObjectTypes())
}

func (p *Breadcrumbs) Relations(_ *state.State) relationutils.Relations {
	return nil
}

func (b *Breadcrumbs) SetCrumbs(ids []string) (err error) {
	s := b.NewState()
	var existingLinks = make(map[string]string)
	s.Iterate(func(b simple.Block) (isContinue bool) {
		if link := b.Model().GetLink(); link != nil {
			existingLinks[link.TargetBlockId] = b.Model().Id
		}
		return true
	})
	root := s.Get(s.RootId()).Model()
	root.ChildrenIds = make([]string, 0, len(ids))
	for _, id := range ids {
		linkId, ok := existingLinks[id]
		if !ok {
			link := simple.New(&model.Block{
				Content: &model.BlockContentOfLink{
					Link: &model.BlockContentLink{
						TargetBlockId: id,
						Style:         model.BlockContentLink_Page,
					},
				},
			})
			s.Add(link)
			linkId = link.Model().Id
		}
		root.ChildrenIds = append(root.ChildrenIds, linkId)
	}
	return b.Apply(s)
}
