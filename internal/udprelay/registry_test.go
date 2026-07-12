package udprelay

import (
	"net"
	"testing"
	"time"
)

func TestAssociationRegistryReusesAndResolvesPeer(t *testing.T) {
	registry := NewAssociationRegistry(8)
	now := time.Now()
	peer := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}

	first, err := registry.Resolve(peer, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Resolve(peer, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if first == 0 || first != second {
		t.Fatalf("association IDs = %d/%d, want one non-zero ID", first, second)
	}
	resolved, ok := registry.Peer(first, now.Add(2*time.Second))
	if !ok || resolved.String() != peer.String() {
		t.Fatalf("Peer(%d) = %v/%v, want %s", first, resolved, ok, peer)
	}
	resolved.Port = 1
	again, _ := registry.Peer(first, now.Add(3*time.Second))
	if again.Port != peer.Port {
		t.Fatal("Peer returned internal address storage instead of a copy")
	}
}

func TestAssociationRegistryExpiresAndDeletes(t *testing.T) {
	registry := NewAssociationRegistry(8)
	now := time.Now()
	oldID, err := registry.Resolve(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1001}, now)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := registry.Resolve(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1002}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	expired := registry.ExpireBefore(now.Add(30 * time.Second))
	if len(expired) != 1 || expired[0] != oldID {
		t.Fatalf("expired = %v, want [%d]", expired, oldID)
	}
	if registry.Len() != 1 {
		t.Fatalf("registry length = %d, want 1", registry.Len())
	}
	registry.Delete(newID)
	if registry.Len() != 0 {
		t.Fatalf("registry length after delete = %d, want 0", registry.Len())
	}
}

func TestAssociationRegistryEnforcesLimitAndProtectsActiveEntries(t *testing.T) {
	registry := NewAssociationRegistry(1)
	now := time.Now()
	id, err := registry.Resolve(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1001}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Resolve(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1002}, now); err != ErrAssociationLimit {
		t.Fatalf("Resolve() error = %v, want ErrAssociationLimit", err)
	}
	if registry.DeleteIfLastSeenBefore(id, now.Add(-time.Second)) {
		t.Fatal("active association should not be deleted by an old close frame")
	}
	if !registry.DeleteIfLastSeenBefore(id, now) {
		t.Fatal("idle association should be deleted at the cutoff")
	}
}
