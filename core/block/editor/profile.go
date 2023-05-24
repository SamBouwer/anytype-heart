package editor

import (
	"github.com/anyproto/anytype-heart/core/block/editor/basic"
	"github.com/anyproto/anytype-heart/core/block/editor/bookmark"
	"github.com/anyproto/anytype-heart/core/block/editor/clipboard"
	"github.com/anyproto/anytype-heart/core/block/editor/file"
	"github.com/anyproto/anytype-heart/core/block/editor/smartblock"
	"github.com/anyproto/anytype-heart/core/block/editor/stext"
	"github.com/anyproto/anytype-heart/core/block/editor/table"
	"github.com/anyproto/anytype-heart/core/block/editor/template"
	"github.com/anyproto/anytype-heart/core/session"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/util/pbtypes"
)

type Profile struct {
	smartblock.SmartBlock
	basic.AllOperations
	basic.IHistory
	file.File
	stext.Text
	clipboard.Clipboard
	bookmark.Bookmark
	table.TableEditor

	sendEvent func(e *pb.Event)
}

func NewProfile(
	objectStore objectstore.ObjectStore,
	anytype core.Service,
	fileBlockService file.BlockService,
	bookmarkBlockService bookmark.BlockService,
	bookmarkService bookmark.BookmarkService,
	sendEvent func(e *pb.Event),
) *Profile {
	sb := smartblock.New()
	f := file.NewFile(
		sb,
		fileBlockService,
		anytype,
	)
	return &Profile{
		SmartBlock:    sb,
		sendEvent:     sendEvent,
		AllOperations: basic.NewBasic(sb),
		IHistory:      basic.NewHistory(sb),
		Text: stext.NewText(
			sb,
			objectStore,
		),
		File: f,
		Clipboard: clipboard.NewClipboard(
			sb,
			f,
			anytype,
		),
		Bookmark: bookmark.NewBookmark(
			sb,
			bookmarkBlockService,
			bookmarkService,
			objectStore,
		),
		TableEditor: table.NewEditor(sb),
	}
}

func (p *Profile) Init(ctx *smartblock.InitContext) (err error) {
	if err = p.SmartBlock.Init(ctx); err != nil {
		return
	}
	return smartblock.ObjectApplyTemplate(p, ctx.State,
		template.WithObjectTypesAndLayout([]string{bundle.TypeKeyProfile.URL()}, model.ObjectType_profile),
		template.WithDetail(bundle.RelationKeyLayoutAlign, pbtypes.Float64(float64(model.Block_AlignCenter))),
		template.WithTitle,
		// template.WithAlignedDescription(model.Block_AlignCenter, true),
		template.WithFeaturedRelations,
		template.WithRequiredRelations(),
	)
}

func (p *Profile) SetDetails(ctx *session.Context, details []*pb.RpcObjectSetDetailsDetail, showEvent bool) (err error) {
	if err = p.SmartBlock.SetDetails(ctx, details, showEvent); err != nil {
		return
	}
	p.sendEvent(&pb.Event{
		Messages: []*pb.EventMessage{
			{
				Value: &pb.EventMessageValueOfAccountDetails{
					AccountDetails: &pb.EventAccountDetails{
						ProfileId: p.Id(),
						Details:   p.Details(),
					},
				},
			},
		},
	})
	return
}
