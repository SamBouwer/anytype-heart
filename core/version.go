package core

import (
	"context"

	"github.com/anyproto/anytype-heart/pb"
)

func (mw *Middleware) AppGetVersion(cctx context.Context, req *pb.RpcAppGetVersionRequest) *pb.RpcAppGetVersionResponse {
	response := func(version, details string, code pb.RpcAppGetVersionResponseErrorCode, err error) *pb.RpcAppGetVersionResponse {
		m := &pb.RpcAppGetVersionResponse{Version: version, Details: details, Error: &pb.RpcAppGetVersionResponseError{Code: code}}
		if err != nil {
			m.Error.Description = err.Error()
		}

		return m
	}

	return response(mw.app.Version(), mw.app.VersionDescription(), pb.RpcAppGetVersionResponseError_NULL, nil)
}
