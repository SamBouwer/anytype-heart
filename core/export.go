package core

import (
	"context"

	"github.com/anytypeio/go-anytype-middleware/core/block"
	"github.com/anytypeio/go-anytype-middleware/core/block/export"
	"github.com/anytypeio/go-anytype-middleware/pb"
)

func (mw *Middleware) ObjectListExport(cctx context.Context, req *pb.RpcObjectListExportRequest) *pb.RpcObjectListExportResponse {
	response := func(path string, succeed int, failed int, err error) (res *pb.RpcObjectListExportResponse) {
		res = &pb.RpcObjectListExportResponse{
			Error: &pb.RpcObjectListExportResponseError{
				Code: pb.RpcObjectListExportResponseError_NULL,
			},
		}
		if err != nil {
			res.Error.Code = pb.RpcObjectListExportResponseError_UNKNOWN_ERROR
			res.Error.Description = err.Error()
			return
		} else {
			res.Path = path
			res.Succeed = int32(succeed)
			res.Failed = int32(failed)
		}
		return res
	}
	var (
		path    string
		succeed int
		failed  int
		err     error
	)
	err = mw.doBlockService(func(_ *block.Service) error {
		es := mw.app.MustComponent(export.CName).(export.Export)
		path, succeed, failed, err = es.Export(*req)
		return err
	})
	return response(path, succeed, failed, err)
}
