package tagaccess

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type HTTPAssertionReplay struct {
	Issuer    string
	Audience  string
	RequestID string
	Nonce     string
	ExpiresAt time.Time
}

type HTTPAssertionReplayStore interface {
	// Consume returns true exactly once for an issuer/audience/request/nonce
	// tuple until its expiry. Errors must be treated as authority outages.
	Consume(context.Context, HTTPAssertionReplay) (bool, error)
}

type memoryHTTPAssertionReplayStore struct {
	mu      sync.Mutex
	clock   Clock
	claimed map[httpAssertionReplayKey]time.Time
}

type httpAssertionReplayKey struct {
	issuer    string
	audience  string
	requestID string
	nonce     string
}

func NewMemoryHTTPAssertionReplayStore(clock Clock) (HTTPAssertionReplayStore, error) {
	if !configuredDependency(clock) {
		return nil, ErrInvalidHTTPAssertion
	}
	return &memoryHTTPAssertionReplayStore{clock: clock, claimed: make(map[httpAssertionReplayKey]time.Time)}, nil
}

func (s *memoryHTTPAssertionReplayStore) Consume(_ context.Context, claim HTTPAssertionReplay) (bool, error) {
	if s == nil || !configuredDependency(s.clock) || !validHTTPAssertionReplay(claim, s.clock.Now()) {
		return false, ErrInvalidHTTPAssertion
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	for key, expiresAt := range s.claimed {
		if !expiresAt.After(now) {
			delete(s.claimed, key)
		}
	}
	key := httpAssertionReplayKey{claim.Issuer, claim.Audience, claim.RequestID, claim.Nonce}
	if expiresAt, exists := s.claimed[key]; exists && expiresAt.After(now) {
		return false, nil
	}
	s.claimed[key] = claim.ExpiresAt
	return true, nil
}

type httpAssertionReplayDB interface {
	Begin(context.Context) (pgx.Tx, error)
}

type postgresHTTPAssertionReplayStore struct {
	db    httpAssertionReplayDB
	clock Clock
}

func NewPostgresHTTPAssertionReplayStore(db httpAssertionReplayDB, clock Clock) (HTTPAssertionReplayStore, error) {
	if !configuredDependency(db) || !configuredDependency(clock) {
		return nil, ErrInvalidHTTPAssertion
	}
	return &postgresHTTPAssertionReplayStore{db: db, clock: clock}, nil
}

func (s *postgresHTTPAssertionReplayStore) Consume(ctx context.Context, claim HTTPAssertionReplay) (bool, error) {
	if s == nil || !configuredDependency(s.db) || !configuredDependency(s.clock) || !validHTTPAssertionReplay(claim, s.clock.Now()) {
		return false, ErrInvalidHTTPAssertion
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := s.clock.Now()
	if _, err := tx.Exec(ctx, `
		WITH expired AS (
			SELECT ctid FROM tag_http_assertion_replay
			WHERE expires_at <= $1
			ORDER BY expires_at
			LIMIT 128
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM tag_http_assertion_replay AS replay
		USING expired
		WHERE replay.ctid = expired.ctid
	`, now); err != nil {
		return false, err
	}
	command, err := tx.Exec(ctx, `
		INSERT INTO tag_http_assertion_replay (
			issuer, audience, request_id, nonce, expires_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (issuer, audience, request_id, nonce) DO NOTHING
	`, claim.Issuer, claim.Audience, claim.RequestID, claim.Nonce, claim.ExpiresAt)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return command.RowsAffected() == 1, nil
}

func validHTTPAssertionReplay(claim HTTPAssertionReplay, now time.Time) bool {
	return claim.Issuer == HTTPAssertionIssuer && claim.Audience == HTTPAssertionAudience &&
		httpAssertionSafeID.MatchString(claim.RequestID) && httpAssertionSafeID.MatchString(claim.Nonce) && claim.ExpiresAt.After(now)
}

var _ HTTPAssertionReplayStore = (*memoryHTTPAssertionReplayStore)(nil)
var _ HTTPAssertionReplayStore = (*postgresHTTPAssertionReplayStore)(nil)
