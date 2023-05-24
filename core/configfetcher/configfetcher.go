package configfetcher

import (
	"context"
	"fmt"
	"github.com/anyproto/anytype-heart/pkg/lib/pb/model"
	"github.com/anyproto/anytype-heart/pkg/lib/util"
	"github.com/gogo/protobuf/proto"
	ds "github.com/ipfs/go-datastore"
	"sync"
	"time"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/core/event"
	pbMiddle "github.com/anyproto/anytype-heart/pb"
	cafeClient "github.com/anyproto/anytype-heart/pkg/lib/cafe"
	"github.com/anyproto/anytype-heart/pkg/lib/cafe/pb"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
)

var log = logging.Logger("anytype-mw-configfetcher")

const CName = "configfetcher"
const accountStateFetchInterval = 15 * time.Minute

type WorkspaceGetter interface {
	GetAllWorkspaces() ([]string, error)
}

var defaultAccountState = &pb.AccountState{
	Config: &pb.Config{
		EnableDataview:             false,
		EnableDebug:                false,
		EnableReleaseChannelSwitch: false,
		EnablePrereleaseChannel:    false,
		SimultaneousRequests:       20,
		EnableSpaces:               false,
		Extra:                      nil,
	},
	Status: &pb.AccountStateStatus{
		Status:       pb.AccountState_Active,
		DeletionDate: 0,
	},
}

type ConfigFetcher interface {
	app.ComponentRunnable
	GetAccountStateWithContext(ctx context.Context) *pb.AccountState
	GetAccountState() *pb.AccountState
	AddAccountStateObserver(observer util.CafeAccountStateUpdateObserver)
	NotifyClientApp()
	Refetch()
}

type configFetcher struct {
	sync.RWMutex
	store           objectstore.ObjectStore
	cafe            cafeClient.Client
	workspaceGetter WorkspaceGetter
	eventSender     func(event *pbMiddle.Event)

	fetched       chan struct{}
	stopped       chan struct{}
	refetch       chan struct{}
	fetchedClosed bool
	refetchClosed bool
	ctx           context.Context
	cancel        context.CancelFunc

	observers []util.CafeAccountStateUpdateObserver
}

func (c *configFetcher) GetAccountState() *pb.AccountState {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return c.GetAccountStateWithContext(ctx)
}

func (c *configFetcher) AddAccountStateObserver(observer util.CafeAccountStateUpdateObserver) {
	c.Lock()
	defer c.Unlock()
	c.observers = append(c.observers, observer)
}

func (c *configFetcher) GetAccountStateWithContext(ctx context.Context) *pb.AccountState {
	state := c.GetCafeAccountStateWithContext(ctx)
	// we could have cached this, but for now it is not needed, because we call this rarely
	enableSpaces := state.GetConfig().GetEnableSpaces()
	workspaces, err := c.workspaceGetter.GetAllWorkspaces()
	if err == nil && len(workspaces) != 0 {
		enableSpaces = true
	}
	state.Config.EnableSpaces = enableSpaces

	return state
}

func New() ConfigFetcher {
	return &configFetcher{}
}

func (c *configFetcher) Run(context.Context) error {
	c.ctx, c.cancel = context.WithCancel(context.Background())
	go c.run()
	return nil
}

func (c *configFetcher) run() {
	defer close(c.stopped)
	var t *time.Timer
	defer func() {
		if t != nil && !t.Stop() {
			<-t.C
		}
	}()
	sentFirstUpdate := false
	timeInterval := time.Duration(0)
	attempt := 0
	for {
		if t == nil {
			t = time.NewTimer(timeInterval)
		} else {
			// we should go here only if we drained the channel and the timer expires
			t.Reset(timeInterval)
		}
		select {
		case <-c.ctx.Done():
			return
		case <-t.C:
			break
		case <-c.refetch:
			if !t.Stop() {
				<-t.C
			}
			break
		}
		state, equal, err := c.fetchAccountState()
		if err == nil {
			if !c.fetchedClosed {
				close(c.fetched)
				c.fetchedClosed = true
			}
			c.RLock()
			for _, observer := range c.observers {
				observer.ObserveAccountStateUpdate(state)
			}
			c.RUnlock()

			if !equal || !sentFirstUpdate {
				c.NotifyClientApp()
				sentFirstUpdate = true
			}
			timeInterval = accountStateFetchInterval
			attempt = 0
		} else {
			attempt++
			timeInterval = 2 * time.Second * time.Duration(attempt)
			if timeInterval > accountStateFetchInterval {
				timeInterval = accountStateFetchInterval
			}
			log.Warnf("failed to fetch cafe config after %d attempts with error: %s", attempt, err.Error())
		}
	}
}

