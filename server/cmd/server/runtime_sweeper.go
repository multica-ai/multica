package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	// sweepInterval is how often we check for stale runtimes and tasks.
	sweepInterval = 30 * time.Second
	// runtimeGCSweepInterval keeps seven-day retention cleanup off the
	// latency-sensitive 30-second liveness path. GC remains independently
	// bounded by runtimeGCTickTimeout once each hourly round begins.
	runtimeGCSweepInterval = time.Hour
	// delegatedFailureRecoverySweepInterval keeps the low-probability durable
	// recovery scan off the latency-sensitive runtime liveness path.
	delegatedFailureRecoverySweepInterval = 5 * time.Minute
	// staleThresholdSeconds marks runtimes offline if no heartbeat for this
	// long. The heartbeat timing derivation lives with the shared service
	// constant so every task release path uses the same eligibility window.
	staleThresholdSeconds = service.RuntimeClaimFreshnessSeconds
	// defaultRuntimeReconnectGrace keeps locally-running work alive through a
	// sustained API/network partition. Runtime state still flips offline after
	// staleThresholdSeconds so the UI remains truthful; only task termination is
	// delayed. A daemon that actually restarts bypasses this grace through the
	// explicit RecoverOrphanedTasksForRuntime path.
	defaultRuntimeReconnectGrace = 3 * time.Hour
	// A reconnect grace below the heartbeat freshness window would allow the
	// stale-task backstop to fail work while the same runtime was still eligible
	// to claim new work. Clamp configuration to keep those policies ordered.
	minimumRuntimeReconnectGrace = time.Duration(staleThresholdSeconds) * time.Second
	// offlineTaskFailBatchSize bounds in-flight tasks failed for long-offline
	// runtimes in one tick.
	offlineTaskFailBatchSize = 500
	// reconnectRetryExpireBatchSize bounds deferred recovery retries that reach
	// their terminal reconnect deadline in one tick.
	reconnectRetryExpireBatchSize = 500
	// offlineRuntimeTTLSeconds deletes offline runtimes with no active agents
	// after this duration. 7 days gives users plenty of time to restart daemons.
	offlineRuntimeTTLSeconds = 7 * 24 * 3600.0
	// runtimeGCBatchSize bounds both the candidate scan and the number of
	// per-runtime transactions one sweeper tick may open. At the hourly cadence,
	// 500 preserves a theoretical capacity of 12,000 candidates per day; the
	// round timeout remains the hard bound on actual work.
	runtimeGCBatchSize = 500
	// runtimeGCTickTimeout bounds each independent hourly GC round so lock
	// contention cannot occupy its worker indefinitely.
	runtimeGCTickTimeout = 15 * time.Second
	// runtimeGCOperationTimeout prevents one contended or unhealthy runtime from
	// stalling every later GC candidate indefinitely.
	runtimeGCOperationTimeout = 5 * time.Second
	// dispatchTimeoutSeconds fails tasks stuck in 'dispatched' beyond this.
	// The dispatched→running transition should be near-instant, so 5 minutes
	// means something went wrong (e.g. StartTask API call failed silently).
	dispatchTimeoutSeconds = 300.0
	// runningTimeoutSeconds fails tasks stuck in 'running' beyond this. It is a
	// coarse server-side backstop keyed on started_at, AND-gated by daemon
	// liveness (agent_runtime.last_seen_at freshness within
	// staleThresholdSeconds): a running task whose runtime is still
	// heartbeating is NEVER killed by this wall clock, even after the timeout
	// elapses. This is what lets healthy multi-hour research / training runs
	// survive on self-hosted deployments (MUL-4107) — the daemon itself
	// decides stuck-vs-long-running via its inactivity watchdogs (idle/tool),
	// so the server-side wall clock is only a defensive backstop for the
	// pathological case where a runtime row somehow retains status='online'
	// with a stale DB heartbeat for longer than this timeout. The primary
	// "daemon died" path is `sweepStaleRuntimes` in the same tick (Redis
	// liveness + DB stale + FailTasksForOfflineRuntimes), which typically
	// reclaims orphaned tasks within ~180s.
	runningTimeoutSeconds = 9000.0
	// queuedExpireBatchSize caps how many queued rows a single sweeper tick
	// transitions to failed. Keeps the sweep transaction short even when
	// the historical backlog is large (~89k at MUL-1899 baseline). At 30s
	// ticks and 500 rows/tick we drain 60k rows/hour worst case — plenty
	// of headroom for the documented backlog without monopolising DB CPU.
	queuedExpireBatchSize = 500
	// chatFinalizeGraceSeconds is how long a cancelled chat task's deferred
	// empty/non-empty judgment (#5219) waits for the daemon's cancel-ack
	// before the sweeper settles it. Covers the daemon's 5s cancellation
	// poll plus the bounded 10s+12s transcript drain wait (#5210) plus
	// network slack; past this the daemon is presumed dead or partitioned.
	chatFinalizeGraceSeconds = 60.0
	// chatFinalizeBatchSize caps deferred finalizations per tick.
	chatFinalizeBatchSize = 100
	// delegatedFailureRecoveryBatchSize bounds the durable recovery-outbox
	// replay so a historical backlog cannot monopolise the runtime sweep tick.
	delegatedFailureRecoveryBatchSize = 100
)

type runtimeGCTxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

type runtimeGCEventPublisher interface {
	PublishRuntimeTeardown(context.Context, service.RuntimeTeardownResult, string, string, string, string, bool)
	PublishRuntimeRefresh(string, string, string, string)
}

type runtimeSweepStageStats struct {
	candidates int
	changed    int
}

func taskServiceMetrics(taskSvc *service.TaskService) *obsmetrics.BusinessMetrics {
	if taskSvc == nil {
		return nil
	}
	return taskSvc.Metrics
}

func observeRuntimeSweepStage(metrics *obsmetrics.BusinessMetrics, stage string, startedAt time.Time, stats runtimeSweepStageStats) {
	metrics.ObserveRuntimeSweepStage(stage, time.Since(startedAt), stats.candidates, stats.changed)
}

// runPeriodicSweep serializes rounds and drops ticker events while a round is
// still running. That preserves the existing no-overlap behavior while letting
// low-frequency maintenance run independently from runtime liveness.
func runPeriodicSweep(ctx context.Context, interval time.Duration, sweep func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}

// runRuntimeSweeper periodically marks runtimes as offline if their
// last_seen_at exceeds the stale threshold, and fails orphaned tasks.
// This handles cases where the daemon crashes, is killed without calling
// the deregister endpoint, or leaves tasks in a non-terminal state.
//
// liveness is consulted before flipping any candidate to offline: when the
// LivenessStore is available and reports the runtime as alive, we skip the
// row even though its DB last_seen_at is old (Redis is the authority on the
// hot heartbeat path; the DB is allowed to lag up to runtimeHeartbeatDBFlushInterval).
// When liveness is unavailable or errors, we fall back to trusting the DB
// stale window — that is the original behavior.
func runRuntimeSweeper(ctx context.Context, queries *db.Queries, liveness handler.LivenessStore, taskSvc *service.TaskService, bus *events.Bus, reconnectGrace time.Duration) {
	runPeriodicSweep(ctx, sweepInterval, func() {
		// These stages retain their existing cadence and ordering. Runtime GC and
		// delegated-failure recovery run in independent lower-frequency loops.
		sweepStaleRuntimes(ctx, queries, liveness, taskSvc, bus)
		sweepOfflineRuntimeTasks(ctx, queries, taskSvc, reconnectGrace)
		sweepExpiredRuntimeReconnectRetries(ctx, queries, taskSvc, reconnectGrace)
		sweepStaleTasks(ctx, queries, taskSvc, bus, reconnectGrace)
		sweepExpiredQueuedTasks(ctx, queries, taskSvc, reconnectGrace)
		sweepDeferredChatFinalizations(ctx, queries, taskSvc)
	})
}

func runDelegatedFailureRecoverySweeper(ctx context.Context, taskSvc *service.TaskService) {
	runPeriodicSweep(ctx, delegatedFailureRecoverySweepInterval, func() {
		sweepPendingDelegatedFailureRecoveries(ctx, taskSvc)
	})
}

