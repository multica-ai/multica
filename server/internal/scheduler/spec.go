// Package scheduler is the DB-backed execution-record scheduler
// described in docs/db-backed-execution-scheduler-rfc.md (MUL-2957).
//
// The scheduler turns the `sys_cron_executions` table into the
// distributed lock + audit log for every internal periodic job. Each
// app instance ticks the same registered jobs, but the table's unique
// key on (job_name, scope_kind, scope_id, plan_time) ensures only one
// instance wins the lease for a given plan; losers no-op silently.
//
// Failure handling, stale-lease theft, retry policy, and catch-up
// behaviour are all driven by the registered JobSpec — the scheduler
// itself is intentionally a thin shell around the SQL primitives in
// db_ops.go.
package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CatchUpMode controls how the scheduler decides which plan_times to
// claim when its tick runs late or after a long pause.
type CatchUpMode int

const (
	// CatchUpLatestOnly only claims the most recently due plan. Use this
	// for jobs whose handler has its own watermark and recovers missed
	// data without per-tick replay (e.g. the task_usage hourly rollup,
	// where rollup_task_usage_hourly_window catches up via
	// task_usage_hourly_rollup_state.watermark_at).
	CatchUpLatestOnly CatchUpMode = iota

	// CatchUpEveryPlan claims every missed plan_time, oldest first, up
	// to MaxPlansPerTick per tick and bounded by CatchUpWindow. Use for
	// jobs where each plan bucket has independent business meaning.
	CatchUpEveryPlan
)

