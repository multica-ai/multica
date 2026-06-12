// TECH-3422
package presence

import (
	"sort"
	"testing"
	"time"
)

// newTestTracker returns a tracker whose clock is driven by a caller-controlled
// pointer, so tests advance time without sleeping.
func newTestTracker(ttl time.Duration, clock *time.Time) *Tracker {
	tr := NewTracker(ttl)
	tr.now = func() time.Time { return *clock }
	return tr
}

func TestTouchBecameOnline(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		advance   time.Duration // time to add before the second touch
		wantFirst bool
		wantSec   bool
	}{
		{
			name:      "first touch is a transition",
			advance:   1 * time.Second,
			wantFirst: true,
			wantSec:   false, // still inside TTL -> no new transition
		},
		{
			name:      "re-touch after TTL is a fresh transition",
			advance:   61 * time.Second,
			wantFirst: true,
			wantSec:   true, // aged past TTL -> online again
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clock := base
			tr := newTestTracker(60*time.Second, &clock)

			if got := tr.Touch("ws", "user"); got != tc.wantFirst {
				t.Fatalf("first Touch becameOnline = %v, want %v", got, tc.wantFirst)
			}
			clock = clock.Add(tc.advance)
			if got := tr.Touch("ws", "user"); got != tc.wantSec {
				t.Fatalf("second Touch becameOnline = %v, want %v", got, tc.wantSec)
			}
		})
	}
}

func TestOnlineUserIDsRespectsTTL(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := base
	tr := newTestTracker(60*time.Second, &clock)

	tr.Touch("ws", "alice")
	clock = clock.Add(30 * time.Second)
	tr.Touch("ws", "bob")

	// At +30s both are within the 60s window.
	if got := sortedOnline(tr, "ws"); !equal(got, []string{"alice", "bob"}) {
		t.Fatalf("online at +30s = %v, want [alice bob]", got)
	}

	// Advance so alice (last seen at +0s) ages out but bob (at +30s) survives.
	clock = clock.Add(31 * time.Second) // now +61s
	if got := sortedOnline(tr, "ws"); !equal(got, []string{"bob"}) {
		t.Fatalf("online at +61s = %v, want [bob]", got)
	}

	// A different workspace is isolated.
	if got := tr.OnlineUserIDs("other"); len(got) != 0 {
		t.Fatalf("online for empty workspace = %v, want []", got)
	}
}

func TestSweepRemovesAgedUsers(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := base
	tr := newTestTracker(60*time.Second, &clock)

	tr.Touch("ws", "alice")
	clock = clock.Add(30 * time.Second)
	tr.Touch("ws", "bob")

	// Advance past alice's TTL but not bob's.
	clock = clock.Add(31 * time.Second) // alice at +61s, bob at +31s

	transitions := tr.Sweep()
	if len(transitions) != 1 {
		t.Fatalf("sweep returned %d transitions, want 1", len(transitions))
	}
	if transitions[0].WorkspaceID != "ws" || transitions[0].UserID != "alice" {
		t.Fatalf("sweep transition = %+v, want {ws alice}", transitions[0])
	}

	// alice is gone; bob remains.
	if got := sortedOnline(tr, "ws"); !equal(got, []string{"bob"}) {
		t.Fatalf("online after sweep = %v, want [bob]", got)
	}

	// Re-touching the swept user is a fresh online transition.
	if got := tr.Touch("ws", "alice"); !got {
		t.Fatalf("Touch after sweep becameOnline = false, want true")
	}
}

func TestSweepPrunesEmptyWorkspace(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := base
	tr := newTestTracker(60*time.Second, &clock)

	tr.Touch("ws", "only")
	clock = clock.Add(61 * time.Second)

	if got := tr.Sweep(); len(got) != 1 {
		t.Fatalf("sweep returned %d transitions, want 1", len(got))
	}

	tr.mu.RLock()
	_, present := tr.lastSeen["ws"]
	tr.mu.RUnlock()
	if present {
		t.Fatalf("empty workspace bucket was not pruned")
	}

	// A second sweep with nothing aged returns no transitions.
	if got := tr.Sweep(); len(got) != 0 {
		t.Fatalf("second sweep returned %d transitions, want 0", len(got))
	}
}

func sortedOnline(tr *Tracker, ws string) []string {
	out := tr.OnlineUserIDs(ws)
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
