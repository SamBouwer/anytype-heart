package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
	pstore "github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/textileio/go-threads/core/net"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/core/anytype/config"
	"github.com/anyproto/anytype-heart/core/configfetcher"
	"github.com/anyproto/anytype-heart/core/event"
	"github.com/anyproto/anytype-heart/core/wallet"
	"github.com/anyproto/anytype-heart/metrics"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/cafe"
	"github.com/anyproto/anytype-heart/pkg/lib/core/smartblock"
	"github.com/anyproto/anytype-heart/pkg/lib/datastore"
	"github.com/anyproto/anytype-heart/pkg/lib/files"
	"github.com/anyproto/anytype-heart/pkg/lib/ipfs"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/addr"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/filestore"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/pkg/lib/pin"
	"github.com/anyproto/anytype-heart/pkg/lib/threads"
	"github.com/anyproto/anytype-heart/pkg/lib/util"
)

var log = logging.Logger("anytype-core")

var ErrObjectDoesNotBelongToWorkspace = fmt.Errorf("object does not belong to workspace")

const (
	CName  = "anytype"
	tmpDir = "tmp"
)

type Service interface {
	Account() string // deprecated, use wallet component
	Device() string  // deprecated, use wallet component
	Start() error
	Stop() error
	IsStarted() bool

	EnsurePredefinedBlocks(ctx context.Context, mustSyncFromRemote bool) error
	PredefinedBlocks() threads.DerivedSmartblockIds
	GetBlock(blockId string) (SmartBlock, error)
	GetBlockCtx(ctx context.Context, blockId string) (SmartBlock, error)

	// FileOffload removes file blocks recursively, but leave details
	FileOffload(id string) (bytesRemoved uint64, err error)

	FileByHash(ctx context.Context, hash string) (File, error)
	FileAdd(ctx context.Context, opts ...files.AddOption) (File, error)
	FileGetKeys(hash string) (*files.FileKeys, error)
	FileStoreKeys(fileKeys ...files.FileKeys) error

	ImageByHash(ctx context.Context, hash string) (Image, error)
	ImageAdd(ctx context.Context, opts ...files.AddOption) (Image, error)

	GetAllWorkspaces() ([]string, error)
	GetWorkspaceIdForObject(objectId string) (string, error)

	ObjectStore() objectstore.ObjectStore // deprecated
	FileStore() filestore.FileStore       // deprecated
	ThreadsIds() ([]string, error)        // deprecated
	ThreadsService() threads.Service

	ObjectInfoWithLinks(id string) (*model.ObjectInfoWithLinks, error)
	ObjectList() ([]*model.ObjectInfo, error)

	ProfileInfo

	app.ComponentRunnable
	TempDir() string
}

var _ app.Component = (*Anytype)(nil)

var _ Service = (*Anytype)(nil)

type CreatorInfoAdder interface {
	AddCreatorInfoIfNeeded(workspaceId string) error
}

type Anytype struct {
	files            *files.Service
	cafe             cafe.Client
	mdns             mdns.Service
	objectStore      objectstore.ObjectStore
	fileStore        filestore.FileStore
	fetcher          configfetcher.ConfigFetcher
	sendEvent        func(event *pb.Event)
	creatorInfoAdder CreatorInfoAdder

	ds datastore.Datastore

	predefinedBlockIds threads.DerivedSmartblockIds
	threadService      threads.Service
	pinService         pin.FilePinService
	ipfs               ipfs.Node
	logLevels          map[string]string

	opts ServiceOptions

	replicationWG    sync.WaitGroup
	migrationOnce    sync.Once
	lock             sync.Mutex
	isStarted        bool // use under the lock
	shutdownStartsCh chan struct {
	} // closed when node shutdown starts

	recordsbatch        batchAdder
	subscribeOnce       sync.Once
	config              *config.Config
	wallet              wallet.Wallet
	tmpFolderAutocreate sync.Once
	tempDir             string
}

func (a *Anytype) ThreadsIds() ([]string, error) {
	tids, err := a.ThreadService().Logstore().Threads()
	if err != nil {
		return nil, err
	}
	return util.ThreadIdsToStings(tids), nil
}

type batchAdder interface {
	Add(msgs ...interface{}) error
	Close() error
}

func New() *Anytype {
	return &Anytype{
		shutdownStartsCh: make(chan struct{}),
	}
}

