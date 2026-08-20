package wecom

// senders_registry_test.go — Connect installs a sender on entry and clears it
// on a defer. When a lease flips while the old socket is still draining, the
// two generations overlap and the loser's defer runs AFTER the winner's set.
// An unconditional delete there evicts a live connection.

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func registryUUID(b byte) pgtype.UUID { return pgtype.UUID{Bytes: [16]byte{b}, Valid: true} }

// TestClearDoesNotEvictASuccessor is the handover: generation 2 takes the
// installation over, then generation 1's deferred clear finally runs. The
// registry must still hold generation 2.
func TestClearDoesNotEvictASuccessor(t *testing.T) {
	reg := newSendersRegistry()
	id := registryUUID(1)

	first := newWSSender(&recordingConn{}, nil)
	second := newWSSender(&recordingConn{}, nil)

	reg.set(id, first)
	reg.set(id, second) // the lease moved; a new connection took over

	reg.clear(id, first) // generation 1's defer, running late

	got := reg.get(id)
	if got == nil {
		t.Fatal("the successor was evicted by its predecessor's deferred clear — every outbound push now resolves to nil and the bot is silent")
	}
	if got != second {
		t.Fatalf("registry holds the wrong sender after handover")
	}
}

// TestClearRemovesItsOwnSender is the ordinary path, and the reason the fence
// cannot simply be "never delete": a connection that really is going away has
// to leave, or the registry hands out a sender whose socket is dead.
func TestClearRemovesItsOwnSender(t *testing.T) {
	reg := newSendersRegistry()
	id := registryUUID(1)

	only := newWSSender(&recordingConn{}, nil)
	reg.set(id, only)
	reg.clear(id, only)

	if got := reg.get(id); got != nil {
		t.Fatal("a sender that owned the slot was not removed; outbound would be dispatched to a dead socket")
	}
}

// TestClearOfAnUnknownInstallationIsANoop: a Connect that fails before set
// still runs its defer.
func TestClearOfAnUnknownInstallationIsANoop(t *testing.T) {
	reg := newSendersRegistry()
	live := newWSSender(&recordingConn{}, nil)
	reg.set(registryUUID(1), live)

	reg.clear(registryUUID(2), newWSSender(&recordingConn{}, nil))

	if got := reg.get(registryUUID(1)); got != live {
		t.Fatal("clearing an unrelated installation disturbed a live one")
	}
}
