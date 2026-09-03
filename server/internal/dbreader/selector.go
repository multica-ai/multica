package dbreader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	DefaultProbeInterval = 2 * time.Second
	DefaultProbeTimeout  = time.Second
	DefaultMaxReplayLag  = 5 * time.Second
)

type Business string

type Consistency string

const (
	StrongConsistency   Consistency = "strong"
	EventualConsistency Consistency = "eventual"
)

type Role string

const (
	RolePrimary Role = "primary"
	RoleReplica Role = "replica"
)

type Reason string

const (
	ReasonStrongConsistency Reason = "strong_consistency"
	ReasonReplicaDisabled   Reason = "replica_disabled"
	ReasonInitializing      Reason = "initializing"
	ReasonHealthy           Reason = "healthy"
	ReasonProbeFailed       Reason = "probe_failed"
	ReasonNotStandby        Reason = "not_standby"
	ReasonNotReadOnly       Reason = "not_read_only"
	ReasonReplayUnknown     Reason = "replay_unknown"
	ReasonDatabaseMismatch  Reason = "database_mismatch"
	ReasonReplayLag         Reason = "replay_lag"
	ReasonQueryFailed       Reason = "query_failed"
)

// Recorder is implemented by the optional Prometheus metrics sink. The
// selector stays usable when metrics are disabled by accepting nil.
type Recorder interface {
	SetReplicaConfigured(configured bool)
	SetReplicaStatus(healthy bool, lagBytes int64, replayLag time.Duration)
	ObserveReplicaProbe(healthy bool, reason string)
	RecordReadRoute(business, role, reason string)
	RecordReadFallback(business, reason string)
}

type Config struct {
	ProbeInterval time.Duration
	ProbeTimeout  time.Duration
	MaxReplayLag  time.Duration
	Logger        *slog.Logger
}

func (c Config) normalized() Config {
	if c.ProbeInterval <= 0 {
		c.ProbeInterval = DefaultProbeInterval
	}
	if c.ProbeTimeout <= 0 {
		c.ProbeTimeout = DefaultProbeTimeout
	}
	if c.MaxReplayLag <= 0 {
		c.MaxReplayLag = DefaultMaxReplayLag
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

type Status struct {
	Configured bool
	Healthy    bool
	Reason     Reason
	LagBytes   int64
	ReplayLag  time.Duration
	CheckedAt  time.Time
}

type Selection struct {
	Queries *db.Queries
	Role    Role
	Reason  Reason
}

type probeSnapshot struct {
	primaryDatabase string
	replicaDatabase string
	inRecovery      bool
	readOnly        bool
	replayKnown     bool
	lagBytes        int64
	replayLag       time.Duration
}

type probeFunc func(context.Context) (probeSnapshot, error)

// Selector keeps primary as the safe default. A configured replica is not
// eligible until a probe proves that it is a read-only standby within the lag
// budget. Existing callers continue to use their primary *db.Queries directly;
// only explicitly opted-in read paths should call Select or Read.
type Selector struct {
	primary    *db.Queries
	replica    *db.Queries
	config     Config
	probe      probeFunc
	recorder   Recorder
	configured bool
	state      atomic.Pointer[Status]
	now        func() time.Time
}

func New(primaryPool, replicaPool *pgxpool.Pool, cfg Config, recorder Recorder) *Selector {
	primary := db.New(primaryPool)
	if replicaPool == nil {
		return newSelector(primary, nil, cfg, nil, recorder)
	}
	return newSelector(primary, db.New(replicaPool), cfg, newSQLProbe(primaryPool, replicaPool), recorder)
}

func NewPrimaryOnly(primary *db.Queries) *Selector {
	return newSelector(primary, nil, Config{}, nil, nil)
}

func newSelector(primary, replica *db.Queries, cfg Config, probe probeFunc, recorder Recorder) *Selector {
	cfg = cfg.normalized()
	s := &Selector{
		primary:    primary,
		replica:    replica,
		config:     cfg,
		probe:      probe,
		recorder:   recorder,
		configured: replica != nil,
		now:        time.Now,
	}
	initial := Status{Reason: ReasonReplicaDisabled}
	if s.configured {
		initial.Configured = true
		initial.Reason = ReasonInitializing
	}
	s.state.Store(&initial)
	if recorder != nil {
		recorder.SetReplicaConfigured(s.configured)
	}
	return s
}

func (s *Selector) Status() Status {
	status := s.state.Load()
	if status == nil {
		return Status{Reason: ReasonReplicaDisabled}
	}
	return *status
}

func (s *Selector) Select(business Business, consistency Consistency) Selection {
	if consistency != EventualConsistency {
		return s.selection(business, s.primary, RolePrimary, ReasonStrongConsistency)
	}
	status := s.Status()
	if !status.Configured {
		return s.selection(business, s.primary, RolePrimary, ReasonReplicaDisabled)
	}
	if !status.Healthy {
		return s.selection(business, s.primary, RolePrimary, status.Reason)
	}
	return s.selection(business, s.replica, RoleReplica, ReasonHealthy)
}

func (s *Selector) selection(business Business, queries *db.Queries, role Role, reason Reason) Selection {
	if s.recorder != nil {
		s.recorder.RecordReadRoute(string(business), string(role), string(reason))
	}
	return Selection{Queries: queries, Role: role, Reason: reason}
}

// Read executes a read against the selected pool. A transport failure or a
// standby serialization conflict immediately ejects the replica and retries
// the explicitly idempotent read once on primary. SQL, permission, read-only,
// and caller cancellation errors are returned unchanged so routing cannot hide
// application bugs or exceed the caller's latency budget.
func Read[T any](
	ctx context.Context,
	selector *Selector,
	business Business,
	consistency Consistency,
	query func(context.Context, *db.Queries) (T, error),
) (T, error) {
	selection := selector.Select(business, consistency)
	result, err := query(ctx, selection.Queries)
	if err == nil || selection.Role != RoleReplica || !shouldFallback(ctx, err) {
		return result, err
	}

	selector.markUnhealthy(ReasonQueryFailed, err)
	if selector.recorder != nil {
		selector.recorder.RecordReadFallback(string(business), string(ReasonQueryFailed))
	}
	return query(ctx, selector.primary)
}

func shouldFallback(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if pgconn.SafeToRetry(err) {
		return true
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	if strings.HasPrefix(pgErr.Code, "08") {
		return true
	}
	switch pgErr.Code {
	case "40001", "57P01", "57P02", "57P03":
		return true
	default:
		return false
	}
}

func (s *Selector) Run(ctx context.Context) {
	if !s.configured {
		return
	}
	s.ProbeNow(ctx)
	ticker := time.NewTicker(s.config.ProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ProbeNow(ctx)
		}
	}
}

func (s *Selector) ProbeNow(ctx context.Context) Status {
	if !s.configured || s.probe == nil {
		return s.Status()
	}
	probeCtx, cancel := context.WithTimeout(ctx, s.config.ProbeTimeout)
	snapshot, err := s.probe(probeCtx)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return s.Status()
		}
		status := s.storeStatus(Status{
			Configured: true,
			Reason:     ReasonProbeFailed,
			CheckedAt:  s.now(),
		}, err)
		s.observeProbe(status)
		return status
	}
	status := classify(snapshot, s.config.MaxReplayLag)
	status.Configured = true
	status.CheckedAt = s.now()
	status = s.storeStatus(status, nil)
	s.observeProbe(status)
	return status
}

