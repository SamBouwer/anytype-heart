package syncer

import (
	"fmt"
	"os"
	"strings"

	"github.com/anyproto/anytype-heart/core/block"
	"github.com/anyproto/anytype-heart/core/block/simple"
	"github.com/anyproto/anytype-heart/core/session"
	"github.com/anyproto/anytype-heart/pb"
)

type FileSyncer struct {
	service *block.Service
}

func NewFileSyncer(service *block.Service) *FileSyncer {
	return &FileSyncer{service: service}
}

func (fs *FileSyncer) Sync(ctx *session.Context, id string, b simple.Block) error {
	params := pb.RpcBlockUploadRequest{
		FilePath: b.Model().GetFile().Name,
		BlockId:  b.Model().Id,
	}
	if strings.HasPrefix(b.Model().GetFile().Name, "http://") || strings.HasPrefix(b.Model().GetFile().Name, "https://") {
		params = pb.RpcBlockUploadRequest{
			Url:     b.Model().GetFile().Name,
			BlockId: b.Model().Id,
		}
	}
	hash, err := fs.service.UploadFileBlockWithHash(ctx, id, params)

	if err != nil {
		return fmt.Errorf("failed syncing file: %s", err)
	}
	b.Model().GetFile().Hash = hash
	os.Remove(b.Model().GetFile().Name)
	return nil
}