func (a *Anytype) Init(ap *app.App) (err error) {
	a.wallet = ap.MustComponent(wallet.CName).(wallet.Wallet)
	a.config = ap.MustComponent(config.CName).(*config.Config)
	a.recordsbatch = ap.MustComponent("recordsbatcher").(batchAdder)
	a.objectStore = ap.MustComponent(objectstore.CName).(objectstore.ObjectStore)
	a.fileStore = ap.MustComponent(filestore.CName).(filestore.FileStore)
	a.ds = ap.MustComponent(datastore.CName).(datastore.Datastore)
	a.threadService = ap.MustComponent(threads.CName).(threads.Service)
	a.cafe = ap.MustComponent(cafe.CName).(cafe.Client)
	a.files = ap.MustComponent(files.CName).(*files.Service)
	a.pinService = ap.MustComponent(pin.CName).(pin.FilePinService)
	a.ipfs = ap.MustComponent(ipfs.CName).(ipfs.Node)
	a.sendEvent = ap.MustComponent(event.CName).(event.Sender).Send
	a.fetcher = ap.MustComponent(configfetcher.CName).(configfetcher.ConfigFetcher)
	a.creatorInfoAdder = ap.MustComponent("blockService").(CreatorInfoAdder)
	return
}

func (a *Anytype) Name() string {
	return CName
}

// Deprecated, use wallet component directly
func (a *Anytype) Account() string {
	pk, _ := a.wallet.GetAccountPrivkey()
	if pk == nil {
		return ""
	}
	return pk.Address()
}

// Deprecated, use wallet component directly
func (a *Anytype) Device() string {
	pk, _ := a.wallet.GetDevicePrivkey()
	if pk == nil {
		return ""
	}
	return pk.Address()
}

func (a *Anytype) Run(ctx context.Context) (err error) {
	if err = a.Start(); err != nil {
		return
	}

	return a.EnsurePredefinedBlocks(ctx, a.config.NewAccount)
}

func (a *Anytype) IsStarted() bool {
	a.lock.Lock()
	defer a.lock.Unlock()

	return a.isStarted
}

func (a *Anytype) GetAllWorkspaces() ([]string, error) {
	return a.threadService.GetAllWorkspaces()
}

func (a *Anytype) ThreadsService() threads.Service {
	return a.threadService
}

func (a *Anytype) GetWorkspaceIdForObject(objectId string) (string, error) {
	if strings.HasPrefix(objectId, "_") {
		return addr.AnytypeMarketplaceWorkspace, nil
	}
	if a.predefinedBlockIds.IsAccount(objectId) {
		return "", ErrObjectDoesNotBelongToWorkspace
	}

	workspaceIds := a.threadService.ThreadQueue().GetWorkspacesForThread(objectId)
	if len(workspaceIds) != 0 {
		return workspaceIds[0], nil
	}

	return "", ErrObjectDoesNotBelongToWorkspace
}

// PredefinedBlocks returns default blocks like home and archive
// ⚠️ Will return empty struct in case it runs before Anytype.Start()
func (a *Anytype) PredefinedBlocks() threads.DerivedSmartblockIds {
	return a.predefinedBlockIds
}

func (a *Anytype) HandlePeerFound(p peer.AddrInfo) {
	a.ThreadService().Threads().Host().Peerstore().AddAddrs(p.ID, p.Addrs, pstore.ConnectedAddrTTL)
}

func (a *Anytype) Start() error {
	err := a.RunMigrations()
	if err != nil {
		return err
	}

	return a.start()
}

func (a *Anytype) start() error {
	a.lock.Lock()
	defer a.lock.Unlock()

	if a.isStarted {
		return nil
	}

	var err error
	a.predefinedBlockIds, err = a.threadService.DerivePredefinedThreadIds()
	if err != nil {
		return err
	}

	if err := a.subscribeForNewRecords(); err != nil {
		return err
	}

	a.isStarted = true
	return nil
}

func (a *Anytype) EnsurePredefinedBlocks(ctx context.Context, newAccount bool) (err error) {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-cctx.Done():
			return
		case <-a.shutdownStartsCh:
			cancel()
		}
	}()

	_, err = a.threadService.EnsurePredefinedThreads(cctx, newAccount)
	if err != nil {
		return err
	}

	return nil
}

func (a *Anytype) Close() (err error) {
	metrics.SharedClient.Close()
	return a.Stop()
}

func (a *Anytype) Stop() error {
	fmt.Printf("stopping the library...\n")
	defer fmt.Println("library has been successfully stopped")
	a.lock.Lock()
	defer a.lock.Unlock()
	a.isStarted = false

	if a.shutdownStartsCh != nil {
		close(a.shutdownStartsCh)
	}

	// fixme useless!
	a.replicationWG.Wait()

	return nil
}

