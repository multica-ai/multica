package selfhosttelemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Fixed across processes and releases. It is session-scoped because the same
// exclusive connection must remain leader through collection, persistence and
// delivery.
const advisoryLockKey int64 = 0x4d554c54454c45

type telemetryState struct {
	InstanceID        uuid.UUID
	LastSuccessfulDay *time.Time
	PendingDay        *time.Time
	PendingBody       []byte
	NextAttemptAt     *time.Time
	AttemptCount      int
}

type leaderStore interface {
	TryLeader(context.Context) (leaderSession, bool, error)
}

type leaderSession interface {
	Connection() *pgx.Conn
	LoadOrCreate(context.Context) (telemetryState, error)
	SavePending(context.Context, time.Time, []byte, time.Time) error
	DropPending(context.Context) error
	MarkSuccessful(context.Context, time.Time) error
	ScheduleRetry(context.Context, time.Time, int) error
	Close() error
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func newPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) TryLeader(ctx context.Context) (leaderSession, bool, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire telemetry connection: %w", err)
	}
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", advisoryLockKey).Scan(&locked); err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("acquire telemetry advisory lock: %w", err)
	}
	if !locked {
		conn.Release()
		return nil, false, nil
	}
	return &postgresSession{conn: conn}, true, nil
}

type postgresSession struct {
	conn      *pgxpool.Conn
	closeOnce sync.Once
	closeErr  error
}

func (s *postgresSession) Connection() *pgx.Conn {
	return s.conn.Conn()
}

func (s *postgresSession) LoadOrCreate(ctx context.Context) (telemetryState, error) {
	// Recreate only after an operator explicitly deletes the singleton row.
	// ON CONFLICT also makes first boot safe if migration/application startup
	// overlap unusually across replicas.
	if _, err := s.conn.Exec(ctx, `
		INSERT INTO instance_telemetry_state (singleton)
		VALUES (true)
		ON CONFLICT (singleton) DO NOTHING`); err != nil {
		return telemetryState{}, fmt.Errorf("ensure telemetry state: %w", err)
	}

	var (
		instanceID                    uuid.UUID
		lastSuccessfulDay, pendingDay pgtype.Date
		nextAttemptAt                 pgtype.Timestamptz
		pendingBody                   []byte
		attemptCount                  int32
	)
	if err := s.conn.QueryRow(ctx, `
		SELECT instance_id, last_successful_day, pending_day, pending_body,
		       next_attempt_at, attempt_count
		  FROM instance_telemetry_state
		 WHERE singleton = true`).Scan(
		&instanceID,
		&lastSuccessfulDay,
		&pendingDay,
		&pendingBody,
		&nextAttemptAt,
		&attemptCount,
	); err != nil {
		return telemetryState{}, fmt.Errorf("load telemetry state: %w", err)
	}

	state := telemetryState{
		InstanceID:   instanceID,
		PendingBody:  append([]byte(nil), pendingBody...),
		AttemptCount: int(attemptCount),
	}
	if lastSuccessfulDay.Valid {
		day := utcDay(lastSuccessfulDay.Time)
		state.LastSuccessfulDay = &day
	}
	if pendingDay.Valid {
		day := utcDay(pendingDay.Time)
		state.PendingDay = &day
	}
	if nextAttemptAt.Valid {
		next := nextAttemptAt.Time.UTC()
		state.NextAttemptAt = &next
	}
	return state, nil
}

func (s *postgresSession) SavePending(ctx context.Context, day time.Time, body []byte, next time.Time) error {
	_, err := s.conn.Exec(ctx, `
		UPDATE instance_telemetry_state
		   SET pending_day = $1, pending_body = $2, next_attempt_at = $3,
		       attempt_count = 0, updated_at = now()
		 WHERE singleton = true`, utcDay(day), body, next.UTC())
	return wrapStoreError("save telemetry pending event", err)
}

func (s *postgresSession) DropPending(ctx context.Context) error {
	_, err := s.conn.Exec(ctx, `
		UPDATE instance_telemetry_state
		   SET pending_day = NULL, pending_body = NULL, next_attempt_at = NULL,
		       attempt_count = 0, updated_at = now()
		 WHERE singleton = true`)
	return wrapStoreError("drop telemetry pending event", err)
}

func (s *postgresSession) MarkSuccessful(ctx context.Context, day time.Time) error {
	_, err := s.conn.Exec(ctx, `
		UPDATE instance_telemetry_state
		   SET last_successful_day = $1, pending_day = NULL, pending_body = NULL,
		       next_attempt_at = NULL, attempt_count = 0, updated_at = now()
		 WHERE singleton = true`, utcDay(day))
	return wrapStoreError("mark telemetry event successful", err)
}

func (s *postgresSession) ScheduleRetry(ctx context.Context, next time.Time, attempt int) error {
	_, err := s.conn.Exec(ctx, `
		UPDATE instance_telemetry_state
		   SET next_attempt_at = $1, attempt_count = $2, updated_at = now()
		 WHERE singleton = true`, next.UTC(), attempt)
	return wrapStoreError("schedule telemetry retry", err)
}

func (s *postgresSession) Close() error {
	s.closeOnce.Do(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var unlocked bool
		err := s.conn.QueryRow(cleanupCtx, "SELECT pg_advisory_unlock($1)", advisoryLockKey).Scan(&unlocked)
		if err == nil && unlocked {
			s.conn.Release()
			return
		}

		// Never return a possibly locked session to the pool. Closing the
		// physical connection releases all PostgreSQL session locks even when
		// the explicit unlock failed during cancellation or a network fault.
		raw := s.conn.Hijack()
		closeErr := raw.Close(cleanupCtx)
		if err == nil && !unlocked {
			err = errors.New("telemetry advisory lock was not held during release")
		}
		s.closeErr = errors.Join(err, closeErr)
	})
	return s.closeErr
}

func wrapStoreError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func utcDay(at time.Time) time.Time {
	year, month, day := at.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
