// This file is needed for cases where we would have dependency cycles and there is no way to define an interface otherwise
package util

import "github.com/anyproto/anytype-heart/pkg/lib/cafe/pb"

type CafeAccountStateUpdateObserver interface {
	ObserveAccountStateUpdate(state *pb.AccountState)
}
