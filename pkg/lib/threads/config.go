package threads

import (
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/pkg/lib/util"
)

type CafeConfigFetcher interface {
	AddAccountStateObserver(observer util.CafeAccountStateUpdateObserver)
}

type CurrentWorkspaceThreadGetter interface {
	GetCurrentWorkspaceId() (string, error)
}

type ThreadCreateQueue interface {
	AddThreadQueueEntry(entry *model.ThreadCreateQueueEntry) (err error)
	RemoveThreadQueueEntry(threadId string) (err error)
	GetAllQueueEntries() ([]*model.ThreadCreateQueueEntry, error)
}

type ObjectDeleter interface {
	DeleteObject(id string) error
}

type Config struct {
	SyncTracking bool
	Debug        bool
	PubSub       bool
	Metrics      bool

	CafeP2PAddr             string
	CafePID                 string
	CafePermanentConnection bool // explicitly watch the connection to this peer and reconnect in case the connection has failed
}

type ThreadsConfigGetter interface {
	ThreadsConfig() Config
}

var DefaultConfig = Config{
	SyncTracking:            true,
	Debug:                   false,
	Metrics:                 false,
	PubSub:                  true,
	CafePermanentConnection: true,
	// CafeP2PAddr is being set later when we have the global config
	// We probably should refactor this default config logic for threads
}
