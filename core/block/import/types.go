package importer

import (
	"context"

	"github.com/gogo/protobuf/types"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/core/block/import/converter"
	"github.com/anyproto/anytype-heart/core/session"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/core/smartblock"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	// import plugins
	_ "github.com/anyproto/anytype-heart/core/block/import/html"
	_ "github.com/anyproto/anytype-heart/core/block/import/markdown"
	_ "github.com/anyproto/anytype-heart/core/block/import/notion"
	_ "github.com/anyproto/anytype-heart/core/block/import/pb"
)

// Importer incapsulate logic with import
type Importer interface {
	app.Component
	Import(ctx *session.Context, req *pb.RpcObjectImportRequest) error
	ListImports(ctx *session.Context, req *pb.RpcObjectImportListRequest) ([]*pb.RpcObjectImportListImportResponse, error)
	ImportWeb(ctx *session.Context, req *pb.RpcObjectImportRequest) (string, *types.Struct, error)
	//nolint: lll
	ValidateNotionToken(ctx context.Context, req *pb.RpcObjectImportNotionValidateTokenRequest) pb.RpcObjectImportNotionValidateTokenResponseErrorCode
}

// Creator incapsulate logic with creation of given smartblocks
type Creator interface {
	//nolint: lll
	Create(ctx *session.Context, cs *model.SmartBlockSnapshotBase, relations []*converter.Relation, pageID string, sbType smartblock.SmartBlockType, updateExisting bool) (*types.Struct, error)
}

// Updater is interface for updating existing objects
type Updater interface {
	//nolint: lll
	Update(ctx *session.Context, cs *model.SmartBlockSnapshotBase, relations []*converter.Relation, pageID string) (*types.Struct, []string, error)
}

// RelationCreator incapsulates logic for creation of relations
type RelationCreator interface {
	//nolint: lll
	ReplaceRelationBlock(ctx *session.Context, oldRelationBlocksToNew map[string]*model.Block, pageID string)
	//nolint: lll
	CreateRelations(ctx *session.Context, snapshot *model.SmartBlockSnapshotBase, pageID string, relations []*converter.Relation) ([]string, map[string]*model.Block, error)
}