func runRuntimeGCSweeper(ctx context.Context, txStarter runtimeGCTxStarter, queries *db.Queries, metrics *obsmetrics.BusinessMetrics, publisher runtimeGCEventPublisher) {
	runPeriodicSweep(ctx, runtimeGCSweepInterval, func() {
		gcRuntimes(ctx, txStarter, queries, metrics, publisher)
	})
}

// sweepPendingDelegatedFailureRecoveries retries durable coordinator handoffs
// that were not acquired by an executable task. It runs independently of
// stale-task discovery, which is what repairs a recovery dispatch lost before
// a server restart.
func sweepPendingDelegatedFailureRecoveries(ctx context.Context, taskSvc *service.TaskService) (stats runtimeSweepStageStats) {
	startedAt := time.Now()
	defer func() {
		observeRuntimeSweepStage(taskServiceMetrics(taskSvc), obsmetrics.RuntimeSweepStageDelegatedFailureRecovery, startedAt, stats)
	}()

	result, err := taskSvc.RecoverPendingDelegatedFailures(ctx, delegatedFailureRecoveryBatchSize)
	stats.candidates = result.Scanned
	stats.changed = result.Replayed + result.Exhausted
	if err != nil {
		slog.Warn("delegated failure recovery sweeper: replay failed",
			"replayed", result.Replayed,
			"exhausted", result.Exhausted,
			"error", err,
		)
		return
	}
	if result.Replayed > 0 {
		slog.Info("delegated failure recovery sweeper: replayed pending recoveries", "count", result.Replayed)
	}
	if result.Exhausted > 0 {
		slog.Warn("delegated failure recovery sweeper: automatic attempts exhausted", "count", result.Exhausted)
	}
	return
}

// sweepStaleRuntimes marks runtimes offline if they haven't heartbeated. Task
// termination is a separate every-tick stage so reconnect grace is measured
// independently of the one tick where the runtime first flips offline.
func sweepStaleRuntimes(ctx context.Context, queries *db.Queries, liveness handler.LivenessStore, taskSvc *service.TaskService, bus *events.Bus) (stats runtimeSweepStageStats) {
	startedAt := time.Now()
	defer func() {
		observeRuntimeSweepStage(taskServiceMetrics(taskSvc), obsmetrics.RuntimeSweepStageLiveness, startedAt, stats)
	}()

	candidates, err := queries.SelectStaleOnlineRuntimes(ctx, staleThresholdSeconds)
	if err != nil {
		slog.Warn("runtime sweeper: failed to list stale online runtimes", "error", err)
		return
	}
	stats.candidates = len(candidates)
	if len(candidates) == 0 {
		return
	}

	toOffline := filterStaleRuntimesByLiveness(ctx, candidates, liveness)
	if len(toOffline) == 0 {
		return
	}

	staleRows, err := queries.MarkRuntimesOfflineByIDs(ctx, db.MarkRuntimesOfflineByIDsParams{
		Ids:          toOffline,
		StaleSeconds: staleThresholdSeconds,
	})
	if err != nil {
		slog.Warn("runtime sweeper: failed to mark stale runtimes offline", "error", err)
		return
	}
	stats.changed = len(staleRows)
	if len(staleRows) == 0 {
		// All filtered candidates raced into a non-online state between the
		// SELECT and the UPDATE. Nothing to broadcast.
		return
	}
	if taskSvc != nil && taskSvc.Analytics != nil {
		for _, row := range staleRows {
			obsmetrics.RecordEvent(taskSvc.Analytics, taskSvc.Metrics, analytics.RuntimeOffline(
				util.UUIDToString(row.OwnerID),
				util.UUIDToString(row.WorkspaceID),
				util.UUIDToString(row.ID),
				row.DaemonID.String,
				row.Provider,
			))
		}
	}

	// Collect unique workspace IDs to notify.
	workspaces := make(map[string]bool)
	for _, row := range staleRows {
		wsID := util.UUIDToString(row.WorkspaceID)
		workspaces[wsID] = true
	}

	// Drop liveness records for confirmed-offline runtimes so a future
	// MGET sweep doesn't see a stray key keep them "alive". TTLs would
	// reap these eventually, but explicit cleanup is cheap and clearer.
	if liveness.Available() {
		for _, row := range staleRows {
			liveness.Forget(ctx, util.UUIDToString(row.ID))
		}
	}

	slog.Info("runtime sweeper: marked stale runtimes offline", "count", len(staleRows), "workspaces", len(workspaces))

	// Notify frontend clients so they re-fetch runtime list.
	for wsID := range workspaces {
		bus.Publish(events.Event{
			Type:        protocol.EventDaemonRegister,
			WorkspaceID: wsID,
			ActorType:   "system",
			Payload: map[string]any{
				"action": "stale_sweep",
			},
		})
	}
	return
}

