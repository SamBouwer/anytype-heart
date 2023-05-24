package editor

import (
	"github.com/anyproto/anytype-heart/core/block/editor/basic"
	"github.com/anyproto/anytype-heart/core/block/editor/smartblock"
	"github.com/anyproto/anytype-heart/core/block/editor/template"
	"github.com/anyproto/anytype-heart/core/block/editor/widget"
	"github.com/anyproto/anytype-heart/core/session"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
)

type WidgetObject struct {
	smartblock.SmartBlock
	basic.IHistory
	basic.Movable
	basic.Unlinkable
	basic.Updatable
	widget.Widget
}

func NewWidgetObject() *WidgetObject {
	sb := smartblock.New()
	bs := basic.NewBasic(sb)
	return &WidgetObject{
		SmartBlock: sb,
		Movable:    bs,
		Updatable:  bs,
		IHistory:   basic.NewHistory(sb),
		Widget:     widget.NewWidget(sb),
	}
}

func (w *WidgetObject) Init(ctx *smartblock.InitContext) (err error) {
	if err = w.SmartBlock.Init(ctx); err != nil {
		return
	}
	return smartblock.ObjectApplyTemplate(w, ctx.State,
		template.WithEmpty,
		template.WithObjectTypesAndLayout([]string{bundle.TypeKeyDashboard.URL()}, model.ObjectType_basic),
	)
}

func (w *WidgetObject) Unlink(ctx *session.Context, ids ...string) (err error) {
	st := w.NewStateCtx(ctx)
	for _, id := range ids {
		if p := st.PickParentOf(id); p != nil && p.Model().GetWidget() != nil {
			st.Unlink(p.Model().Id)
		}
		st.Unlink(id)
	}
	return w.Apply(st)
}
