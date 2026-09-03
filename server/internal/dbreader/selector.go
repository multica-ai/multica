package dbreader

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// Availability failures prove that the replica cannot serve this request,
	// so one failure is enough to stop making every caller pay the connection
	// timeout. The read callback is explicitly idempotent, so the failed attempt
	// can be retried once on primary.
	defaultReplicaCircuitFailureThreshold = 1

	// A two-second cooldown caps failed replica connection attempts at one per
	// API process per interval while restoring replica traffic quickly. Recovery
	// is driven by one half-open business read, not a background database query.
	defaultReplicaCircuitCooldown = 2 * time.Second
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
	ReasonReplicaSelected   Reason = "replica_selected"
	ReasonCircuitOpen       Reason = "circuit_open"
	ReasonConnectionFailed  Reason = "connection_failed"
	ReasonRecoveryConflict  Reason = "recovery_conflict"
)

// Recorder is implemented by the optional Prometheus metrics sink. The
// selector stays usable when metrics are disabled by accepting nil.
type Recorder interface {
	RecordReadRoute(business, role, reason string)
}

// Selector keeps primary as the safe default. Existing handlers continue to
// use their primary *db.Queries directly; an eventual-consistency read must
// explicitly use Read, which owns replica selection, fallback, and recovery.
type Selector struct {
	primary  *db.Queries
	replica  *db.Queries
	recorder Recorder
	logger   *slog.Logger
	now      func() time.Time
	circuit  replicaCircuit
}

type replicaCircuit struct {
	mu                  sync.Mutex
	generation          uint64
	consecutiveFailures int
	openUntil           time.Time
	halfOpenInFlight    bool
}

type selection struct {
	queries    *db.Queries
	role       Role
	reason     Reason
	generation uint64
	halfOpen   bool
}

type fallbackDecision struct {
	retry       bool
	openCircuit bool
	reason      Reason
}

func New(primaryPool, replicaPool *pgxpool.Pool, recorder Recorder) *Selector {
	primary := db.New(primaryPool)
	if replicaPool == nil {
		return newSelector(primary, nil, recorder, slog.Default())
	}
	return newSelector(primary, db.New(replicaPool), recorder, slog.Default())
}

func NewPrimaryOnly(primary *db.Queries) *Selector {
	return newSelector(primary, nil, nil, slog.Default())
}

func newSelector(primary, replica *db.Queries, recorder Recorder, logger *slog.Logger) *Selector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Selector{
		primary:  primary,
		replica:  replica,
		recorder: recorder,
		logger:   logger,
		now:      time.Now,
	}
}

// Read executes one explicitly idempotent read. Strong-consistency and
// unconfigured reads use primary. Eventual-consistency reads use replica when
// its passive circuit permits, then retry once on primary for connection,
// transport, server-availability, or standby recovery-conflict errors. SQL,
// permission, and caller-cancellation errors are returned unchanged.
func Read[T any](
	ctx context.Context,
	selector *Selector,
	business Business,
	consistency Consistency,
	query func(context.Context, *db.Queries) (T, error),
) (T, error) {
	selected := selector.selectForRead(business, consistency)
	result, err := query(ctx, selected.queries)
	if selected.role != RoleReplica {
		return result, err
	}
	if err == nil {
		selector.replicaSucceeded(selected)
		return result, nil
	}

	decision := fallbackFor(ctx, err)
	if !decision.retry {
		selector.replicaDidNotProveAvailability(selected, err)
		return result, err
	}
	if decision.openCircuit {
		selector.replicaFailed(selected, decision.reason, err)
	} else {
		// A structured server response proves the connection is usable. A
		// recovery conflict affects only this query and must not eject the
		// replica globally.
		selector.replicaSucceeded(selected)
	}

	selector.recordRoute(business, RolePrimary, decision.reason)
	return query(ctx, selector.primary)
}

func (s *Selector) selectForRead(business Business, consistency Consistency) selection {
	if consistency != EventualConsistency {
		return s.route(business, s.primary, RolePrimary, ReasonStrongConsistency, 0, false)
	}
	if s.replica == nil {
		return s.route(business, s.primary, RolePrimary, ReasonReplicaDisabled, 0, false)
	}

	allowed, generation, halfOpen := s.circuit.allow(s.now())
	if !allowed {
		return s.route(business, s.primary, RolePrimary, ReasonCircuitOpen, generation, false)
	}
	return s.route(business, s.replica, RoleReplica, ReasonReplicaSelected, generation, halfOpen)
}