func (a *Anytype) ThreadService() threads.Service {
	return a.threadService
}

func (a *Anytype) TempDir() string {
	// it shouldn't be a case when it is called before wallet init, but just in case lets add the check here
	if a.wallet == nil || a.wallet.RootPath() == "" {
		return os.TempDir()
	}

	var err error
	// simultaneous calls to TempDir will wait for the once func to finish, so it will be fine
	a.tmpFolderAutocreate.Do(func() {
		path := filepath.Join(a.wallet.RootPath(), tmpDir)
		err = os.MkdirAll(path, 0700)
		if err != nil {
			log.Errorf("failed to make temp dir, use the default system one: %s", err.Error())
			a.tempDir = os.TempDir()
		} else {
			a.tempDir = path
		}
	})

	return a.tempDir
}

func (a *Anytype) addCreatorData(rec net.ThreadRecord,
	readMx *sync.RWMutex,
	checkedThreads map[string]struct{},
	checkedWorkspaces map[string]struct{}) {
	threadId := rec.ThreadID().String()
	var err error
	defer func() {
		if err != nil && err != ErrObjectDoesNotBelongToWorkspace {
			threads.WorkspaceLogger.
				With("thread id", threadId).
				Errorf("error checking or adding creator info: %v", err)
		}
	}()

	if rec.LogID().String() != a.Device() {
		return
	}

	readMx.RLock()
	// if we already added info for this thread
	if _, ok := checkedThreads[threadId]; ok {
		readMx.RUnlock()
		return
	}
	readMx.RUnlock()

	sbType, err := smartblock.SmartBlockTypeFromID(threadId)
	if err != nil {
		return
	}
	var workspaceId string
	if sbType == smartblock.SmartBlockTypeWorkspace {
		// if we add something to workspace itself, it means that we should add our creator info
		workspaceId = threadId
	} else {
		workspaceId, err = a.GetWorkspaceIdForObject(threadId)
		if err != nil {
			if err == ErrObjectDoesNotBelongToWorkspace {
				readMx.Lock()
				defer readMx.Unlock()
				checkedThreads[threadId] = struct{}{}
			}
			return
		}
	}

	readMx.RLock()
	if _, ok := checkedWorkspaces[workspaceId]; ok {
		readMx.RUnlock()
		readMx.Lock()
		defer readMx.Unlock()
		checkedThreads[threadId] = struct{}{}
		return
	}
	readMx.RUnlock()

	err = a.creatorInfoAdder.AddCreatorInfoIfNeeded(workspaceId)
	if err != nil {
		return
	}
	readMx.Lock()
	defer readMx.Unlock()
	checkedThreads[threadId] = struct{}{}
	checkedWorkspaces[workspaceId] = struct{}{}
}

// subscribeForNewRecords should be called only once as early as possible.
// Subscribes to new records for all threads and add them to the batcher
func (a *Anytype) subscribeForNewRecords() (err error) {
	checkedWorkspaces := make(map[string]struct{})
	checkedThreads := make(map[string]struct{})
	creatorInfoMx := sync.RWMutex{}

	isWorkspaceEventSent := false
	isWorkspace := func(id string) bool {
		sbType, err := smartblock.SmartBlockTypeFromID(id)
		return err == nil && !a.predefinedBlockIds.IsAccount(id) && sbType == smartblock.SmartBlockTypeWorkspace
	}

	ctx, cancel := context.WithCancel(context.Background())
	// do not defer cancel, cancel only on shutdown
	threadsCh, err := a.threadService.PresubscribedNewRecords()
	if err != nil {
		return err
	}
	go func() {
		a.lock.Lock()
		shutdownCh := a.shutdownStartsCh
		a.lock.Unlock()
		defer a.recordsbatch.Close()
		for {
			select {
			case val, ok := <-threadsCh:
				if !ok {
					return
				}
				go a.addCreatorData(val, &creatorInfoMx, checkedThreads, checkedWorkspaces)
				id := val.ThreadID().String()
				if a.predefinedBlockIds.IsAccount(id) {
					continue
				}
				if !isWorkspaceEventSent && isWorkspace(id) {
					go a.fetcher.NotifyClientApp()
					isWorkspaceEventSent = true
				}

				err = a.recordsbatch.Add(ThreadRecordInfo{
					LogId:    val.LogID().String(),
					ThreadID: id,
				})

				if err != nil {
					log.Errorf("failed to add thread record to batcher: %s", err.Error())
					continue
				}
			case <-ctx.Done():
				return
			case <-shutdownCh:
				cancel()
			}
		}
	}()

	return nil
}
