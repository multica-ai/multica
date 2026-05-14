package inbound

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

// DedupStore is the narrow persistence contract the dedup Step depends
// on. Production wires the adapter returned by NewDBDedupStore (which
// translates the sqlc-generated *db.Queries shape into this contract);
// tests pass a fake satisfying DedupStore directly. Keeping the
// interface narrow — strings in, bool out — keeps the dedup Step
// itself free of pgx / sqlc types and mirrors the pattern used by
// binding.TokenStore.
//
// TryRecordInboundEvent attempts to insert (provider, eventID) into the
// dedup table. It returns inserted=true when the row is newly written
// (the event is being seen for the first time) and inserted=false when
// the row already existed (the platform replayed an event we have
// already processed). The implementation uses INSERT ... ON CONFLICT
// DO NOTHING — the canonical PostgreSQL idiom for "did we just write
// this row?".
type DedupStore interface {
	TryRecordInboundEvent(ctx context.Context, provider, connectionID, eventID string) (bool, error)
}

type DedupOutcomeStore interface {
	MarkInboundEventProcessed(ctx context.Context, connectionID, eventID string) error
	MarkInboundEventFailed(ctx context.Context, connectionID, eventID, lastError string) error
}

// dedupStep is the Step implementation that consults DedupStore on every
// event. It is unexported because callers must construct it via
// NewDedupStep so the *Step interface return type stays stable across
// future refactors.
type dedupStep struct {
	store DedupStore
}

// NewDedupStep returns a Step that consults store on each invocation
// and short-circuits the pipeline when a duplicate is observed. The
// Step uses the event's ChannelName as the dedup table's `provider`
// column and EventID as the dedup key — both fields are populated by
// the adapter layer before the event reaches this Step.
func NewDedupStep(store DedupStore) Step {
	return &dedupStep{store: store}
}

// Name returns the stable telemetry label for this Step.
func (s *dedupStep) Name() string { return "dedup" }

// Run records the (provider, eventID) pair. On a fresh insertion the
// pipeline continues; on a collision it Skips so downstream Steps do
// not re-process a replayed event.
func (s *dedupStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	if evt.ChannelName == "" || evt.EventID == "" {
		return evt, DecisionContinue, errors.New("dedup: missing channel_name or event_id")
	}
	inserted, err := s.store.TryRecordInboundEvent(ctx, evt.ChannelName, evt.ConnectionID(), evt.EventID)
	if err != nil {
		return evt, DecisionContinue, err
	}
	if !inserted {
		return evt, DecisionSkip, nil
	}
	return evt, DecisionContinue, nil
}

func (s *dedupStep) Finalize(ctx context.Context, evt port.InboundEvent, outcome Outcome, runErr error) error {
	outcomes, ok := s.store.(DedupOutcomeStore)
	if !ok || evt.ChannelName == "" || evt.EventID == "" {
		return nil
	}
	if outcome.Terminal == s.Name() && outcome.Decision == DecisionSkip {
		return nil
	}
	if runErr != nil {
		return outcomes.MarkInboundEventFailed(ctx, evt.ConnectionID(), evt.EventID, runErr.Error())
	}
	return outcomes.MarkInboundEventProcessed(ctx, evt.ConnectionID(), evt.EventID)
}

type dedupDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type dbDedupStore struct {
	db dedupDB
}

// NewDBDedupStore wires the production DedupStore against the
// database. Callers pass *pgxpool.Pool or a transaction.
func NewDBDedupStore(db dedupDB) DedupStore {
	return &dbDedupStore{db: db}
}

func (s *dbDedupStore) TryRecordInboundEvent(ctx context.Context, provider, connectionID, eventID string) (bool, error) {
	const q = `
INSERT INTO channel_inbound_event_dedup (provider, connection_id, event_id, status, attempts, processed_at, updated_at)
VALUES ($1, $2, $3, 'processing', 1, now(), now())
ON CONFLICT (connection_id, event_id) DO UPDATE SET
    status = 'processing',
    attempts = channel_inbound_event_dedup.attempts + 1,
    last_error = NULL,
    updated_at = now()
WHERE channel_inbound_event_dedup.status = 'failed'
   OR (
        channel_inbound_event_dedup.status = 'processing'
        AND channel_inbound_event_dedup.updated_at < now() - interval '5 minutes'
   )
RETURNING status
`
	var status string
	err := s.db.QueryRow(ctx, q, provider, connectionID, eventID).Scan(&status)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (s *dbDedupStore) MarkInboundEventProcessed(ctx context.Context, connectionID, eventID string) error {
	const q = `
UPDATE channel_inbound_event_dedup SET
    status = 'processed',
    processed_at = now(),
    updated_at = now(),
    last_error = NULL
WHERE connection_id = $1
  AND event_id = $2
  AND status = 'processing'
`
	_, err := s.db.Exec(ctx, q, connectionID, eventID)
	return err
}

func (s *dbDedupStore) MarkInboundEventFailed(ctx context.Context, connectionID, eventID, lastError string) error {
	const q = `
UPDATE channel_inbound_event_dedup SET
    status = 'failed',
    last_error = $3,
    updated_at = now()
WHERE connection_id = $1
  AND event_id = $2
  AND status = 'processing'
`
	if len(lastError) > 2000 {
		lastError = lastError[:2000]
	}
	_, err := s.db.Exec(ctx, q, connectionID, eventID, lastError)
	if err != nil {
		return fmt.Errorf("dedup: mark failed: %w", err)
	}
	return nil
}

// Compile-time interface conformance: a clear compile-time error here
// is friendlier than a confusing one at every call site if a method
// signature drifts.
var (
	_ Step       = (*dedupStep)(nil)
	_ Finalizer  = (*dedupStep)(nil)
	_ DedupStore = (*dbDedupStore)(nil)
)
