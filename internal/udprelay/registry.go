package udprelay

import (
	"errors"
	"net"
	"sync"
	"time"
)

const DefaultMaxAssociations = 1024

var ErrAssociationLimit = errors.New("UDP association limit reached")

type AssociationRegistry struct {
	mu     sync.Mutex
	nextID uint32
	byPeer map[string]*peerAssociation
	byID   map[uint32]*peerAssociation
	max    int
}

type peerAssociation struct {
	id       uint32
	peer     *net.UDPAddr
	lastSeen time.Time
}

func NewAssociationRegistry(maxAssociations int) *AssociationRegistry {
	if maxAssociations <= 0 {
		maxAssociations = DefaultMaxAssociations
	}
	return &AssociationRegistry{
		nextID: 1,
		byPeer: make(map[string]*peerAssociation),
		byID:   make(map[uint32]*peerAssociation),
		max:    maxAssociations,
	}
}

func (r *AssociationRegistry) Resolve(peer *net.UDPAddr, now time.Time) (uint32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := peer.String()
	if association := r.byPeer[key]; association != nil {
		association.lastSeen = now
		return association.id, nil
	}
	if len(r.byID) >= r.max {
		return 0, ErrAssociationLimit
	}
	id := r.allocateID()
	association := &peerAssociation{id: id, peer: cloneUDPAddr(peer), lastSeen: now}
	r.byPeer[key] = association
	r.byID[id] = association
	return id, nil
}

func (r *AssociationRegistry) Peer(id uint32, now time.Time) (*net.UDPAddr, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	association := r.byID[id]
	if association == nil {
		return nil, false
	}
	association.lastSeen = now
	return cloneUDPAddr(association.peer), true
}

func (r *AssociationRegistry) Delete(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	association := r.byID[id]
	if association == nil {
		return
	}
	delete(r.byID, id)
	delete(r.byPeer, association.peer.String())
}

func (r *AssociationRegistry) DeleteIfLastSeenBefore(id uint32, cutoff time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	association := r.byID[id]
	if association == nil || association.lastSeen.After(cutoff) {
		return false
	}
	delete(r.byID, id)
	delete(r.byPeer, association.peer.String())
	return true
}

func (r *AssociationRegistry) ExpireBefore(cutoff time.Time) []uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var expired []uint32
	for id, association := range r.byID {
		if association.lastSeen.After(cutoff) {
			continue
		}
		expired = append(expired, id)
		delete(r.byID, id)
		delete(r.byPeer, association.peer.String())
	}
	return expired
}

func (r *AssociationRegistry) allocateID() uint32 {
	for {
		id := r.nextID
		r.nextID++
		if r.nextID == 0 {
			r.nextID = 1
		}
		if id != 0 && r.byID[id] == nil {
			return id
		}
	}
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	ip := make(net.IP, len(addr.IP))
	copy(ip, addr.IP)
	return &net.UDPAddr{IP: ip, Port: addr.Port, Zone: addr.Zone}
}
