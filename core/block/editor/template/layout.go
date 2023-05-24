package template

import (
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
)

func ByLayout(layout model.ObjectTypeLayout, templates ...StateTransformer) []StateTransformer {
	// TODO: not complete, need to describe all layouts
	templates = append(templates,
		WithLayout(layout),
		WithDefaultFeaturedRelations,
		WithFeaturedRelations,
		WithRequiredRelations(),
		WithLinkFieldsMigration,
		WithCreatorRemovedFromFeaturedRelations,
	)

	switch layout {
	case model.ObjectType_note:
		templates = append(templates,
			WithNoTitle,
			WithNoDescription,
		)
	case model.ObjectType_todo:
		templates = append(templates,
			WithTitle,
			WithDescription,
			WithRelations([]bundle.RelationKey{bundle.RelationKeyDone}),
		)
	case model.ObjectType_bookmark:
		templates = append(templates,
			WithTitle,
			WithDescription,
			WithBookmarkBlocks,
		)
	default:
		templates = append(templates,
			WithTitle,
			WithDescription,
		)
	}
	return templates
}
