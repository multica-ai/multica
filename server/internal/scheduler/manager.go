package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Options configure a Manager. Defaults are set in NewManager so all
// fields are optional for callers.
type Options struct {
	// RunnerID identifies this process in audit rows. Empty defaults
	// to a fresh UUID — readable enough for short-lived debugging,
	// still unique across replicas.
	RunnerID string

	// TickInterval is how often the manager wakes up to evaluate due
	// plans across all registered jobs. Should be smaller than the
	// shortest job cadence; defaults to 30 * time.Second.
	TickInterval time.Duration

	// Logger is used for structured logs. nil defaults to
	// slog.Default().
	Logger *slog.Logger
}

// Manager is the per-process scheduler. Register one or more jobs and
// call Run with a cancellable context.
type Manager struct {
	pool   *pgxpool.Pool
	opts   Options
	jobs   map[string]*JobSpec
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewManager constructs a Manager. The pool MUST point at the database
// containing the sys_cron_executions table. The manager does not start
// any goroutine until Run is called.
func NewManager(pool *pgxpool.Pool, opts Options) *Manager {
	if opts.RunnerID == "" {
		opts.RunnerID = uuid.NewString()
	}
	if opts.TickInterval <= 0 {
		opts.TickInterval = 30 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Manager{
		pool:   pool,
		opts:   opts,
		jobs:   make(map[string]*JobSpec),
		logger: opts.Logger.With("component", "scheduler", "runner_id", opts.RunnerID),
	}
}

// Register adds a job to the manager. Must be called before Run; later
// registrations are also accepted but the new job will not tick until
// the next loop iteration.
func (m *Manager) Register(job JobSpec) error {
	if err := job.validate(); err != nil {
		return err
	}
	spec := job
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.jobs[spec.Name]; exists {
		return fmt.Errorf("scheduler: duplicate job name %q", spec.Name)
	}
	m.jobs[spec.Name] = &spec
	return nil
}

// snapshot returns a copy of registered specs so the loop iterates
// without holding the lock.
func (m *Manager) snapshot() []*JobSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*JobSpec, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j)
	}
	return out
}

// Run blocks until ctx is cancelled, ticking every Options.TickInterval.
// Returns the cause of ctx termination (typically context.Canceled).
func (m *Manager) Run(ctx context.Context) error {
	m.logger.Info("scheduler starting",
		"tick_interval", m.opts.TickInterval.String(),
		"jobs", len(m.snapshot()))

	// First tick immediately so a fresh start does not wait a full
	// interval; backoff to TickInterval thereafter.
	if err := m.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Warn("scheduler tick error", "error", err)
	}

	t := time.NewTicker(m.opts.TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.logger.Info("scheduler stopped", "reason", ctx.Err())
			return ctx.Err()
		case <-t.C:
			if err := m.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				m.logger.Warn("scheduler tick error", "error", err)
			}
		}
	}
}

// RunOnce executes a single tick across every registered job. Exposed
// for tests, one-shot CLIs, and any caller that wants to drive the
// scheduler without owning a goroutine.
func (m *Manager) RunOnce(ctx context.Context) error {
	now, err := dbNow(ctx, m.pool)
	if err != nil {
		return err
	}
	for _, job := range m.snapshot() {
		if err := m.runJob(ctx, job, now); err != nil {
			m.logger.Warn("job tick error",
				"job", job.Name, "error", err)
		}
	}
	return nil
}