func (s *Selector) route(
	business Business,
	queries *db.Queries,
	role Role,
	reason Reason,
	generation uint64,
	halfOpen bool,
) selection {
	s.recordRoute(business, role, reason)
	return selection{
		queries:    queries,
		role:       role,
		reason:     reason,
		generation: generation,
		halfOpen:   halfOpen,
	}
}

func (s *Selector) recordRoute(business Business, role Role, reason Reason) {
	if s.recorder != nil {
		s.recorder.RecordReadRoute(string(business), string(role), string(reason))
	}
}

func fallbackFor(ctx context.Context, err error) fallbackDecision {
	if err == nil || ctx.Err() != nil {
		return fallbackDecision{}
	}

	// ConnectError covers the failures that happen before PostgreSQL can emit
	// a SQLSTATE: refused connections, DNS, authentication, and TLS errors.
	// The query cannot have executed, so retrying an idempotent read is safe.
	var connectErr *pgconn.ConnectError
	if errors.As(err, &connectErr) {
		return fallbackDecision{retry: true, openCircuit: true, reason: ReasonConnectionFailed}
	}
	// A bare context error belongs to a narrower operation inside the callback,
	// not the replica connection. ConnectError is checked first because pgx may
	// wrap its own connection timeout with context.DeadlineExceeded.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fallbackDecision{}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fallbackDecision{retry: true, openCircuit: true, reason: ReasonConnectionFailed}
	}
	if pgconn.SafeToRetry(err) {
		return fallbackDecision{retry: true, openCircuit: true, reason: ReasonConnectionFailed}
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fallbackDecision{}
	}
	if strings.HasPrefix(pgErr.Code, "08") {
		return fallbackDecision{retry: true, openCircuit: true, reason: ReasonConnectionFailed}
	}
	switch pgErr.Code {
	case "40001":
		return fallbackDecision{retry: true, reason: ReasonRecoveryConflict}
	case "57P01", "57P02", "57P03":
		return fallbackDecision{retry: true, openCircuit: true, reason: ReasonConnectionFailed}
	default:
		return fallbackDecision{}
	}
}

func shouldFallback(ctx context.Context, err error) bool {
	return fallbackFor(ctx, err).retry
}

func (s *Selector) replicaSucceeded(selected selection) {
	if s.circuit.succeed(selected.generation, selected.halfOpen) {
		s.logger.Info("database replica circuit closed after successful read")
	}
}

func (s *Selector) replicaFailed(selected selection, reason Reason, err error) {
	if s.circuit.fail(
		selected.generation,
		s.now(),
		defaultReplicaCircuitFailureThreshold,
		defaultReplicaCircuitCooldown,
	) {
		s.logger.Warn("database replica circuit opened; using primary",
			"reason", reason,
			"cooldown", defaultReplicaCircuitCooldown.String(),
			"error", err,
		)
	}
}

func (s *Selector) replicaDidNotProveAvailability(selected selection, err error) {
	if !selected.halfOpen {
		return
	}
	if isPostgresResponse(err) {
		s.replicaSucceeded(selected)
		return
	}
	s.circuit.abandonTrial(selected.generation, s.now(), defaultReplicaCircuitCooldown)
}

func isPostgresResponse(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr)
}

func (c *replicaCircuit) allow(now time.Time) (allowed bool, generation uint64, halfOpen bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.openUntil.IsZero() {
		return true, c.generation, false
	}
	if now.Before(c.openUntil) || c.halfOpenInFlight {
		return false, c.generation, false
	}
	c.halfOpenInFlight = true
	return true, c.generation, true
}

func (c *replicaCircuit) succeed(generation uint64, halfOpen bool) (closed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if generation != c.generation {
		return false
	}
	if c.openUntil.IsZero() {
		c.consecutiveFailures = 0
		return false
	}
	if !halfOpen || !c.halfOpenInFlight {
		return false
	}
	c.generation++
	c.consecutiveFailures = 0
	c.openUntil = time.Time{}
	c.halfOpenInFlight = false
	return true
}

func (c *replicaCircuit) fail(generation uint64, now time.Time, threshold int, cooldown time.Duration) (opened bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if generation != c.generation {
		return false
	}
	c.consecutiveFailures++
	if c.consecutiveFailures < threshold {
		return false
	}
	wasOpen := !c.openUntil.IsZero()
	c.generation++
	c.consecutiveFailures = 0
	c.openUntil = now.Add(cooldown)
	c.halfOpenInFlight = false
	return !wasOpen
}

func (c *replicaCircuit) abandonTrial(generation uint64, now time.Time, cooldown time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if generation != c.generation || !c.halfOpenInFlight {
		return
	}
	c.generation++
	c.openUntil = now.Add(cooldown)
	c.halfOpenInFlight = false
}