// sweepOfflineRuntimeTasks terminates work only after the runtime's last
// heartbeat has exceeded the bounded reconnect grace. Running it every tick is
// essential: the grace usually expires long after sweepStaleRuntimes performed
// the one-time online→offline transition.
func sweepOfflineRuntimeTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, reconnectGrace time.Duration) (stats runtimeSweepStageStats) {
	startedAt := time.Now()
	defer func() {
		observeRuntimeSweepStage(taskServiceMetrics(taskSvc), obsmetrics.RuntimeSweepStageOfflineTasks, startedAt, stats)
	}()

	failedTasks, err := taskSvc.FailTasksForOfflineRuntimes(ctx, db.FailTasksForOfflineRuntimesParams{
		ReconnectGraceSecs: reconnectGrace.Seconds(),
		MaxPerTick:         offlineTaskFailBatchSize,
	})
	if err != nil {
		slog.Warn("runtime sweeper: failed to clean up long-offline tasks", "error", err)
		return
	}
	stats.candidates = len(failedTasks)
	stats.changed = len(failedTasks)
	if len(failedTasks) == 0 {
		return
	}

	slog.Info("runtime sweeper: failed tasks beyond reconnect grace", "count", len(failedTasks))
	taskSvc.HandleFailedTasks(ctx, failedTasks)
	return
}

// sweepExpiredRuntimeReconnectRetries gives health-gated retry waiting a
// bounded terminal path. Without this stage a daemon that never returns leaves
// the issue active and prevents its runtime from ever becoming GC-eligible.
func sweepExpiredRuntimeReconnectRetries(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, reconnectGrace time.Duration) (stats runtimeSweepStageStats) {
	startedAt := time.Now()
	defer func() {
		observeRuntimeSweepStage(taskServiceMetrics(taskSvc), obsmetrics.RuntimeSweepStageReconnectRetries, startedAt, stats)
	}()

	failedTasks, err := taskSvc.FailExpiredRuntimeReconnectRetries(ctx, db.FailExpiredRuntimeReconnectRetriesParams{
		ReconnectGraceSecs: reconnectGrace.Seconds(),
		RuntimeStaleSecs:   staleThresholdSeconds,
		MaxPerTick:         reconnectRetryExpireBatchSize,
	})
	if err != nil {
		slog.Warn("runtime sweeper: failed to expire reconnect retries", "error", err)
		return
	}
	stats.candidates = len(failedTasks)
	stats.changed = len(failedTasks)
	if len(failedTasks) == 0 {
		return
	}

	slog.Info("runtime sweeper: expired reconnect retries", "count", len(failedTasks))
	taskSvc.HandleFailedTasks(ctx, failedTasks)
	return
}