func (c *configFetcher) GetCafeAccountStateWithContext(ctx context.Context) *pb.AccountState {
	select {
	case <-c.fetched:
	case <-ctx.Done():
	}

	state, err := c.store.GetAccountState()
	if err != nil {
		log.Errorf("failed to account state config from the store: %s", err.Error())
		state = defaultAccountState
	}
	return state
}

func (c *configFetcher) Init(a *app.App) (err error) {
	c.store = a.MustComponent(objectstore.CName).(objectstore.ObjectStore)
	c.cafe = a.MustComponent(cafeClient.CName).(cafeClient.Client)
	c.workspaceGetter = a.MustComponent("threads").(WorkspaceGetter)
	c.eventSender = a.MustComponent(event.CName).(event.Sender).Send
	c.fetched = make(chan struct{})
	c.stopped = make(chan struct{})
	c.refetch = make(chan struct{})
	c.fetchedClosed = false
	c.refetchClosed = false
	c.cancel = func() {}
	return nil
}

func (c *configFetcher) Name() (name string) {
	return CName
}

func (c *configFetcher) fetchAccountState() (state *pb.AccountState, equal bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	resp, err := c.cafe.GetAccountState(ctx, &pb.GetAccountStateRequest{})
	cancel()
	if err != nil {
		err = fmt.Errorf("failed to request cafe config: %w", err)
		return
	}
	oldState, err := c.store.GetAccountState()
	if err != nil && err != ds.ErrNotFound {
		err = fmt.Errorf("failed to get cafe config: %w", err)
		return
	}

	if oldState != nil {
		equal = proto.Equal(resp.AccountState, oldState)
	}

	if resp != nil && !equal {
		err = c.store.SaveAccountState(resp.AccountState)
		if err != nil {
			err = fmt.Errorf("failed to save cafe account state to objectstore: %w", err)
			return
		}
	}
	state = resp.AccountState
	return
}

func (c *configFetcher) Refetch() {
	c.Lock()
	defer c.Unlock()
	if c.refetchClosed {
		return
	}
	select {
	case c.refetch <- struct{}{}:
	default:
	}
}

func (c *configFetcher) Close() (err error) {
	c.cancel()
	<-c.stopped

	c.Lock()
	close(c.refetch)
	c.refetchClosed = true
	c.Unlock()

	if !c.fetchedClosed {
		close(c.fetched)
		c.fetchedClosed = true
	}
	return nil
}

func (c *configFetcher) NotifyClientApp() {
	accountState := c.GetAccountState()
	ev := &pbMiddle.Event{
		Messages: []*pbMiddle.EventMessage{
			&pbMiddle.EventMessage{
				Value: &pbMiddle.EventMessageValueOfAccountUpdate{
					AccountUpdate: &pbMiddle.EventAccountUpdate{
						Config: convertToAccountConfigModel(accountState.Config),
						Status: convertToAccounStatusModel(accountState.Status),
					},
				},
			},
		},
	}
	if c.eventSender != nil {
		c.eventSender(ev)
	}
}

func convertToAccountConfigModel(cfg *pb.Config) *model.AccountConfig {
	return &model.AccountConfig{
		EnableDataview:          cfg.EnableDataview,
		EnableDebug:             cfg.EnableDebug,
		EnablePrereleaseChannel: cfg.EnablePrereleaseChannel,
		EnableSpaces:            cfg.EnableSpaces,
		Extra:                   cfg.Extra,
	}
}

func convertToAccounStatusModel(status *pb.AccountStateStatus) *model.AccountStatus {
	return &model.AccountStatus{
		StatusType:   model.AccountStatusType(status.Status),
		DeletionDate: status.DeletionDate,
	}
}
