package filesync

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/anyproto/any-sync/app"
	"github.com/anyproto/any-sync/app/logger"
	"github.com/anyproto/any-sync/commonfile/fileservice"
	ipld "github.com/ipfs/go-ipld-format"
	"go.uber.org/zap"

	"github.com/anyproto/anytype-heart/core/filestorage/rpcstore"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/datastore"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/filestore"
)

const CName = "filesync"

var log = logger.NewNamed(CName)

var loopTimeout = time.Minute

var errReachedLimit = fmt.Errorf("file upload limit has been reached")

//go:generate mockgen -package mock_filesync -destination ./mock_filesync/filesync_mock.go github.com/anyproto/anytype-heart/core/filestorage/filesync FileSync
type FileSync interface {
	AddFile(spaceId, fileId string, uploadedByUser bool) (err error)
	RemoveFile(spaceId, fileId string) (err error)
	SpaceStat(ctx context.Context, spaceId string) (ss SpaceStat, err error)
	FileStat(ctx context.Context, spaceId, fileId string) (fs FileStat, err error)
	FileListStats(ctx context.Context, spaceId string, fileIDs []string) ([]FileStat, error)
	SyncStatus() (ss SyncStatus, err error)
	FetchChunksCount(ctx context.Context, node ipld.Node) (int, error)
	HasUpload(spaceId, fileId string) (ok bool, err error)
	IsFileUploadLimited(spaceId, fileId string) (ok bool, err error)
	app.ComponentRunnable
}

type SyncStatus struct {
	QueueLen int
}

type fileSync struct {
	dbProvider   datastore.Datastore
	rpcStore     rpcstore.RpcStore
	queue        *fileSyncStore
	loopCtx      context.Context
	loopCancel   context.CancelFunc
	uploadPingCh chan struct{}
	removePingCh chan struct{}
	dagService   ipld.DAGService
	fileStore    filestore.FileStore
	sendEvent    func(event *pb.Event)

	spaceStatsLock sync.Mutex
	spaceStats     map[string]SpaceStat
}

func New(sendEvent func(event *pb.Event)) FileSync {
	return &fileSync{
		sendEvent:  sendEvent,
		spaceStats: map[string]SpaceStat{},
	}
}

func (f *fileSync) Init(a *app.App) (err error) {
	f.dbProvider = a.MustComponent(datastore.CName).(datastore.Datastore)
	f.rpcStore = a.MustComponent(rpcstore.CName).(rpcstore.Service).NewStore()
	f.dagService = a.MustComponent(fileservice.CName).(fileservice.FileService).DAGService()
	f.fileStore = app.MustComponent[filestore.FileStore](a)
	f.removePingCh = make(chan struct{})
	f.uploadPingCh = make(chan struct{})
	return
}

func (f *fileSync) Name() (name string) {
	return CName
}

func (f *fileSync) Run(ctx context.Context) (err error) {
	db, err := f.dbProvider.SpaceStorage()
	if err != nil {
		return
	}
	f.queue = &fileSyncStore{db: db}
	f.loopCtx, f.loopCancel = context.WithCancel(context.Background())
	go f.addLoop()
	go f.removeLoop()
	return
}

func (f *fileSync) Close(ctx context.Context) (err error) {
	if f.loopCancel != nil {
		f.loopCancel()
	}
	if closer, ok := f.rpcStore.(io.Closer); ok {
		if err = closer.Close(); err != nil {
			log.Error("can't close rpc store", zap.Error(err))
		}
	}
	return nil
}

func (f *fileSync) SyncStatus() (ss SyncStatus, err error) {
	ql, err := f.queue.QueueLen()
	if err != nil {
		return
	}
	return SyncStatus{
		QueueLen: ql,
	}, nil
}