func classify(snapshot probeSnapshot, maxReplayLag time.Duration) Status {
	status := Status{
		Healthy:   false,
		Reason:    ReasonHealthy,
		LagBytes:  max(snapshot.lagBytes, 0),
		ReplayLag: max(snapshot.replayLag, 0),
	}
	switch {
	case !snapshot.inRecovery:
		status.Reason = ReasonNotStandby
	case !snapshot.readOnly:
		status.Reason = ReasonNotReadOnly
	case !snapshot.replayKnown:
		status.Reason = ReasonReplayUnknown
	case snapshot.primaryDatabase != snapshot.replicaDatabase:
		status.Reason = ReasonDatabaseMismatch
	case status.LagBytes > 0 && status.ReplayLag > maxReplayLag:
		status.Reason = ReasonReplayLag
	default:
		status.Healthy = true
	}
	return status
}

func (s *Selector) markUnhealthy(reason Reason, err error) {
	status := s.Status()
	status.Healthy = false
	status.Reason = reason
	status.CheckedAt = s.now()
	s.storeStatus(status, err)
}

func (s *Selector) storeStatus(next Status, err error) Status {
	previous := s.Status()
	s.state.Store(&next)
	if s.recorder != nil {
		s.recorder.SetReplicaStatus(next.Healthy, next.LagBytes, next.ReplayLag)
	}
	if previous.Healthy == next.Healthy && previous.Reason == next.Reason {
		return next
	}
	fields := []any{
		"healthy", next.Healthy,
		"reason", next.Reason,
		"lag_bytes", next.LagBytes,
		"replay_lag", next.ReplayLag.String(),
	}
	if err != nil {
		fields = append(fields, "error", err)
	}
	if next.Healthy {
		s.config.Logger.Info("database replica became eligible for reads", fields...)
	} else {
		s.config.Logger.Warn("database replica is ineligible for reads; using primary", fields...)
	}
	return next
}

func (s *Selector) observeProbe(status Status) {
	if s.recorder != nil {
		s.recorder.ObserveReplicaProbe(status.Healthy, string(status.Reason))
	}
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func newSQLProbe(primary, replica queryRower) probeFunc {
	return func(ctx context.Context) (probeSnapshot, error) {
		var snapshot probeSnapshot
		var primaryLSN string
		if err := primary.QueryRow(ctx, `SELECT current_database(), pg_current_wal_lsn()::text`).Scan(
			&snapshot.primaryDatabase,
			&primaryLSN,
		); err != nil {
			return probeSnapshot{}, fmt.Errorf("read primary WAL position: %w", err)
		}

		var replayLagSeconds float64
		if err := replica.QueryRow(ctx, `
SELECT
    current_database(),
    pg_is_in_recovery(),
    current_setting('transaction_read_only') = 'on',
    pg_last_wal_replay_lsn() IS NOT NULL,
    COALESCE(
        GREATEST(pg_wal_lsn_diff($1::pg_lsn, pg_last_wal_replay_lsn()), 0),
        0
    )::bigint,
    CASE
        WHEN pg_last_wal_replay_lsn() IS NULL
          OR pg_last_wal_replay_lsn() >= $1::pg_lsn THEN 0
        ELSE COALESCE(
            EXTRACT(EPOCH FROM (clock_timestamp() - pg_last_xact_replay_timestamp())),
            0
        )
    END::double precision
`, primaryLSN).Scan(
			&snapshot.replicaDatabase,
			&snapshot.inRecovery,
			&snapshot.readOnly,
			&snapshot.replayKnown,
			&snapshot.lagBytes,
			&replayLagSeconds,
		); err != nil {
			return probeSnapshot{}, fmt.Errorf("read replica recovery position: %w", err)
		}
		snapshot.replayLag = time.Duration(replayLagSeconds * float64(time.Second))
		return snapshot, nil
	}
}
