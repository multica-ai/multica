package cerebra

import (
	"context"
	"sync"
	"time"
)

// SessionPin is the pinned model for a session (issue or chat_session).
type SessionPin struct {
	RuntimeID      string
	Model          string
	Tier           Tier
	UpdatedAt      time.Time
	CreatedAt      time.Time
	EscalatedTurns int // consecutive turns served at escalated tier without re-triggering
}

// SessionStore manages session affinity for the router. It pins a model to a
// session when the router first selects one, and reuses it for subsequent
// requests at the same tier (refreshing the TTL on every hit).
//
// Security & Cost Controls:
//   - MaxPins: caps in-memory pin count to prevent memory exhaustion (evicts oldest).
//   - MaxEscalatedTurns: bounds sticky TierHeavy escalation turns before decaying to natural tier.
//   - MaxPinLifetime: absolute upper bound on a session pin, preventing infinite keep-alive.
type SessionStore struct {
	mu                sync.Mutex
	pins              map[string]*SessionPin // key: sessionKey(issueID, sessionID)
	ttl               time.Duration
	maxPins           int
	maxEscalatedTurns int
	maxPinLifetime    time.Duration
}

// Default session affinity constants.
const (
	DefaultSessionTTL        = 2 * time.Hour
	DefaultMaxPins           = 5000
	DefaultMaxEscalatedTurns = 5
	DefaultMaxPinLifetime    = 24 * time.Hour
)

// NewSessionStore creates a SessionStore with the given TTL.
func NewSessionStore(ttl time.Duration) *SessionStore {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &SessionStore{
		pins:              make(map[string]*SessionPin),
		ttl:               ttl,
		maxPins:           DefaultMaxPins,
		maxEscalatedTurns: DefaultMaxEscalatedTurns,
		maxPinLifetime:    DefaultMaxPinLifetime,
	}
}

// SetLimits configures capacity and turn limits for session affinity.
func (s *SessionStore) SetLimits(maxPins, maxEscalatedTurns int, maxLifetime time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxPins > 0 {
		s.maxPins = maxPins
	}
	if maxEscalatedTurns > 0 {
		s.maxEscalatedTurns = maxEscalatedTurns
	}
	if maxLifetime > 0 {
		s.maxPinLifetime = maxLifetime
	}
}

// Count returns the number of active entries in the session store.
func (s *SessionStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pins)
}

// Get returns the current pin for the session, or nil if absent/expired.
func (s *SessionStore) Get(_ context.Context, issueID, sessionID string) *SessionPin {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(issueID, sessionID)
	pin, ok := s.pins[key]
	if !ok {
		return nil
	}
	now := time.Now()
	if now.Sub(pin.UpdatedAt) > s.ttl || (s.maxPinLifetime > 0 && now.Sub(pin.CreatedAt) > s.maxPinLifetime) {
		delete(s.pins, key)
		return nil
	}
	return pin
}

// Set pins a model to the session. If the session already has an active higher-tier pin,
// this call preserves the higher-tier pin (sticky escalation) up to maxEscalatedTurns.
// If the turn limit is exceeded, or the requested tier is equal/higher, the pin updates.
func (s *SessionStore) Set(_ context.Context, issueID, sessionID, runtimeID, model string, tier Tier) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(issueID, sessionID)
	existing, ok := s.pins[key]
	now := time.Now()

	if ok && now.Sub(existing.UpdatedAt) <= s.ttl && (s.maxPinLifetime == 0 || now.Sub(existing.CreatedAt) <= s.maxPinLifetime) {
		if tierRank(existing.Tier) > tierRank(tier) {
			// Existing pin is at a higher tier.
			existing.EscalatedTurns++
			if s.maxEscalatedTurns <= 0 || existing.EscalatedTurns <= s.maxEscalatedTurns {
				existing.UpdatedAt = now
				return
			}
			// Escalation turn quota reached — decay back to requested tier to prevent cost explosion.
		} else if tierRank(tier) >= tierRank(existing.Tier) {
			// Request naturally met or exceeded current tier — reset turn counter.
			existing.EscalatedTurns = 0
		}
	}

	// Capacity guard: evict expired or oldest pin if map is full
	if len(s.pins) >= s.maxPins && !ok {
		s.evictOverCapacityLocked(now)
	}

	createdAt := now
	if ok && !existing.CreatedAt.IsZero() {
		createdAt = existing.CreatedAt
	}

	s.pins[key] = &SessionPin{
		RuntimeID:      runtimeID,
		Model:          model,
		Tier:           tier,
		UpdatedAt:      now,
		CreatedAt:      createdAt,
		EscalatedTurns: 0,
	}
}

func (s *SessionStore) evictOverCapacityLocked(now time.Time) {
	// 1. Evict expired
	for k, p := range s.pins {
		if now.Sub(p.UpdatedAt) > s.ttl || (s.maxPinLifetime > 0 && now.Sub(p.CreatedAt) > s.maxPinLifetime) {
			delete(s.pins, k)
		}
	}
	if len(s.pins) < s.maxPins {
		return
	}
	// 2. Evict oldest
	var oldestKey string
	var oldestTime time.Time
	for k, p := range s.pins {
		if oldestKey == "" || p.UpdatedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = p.UpdatedAt
		}
	}
	if oldestKey != "" {
		delete(s.pins, oldestKey)
	}
}

// Refresh updates the TTL timestamp for an existing pin without changing the model.
func (s *SessionStore) Refresh(_ context.Context, issueID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := sessionKey(issueID, sessionID)
	if pin, ok := s.pins[key]; ok {
		pin.UpdatedAt = time.Now()
	}
}

// Delete removes the pin for a session (e.g. when the session ends).
func (s *SessionStore) Delete(_ context.Context, issueID, sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pins, sessionKey(issueID, sessionID))
}

func sessionKey(issueID, sessionID string) string {
	if sessionID != "" {
		return "session:" + sessionID
	}
	return "issue:" + issueID
}