// runJob iterates the scopes for a single job and processes each due
// plan according to the catch-up mode.
func (m *Manager) runJob(ctx context.Context, job *JobSpec, now time.Time) error {
	scopes, err := job.Scopes(ctx, now)
	if err != nil {
		return fmt.Errorf("scheduler: scope provider for %q: %w", job.Name, err)
	}

	// Close out abandoned RUNNING leases before planning. We run this
	// for EVERY job, regardless of AllowStaleReentry, because:
	//
	//   * Non-reentrant jobs (AllowStaleReentry=false) need the FAILED
	//     audit row + alert; this was the original motivation.
	//
	//   * Reentrant jobs (AllowStaleReentry=true) running in
	//     `latest_only` mode never re-claim historical plan_times, so
	//     a stuck RUNNING row from a crashed pod would otherwise sit
	//     in the table forever and pin
	//     `scheduler_running_stale_total > 0`. Marking it FAILED keeps
	//     the gauge truthful and (because tryClaim's
	//     retry-from-FAILED branch is still eligible at the same
	//     plan_time when attempts remain) preserves the retry path.
	//
	//   * Reentrant `every_plan` jobs would otherwise rely on the
	//     stale-steal branch in tryClaim — but that only fires when
	//     the same plan_time is being attempted again, which races
	//     this sweep harmlessly: whichever wins, the row leaves
	//     RUNNING.
	//
	// MUL-2957 review: see张大彪's blocker #1.
	if affected, err := markStaleAsFailed(ctx, m.pool, job.Name, now); err != nil {
		m.logger.Warn("scheduler: mark stale failed",
			"job", job.Name, "error", err)
	} else if affected > 0 {
		m.logger.Warn("scheduler: closed out abandoned RUNNING leases",
			"job", job.Name,
			"rows", affected,
			"reentrant", job.AllowStaleReentry)
	}

	for _, scope := range scopes {
		plans, err := m.plansForTick(ctx, job, scope, now)
		if err != nil {
			m.logger.Warn("scheduler: plan computation",
				"job", job.Name, "scope", scope.String(), "error", err)
			continue
		}
		for _, planTime := range plans {
			m.processPlan(ctx, job, scope, planTime, now)
		}
	}
	return nil
}

// plansForTick computes the list of plan_times to attempt this tick,
// respecting the catch-up mode and bounds.
func (m *Manager) plansForTick(
	ctx context.Context,
	job *JobSpec,
	scope Scope,
	now time.Time,
) ([]time.Time, error) {
	// Hook-driven jobs (e.g. Autopilot schedule triggers, which derive
	// plan_times from per-trigger cron expressions instead of a
	// uniform Cadence grid) bypass the Cadence planner entirely. We
	// still read the latest stored plan so the hook can decide whether
	// to resume from there, and still apply MaxPlansPerTick as a
	// safety cap on whatever the hook returns.
	if job.PlansForScope != nil {
		info, err := latestPlan(ctx, m.pool, job.Name, scope)
		if err != nil {
			return nil, err
		}
		plans, err := job.PlansForScope(ctx, scope, now, info)
		if err != nil {
			return nil, fmt.Errorf("scheduler: plans hook for %q: %w", job.Name, err)
		}
		if job.MaxPlansPerTick > 0 && len(plans) > job.MaxPlansPerTick {
			plans = plans[:job.MaxPlansPerTick]
		}
		return plans, nil
	}

	eligible := now.Add(-job.ScheduleDelay)
	latest := FloorPlan(eligible, job.Cadence)
	if latest.After(eligible) {
		// Truncate landed in the future — only happens at very small
		// cadences with rounding; nothing is due yet.
		return nil, nil
	}

	switch job.CatchUpMode {
	case CatchUpLatestOnly:
		return []time.Time{latest}, nil

	case CatchUpEveryPlan:
		info, err := latestPlan(ctx, m.pool, job.Name, scope)
		if err != nil {
			return nil, err
		}
		// Determine the oldest plan we still consider in-window.
		oldestAllowed := now.Add(-job.CatchUpWindow)
		if job.CatchUpWindow <= 0 {
			oldestAllowed = latest
		}
		var start time.Time
		switch {
		case info.Found && info.RetryEligible(now):
			// FAILED at info.PlanTime with attempts remaining and
			// next_retry_at <= now. Keep the cursor on the same
			// plan_time so tryClaim's retry-from-FAILED branch picks
			// it up; then advance forward through any newer plans
			// that may also be due. Without this case the cursor
			// would unconditionally jump to PlanTime+cadence and
			// strand the FAILED row until max_attempts is reached
			// elsewhere — which never happens in steady state.
			//
			// (MUL-2957 review: see张大彪's retry blocker.)
			start = info.PlanTime
		case info.Found:
			// Latest stored plan is SUCCESS, RUNNING, or FAILED with
			// no remaining retry budget — advance past it so we do
			// not redundantly attempt to insert the same plan_time.
			start = info.PlanTime.Add(job.Cadence)
		default:
			// No history yet for this (job, scope). Fall through to
			// the latest plan to bootstrap.
			start = latest
		}
		if start.Before(oldestAllowed) {
			start = FloorPlan(oldestAllowed, job.Cadence)
			if start.Before(oldestAllowed) {
				start = start.Add(job.Cadence)
			}
		}
		var plans []time.Time
		for t := start; !t.After(latest) && len(plans) < job.MaxPlansPerTick; t = t.Add(job.Cadence) {
			plans = append(plans, t)
		}
		return plans, nil

	default:
		return nil, fmt.Errorf("scheduler: job %q: unknown catch_up_mode %v", job.Name, job.CatchUpMode)
	}
}

