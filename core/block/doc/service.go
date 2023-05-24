package doc

import (
	"context"
	"sync"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/core/block/editor/state"
	"github.com/anyproto/anytype-heart/core/recordsbatcher"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
	"github.com/anyproto/anytype-heart/util/slice"
)

const CName = "docService"

var log = logging.Logger("anytype-mw-block-doc")

func New() Service {
	return &listener{}
}

type DocInfo struct {
	Id         string
	Links      []string
	FileHashes []string
	LogHeads   map[string]string
	Creator    string
	State      *state.State
}

type OnDocChangeCallback func(ctx context.Context, info DocInfo) error

type Service interface {
	GetDocInfo(ctx context.Context, id string) (info DocInfo, err error)
	OnWholeChange(cb OnDocChangeCallback)
	ReportChange(ctx context.Context, info DocInfo)
	WakeupIds(ids ...string)

	app.ComponentRunnable
}

type docInfoHandler interface {
	GetDocInfo(ctx context.Context, id string) (info DocInfo, err error)
	Wakeup(id string) (err error)
}

type listener struct {
	wholeCallbacks []OnDocChangeCallback
	docInfoHandler docInfoHandler
	records        recordsbatcher.RecordsBatcher

	m sync.RWMutex
}

func (l *listener) Init(a *app.App) (err error) {
	l.docInfoHandler = a.MustComponent("blockService").(docInfoHandler)
	l.records = a.MustComponent(recordsbatcher.CName).(recordsbatcher.RecordsBatcher)
	return
}

func (l *listener) Run(context.Context) (err error) {
	go l.wakeupLoop()
	return
}

func (l *listener) Name() (name string) {
	return CName
}

func (l *listener) ReportChange(ctx context.Context, info DocInfo) {
	l.m.RLock()
	defer l.m.RUnlock()
	for _, cb := range l.wholeCallbacks {
		if err := cb(ctx, info); err != nil {
			log.Errorf("state change callback error: %v", err)
		}
	}
}

func (l *listener) OnWholeChange(cb OnDocChangeCallback) {
	l.m.Lock()
	defer l.m.Unlock()
	l.wholeCallbacks = append(l.wholeCallbacks, cb)
}

func (l *listener) WakeupIds(ids ...string) {
	for _, id := range ids {
		l.records.Add(core.ThreadRecordInfo{ThreadID: id})
	}
}

func (l *listener) GetDocInfo(ctx context.Context, id string) (info DocInfo, err error) {
	return l.docInfoHandler.GetDocInfo(ctx, id)
}

func (l *listener) wakeupLoop() {
	var buf = make([]interface{}, 50)
	var idsToWakeup []string
	for {
		n := l.records.Read(buf)
		if n == 0 {
			return
		}
		idsToWakeup = idsToWakeup[:0]
		for _, rec := range buf[:n] {
			if val, ok := rec.(core.ThreadRecordInfo); !ok {
				log.Errorf("doc listner got unknown type %t", rec)
			} else {
				if slice.FindPos(idsToWakeup, val.ThreadID) == -1 {
					idsToWakeup = append(idsToWakeup, val.ThreadID)
					if err := l.docInfoHandler.Wakeup(val.ThreadID); err != nil {
						log.With("thread", val.ThreadID).Errorf("can't wakeup thread")
					}
				}
			}
		}
	}
}

func (l *listener) Close() (err error) {
	return
}
