package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// JobNameIssueScheduleDispatch is the canonical job name written to
// sys_cron_executions audit rows. Stable across releases — renaming it
// would orphan historic rows.
const JobNameIssueScheduleDispatch = "issue_schedule_dispatch"

// ScopeKindIssueScheduledTrigger labels the scope dimension. Each due,
// pending trigger is one scope; scope_id is the trigger UUID.
const ScopeKindIssueScheduledTrigger = "issue_scheduled_trigger"

// maxIssueScheduleTriggersPerTick bounds ListDueIssueScheduledTriggers so a
// large backlog of simultaneously-due schedules cannot blow up a single
// tick; anything left over is picked up a few seconds later on the next
// tick.
const maxIssueScheduleTriggersPerTick = 200

// IssueScheduleDispatcher is the narrow contract this job needs from
// service.IssueScheduleService. Defined here (mirroring
// AutopilotScheduleDispatcher in jobs_autopilot.go) so this package never
// needs to import service, and — the reason this indirection exists at
// all — never needs to import handler: the enqueue permission gate
// (canInvokeAgent / canEnqueueSquadLeader) stays in handler, checked once at
// schedule-creation time. This job only needs the resulting dispatch call.
type IssueScheduleDispatcher interface {
	DispatchIssueSchedule(ctx context.Context, trigger db.IssueScheduledTrigger) error
}

// issueScheduleCache holds the per-tick set of due triggers, keyed by scope
// id (the trigger id as a string) so PlansForScope can read the run_at
// Scopes already fetched instead of re-querying per scope.
type issueScheduleCache struct {
	mu       sync.RWMutex
	triggers map[string]db.IssueScheduledTrigger
}

func newIssueScheduleCache() *issueScheduleCache {
	return &issueScheduleCache{triggers: make(map[string]db.IssueScheduledTrigger)}
}

func (c *issueScheduleCache) replace(next map[string]db.IssueScheduledTrigger) {
	c.mu.Lock()
	c.triggers = next
	c.mu.Unlock()
}

func (c *issueScheduleCache) get(id string) (db.IssueScheduledTrigger, bool) {
	c.mu.RLock()
	v, ok := c.triggers[id]
	c.mu.RUnlock()
	return v, ok
}

// IssueScheduleDispatchJob returns the JobSpec that drives one-time
// issue-scheduled-trigger dispatch through the existing DB-backed execution
// scheduler + sys_cron_executions lease infrastructure (#5927). Each due,
// pending trigger is its own scope (scope_kind = ScopeKindIssueScheduledTrigger,
// scope_id = trigger.id); plan_time is always the trigger's own run_at, so —
// unlike Autopilot's cron-driven schedule in jobs_autopilot.go — there is
// exactly one possible occurrence per trigger, ever. No cron evaluation, no
// catch-up-collapse, no "which of several missed slots" question.
//
// MaxAttempts is 2 (one retry), NOT to retry the business condition a missed
// fire represents — the resolved #5927 answer is "notify, don't silently
// retry", and IssueScheduleService.DispatchIssueSchedule itself decides
// fired vs missed and never asks the scheduler to try again for that
// reason. The one retry exists purely so this process's own crash between
// claiming the lease and writing the terminal status does not strand a
// trigger at status='pending' forever. Because run_at never changes, a
// retried attempt's plan_time is identical to the original, so
// tryClaim's FAILED-with-retry branch finds it automatically; and because
// DispatchIssueSchedule no-ops on a trigger that is no longer 'pending', a
// retry that races a dispatch which already completed is a safe no-op
// rather than a double-fire.
func IssueScheduleDispatchJob(pool *pgxpool.Pool, queries *db.Queries, dispatcher IssueScheduleDispatcher) JobSpec {
	cache := newIssueScheduleCache()

	return JobSpec{
		Name:              JobNameIssueScheduleDispatch,
		Cadence:           0, // hook-driven; each trigger has its own single-shot run_at
		ScheduleDelay:     0,
		CatchUpMode:       CatchUpLatestOnly, // ignored when PlansForScope is set; documents intent
		CatchUpWindow:     24 * time.Hour,
		RunTimeout:        1 * time.Minute,
		StaleTimeout:      3 * time.Minute,
		HeartbeatInterval: 20 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       2,
		RetryBackoff:      []time.Duration{1 * time.Minute},
		MaxPlansPerTick:   1, // exactly one occurrence per trigger, ever

		Scopes:        issueScheduleScopes(pool, queries, cache),
		PlansForScope: issueSchedulePlansForScope(cache),
		Handler:       issueScheduleHandler(queries, dispatcher),
	}
}

