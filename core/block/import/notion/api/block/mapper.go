package block

import (
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
)

// NotionImportContext is a data object with all necessary structures for blocks
type NotionImportContext struct {
	Blocks []interface{}
	// Need all these maps for correct mapping of pages and databases from notion to anytype
	// for such blocks as mentions or links to objects
	NotionPageIdsToAnytype     map[string]string
	NotionDatabaseIdsToAnytype map[string]string
	PageNameToID               map[string]string
	DatabaseNameToID           map[string]string
}

func NewNotionImportContext() *NotionImportContext {
	return &NotionImportContext{
		NotionPageIdsToAnytype:     make(map[string]string, 0),
		NotionDatabaseIdsToAnytype: make(map[string]string, 0),
		PageNameToID:               make(map[string]string, 0),
		DatabaseNameToID:           make(map[string]string, 0),
		ParentPageToChildIDs:       make(map[string][]string, 0),
	}
}

type MapResponse struct {
	Blocks   []*model.Block
	BlockIDs []string
}

func (m *MapResponse) Merge(mergedResp *MapResponse) {
	if mergedResp != nil {
		m.BlockIDs = append(m.BlockIDs, mergedResp.BlockIDs...)
		m.Blocks = append(m.Blocks, mergedResp.Blocks...)
	}
}

func MapBlocks(req *NotionImportContext, pageID string) *MapResponse {
	resp := &MapResponse{}
	for _, bl := range req.Blocks {
		if ba, ok := bl.(Getter); ok {
			textResp := ba.GetBlocks(req, pageID)
			resp.Merge(textResp)
			continue
		}
	}
	return resp
}
