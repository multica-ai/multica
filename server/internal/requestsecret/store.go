// Package requestsecret is the server-side request-scoped secret-handle store
// (WS-11 P1 front half). A server-authenticated visitor invoke path calls Issue
// to mint an OPAQUE handle bound to (task, authenticated principal), storing the
// short-lived secret server-side. The browser never sees or supplies the
// secret; it only ever receives the opaque handle, which is later placed on the
// task as RequestSecretRef. The daemon resolves the handle to the secret at
// child-launch time.
//
// It lives in its own package (no DB dependency) so its behavior is unit- and
// integration-testable in-repo without a Postgres instance.
//
// Security properties enforced here:
//   - The handle is generated server-side from crypto/rand; a caller cannot
//     choose or forge it.
//   - Every mint binds the handle to the authenticating principal (user id) and
//     the target task id. Consume requires BOTH to match — a different user's
//     claim is rejected (cross-user isolation).
//   - Consume is one-time: the entry is deleted on first successful consume, so
//     a replay returns "not found" (replay rejection).
//   - Entries carry a TTL; expired entries are swept and never consumed.
//   - The secret value is never logged and never returned by Issue (only the
//     handle is). Sentinel errors deliberately avoid the secret.
package requestsecret

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Sentinel errors returned by the store. None carries the secret value.
var (
	ErrNotFound    = errors.New("request secret handle not found or already consumed")
	ErrExpired     = errors.New("request secret handle expired")
	ErrPrincipal   = errors.New("request secret handle principal mismatch")
	ErrTask        = errors.New("request secret handle task mismatch")
	ErrEmptySecret = errors.New("request secret handle refuses empty secret")
	ErrUnbound     = errors.New("request secret handle requires principal and task binding")
)

type entry struct {
	secret      string
	principalID string
	taskID      string
	expiresAt   time.Time
}

// Store is a principal-bound, one-time, TTL-swept handle store. Safe for
// concurrent use.
type Store struct {
	mu       sync.Mutex
	entries  map[string]entry
	ttl      time.Duration
	now      func() time.Time
	randRead func([]byte) (int, error)
}

// DefaultTTL is the fallback lifetime for a minted handle.
const DefaultTTL = 5 * time.Minute

// New returns a store with the given TTL (DefaultTTL when <= 0).
func New(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Store{
		entries:  make(map[string]entry),
		ttl:      ttl,
		now:      time.Now,
		randRead: rand.Read,
	}
}

// Issue mints a new opaque handle for the authenticated principal, bound to
// taskID, storing secret server-side. Returns only the handle — never the
// secret. Fails closed on missing binding or empty secret.
func (s *Store) Issue(principalID, taskID, secret string) (string, error) {
	if principalID == "" || taskID == "" {
		return "", ErrUnbound
	}
	if secret == "" {
		return "", ErrEmptySecret
	}
	buf := make([]byte, 32)
	if _, err := s.randRead(buf); err != nil {
		return "", err
	}
	handle := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.entries[handle] = entry{
		secret:      secret,
		principalID: principalID,
		taskID:      taskID,
		expiresAt:   s.now().Add(s.ttl),
	}
	return handle, nil
}

// Consume validates the handle against the requesting principal and task, then
// returns the secret exactly once. Cross-user or cross-task requests are
// rejected WITHOUT consuming, so a wrong-principal probe cannot burn a
// legitimate one-time handle. Expired entries are rejected and swept.
func (s *Store) Consume(handle, principalID, taskID string) (string, error) {
	if handle == "" {
		return "", ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[handle]
	if !ok {
		return "", ErrNotFound
	}
	if s.now().After(e.expiresAt) {
		delete(s.entries, handle)
		return "", ErrExpired
	}
	if e.principalID != principalID {
		return "", ErrPrincipal
	}
	if e.taskID != taskID {
		return "", ErrTask
	}
	delete(s.entries, handle)
	return e.secret, nil
}

func (s *Store) sweepLocked() {
	now := s.now()
	for h, e := range s.entries {
		if now.After(e.expiresAt) {
			delete(s.entries, h)
		}
	}
}

// Sweep removes expired entries. Safe for a background TTL-cleanup goroutine.
func (s *Store) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
}

// Len reports the current entry count (test/observability helper).
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// SetClock overrides the internal clock (tests only).
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// SetRandReader overrides the entropy source (tests only).
func (s *Store) SetRandReader(r func([]byte) (int, error)) { s.randRead = r }
