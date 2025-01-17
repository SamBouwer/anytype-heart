package space

import (
	"context"

	"github.com/anyproto/any-sync/commonspace"
)

func newClientSpace(cc commonspace.Space) (commonspace.Space, error) {
	return &clientSpace{cc}, nil
}

type clientSpace struct {
	commonspace.Space
}

func (s *clientSpace) Init(ctx context.Context) (err error) {
	return s.Space.Init(ctx)
}

func (s *clientSpace) Close() (err error) {
	return s.Space.Close()
}
