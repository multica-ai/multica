package service

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// Claim decision observability (#8551, queue-wait attribution).
//
// Every daemon poll decides whether a task is dispatched, skipped, or left
// waiting, and until now most of those decisions were invisible: claimTask's
// internal outcome string only surfaced through the >=300ms slow log, two
// outcomes had scattered debug lines, and no_runtime / runtime_mismatch had
// nothing at all. The worst blind spot is outcome=no_tasks — it collapses the
// ClaimAgentTask SQL fences (wakeup gate, runtime liveness, per-(issue, agent)
// serialization, rebind fence) into one value even though the caller's
// candidate list knew whether work was visible at all.
//
// recordClaimDecision below is the single funnel for this. Counting unit: one
// increment per claim attempt (one claimTask call, from its defer) plus one
// per poll-level decision that never enters claimTask — a poll
// short-circuiting on the empty cache, a poll whose candidate query returns
// nothing claimable, each recovered stale-dispatched task, and each poll-level
// error exit. Poll decisions and claim attempts are distinct outcomes, so the
// two layers cannot double-count the same decision. Nothing here changes any
// decision — the outcomes mirror strings the claim paths already produce, and
// the mapping is pure.
//
// Reason semantics are deliberately evidence-bounded. The candidate queries
// already filter on wakeup validity and agent/runtime binding, so an empty
// result only proves no *visible* candidates — never that the queue holds no
// tasks (reason: no_visible_candidates). Conversely, candidates observed with
// a no_tasks outcome proves only that no task was claimed on this attempt —
// the exact cause (a SQL fence versus another request winning the row) is
// unknown (reason: candidates_unclaimed).

// Outcomes produced inside claimTask, shared by every caller.
const (
	claimOutcomeClaimed                = "claimed"
	claimOutcomeNoTasks                = "no_tasks"
	claimOutcomeNoCapacity             = "no_capacity"
	claimOutcomeNoRuntime              = "no_runtime"
	claimOutcomeRuntimeMismatch        = "runtime_mismatch"
	claimOutcomeErrorGetAgent          = "error_get_agent"
	claimOutcomeErrorCountRunning      = "error_count_running"
	claimOutcomeErrorClaim             = "error_claim"
	claimOutcomeErrorReanchorChatInput = "error_reanchor_chat_input"
	claimOutcomeErrorTransaction       = "error_transaction"
)

// Outcomes of claim polls (ClaimTaskForRuntime and the batch
// ClaimTasksForRuntimes) that return before claimTask is ever entered, plus
// stale-dispatch recovery. claimOutcomeNoTask is ClaimTaskForRuntime's initial
// slow-log outcome; no funnel call records it.
const (
	claimOutcomeNoTask                 = "no_task"
	claimOutcomeEmptyCacheHit          = "empty_cache_hit"
	claimOutcomeEmptyDB                = "empty_db"
	claimOutcomeReclaimedDispatched    = "reclaimed_dispatched"
	claimOutcomeErrorPromoteDeferred   = "error_promote_deferred"
	claimOutcomeErrorReclaimDispatched = "error_reclaim_dispatched"
	claimOutcomeErrorList              = "error_list"
)

// Reasons narrow an outcome into the cause a dashboard or debug-log reader
// cares about, stating only what the evidence supports. The
// candidates_unclaimed vs no_visible_candidates split inside no_tasks is the
// #8551 signal: the former says work was visible yet nothing was claimed on
// this attempt (cause unknown — fence or lost race), the latter says the
// candidate query itself saw nothing claimable.
const (
	claimReasonClaimed              = "claimed"
	claimReasonNoVisibleCandidates  = "no_visible_candidates"
	claimReasonCandidatesUnclaimed  = "candidates_unclaimed"
	claimReasonUnknownCandidates    = "unknown_candidates"
	claimReasonAgentConcurrencyCap  = "agent_concurrency_cap"
	claimReasonAgentUnbound         = "agent_unbound"
	claimReasonAgentRebound         = "agent_rebound"
	claimReasonEmptyCache           = "empty_cache"
	claimReasonStaleDispatchRecover = "stale_dispatch_recovered"
	claimReasonError                = "error"
)

