// TECH-3422
// Package presence implements an in-memory member-presence service. A logged-in
// human "pings" periodically; we track who is online per workspace and broadcast
// online/offline transitions over the websocket so other members' UIs can show
// live presence indicators.
package presence

import (
	"sync"
	"time"
)

// DefaultTTL is the window within which a member is considered online after
// their last ping. A member who has not pinged within this window is swept
// offline.
const DefaultTTL = 60 * time.Second

// Transition describes a member-presence change the caller should broadcast.
// Sweep returns one Transition per user that aged past the TTL and was removed.
type Transition struct {
	WorkspaceID string
	UserID      string
}

// Tracker holds the last-seen timestamp for every online member, partitioned
// by workspace. It is safe for concurrent use.
type Tracker struct {
	mu sync.RWMutex
	// lastSeen maps workspaceID -> userID -> last-seen instant.
	lastSeen map[string]map[string]time.Time
	ttl      time.Duration

	// now is injectable so tests can advance time without sleeping. Defaults
	// to time.Now in NewTracker.
	now func() time.Time
}

// NewTracker builds a Tracker with the given TTL. A non-positive ttl falls back
// to DefaultTTL.
func NewTracker(ttl time.Duration) *Tracker {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Tracker{
		lastSeen: make(map[string]map[string]time.Time),
		ttl:      ttl,
		now:      time.Now,
	}
}

// Touch records that userID is alive in workspaceID as of now. It returns
// becameOnline=true when the user was previously offline or absent — i.e. when
// the caller should broadcast an online transition. A repeat ping inside the
// TTL window returns false (no transition).
func (t *Tracker) Touch(workspaceID, userID string) (becameOnline bool) {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()

	users := t.lastSeen[workspaceID]
	if users == nil {
		users = make(map[string]time.Time)
		t.lastSeen[workspaceID] = users
	}

	prev, existed := users[userID]
	// A user counts as "online" only if their previous last-seen is still
	// within the TTL window. An aged-but-not-yet-swept entry is treated as
	// offline, so re-touching it is a fresh online transition.
	wasOnline := existed && now.Sub(prev) <= t.ttl

	users[userID] = now
	return !wasOnline
}

// OnlineUserIDs returns the user IDs in workspaceID whose last-seen is within
// the TTL window. The result order is unspecified.
func (t *Tracker) OnlineUserIDs(workspaceID string) []string {
	now := t.now()
	t.mu.RLock()
	defer t.mu.RUnlock()

	users := t.lastSeen[workspaceID]
	out := make([]string, 0, len(users))
	for userID, seen := range users {
		if now.Sub(seen) <= t.ttl {
			out = append(out, userID)
		}
	}
	return out
}

// Sweep removes every user whose last-seen has aged past the TTL and returns a
// Transition for each removed user so the caller can broadcast offline. Empty
// workspace buckets are pruned to keep the map from growing unbounded.
func (t *Tracker) Sweep() []Transition {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()

	var transitions []Transition
	for workspaceID, users := range t.lastSeen {
		for userID, seen := range users {
			if now.Sub(seen) > t.ttl {
				delete(users, userID)
				transitions = append(transitions, Transition{
					WorkspaceID: workspaceID,
					UserID:      userID,
				})
			}
		}
		if len(users) == 0 {
			delete(t.lastSeen, workspaceID)
		}
	}
	return transitions
}
