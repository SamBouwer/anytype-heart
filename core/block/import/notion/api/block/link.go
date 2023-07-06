package block

import (
	"github.com/globalsign/mgo/bson"
	"golang.org/x/exp/slices"

	"github.com/anyproto/anytype-heart/core/block/editor/template"
	"github.com/anyproto/anytype-heart/core/block/import/notion/api"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	textUtil "github.com/anyproto/anytype-heart/util/text"
)

const notFoundPageMessage = "Can't access object in Notion, please provide access in API"

type EmbedBlock struct {
	Block
	Embed LinkToWeb `json:"embed"`
}

func (b *EmbedBlock) GetBlocks(req *NotionImportContext, _ string) *MapResponse {
	return b.Embed.GetBlocks(req, "")
}

type LinkToWeb struct {
	URL string `json:"url"`
}

type LinkPreviewBlock struct {
	Block
	LinkPreview LinkToWeb `json:"link_preview"`
}

func (b *LinkPreviewBlock) GetBlocks(req *NotionImportContext, _ string) *MapResponse {
	return b.LinkPreview.GetBlocks(req, "")
}

func (b *LinkToWeb) GetBlocks(*NotionImportContext, string) *MapResponse {
	id := bson.NewObjectId().Hex()

	to := textUtil.UTF16RuneCountString(b.URL)

	bl := &model.Block{
		Id:          id,
		ChildrenIds: nil,
		Content: &model.BlockContentOfText{
			Text: &model.BlockContentText{
				Text: b.URL,
				Marks: &model.BlockContentTextMarks{
					Marks: []*model.BlockContentTextMark{
						{
							Range: &model.Range{
								From: int32(0),
								To:   int32(to),
							},
							Type:  model.BlockContentTextMark_Link,
							Param: b.URL,
						},
					},
				},
			},
		},
	}
	return &MapResponse{
		Blocks:   []*model.Block{bl},
		BlockIDs: []string{id},
	}
}

type ChildPageBlock struct {
	Block
	ChildPage ChildPage `json:"child_page"`
}

type ChildPage struct {
	Title string `json:"title"`
}

func (b *ChildPageBlock) GetBlocks(req *NotionImportContext, pageID string) *MapResponse {
	bl := b.ChildPage.GetLinkToObjectBlock(req, pageID)
	return &MapResponse{
		Blocks:   []*model.Block{bl},
		BlockIDs: []string{bl.Id},
	}
}

func (p ChildPage) GetLinkToObjectBlock(importContext *NotionImportContext, pageID string) *model.Block {
	targetBlockID, ok := getTargetBlock(importContext.ParentPageToChildIDs, importContext.PageNameToID, importContext.NotionPageIdsToAnytype, pageID, p.Title)

	id := bson.NewObjectId().Hex()
	if !ok {
		return &model.Block{
			Id:          id,
			ChildrenIds: nil,
			Content: &model.BlockContentOfText{
				Text: &model.BlockContentText{
					Text: notFoundPageMessage,
					Marks: &model.BlockContentTextMarks{
						Marks: []*model.BlockContentTextMark{},
					},
				},
			},
		}
	}

	return &model.Block{
		Id:          id,
		ChildrenIds: nil,
		Content: &model.BlockContentOfLink{
			Link: &model.BlockContentLink{
				TargetBlockId: targetBlockID,
			},
		}}
}

type ChildDatabaseBlock struct {
	Block
	ChildDatabase ChildDatabase `json:"child_database"`
}

func (b *ChildDatabaseBlock) GetBlocks(req *NotionImportContext, pageID string) *MapResponse {
	bl := b.ChildDatabase.GetDataviewBlock(req, pageID)
	return &MapResponse{
		Blocks:   []*model.Block{bl},
		BlockIDs: []string{bl.Id},
	}
}

type ChildDatabase struct {
	Title string `json:"title"`
}

func (c *ChildDatabase) GetDataviewBlock(importContext *NotionImportContext, pageID string) *model.Block {
	targetBlockID, _ := getTargetBlock(importContext.ParentPageToChildIDs,
		importContext.DatabaseNameToID,
		importContext.NotionDatabaseIdsToAnytype,
		pageID, c.Title)

	id := bson.NewObjectId().Hex()
	block := template.MakeCollectionDataviewContent()
	block.Dataview.TargetObjectId = targetBlockID

	return &model.Block{
		Id:          id,
		ChildrenIds: nil,
		Content:     block,
	}
}

type LinkToPageBlock struct {
	Block
	LinkToPage api.Parent `json:"link_to_page"`
}

func (l *LinkToPageBlock) GetBlocks(req *NotionImportContext, _ string) *MapResponse {
	var anytypeID string
	if l.LinkToPage.PageID != "" {
		anytypeID = req.NotionPageIdsToAnytype[l.LinkToPage.PageID]
	}
	if l.LinkToPage.DatabaseID != "" {
		anytypeID = req.NotionDatabaseIdsToAnytype[l.LinkToPage.DatabaseID]
	}
	id := bson.NewObjectId().Hex()
	bl := &model.Block{
		Id:          id,
		ChildrenIds: nil,
		Content: &model.BlockContentOfLink{
			Link: &model.BlockContentLink{
				TargetBlockId: anytypeID,
			},
		}}
	return &MapResponse{
		Blocks:   []*model.Block{bl},
		BlockIDs: []string{id},
	}
}

type BookmarkBlock struct {
	Block
	Bookmark BookmarkObject `json:"bookmark"`
}

func (b *BookmarkBlock) GetBlocks(*NotionImportContext, string) *MapResponse {
	bl, id := b.Bookmark.GetBookmarkBlock()
	return &MapResponse{
		Blocks:   []*model.Block{bl},
		BlockIDs: []string{id},
	}
}

type BookmarkObject struct {
	URL     string          `json:"url"`
	Caption []*api.RichText `json:"caption"`
}

func (b BookmarkObject) GetBookmarkBlock() (*model.Block, string) {
	id := bson.NewObjectId().Hex()
	title := api.RichTextToDescription(b.Caption)

	return &model.Block{
		Id:          id,
		ChildrenIds: []string{},
		Content: &model.BlockContentOfBookmark{
			Bookmark: &model.BlockContentBookmark{
				Url:   b.URL,
				Title: title,
			},
		}}, id
}

// getTargetBlock has the logic, that each page in Notion can have many child pages inside, which can also have the same name.
// But the Notion API sends us only the names of these pages. Therefore, as a result, we can get an approximate answers like
//
//	parentPage {
//	      childPage: “Title”,
//	      childPage: “Title”,
//	      childPage: "Title"
//	}
//
// And these pages are different. Therefore, we get children of given page and compare its name with those
// returned to us by the Notion API. As a result we get Anytype ID of child page and make it targetBlockID
// But we can end up with 3 links to the same page, and to avoid that,
// we remove the childID from the parentIDToChildrenID map. So it helps to not create links with the same targetBlockID.
func getTargetBlock(parentPageIDToChildIDs map[string][]string, pageIDToName, notionIDsToAnytype map[string]string, pageID, title string) (string, bool) {
	var (
		targetBlockID string
		ok            bool
	)
	if childIDs, exist := parentPageIDToChildIDs[pageID]; exist {
		for childIdx, childID := range childIDs {
			if pageName, pageExist := pageIDToName[childID]; pageExist && pageName == title {
				targetBlockID, ok = notionIDsToAnytype[childID]
				childIDs = slices.Delete(childIDs, childIdx, childIdx+1)
				break
			}
		}
		parentPageIDToChildIDs[pageID] = childIDs
	}
	return targetBlockID, ok
}