// Claim poll paths, as seen by the counter's claim_path label. Distinct from
// the task-origin `source` label used by the other multica_agent_task_*
// metrics: those classify where a task came from (issue, chat, autopilot),
// while this classifies which poll path made the decision.
const (
	claimPathAgent   = "agent"   // ClaimTask (agent-triggered claim)
	claimPathRuntime = "runtime" // ClaimTaskForRuntime (single-runtime poll)
	claimPathBatch   = "batch"   // ClaimTasksForRuntimes (machine-level batch)
)

// claimScope is informational context the caller already holds. candidates is
// how many rows in the caller's candidate list belong to the agent being
// claimed; the agent path has no list, so it reports zero and its no_tasks
// outcome maps to unknown_candidates rather than a definitive
// no_visible_candidates.
type claimScope struct {
	claimPath  string
	candidates int
}

// claimDecisionDetail carries the capacity numbers behind a no_capacity
// decision. Left zero for every other outcome.
type claimDecisionDetail struct {
	running       int64
	maxConcurrent int64
}

// claimReasonFor maps an outcome onto the closed reason enum above. Unknown
// inputs degrade to "error" rather than minting a new series: a value nobody
// classified is not evidence of a healthy path.
func claimReasonFor(outcome string, scope claimScope) string {
	switch outcome {
	case claimOutcomeNoTasks:
		if scope.candidates > 0 {
			return claimReasonCandidatesUnclaimed
		}
		if scope.claimPath == claimPathAgent {
			return claimReasonUnknownCandidates
		}
		return claimReasonNoVisibleCandidates
	case claimOutcomeNoCapacity:
		return claimReasonAgentConcurrencyCap
	case claimOutcomeRuntimeMismatch:
		return claimReasonAgentRebound
	case claimOutcomeNoRuntime:
		return claimReasonAgentUnbound
	case claimOutcomeClaimed:
		return claimReasonClaimed
	case claimOutcomeEmptyCacheHit:
		return claimReasonEmptyCache
	case claimOutcomeEmptyDB:
		return claimReasonNoVisibleCandidates
	case claimOutcomeReclaimedDispatched:
		return claimReasonStaleDispatchRecover
	default:
		// Every error_* outcome, and anything unrecognized.
		return claimReasonError
	}
}

// normalizeClaimPath keeps the counter's claim_path label a closed enum:
// values outside the three known poll paths land on "other".
func normalizeClaimPath(claimPath string) string {
	switch claimPath {
	case claimPathAgent, claimPathRuntime, claimPathBatch:
		return claimPath
	default:
		return "other"
	}
}

// recordClaimDecision is the single observability funnel for the claim path:
// metric first, then log, so a logging problem can never lose the counter.
// runtimeID may be the zero UUID (the agent path passes no runtime).
//
// The empty-poll outcomes (empty_cache_hit, empty_db) fire on every idle
// poll — the hottest path in the scheduler — so they are counted but
// deliberately not logged; a debug line per idle poll would flood any
// deployment running with debug logging enabled.
func (s *TaskService) recordClaimDecision(agentID, runtimeID pgtype.UUID, outcome string, scope claimScope, detail claimDecisionDetail) {
	scope.claimPath = normalizeClaimPath(scope.claimPath)
	reason := claimReasonFor(outcome, scope)
	if s.Metrics != nil {
		s.Metrics.RecordClaimDecision(scope.claimPath, outcome, reason)
	}
	if outcome == claimOutcomeEmptyCacheHit || outcome == claimOutcomeEmptyDB {
		return
	}
	attrs := []any{
		"outcome", outcome,
		"reason", reason,
		"claim_path", scope.claimPath,
		"agent_id", util.UUIDToString(agentID),
		"runtime_id", util.UUIDToString(runtimeID),
		"candidates", scope.candidates,
	}
	if outcome == claimOutcomeNoCapacity {
		attrs = append(attrs, "running", detail.running, "max", detail.maxConcurrent)
	}
	slog.Debug("task claim decision", attrs...)
}