// (legacy wrapper removed; the previous latestPlan_ shim is no longer
// needed because plansForTick renames its bucket variable to `latest`.)

// processPlan owns one (job, scope, plan_time) attempt: claim → run
// handler with heartbeat → terminal update.
func (m *Manager) processPlan(
	ctx context.Context,
	job *JobSpec,
	scope Scope,
	planTime time.Time,
	now time.Time,
) {
	c, err := tryClaim(ctx, m.pool, job, scope, planTime, now, m.opts.RunnerID)
	if err != nil {
		m.logger.Warn("scheduler claim error",
			"job", job.Name, "scope", scope.String(),
			"plan_time", planTime.Format(time.RFC3339), "error", err)
		return
	}
	if c.Conflicted && !c.Won && !c.Stole {
		// Another runner owns this plan, or it is already terminal.
		// Silent no-op is the expected case.
		return
	}
	if !c.Won && !c.Stole {
		// Defensive — should not be reachable but covers future SQL
		// changes that add a fourth path.
		return
	}

	m.runClaimed(ctx, job, scope, planTime, c)
}

// runClaimed runs the handler for an already-claimed lease and writes
// the terminal state.
func (m *Manager) runClaimed(
	ctx context.Context,
	job *JobSpec,
	scope Scope,
	planTime time.Time,
	c claim,
) {
	log := m.logger.With(
		"job", job.Name,
		"scope", scope.String(),
		"plan_time", planTime.Format(time.RFC3339),
		"attempt", c.Attempt,
		"execution_id", c.ID.String())

	if c.Stole {
		log.Info("scheduler stole stale lease")
	} else {
		log.Info("scheduler claimed plan")
	}

	// Per-handler context bounded by RunTimeout. Heartbeats use a
	// detached background context so a slow ctx cancellation cannot
	// drop the renewal.
	runCtx, cancel := context.WithTimeout(ctx, job.RunTimeout)
	defer cancel()

	hbCtx, hbCancel := context.WithCancel(context.Background())
	defer hbCancel()
	hbDone := make(chan struct{})
	go m.runHeartbeats(hbCtx, hbDone, job, c, log)

	start := time.Now()
	res, handlerErr := func() (out HandlerResult, retErr error) {
		// recover() inside the deferred closure assigns to the named
		// return retErr so that a panicking handler is treated exactly
		// like a returned error: classifyError records it as
		// "handler_panic" and finishFailure writes the FAILED audit
		// row. Without the named return the panic was being silently
		// swallowed and the outer code wrote a SUCCESS row with
		// rows_affected=0.
		defer func() {
			if r := recover(); r != nil {
				log.Error("scheduler handler panic", "panic", r)
				retErr = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
			}
		}()
		return job.Handler(runCtx, HandlerInput{
			Job:      job,
			Scope:    scope,
			PlanTime: planTime,
			Attempt:  c.Attempt,
			RunnerID: m.opts.RunnerID,
			Heartbeat: func(ctx context.Context) error {
				return heartbeat(ctx, m.pool, c.ID, c.LeaseToken, job.StaleTimeout)
			},
		})
	}()
	duration := time.Since(start)

	hbCancel()
	<-hbDone

	dur := duration.Milliseconds()
	dbTime, dberr := dbNow(context.Background(), m.pool)
	if dberr != nil {
		// Falling back to local time keeps the audit row honest enough
		// for a triage; the watermark indices use plan_time, not
		// finished_at, for steady-state queries.
		dbTime = time.Now().UTC()
	}

	if handlerErr != nil {
		nextRetry := time.Time{}
		if c.Attempt < job.MaxAttempts {
			delay := job.retryDelay(c.Attempt)
			nextRetry = dbTime.Add(delay)
		}
		errCode := classifyError(handlerErr)
		if err := finishFailure(context.Background(), m.pool, c.ID, c.LeaseToken,
			dbTime, dur, errCode, handlerErr.Error(), nextRetry); err != nil {
			if errors.Is(err, ErrLeaseLost) {
				log.Warn("scheduler: terminal FAILED ignored, lease was stolen",
					"duration_ms", dur, "error", handlerErr.Error())
				return
			}
			log.Error("scheduler: write terminal FAILED",
				"duration_ms", dur, "handler_error", handlerErr.Error(), "error", err)
			return
		}
		log.Warn("scheduler: handler failed",
			"duration_ms", dur,
			"error_code", errCode,
			"error", handlerErr.Error(),
			"will_retry", c.Attempt < job.MaxAttempts)
		return
	}

	if err := finishSuccess(context.Background(), m.pool, c.ID, c.LeaseToken,
		dbTime, dur, res); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			log.Warn("scheduler: terminal SUCCESS ignored, lease was stolen",
				"duration_ms", dur)
			return
		}
		log.Error("scheduler: write terminal SUCCESS",
			"duration_ms", dur, "error", err)
		return
	}
	log.Info("scheduler: handler succeeded",
		"duration_ms", dur,
		"rows_affected", res.RowsAffected)
}