// filterStaleRuntimesByLiveness narrows a SELECT-of-stale-candidates down to
// the set that should actually be flipped offline. When liveness is available
// and reports a candidate as alive, we skip it (DB is just lagging). When the
// store is unavailable or errors, we trust the DB stale window — i.e. every
// candidate flips, matching the legacy MarkStaleRuntimesOffline behavior.
func filterStaleRuntimesByLiveness(ctx context.Context, candidates []db.SelectStaleOnlineRuntimesRow, liveness handler.LivenessStore) []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(candidates))
	if !liveness.Available() {
		for _, c := range candidates {
			ids = append(ids, c.ID)
		}
		return ids
	}
	idStrs := make([]string, len(candidates))
	for i, c := range candidates {
		idStrs[i] = util.UUIDToString(c.ID)
	}
	alive, ok := liveness.IsAliveBatch(ctx, idStrs)
	if !ok {
		// Store hiccup: degrade to DB-only behavior for this tick.
		for _, c := range candidates {
			ids = append(ids, c.ID)
		}
		return ids
	}
	for i, c := range candidates {
		if alive[idStrs[i]] {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

// gcRuntimes deletes old offline runtimes without relying on the legacy
// agent_task_queue.runtime_id ON DELETE CASCADE. Candidate discovery is bounded;
// each runtime then gets an independent transaction so one bad row cannot abort
// the whole sweep.
func gcRuntimes(ctx context.Context, txStarter runtimeGCTxStarter, queries *db.Queries, metrics *obsmetrics.BusinessMetrics, publisher runtimeGCEventPublisher) (stats runtimeSweepStageStats) {
	startedAt := time.Now()
	defer func() {
		observeRuntimeSweepStage(metrics, obsmetrics.RuntimeSweepStageGC, startedAt, stats)
	}()
	return gcRuntimesWithBudget(ctx, txStarter, queries, metrics, publisher, runtimeGCTickTimeout)
}

func gcRuntimesWithBudget(ctx context.Context, txStarter runtimeGCTxStarter, queries *db.Queries, metrics *obsmetrics.BusinessMetrics, publisher runtimeGCEventPublisher, budget time.Duration) (stats runtimeSweepStageStats) {
	gcCtx, cancelGC := context.WithTimeout(ctx, budget)
	defer cancelGC()

	listCtx, cancelList := context.WithTimeout(gcCtx, runtimeGCOperationTimeout)
	candidates, err := queries.ListStaleOfflineRuntimeGCCandidates(listCtx, db.ListStaleOfflineRuntimeGCCandidatesParams{
		StaleSeconds: offlineRuntimeTTLSeconds,
		MaxPerTick:   runtimeGCBatchSize,
	})
	cancelList()
	if err != nil {
		slog.Warn("runtime GC: failed to list stale offline runtimes", "error", err)
		metrics.RecordRuntimeGCFailed()
		return
	}
	stats.candidates = len(candidates)
	if len(candidates) == 0 {
		return
	}

	gcWorkspaces := make(map[string]bool)
	deleted := 0
	for i, runtimeID := range candidates {
		if gcCtx.Err() != nil {
			slog.Info("runtime GC: tick budget exhausted",
				"deleted", deleted, "remaining_candidates", len(candidates)-i)
			break
		}
		runtimeCtx, cancelRuntime := context.WithTimeout(gcCtx, runtimeGCOperationTimeout)
		result, err := gcRuntime(runtimeCtx, txStarter, queries, runtimeID)
		cancelRuntime()
		if err != nil {
			if gcCtx.Err() != nil {
				slog.Info("runtime GC: tick budget exhausted",
					"deleted", deleted, "remaining_candidates", len(candidates)-i)
				break
			}
			slog.Warn("runtime GC: failed to delete stale offline runtime",
				"runtime_id", util.UUIDToString(runtimeID), "error", err)
			metrics.RecordRuntimeGCFailed()
			continue
		}
		if result.skipReason != "" {
			metrics.RecordRuntimeGCSkipped(result.skipReason)
			slog.Warn("runtime GC: candidate no longer safe to delete; skipping",
				"runtime_id", util.UUIDToString(runtimeID), "reason", result.skipReason)
			continue
		}
		if !result.deleted {
			continue
		}
		deleted++
		stats.changed++
		metrics.RecordRuntimeGCDeleted()
		gcWorkspaces[result.workspaceID] = true
		if publisher != nil {
			publisher.PublishRuntimeTeardown(gcCtx, result.teardown, result.workspaceID, "system", "", "runtime_gc", false)
		}
	}
	if deleted == 0 {
		return
	}

	slog.Info("runtime GC: deleted stale offline runtimes", "count", deleted, "workspaces", len(gcWorkspaces))

	for wsID := range gcWorkspaces {
		if publisher != nil {
			publisher.PublishRuntimeRefresh(wsID, "system", "", "runtime_gc")
		}
	}
	return
}

type runtimeGCResult struct {
	workspaceID string
	teardown    service.RuntimeTeardownResult
	deleted     bool
	skipReason  string
}

// gcRuntime re-checks and deletes one candidate under a runtime-row FOR UPDATE
// lock. Task ownership writes take FOR KEY SHARE on the same row through
// lock_task_owner_rows, so a concurrent enqueue either commits before the
// checks below and blocks deletion, or waits until deletion commits and then
// observes that its runtime no longer exists.
func gcRuntime(ctx context.Context, txStarter runtimeGCTxStarter, queries *db.Queries, runtimeID pgtype.UUID) (runtimeGCResult, error) {
	var result runtimeGCResult
	tx, err := txStarter.Begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// The operation context may already be cancelled on a timeout. Give
		// rollback its own short window so the pool does not retain an open
		// transaction/connection after a failed GC attempt.
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeGCOperationTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	qtx := queries.WithTx(tx)

	runtime, err := qtx.LockAgentRuntime(ctx, runtimeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("lock runtime: %w", err)
	}
	result.workspaceID = util.UUIDToString(runtime.WorkspaceID)

	lockedAgents, err := qtx.ListUserAgentsByRuntimeForUpdate(ctx, runtimeID)
	if err != nil {
		return result, fmt.Errorf("lock runtime agents: %w", err)
	}

	eligible, err := qtx.IsAgentRuntimeEligibleForGC(ctx, db.IsAgentRuntimeEligibleForGCParams{
		ID:           runtimeID,
		StaleSeconds: offlineRuntimeTTLSeconds,
	})
	if err != nil {
		return result, fmt.Errorf("re-check eligibility: %w", err)
	}
	if !eligible {
		result.skipReason = obsmetrics.RuntimeGCSkipEligibilityChanged
		return result, nil
	}
	if err := service.ValidateRuntimeAgentWorkspaces(runtime, lockedAgents); err != nil {
		if errors.Is(err, service.ErrRuntimeWorkspaceMismatch) {
			result.skipReason = obsmetrics.RuntimeGCSkipWorkspaceMismatch
			return result, nil
		}
		return result, fmt.Errorf("validate runtime agent workspaces: %w", err)
	}

	lockedAgentIDs := make([]pgtype.UUID, len(lockedAgents))
	for i, agent := range lockedAgents {
		lockedAgentIDs[i] = agent.ID
	}

	undrained, err := qtx.CountUndrainedTasksByRuntimeOrAgent(ctx, db.CountUndrainedTasksByRuntimeOrAgentParams{
		RuntimeIds: []pgtype.UUID{runtimeID},
		AgentIds:   lockedAgentIDs,
	})
	if err != nil {
		return result, fmt.Errorf("count non-terminal tasks: %w", err)
	}
	if undrained > 0 {
		result.skipReason = obsmetrics.RuntimeGCSkipNonTerminalTask
		return result, nil
	}

	teardown, err := service.TeardownRuntime(ctx, qtx, runtimeID, service.RuntimeTeardownOptions{CancelNonTerminalTasks: false})
	if err != nil {
		if errors.Is(err, service.ErrRuntimeNotDrained) {
			result.skipReason = obsmetrics.RuntimeGCSkipNonTerminalTask
			return result, nil
		}
		if errors.Is(err, service.ErrRuntimeWorkspaceMismatch) {
			result.skipReason = obsmetrics.RuntimeGCSkipWorkspaceMismatch
			return result, nil
		}
		return result, fmt.Errorf("teardown runtime: %w", err)
	}
	if err := qtx.DeleteAgentRuntime(ctx, runtimeID); err != nil {
		return result, fmt.Errorf("delete runtime: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit transaction: %w", err)
	}

	result.teardown = teardown
	result.deleted = true
	return result, nil
}

// sweepStaleTasks fails tasks stuck in dispatched/running for too long,
// even when the runtime is still online at the row level. Each branch pairs
// the wall clock with a task-appropriate liveness signal so healthy long
// runs are preserved:
//   - dispatched: excludes rows with an active prepare_lease (renewed by
//     the daemon between claim and StartTask).
//   - running: excludes rows whose runtime is 'online' with a fresh
//     last_seen_at (renewed by the daemon heartbeat ~every 15s).
//
// The daemon-dead case is primarily handled upstream by sweepStaleRuntimes
// in the same tick; this function is a defensive backstop for the residual
// edge where a runtime row lingers online-with-stale-heartbeat past the
// wall clock (MUL-4107).
func sweepStaleTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, bus *events.Bus, reconnectGrace time.Duration) (stats runtimeSweepStageStats) {
	startedAt := time.Now()
	defer func() {
		observeRuntimeSweepStage(taskServiceMetrics(taskSvc), obsmetrics.RuntimeSweepStageStaleTasks, startedAt, stats)
	}()

	failedTasks, err := taskSvc.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs: dispatchTimeoutSeconds,
		RunningTimeoutSecs:  runningTimeoutSeconds,
		// Reuse the runtime stale window so the running-task backstop
		// exactly matches what sweepStaleRuntimes considers "not alive".
		RuntimeStaleSecs:          staleThresholdSeconds,
		RuntimeReconnectGraceSecs: reconnectGrace.Seconds(),
	})
	if err != nil {
		slog.Warn("task sweeper: failed to clean up stale tasks", "error", err)
		return
	}
	stats.candidates = len(failedTasks)
	stats.changed = len(failedTasks)
	if len(failedTasks) == 0 {
		return
	}

	slog.Info("task sweeper: failed stale tasks", "count", len(failedTasks))
	taskSvc.CaptureLeaseExpiredTasks(ctx, failedTasks)
	taskSvc.HandleFailedTasks(ctx, failedTasks)
	return
}

