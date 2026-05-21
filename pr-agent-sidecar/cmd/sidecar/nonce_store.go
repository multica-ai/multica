package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// PRContext is what the skill needs to do its job. Stored against a one-time
// nonce that lands in the Multica issue body. The skill calls back to swap the
// nonce for a fresh GitHub installation token scoped to InstallationID.
type PRContext struct {
	InstallationID int64
	Owner          string
	Repo           string
	PRNumber       int
	HeadSHA        string
}

type nonceEntry struct {
	ctx       PRContext
	expiresAt time.Time
}

// NonceStore holds short-lived nonce -> PRContext mappings. Single-use:
// Consume returns the entry and deletes it atomically.
type NonceStore struct {
	mu     sync.Mutex
	items  map[string]nonceEntry
	ttl    time.Duration
	stop   chan struct{}
	closed bool
}

func NewNonceStore(ttl time.Duration) *NonceStore {
	s := &NonceStore{
		items: make(map[string]nonceEntry),
		ttl:   ttl,
		stop:  make(chan struct{}),
	}
	go s.sweep()
	return s
}

// Put generates a fresh nonce, stores ctx under it, and returns the nonce.
func (s *NonceStore) Put(ctx PRContext) (string, error) {
	nonce, err := newNonce()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.items[nonce] = nonceEntry{ctx: ctx, expiresAt: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return nonce, nil
}

// Consume returns the entry for nonce and atomically removes it. Returns
// (zero, false) if missing or expired.
func (s *NonceStore) Consume(nonce string) (PRContext, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.items[nonce]
	if !ok {
		return PRContext{}, false
	}
	delete(s.items, nonce)
	if time.Now().After(entry.expiresAt) {
		return PRContext{}, false
	}
	return entry.ctx, true
}

func (s *NonceStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// Close stops the background sweeper. Safe to call multiple times.
func (s *NonceStore) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.stop)
	}
	s.mu.Unlock()
}

func (s *NonceStore) sweep() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.sweepOnce(time.Now())
		}
	}
}

func (s *NonceStore) sweepOnce(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.items {
		if now.After(v.expiresAt) {
			delete(s.items, k)
		}
	}
}

func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