// runHeartbeats keeps the lease alive for the duration of the handler.
// Exits when ctx is cancelled. Closes done when finished so the caller
// can be sure no further heartbeat updates fire after run completion.
func (m *Manager) runHeartbeats(
	ctx context.Context,
	done chan<- struct{},
	job *JobSpec,
	c claim,
	log *slog.Logger,
) {
	defer close(done)
	t := time.NewTicker(job.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			hbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := heartbeat(hbCtx, m.pool, c.ID, c.LeaseToken, job.StaleTimeout)
			cancel()
			if errors.Is(err, ErrLeaseLost) {
				log.Warn("scheduler: lease lost during heartbeat, runner should stop")
				return
			}
			if err != nil {
				log.Warn("scheduler: heartbeat error", "error", err)
			}
		}
	}
}

// classifyError maps handler errors to short error_code strings stored
// on the audit row. Unknown errors get a generic code; specific codes
// are reserved for sentinels we recognise (context timeout, lease lost
// before a terminal write, panic recovered by the scheduler, etc.).
func classifyError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "run_timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrLeaseLost):
		return "lease_lost"
	case errors.Is(err, ErrHandlerPanic):
		return "handler_panic"
	default:
		return "handler_error"
	}
}

// ErrHandlerPanic wraps a panic value recovered from a job handler so
// the scheduler can record it on the audit row and (if max_attempts
// allows) retry. Production handlers should prefer returning errors,
// but we treat panics as failures rather than letting them either
// crash the process or — worse — be silently dropped into a SUCCESS
// audit row.
var ErrHandlerPanic = errors.New("scheduler: handler panic")
