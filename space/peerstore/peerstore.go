package peerstore

import (
	"github.com/anytypeio/any-sync/app"
	"github.com/anytypeio/any-sync/nodeconf"
	libSlice "github.com/anytypeio/any-sync/util/slice"
	"github.com/anytypeio/go-anytype-middleware/util/slice"
	"golang.org/x/exp/slices"
	"sync"
)

const CName = "client.space.peerstore"

type PeerStore interface {
	app.Component
	ResponsibleNodeIds(spaceId string) []string
	ResponsibleFilePeers() []string
	LocalPeerIds(spaceId string) []string
	AllLocalPeers() []string
	UpdateLocalPeer(peerId string, spaceIds []string)
	AddObserver(observer Observer)
}

func New() PeerStore {
	return &peerStore{
		localPeerIdsBySpace:  map[string][]string{},
		responsibleIds:       map[string][]string{},
		spacesByLocalPeerIds: map[string][]string{},
		Mutex:                sync.Mutex{},
	}
}

type Observer func(peerId string, spaceIds []string)

type peerStore struct {
	nodeConf             nodeconf.Service
	localPeerIds         []string
	localPeerIdsBySpace  map[string][]string
	spacesByLocalPeerIds map[string][]string
	responsibleIds       map[string][]string
	observers            []Observer
	sync.Mutex
}

func (p *peerStore) Init(a *app.App) (err error) {
	p.nodeConf = a.MustComponent(nodeconf.CName).(nodeconf.Service)
	return
}

func (p *peerStore) Name() (name string) {
	return CName
}

func (p *peerStore) AddObserver(observer Observer) {
	p.Lock()
	defer p.Unlock()
	p.observers = append(p.observers, observer)
}

func (p *peerStore) UpdateLocalPeer(peerId string, spaceIds []string) {
	notify := true
	p.Lock()
	defer func() {
		observers := p.observers
		p.Unlock()
		if !notify {
			return
		}

		for _, ob := range observers {
			ob(peerId, spaceIds)
		}
	}()
	if oldIds, ok := p.spacesByLocalPeerIds[peerId]; ok {
		slices.Sort(oldIds)
		slices.Sort(spaceIds)
		if slices.Equal(oldIds, spaceIds) {
			notify = false
			return
		}
		p.updatePeer(peerId, oldIds, spaceIds)
		return
	}
	p.addNewPeer(peerId, spaceIds)
}

func (p *peerStore) addNewPeer(peerId string, newIds []string) {
	p.localPeerIds = append(p.localPeerIds, peerId)
	p.spacesByLocalPeerIds[peerId] = newIds
	for _, spaceId := range newIds {
		spacePeerIds := p.localPeerIdsBySpace[spaceId]
		spacePeerIds = append(spacePeerIds, peerId)
		p.localPeerIdsBySpace[spaceId] = spacePeerIds
	}
}

func (p *peerStore) updatePeer(peerId string, oldIds, newIds []string) {
	removed, added := slice.DifferenceRemovedAdded(oldIds, newIds)
	p.spacesByLocalPeerIds[peerId] = newIds
	for _, spaceId := range added {
		peerIds := p.localPeerIdsBySpace[spaceId]
		peerIds = append(peerIds, peerId)
		p.localPeerIdsBySpace[spaceId] = peerIds
	}
	for _, spaceId := range removed {
		peerIds := p.localPeerIdsBySpace[spaceId]
		peerIds = libSlice.DiscardFromSlice(peerIds, func(s string) bool {
			return s == peerId
		})
		p.localPeerIdsBySpace[spaceId] = peerIds
	}
}

func (p *peerStore) AllLocalPeers() []string {
	return p.localPeerIds
}

func (p *peerStore) LocalPeerIds(spaceId string) []string {
	p.Lock()
	defer p.Unlock()
	return p.localPeerIdsBySpace[spaceId]
}

func (p *peerStore) ResponsibleNodeIds(spaceId string) (ids []string) {
	return p.nodeConf.GetLast().NodeIds(spaceId)
}

func (p *peerStore) ResponsibleFilePeers() (ids []string) {
	return p.nodeConf.GetLast().FilePeers()
}