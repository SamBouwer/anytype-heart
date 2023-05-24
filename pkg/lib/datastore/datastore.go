package datastore

import (
	"context"
	"github.com/anyproto/anytype-heart/app"
	ds "github.com/ipfs/go-datastore"
	"github.com/textileio/go-threads/db/keytransform"
)

const CName = "datastore"

type Datastore interface {
	app.ComponentRunnable
	PeerstoreDS() (ds.Batching, error)
	BlockstoreDS() (ds.Batching, error)
	RunBlockstoreGC() (freed int64, err error)
	LogstoreDS() (DSTxnBatching, error)
	LocalstoreDS() (DSTxnBatching, error)
	ThreadsDbDS() (keytransform.TxnDatastoreExtended, error)
}

type DSTxnBatching interface {
	ds.TxnDatastore
	Batch(ctx context.Context) (ds.Batch, error)
}
