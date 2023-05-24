package restriction

import (
	"fmt"

	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
)

var (
	objRestrictAll = ObjectRestrictions{
		model.Restrictions_Blocks,
		model.Restrictions_Relations,
		model.Restrictions_Details,
		model.Restrictions_Delete,
		model.Restrictions_LayoutChange,
		model.Restrictions_TypeChange,
		model.Restrictions_Template,
		model.Restrictions_Duplicate,
	}
	objRestrictEdit = ObjectRestrictions{
		model.Restrictions_Blocks,
		model.Restrictions_LayoutChange,
		model.Restrictions_TypeChange,
		model.Restrictions_Template,
	}

	objectRestrictionsByPbType = map[model.SmartBlockType]ObjectRestrictions{
		model.SmartBlockType_Breadcrumbs:    objRestrictAll,
		model.SmartBlockType_ProfilePage:    {model.Restrictions_LayoutChange, model.Restrictions_TypeChange, model.Restrictions_Delete},
		model.SmartBlockType_AnytypeProfile: objRestrictAll,
		model.SmartBlockType_Page:           {},
		model.SmartBlockType_Home: {
			model.Restrictions_Details,
			model.Restrictions_Relations,
			model.Restrictions_Delete,
			model.Restrictions_LayoutChange,
			model.Restrictions_TypeChange,
			model.Restrictions_Template,
			model.Restrictions_Duplicate,
		},
		model.SmartBlockType_Workspace:           objRestrictAll,
		model.SmartBlockType_File:                objRestrictAll,
		model.SmartBlockType_MarketplaceRelation: objRestrictAll,
		model.SmartBlockType_MarketplaceTemplate: objRestrictAll,
		model.SmartBlockType_MarketplaceType:     objRestrictAll,
		model.SmartBlockType_Archive:             objRestrictAll,
		model.SmartBlockType_Set:                 objRestrictEdit,
		model.SmartBlockType_BundledRelation:     objRestrictAll,
		model.SmartBlockType_SubObject: {
			model.Restrictions_Blocks,
			model.Restrictions_LayoutChange,
			model.Restrictions_TypeChange,
			model.Restrictions_Template,
		},
		model.SmartBlockType_BundledObjectType: objRestrictAll,
		model.SmartBlockType_STObjectType:      objRestrictEdit,
		model.SmartBlockType_BundledTemplate:   objRestrictAll,
		model.SmartBlockType_Template:          {},
		// TODO: Check with Razor
		model.SmartBlockType_Widget: {
			model.Restrictions_Relations,
			model.Restrictions_Details,
			model.Restrictions_Delete,
			model.Restrictions_LayoutChange,
			model.Restrictions_TypeChange,
			model.Restrictions_Template,
			model.Restrictions_Duplicate,
		},
	}
)

type ObjectRestrictions []model.RestrictionsObjectRestriction

func (or ObjectRestrictions) Check(cr ...model.RestrictionsObjectRestriction) (err error) {
	for _, r := range cr {
		for _, er := range or {
			if er == r {
				return fmt.Errorf("%w: %s", ErrRestricted, r.String())
			}
		}
	}
	return
}

func (or ObjectRestrictions) Equal(or2 ObjectRestrictions) bool {
	if len(or) != len(or2) {
		return false
	}
	for _, r := range or {
		if or2.Check(r) == nil {
			return false
		}
	}
	return true
}

func (or ObjectRestrictions) Copy() ObjectRestrictions {
	obj := make(ObjectRestrictions, len(or))
	copy(obj, or)
	return obj
}

func (s *service) ObjectRestrictionsByObj(obj Object) (r ObjectRestrictions) {
	var ok bool
	if obj.Type() == model.SmartBlockType_ProfilePage && s.anytype.PredefinedBlocks().Profile != obj.Id() {
		if r, ok = objectRestrictionsByPbType[model.SmartBlockType_Page]; ok {
			return
		}
	}

	if r, ok = objectRestrictionsByPbType[obj.Type()]; ok {
		return
	}
	log.Warnf("restrctions not found for object: id='%s' type='%v'", obj.Id(), obj.Type())
	return objRestrictAll
}
