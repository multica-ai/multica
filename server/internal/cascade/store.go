package cascade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/webhooks"
)

// pgPool is the minimal pgx surface the Store needs. Mirrors the
// pgxpool.Pool methods we use without taking a direct dependency on
// the concrete type — lets tests substitute a fake without standing up
// a real database for cases where that is overkill. Real wiring uses
// *pgxpool.Pool, which satisfies this interface naturally.
type pgPool interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store is the data-access façade for the cascade subsystem. Lives in
// internal/cascade (next to Progress) so PR3's GitHub adapter, PR4's
// background worker, PR7's dashboard, and PR8's reconciliation cron
// all import from a single package. Uses raw pgx instead of sqlc
// because (a) the entire cascade pipeline is new code with no
// existing callers, (b) handwriting matches what sqlc would generate
// for these queries closely enough that a future regen-into-sqlc is a
// mechanical refactor, and (c) keeps PR3 self-contained — no need to
// hand-roll a pkg/db/generated/cascade.sql.go file that would diff on
// every `make sqlc` run.
type Store struct {
	pool pgPool
}

// NewStore wraps a pgx pool (or compatible) so the cascade subsystem
// can persist webhook events. Constructed once at server startup and
// passed to webhooks.MountFromEnv.
func NewStore(pool pgPool) *Store {
	return &Store{pool: pool}
}

// ErrRetriggerAlreadyExists is returned by InsertRetrigger when a row
// with the same event_id is already in the table. GitHub re-delivers
// the same webhook on receiver-side 5xx, and the UNIQUE constraint
// on event_id is the idempotency mechanism — the caller should treat
// this as a benign no-op (we already accepted the event).
var ErrRetriggerAlreadyExists = errors.New("cascade: retrigger event_id already persisted")

// RetriggerInsert is the parameter bundle for InsertRetrigger. Mirrors
// the fields the cascade_retrigger schema requires at INSERT time.
// issue_id is intentionally NOT here — PR3 router defers lookup to
// PR4's worker (A6 async decoupling), so this insert path always
// writes NULL into issue_id. The worker fills it in later via
// SetRetriggerIssue.
type RetriggerInsert struct {
	EventID   uuid.UUID
	PRURL     string
	PRNumber  int32
	PRTitle   string
	HeadSHA   string
	Branch    string
	EventType string
	FiredAt   time.Time // optional; zero → DB default now()
}

// insertRetriggerSQL writes NULLIF on pr_title and branch so empty
// strings (the natural Go zero value when the source payload didn't
// expose the field — workflow_run has no title, check_run has neither)
// land as SQL NULL. The worker's lookup loop reads NULL pr_title and
// NULL branch as "no lookup data available — scope skip".
const insertRetriggerSQL = `
INSERT INTO cascade_retrigger (
    event_id, pr_url, pr_number, pr_title, head_sha, branch, event_type, fired_at
) VALUES (
    $1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''), $7, COALESCE($8, now())
)
ON CONFLICT (event_id) DO NOTHING
RETURNING id`

// Insert adapts InsertRetrigger to the webhooks.EventStore contract.
// Returns webhooks.ErrAlreadyExists on idempotent re-delivery so the
// router can sentinel-check without importing the cascade package.
// Direct callers (worker, reconciliation cron) still prefer
// InsertRetrigger for the row-id return value.
func (s *Store) Insert(ctx context.Context, e webhooks.PersistableEvent) error {
	parsed, err := uuid.Parse(e.EventID)
	if err != nil {
		return fmt.Errorf("cascade: parse event id %q: %w", e.EventID, err)
	}
	_, err = s.InsertRetrigger(ctx, RetriggerInsert{
		EventID:   parsed,
		PRURL:     e.PRURL,
		PRNumber:  int32(e.PRNumber),
		PRTitle:   e.PRTitle,
		HeadSHA:   e.HeadSHA,
		Branch:    e.Branch,
		EventType: e.EventType,
		FiredAt:   e.FiredAt,
	})
	if errors.Is(err, ErrRetriggerAlreadyExists) {
		return webhooks.ErrAlreadyExists
	}
	return err
}

// InsertRetrigger persists a webhook event. Returns the row id on
// fresh inserts; ErrRetriggerAlreadyExists when the event_id was
// already present (idempotent re-delivery — caller treats as a 200
// no-op). Any other error is surfaced unchanged.
//
// The `ON CONFLICT (event_id) DO NOTHING ... RETURNING id` pattern
// uses Postgres' "returns no row on conflict" semantics: a successful
// re-insert returns a row, a conflict returns zero rows. We map zero
// rows to ErrRetriggerAlreadyExists so callers can sentinel-check
// without round-tripping the unique-violation pgError.
func (s *Store) InsertRetrigger(ctx context.Context, p RetriggerInsert) (int64, error) {
	var firedAt pgtype.Timestamptz
	if !p.FiredAt.IsZero() {
		firedAt = pgtype.Timestamptz{Time: p.FiredAt, Valid: true}
	}
	row := s.pool.QueryRow(ctx, insertRetriggerSQL,
		pgtype.UUID{Bytes: p.EventID, Valid: true},
		p.PRURL,
		p.PRNumber,
		p.PRTitle,
		p.HeadSHA,
		p.Branch,
		p.EventType,
		firedAt,
	)
	var id int64
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrRetriggerAlreadyExists
		}
		return 0, fmt.Errorf("cascade: insert retrigger: %w", err)
	}
	return id, nil
}