// issueScheduleScopes lists every trigger due to fire this tick
// (status='pending' AND run_at <= now) and populates the cache the planner
// hook reads from.
func issueScheduleScopes(pool *pgxpool.Pool, queries *db.Queries, cache *issueScheduleCache) ScopeProvider {
	_ = pool // reserved for future tx-bounded reads, matching autopilotScopes' shape
	return func(ctx context.Context, now time.Time) ([]Scope, error) {
		rows, err := queries.ListDueIssueScheduledTriggers(ctx, db.ListDueIssueScheduledTriggersParams{
			Now:        pgtype.Timestamptz{Time: now, Valid: true},
			LimitCount: maxIssueScheduleTriggersPerTick,
		})
		if err != nil {
			return nil, fmt.Errorf("issue schedule scope: list due triggers: %w", err)
		}
		next := make(map[string]db.IssueScheduledTrigger, len(rows))
		scopes := make([]Scope, 0, len(rows))
		for _, r := range rows {
			id := util.UUIDToString(r.ID)
			if id == "" {
				continue
			}
			next[id] = r
			scopes = append(scopes, Scope{Kind: ScopeKindIssueScheduledTrigger, ID: id})
		}
		cache.replace(next)
		return scopes, nil
	}
}

// issueSchedulePlansForScope always returns the trigger's own run_at.
// Because Scopes already filtered to status='pending' AND run_at <= now,
// every scope handed to this hook is due — there is nothing to enumerate or
// collapse.
func issueSchedulePlansForScope(cache *issueScheduleCache) func(
	ctx context.Context, scope Scope, now time.Time, latest LatestPlanInfo,
) ([]time.Time, error) {
	return func(ctx context.Context, scope Scope, now time.Time, latest LatestPlanInfo) ([]time.Time, error) {
		trigger, ok := cache.get(scope.ID)
		if !ok {
			// Fired, cancelled, or deleted between scope-list and
			// plan-compute (a concurrent request/tick) — status is no
			// longer 'pending', so it fell out of the scan. Nothing to
			// plan; silent no-op is correct.
			return nil, nil
		}
		return []time.Time{trigger.RunAt.Time.UTC()}, nil
	}
}

// issueScheduleHandler dispatches one (trigger, planTime) attempt. It
// re-loads the trigger inside the handler (rather than trusting the cache)
// so a between-tick state change — cancelled after scope-list, or already
// resolved by a stale-lease retry that won the race first — takes effect
// immediately.
func issueScheduleHandler(queries *db.Queries, dispatcher IssueScheduleDispatcher) Handler {
	return func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
		triggerID, err := parseScopeUUID(in.Scope.ID)
		if err != nil {
			return HandlerResult{}, fmt.Errorf("issue schedule handler: scope id is not a valid uuid: %w", err)
		}

		trigger, err := queries.GetIssueScheduledTrigger(ctx, triggerID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Trigger row itself was deleted between scope-list and
				// run (rows are never deleted by product code today, but
				// nothing prevents a future admin/cleanup path from doing
				// so). A SUCCESS row in sys_cron_executions for a vanished
				// trigger is fine — there is nothing left to dispatch.
				return HandlerResult{RowsAffected: 0, Result: map[string]any{
					"skipped_reason": "trigger_not_found",
				}}, nil
			}
			return HandlerResult{}, fmt.Errorf("load trigger: %w", err)
		}

		if err := dispatcher.DispatchIssueSchedule(ctx, trigger); err != nil {
			return HandlerResult{}, fmt.Errorf("dispatch issue schedule: %w", err)
		}

		return HandlerResult{
			RowsAffected: 1,
			Result: map[string]any{
				"trigger_id": util.UUIDToString(trigger.ID),
			},
		}, nil
	}
}