// sweepExpiredQueuedTasks fails queued tasks whose runtime has stopped proving
// it is alive. Companion to the dispatch-time admission gate added in MUL-1899:
// that gate prevents new doomed enqueues; this one retires work already queued
// against a runtime that then went away. It deliberately does NOT expire on
// queue age — a heartbeating runtime is busy, not dead, and MUL-6558 showed a
// wall clock killing healthy work behind a slow queue. Capped to
// queuedExpireBatchSize per tick so a big backlog can't monopolise the DB.
func sweepExpiredQueuedTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, reconnectGrace time.Duration) (stats runtimeSweepStageStats) {
	startedAt := time.Now()
	defer func() {
		observeRuntimeSweepStage(taskServiceMetrics(taskSvc), obsmetrics.RuntimeSweepStageQueuedExpiry, startedAt, stats)
	}()

	failedTasks, err := taskSvc.ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		ReconnectGraceSecs: reconnectGrace.Seconds(),
		MaxPerTick:         queuedExpireBatchSize,
	})
	if err != nil {
		slog.Warn("task sweeper: failed to expire stale queued tasks", "error", err)
		return
	}
	stats.candidates = len(failedTasks)
	stats.changed = len(failedTasks)
	if len(failedTasks) == 0 {
		return
	}

	slog.Info("task sweeper: expired stale queued tasks", "count", len(failedTasks))
	taskSvc.CaptureQueuedExpiredTasks(ctx, failedTasks)
	taskSvc.HandleFailedTasks(ctx, failedTasks)
	return
}

