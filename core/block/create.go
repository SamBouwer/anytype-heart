package block

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/types"

	"github.com/anyproto/anytype-heart/core/block/editor/basic"
	"github.com/anyproto/anytype-heart/core/block/editor/smartblock"
	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/core/relation/relationutils"
	"github.com/anyproto/anytype-heart/core/session"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/bundle"
	coresb "github.com/anyproto/anytype-heart/pkg/lib/core/smartblock"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/util/pbtypes"
)

func (s *Service) TemplateCreateFromObject(id string) (templateID string, err error) {
	var st *state.State
	if err = s.Do(id, func(b smartblock.SmartBlock) error {
		if b.Type() != model.SmartBlockType_Page {
			return fmt.Errorf("can't make template from this obect type")
		}
		st, err = b.TemplateCreateFromObjectState()
		return err
	}); err != nil {
		return
	}

	templateID, _, err = s.objectCreator.CreateSmartBlockFromState(context.TODO(), coresb.SmartBlockTypeTemplate, nil, st)
	if err != nil {
		return
	}
	return
}

func (s *Service) TemplateClone(id string) (templateID string, err error) {
	var st *state.State
	if err = s.Do(id, func(b smartblock.SmartBlock) error {
		if b.Type() != model.SmartBlockType_BundledTemplate {
			return fmt.Errorf("can clone bundled templates only")
		}
		st = b.NewState().Copy()
		st.RemoveDetail(bundle.RelationKeyTemplateIsBundled.String())
		st.SetLocalDetails(nil)
		st.SetDetailAndBundledRelation(bundle.RelationKeySourceObject, pbtypes.String(id))
		t := st.ObjectTypes()
		t, _ = relationutils.MigrateObjectTypeIds(t)
		st.SetObjectTypes(t)
		targetObjectType, _ := relationutils.MigrateObjectTypeId(pbtypes.GetString(st.Details(), bundle.RelationKeyTargetObjectType.String()))
		st.SetDetail(bundle.RelationKeyTargetObjectType.String(), pbtypes.String(targetObjectType))
		return nil
	}); err != nil {
		return
	}
	templateID, _, err = s.objectCreator.CreateSmartBlockFromState(context.TODO(), coresb.SmartBlockTypeTemplate, nil, st)
	if err != nil {
		return
	}
	return
}

func (s *Service) ObjectDuplicate(id string) (objectID string, err error) {
	var st *state.State
	if err = s.Do(id, func(b smartblock.SmartBlock) error {
		if err = b.Restrictions().Object.Check(model.Restrictions_Duplicate); err != nil {
			return err
		}
		st = b.NewState().Copy()
		st.SetLocalDetails(nil)
		return nil
	}); err != nil {
		return
	}

	sbt, err := coresb.SmartBlockTypeFromID(id)
	if err != nil {
		return
	}

	objectID, _, err = s.objectCreator.CreateSmartBlockFromState(context.TODO(), sbt, nil, st)
	if err != nil {
		return
	}
	return
}

func (s *Service) TemplateCreateFromObjectByObjectType(otID string) (templateID string, err error) {
	if err = s.Do(otID, func(_ smartblock.SmartBlock) error { return nil }); err != nil {
		return "", fmt.Errorf("can't open objectType: %v", err)
	}
	var st = state.NewDoc("", nil).(*state.State)
	st.SetDetail(bundle.RelationKeyTargetObjectType.String(), pbtypes.String(otID))
	st.SetObjectTypes([]string{bundle.TypeKeyTemplate.URL(), otID})
	templateID, _, err = s.objectCreator.CreateSmartBlockFromState(context.TODO(), coresb.SmartBlockTypeTemplate, nil, st)
	if err != nil {
		return
	}
	return
}

func (s *Service) CreateWorkspace(req *pb.RpcWorkspaceCreateRequest) (workspaceID string, err error) {
	id, _, err := s.objectCreator.CreateSmartBlockFromState(context.TODO(), coresb.SmartBlockTypeWorkspace, &types.Struct{Fields: map[string]*types.Value{
		bundle.RelationKeyName.String():      pbtypes.String(req.Name),
		bundle.RelationKeyType.String():      pbtypes.String(bundle.TypeKeySpace.URL()),
		bundle.RelationKeyIconEmoji.String(): pbtypes.String("🌎"),
		bundle.RelationKeyLayout.String():    pbtypes.Float64(float64(model.ObjectType_space)),
	}}, nil)
	return id, err
}

// CreateLinkToTheNewObject creates an object and stores the link to it in the context block
func (s *Service) CreateLinkToTheNewObject(ctx *session.Context, req *pb.RpcBlockLinkCreateWithObjectRequest) (linkID string, objectID string, err error) {
	if req.ContextId == req.TemplateId && req.ContextId != "" {
		err = fmt.Errorf("unable to create link to template from this template")
		return
	}

	s.objectCreator.InjectWorkspaceID(req.Details, req.ContextId)
	objectID, _, err = s.CreateObject(req, "")
	if err != nil {
		return
	}
	if req.ContextId == "" {
		return
	}

	err = DoStateCtx(s, ctx, req.ContextId, func(st *state.State, sb basic.Creatable) error {
		linkID, err = sb.CreateBlock(st, pb.RpcBlockCreateRequest{
			TargetId: req.TargetId,
			Block: &model.Block{
				Content: &model.BlockContentOfLink{
					Link: &model.BlockContentLink{
						TargetBlockId: objectID,
						Style:         model.BlockContentLink_Page,
					},
				},
				Fields: req.Fields,
			},
			Position: req.Position,
		})
		if err != nil {
			return fmt.Errorf("link create error: %v", err)
		}
		return nil
	})
	return
}

func (s *Service) ObjectToSet(id string, source []string) (string, error) {
	var details *types.Struct
	if err := s.Do(id, func(b smartblock.SmartBlock) error {
		details = pbtypes.CopyStruct(b.Details())

		s := b.NewState()
		if layout, ok := s.Layout(); ok && layout == model.ObjectType_note {
			textBlock, err := s.GetFirstTextBlock()
			if err != nil {
				return err
			}
			if textBlock != nil {
				details.Fields[bundle.RelationKeyName.String()] = pbtypes.String(textBlock.Text.Text)
			}
		}

		return nil
	}); err != nil {
		return "", err
	}
	// pass layout in case it was provided by client in the RPC
	details.Fields[bundle.RelationKeySetOf.String()] = pbtypes.StringList(source)
	// cleanup details
	delete(details.Fields, bundle.RelationKeyFeaturedRelations.String())
	delete(details.Fields, bundle.RelationKeyLayout.String())
	delete(details.Fields, bundle.RelationKeyType.String())

	newID, _, err := s.objectCreator.CreateObject(&pb.RpcObjectCreateRequest{
		Details: details,
	}, bundle.TypeKeySet)
	if err != nil {
		return "", err
	}

	res, err := s.objectStore.GetWithLinksInfoByID(id)
	if err != nil {
		return "", err
	}
	for _, il := range res.Links.Inbound {
		if err = s.replaceLink(il.Id, id, newID); err != nil {
			return "", err
		}
	}
	err = s.DeleteObject(id)
	if err != nil {
		// intentionally do not return error here
		log.Errorf("failed to delete object after conversion to set: %s", err.Error())
	}

	return newID, nil
}

func (s *Service) CreateObject(req DetailsGetter, forcedType bundle.TypeKey) (id string, details *types.Struct, err error) {
	return s.objectCreator.CreateObject(req, forcedType)
}