func (m CatchUpMode) String() string {
	switch m {
	case CatchUpLatestOnly:
		return "latest_only"
	case CatchUpEveryPlan:
		return "every_plan"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

// Scope identifies the locking dimension for a planned execution. For
// global jobs the canonical value is ScopeGlobal — the literal string
// "global" is used for both kind and id so the unique key has no NULL
// columns.
type Scope struct {
	Kind string
	ID   string
}

// ScopeGlobal is the singleton scope used by jobs that lock the whole
// database (e.g. rollup_task_usage_hourly).
var ScopeGlobal = Scope{Kind: "global", ID: "global"}

func (s Scope) String() string { return s.Kind + "/" + s.ID }

// ScopeProvider produces the list of scopes the scheduler should tick
// for a given job at a given time. For global jobs the function returns
// {ScopeGlobal}; sharded jobs may return one entry per shard.
type ScopeProvider func(ctx context.Context, now time.Time) ([]Scope, error)

// HandlerInput is what the scheduler passes to a job handler.
type HandlerInput struct {
	Job       *JobSpec
	Scope     Scope
	PlanTime  time.Time
	Attempt   int
	RunnerID  string
	Heartbeat func(ctx context.Context) error
}

// HandlerResult is what a handler returns. RowsAffected and Result feed
// the audit row; Result must be small (the table caps it implicitly via
// JSONB plus a runtime guard in finishSuccess).
type HandlerResult struct {
	RowsAffected int64
	Result       map[string]any
}

// Handler is the business logic for a job. The scheduler owns the
// lease, calls Handler exactly once per claimed (job, scope, plan_time)
// row, and writes the terminal status back guarded by lease_token.
//
// Long-running handlers MUST call HandlerInput.Heartbeat periodically
// (e.g. every 30s) so the scheduler can extend stale_after; if the
// returned error is ErrLeaseLost, the handler should stop and return.
type Handler func(ctx context.Context, in HandlerInput) (HandlerResult, error)

// JobSpec describes one registered job. The scheduler stores specs in
// its registry keyed by Name; Name MUST be stable across releases
// because it is the audit/index key.
type JobSpec struct {
	// Name is the canonical job identifier. Use snake_case ASCII.
	Name string

	// Cadence is the plan bucket size (e.g. 5 * time.Minute). The
	// scheduler floors `db_now - ScheduleDelay` to a multiple of
	// Cadence to derive the canonical UTC plan_time.
	Cadence time.Duration

	// ScheduleDelay shifts the eligibility horizon back from "now". A
	// 5-minute delay means the 12:00 plan only becomes eligible at
	// 12:05 (db_now). This keeps just-arrived data from being missed
	// by handlers that compare against `now() - 5 min` upper bounds
	// (e.g. rollup_task_usage_hourly_window).
	ScheduleDelay time.Duration

	// CatchUpMode selects between latest-only and every-plan replay.
	CatchUpMode CatchUpMode

	// CatchUpWindow bounds how far back the scheduler will go when
	// replaying missed plans (CatchUpEveryPlan) or counting skipped
	// plans (CatchUpLatestOnly). Plans older than now - CatchUpWindow
	// are ignored.
	CatchUpWindow time.Duration

	// MaxPlansPerTick caps the number of plans claimed in a single
	// tick under CatchUpEveryPlan. Latest-only jobs ignore this value.
	MaxPlansPerTick int

	// RunTimeout bounds the per-handler context. Must be smaller than
	// StaleTimeout.
	RunTimeout time.Duration

	// StaleTimeout is how long after the last heartbeat a RUNNING
	// lease is considered stale. If the lease is stale and
	// AllowStaleReentry is true, another runner may steal it.
	StaleTimeout time.Duration

	// HeartbeatInterval is how often the scheduler renews stale_after
	// while the handler is running. Must be smaller than StaleTimeout.
	HeartbeatInterval time.Duration

	// AllowStaleReentry permits another runner to steal a stale
	// RUNNING lease. Set false for non-idempotent jobs; stale leases
	// then transition to FAILED with error_code='stale_timeout' and
	// require manual repair.
	AllowStaleReentry bool

	// MaxAttempts caps the number of times the same plan_time may be
	// attempted before staying in FAILED. Includes the first attempt.
	MaxAttempts int

	// RetryBackoff[i] is the delay before attempt i+2 (the second
	// attempt). Index past len-1 reuses the last entry. Empty slice
	// disables retry.
	RetryBackoff []time.Duration

	// Scopes returns the scopes to tick on each loop. For global jobs
	// use the helper StaticScopes(ScopeGlobal).
	Scopes ScopeProvider

	// PlansForScope, if non-nil, REPLACES the Cadence-based planner for
	// this job. The hook receives the most recent stored plan for the
	// (job, scope) pair and the current DB-derived `now`; it returns
	// the list of plan_times (canonical UTC) to attempt this tick.
	// Returning an empty slice means "nothing due this tick".
	//
	// When PlansForScope is set, Cadence / CatchUpMode / CatchUpWindow
	// have no effect on planning — the hook is in full control of
	// which plan_times exist. The remaining timing fields (RunTimeout,
	// StaleTimeout, HeartbeatInterval, MaxAttempts, RetryBackoff,
	// AllowStaleReentry) still govern lease and retry behaviour, and
	// MaxPlansPerTick still acts as a safety cap on the slice returned
	// by the hook (the manager truncates anything beyond it).
	//
	// Designed for jobs whose plan_times do not form a uniform Cadence
	// grid — e.g. Autopilot schedule triggers driven by arbitrary cron
	// expressions, where each trigger is its own scope and the
	// occurrence times are computed per-trigger from cron + timezone.
	//
	// The hook is invoked once per (job, scope) per tick. Plan_times
	// returned by the hook are passed unchanged to tryClaim, so the
	// hook is responsible for returning canonical UTC timestamps. A
	// plan_time that is already finalised is safe to return: tryClaim
	// treats it as a conflict and the row is not re-run.
	PlansForScope func(ctx context.Context, scope Scope, now time.Time,
		latest LatestPlanInfo) ([]time.Time, error)

	// Handler is the per-execution business logic.
	Handler Handler
}

// StaticScopes returns a ScopeProvider that always emits the supplied
// scopes. Use for jobs whose scope set never changes (e.g.
// global/global, or a fixed shard count).
func StaticScopes(scopes ...Scope) ScopeProvider {
	frozen := append([]Scope(nil), scopes...)
	return func(_ context.Context, _ time.Time) ([]Scope, error) {
		return frozen, nil
	}
}

// validate enforces invariants the SQL primitives rely on.
func (j *JobSpec) validate() error {
	if strings.TrimSpace(j.Name) == "" {
		return fmt.Errorf("scheduler: job name is required")
	}
	if j.PlansForScope == nil && j.Cadence <= 0 {
		return fmt.Errorf("scheduler: job %q: cadence must be > 0 (or set PlansForScope)", j.Name)
	}
	if j.RunTimeout <= 0 {
		return fmt.Errorf("scheduler: job %q: run_timeout must be > 0", j.Name)
	}
	if j.StaleTimeout <= j.RunTimeout {
		return fmt.Errorf("scheduler: job %q: stale_timeout (%s) must be greater than run_timeout (%s)",
			j.Name, j.StaleTimeout, j.RunTimeout)
	}
	if j.HeartbeatInterval <= 0 || j.HeartbeatInterval >= j.StaleTimeout {
		return fmt.Errorf("scheduler: job %q: heartbeat_interval must be > 0 and < stale_timeout", j.Name)
	}
	if j.MaxAttempts < 1 {
		return fmt.Errorf("scheduler: job %q: max_attempts must be >= 1", j.Name)
	}
	if j.Scopes == nil {
		return fmt.Errorf("scheduler: job %q: scopes provider is required", j.Name)
	}
	if j.Handler == nil {
		return fmt.Errorf("scheduler: job %q: handler is required", j.Name)
	}
	if j.PlansForScope == nil && j.CatchUpMode == CatchUpEveryPlan && j.MaxPlansPerTick <= 0 {
		return fmt.Errorf("scheduler: job %q: max_plans_per_tick must be > 0 for every_plan catch-up", j.Name)
	}
	return nil
}

// retryDelay returns the wait between attempt N (1-indexed) failing and
// the next attempt being eligible.
func (j *JobSpec) retryDelay(attempt int) time.Duration {
	if len(j.RetryBackoff) == 0 {
		return 0
	}
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(j.RetryBackoff) {
		idx = len(j.RetryBackoff) - 1
	}
	return j.RetryBackoff[idx]
}

// FloorPlan returns the canonical UTC plan_time bucket that contains
// `eligible` for cadence c. Exposed for tests.
func FloorPlan(eligible time.Time, c time.Duration) time.Time {
	if c <= 0 {
		return eligible.UTC()
	}
	t := eligible.UTC()
	return t.Truncate(c)
}