// sweepDeferredChatFinalizations settles cancelled chat tasks whose deferred
// empty/non-empty judgment (#5219) never received a daemon cancel-ack within
// the grace period — the daemon died, was partitioned, or its ack was lost.
// FinalizeDeferredCancelledChat claims the marker atomically, so racing a
// late ack is harmless.
func sweepDeferredChatFinalizations(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) (stats runtimeSweepStageStats) {
	startedAt := time.Now()
	defer func() {
		observeRuntimeSweepStage(taskServiceMetrics(taskSvc), obsmetrics.RuntimeSweepStageDeferredChatFinalization, startedAt, stats)
	}()

	rows, err := queries.ListChatFinalizeDeferredExpired(ctx, db.ListChatFinalizeDeferredExpiredParams{
		GraceSecs:  chatFinalizeGraceSeconds,
		MaxPerTick: chatFinalizeBatchSize,
	})
	if err != nil {
		slog.Warn("chat finalize sweeper: list deferred failed", "error", err)
		return
	}
	stats.candidates = len(rows)
	if len(rows) == 0 {
		return
	}
	for _, t := range rows {
		if taskSvc.FinalizeDeferredCancelledChat(ctx, t.ID) {
			stats.changed++
		}
	}
	slog.Info("chat finalize sweeper: settled deferred cancellations", "count", len(rows))
	return
}

// broadcastFailedTasks is preserved as a thin shim for the integration tests
// in this package. New call sites should use TaskService.HandleFailedTasks
// directly so the side effects (event broadcast, agent reconcile, issue
// rollback, auto-retry) are guaranteed in one place.
func broadcastFailedTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, bus *events.Bus, tasks []db.AgentTaskQueue) {
	if taskSvc != nil {
		taskSvc.HandleFailedTasks(ctx, tasks)
		return
	}
	// Fallback path used by tests that don't construct a TaskService:
	// publish task:failed events with workspace IDs and reset stuck issues.
	processedIssues := make(map[string]bool)
	affectedAgents := make(map[string]pgtype.UUID)
	for _, t := range tasks {
		failureReason := "agent_error"
		if t.FailureReason.Valid && t.FailureReason.String != "" {
			failureReason = t.FailureReason.String
		}
		workspaceID := ""
		if t.IssueID.Valid {
			if issue, err := queries.GetIssue(ctx, t.IssueID); err == nil {
				workspaceID = util.UUIDToString(issue.WorkspaceID)
				issueKey := util.UUIDToString(t.IssueID)
				// Only issues whose status means "an agent is actively working"
				// get reset. in_review and blocked are deliberately excluded —
				// they mean a human or an external dependency owns the issue
				// now, and resetting those to todo would re-trigger an agent on
				// work someone else is holding. A custom status resolves to the
				// canonical status it inherits, so a custom review gate is
				// excluded for the same reason In Review is. (MUL-6243)
				effectiveStatus := issuestatus.Effective(ctx, queries, issue.WorkspaceID, issue.Status)
				if effectiveStatus == "in_progress" && !processedIssues[issueKey] {
					processedIssues[issueKey] = true
					if hasActive, herr := queries.HasActiveTaskForIssue(ctx, t.IssueID); herr == nil && !hasActive {
						queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: t.IssueID, Status: "todo", WorkspaceID: issue.WorkspaceID})
					}
				}
			}
		}
		payload := map[string]any{
			"task_id":        util.UUIDToString(t.ID),
			"agent_id":       util.UUIDToString(t.AgentID),
			"issue_id":       util.UUIDToString(t.IssueID),
			"status":         "failed",
			"failure_reason": failureReason,
			"retry_pending":  false,
		}
		if t.Error.Valid && t.Error.String != "" {
			payload["error"] = redact.Text(t.Error.String)
		}
		e := events.Event{
			Type:        protocol.EventTaskFailed,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			TaskID:      util.UUIDToString(t.ID),
			Payload:     payload,
		}
		if t.ChatSessionID.Valid {
			e.ChatSessionID = util.UUIDToString(t.ChatSessionID)
			payload["chat_session_id"] = e.ChatSessionID
		}
		bus.Publish(e)
		affectedAgents[util.UUIDToString(t.AgentID)] = t.AgentID
	}
	for _, agentID := range affectedAgents {
		reconcileAgentStatus(ctx, queries, bus, agentID)
	}
}

// reconcileAgentStatus refreshes agent status from the current working task
// set. A no-op returns no row, so the fallback emits no redundant status event.
// Used only by the test-fallback path of broadcastFailedTasks above.
func reconcileAgentStatus(ctx context.Context, queries *db.Queries, bus *events.Bus, agentID pgtype.UUID) {
	agent, err := queries.RefreshAgentStatusFromTasks(ctx, agentID)
	if err != nil {
		return
	}
	bus.Publish(events.Event{
		Type:        protocol.EventAgentStatus,
		WorkspaceID: util.UUIDToString(agent.WorkspaceID),
		ActorType:   "system",
		Payload:     map[string]any{"agent_id": util.UUIDToString(agent.ID), "status": agent.Status},
	})
}
