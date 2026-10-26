package service

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Per-task queue-wait attribution (route B, the display half of #8551:
// "Show the actual queue constraint when known"). After each claim poll, the
// candidates the poll saw but did not claim get one persisted reason on their
// queue rows, so the UI can say "Queued — agent capacity" instead of a bare
// "Waiting for an available agent."
//
// Evidence boundaries, mirroring the wording discipline the claim-decision
// funnel established (#9184 review):
//   - agent_capacity is an agent-level fact (running >= max_concurrent_tasks
//     observed under the agent row lock), so it is written to every candidate
//     row of that agent.
//   - attempted_unclaimed states only that this poll attempted the head
//     candidate and claimed nothing: the exact cause (a ClaimAgentTask fence
//     versus another poller winning the row) is unknowable from the Go side,
//     and losing that race is normal scheduler behavior, not a fault.
//   - agent_rebound / agent_unbound are agent-level binding facts observed
//     under the agent row lock; like agent_capacity they apply to every
//     candidate row of the agent.
//   - daemon_capacity (candidates the batch path never attempted after the
//     maxTasks cap) is deliberately NOT written in v1: the server cannot
//     distinguish a real daemon slot shortage from the claimBatchMaxTasksCap
//     truncation, so the evidence is partial. The #9184 review already
//     acknowledged that server-side counters alone cannot attribute
//     daemon-capacity waits; this column does not pretend otherwise.
//   - Empty polls write nothing, matching the funnel's "count, don't log"
//     discipline for the idle path.

// queueWaitReason values persisted to agent_task_queue.queue_wait_reason.
// Closed enum, Go-side only (the column is unconstrained TEXT, same shape as
// failure_reason and autopilot_runs.reason_code).
const (
	// queueWaitAgentCapacity: the agent is already running max_concurrent_tasks.
	queueWaitAgentCapacity = "agent_capacity"
	// queueWaitAttemptedUnclaimed: this poll attempted the row's head candidate
	// and nothing was claimed; concurrent competition or a claim fence, not
	// distinguishable here.
	queueWaitAttemptedUnclaimed = "attempted_unclaimed"
	// queueWaitAgentRebound: the agent rebound to another runtime after this
	// poll's candidate list was read.
	queueWaitAgentRebound = "agent_rebound"
	// queueWaitAgentUnbound: the agent has no runtime bound at claim time.
	queueWaitAgentUnbound = "agent_unbound"
)

// claimAttempt is the outcome of one claimTask call, surfaced to the poll
// loops so they can attribute the wait on the candidate rows they hold.
// Outcome reuses the claimOutcome* constants. The capacity numbers behind a
// no_capacity outcome are claim-decision funnel inputs (claimDecisionDetail)
// and are not needed here: attribution only consumes the outcome.
type claimAttempt struct {
	Outcome string
}

// queueWaitReasonForOutcome maps a claim outcome onto the persisted wait
// reason. Every other outcome — claimed, errors, empty polls — returns "" and
// writes nothing: an error says nothing about why the task waits, and a
// claimed task's reason is cleared transactionally by the claim UPDATE.
func queueWaitReasonForOutcome(outcome string) string {
	switch outcome {
	case claimOutcomeNoCapacity:
		return queueWaitAgentCapacity
	case claimOutcomeNoTasks:
		return queueWaitAttemptedUnclaimed
	case claimOutcomeRuntimeMismatch:
		return queueWaitAgentRebound
	case claimOutcomeNoRuntime:
		return queueWaitAgentUnbound
	default:
		return ""
	}
}

// queueWaitUpdate is one (task, reason) pair a poll wants persisted.
type queueWaitUpdate struct {
	taskID pgtype.UUID
	reason string
}

// queueWaitUpdatesForAgent maps one agent's final claim attempt onto the rows
// it explains, dropping rows whose stored queue_wait_reason already equals the
// new value so a steady-state poll produces no write at all (the UPDATE's
// IS DISTINCT FROM guard is the second line of defense). candidates is the
// agent's slice of this poll's candidate list in poll order; a no_tasks
// attempt is narrowed to candidates[0], the head row the poll actually
// attempted, because the attempt proves nothing about the rows behind it.
func queueWaitUpdatesForAgent(candidates []db.AgentTaskQueue, attempt claimAttempt) []queueWaitUpdate {
	reason := queueWaitReasonForOutcome(attempt.Outcome)
	if reason == "" || len(candidates) == 0 {
		return nil
	}
	rows := candidates
	if attempt.Outcome == claimOutcomeNoTasks {
		rows = candidates[:1]
	}
	var updates []queueWaitUpdate
	for i := range rows {
		if cur := rows[i].QueueWaitReason; cur.Valid && cur.String == reason {
			continue
		}
		updates = append(updates, queueWaitUpdate{taskID: rows[i].ID, reason: reason})
	}
	return updates
}

// recordQueueWaitReasons persists the attribution for every agent this poll
// attempted without claiming. agentCandidates maps agent key -> that agent's
// candidate rows in poll order; attempts holds the final claimTask outcome per
// attempted agent key.
//
// Red lines (route B design): called only AFTER the candidate loop returns,
// never inside claimTask's transaction; runs on the pool without an explicit
// transaction; partial-success early returns skip it entirely — the next poll
// re-evaluates. A failed write is logged and swallowed — losing attribution
// must never fail a claim (same discipline as cancelSupersededDeferredRetries).
func (s *TaskService) recordQueueWaitReasons(ctx context.Context, agentCandidates map[string][]db.AgentTaskQueue, attempts map[string]claimAttempt) {
	var taskIDs []pgtype.UUID
	var reasons []string
	for agentKey, attempt := range attempts {
		for _, u := range queueWaitUpdatesForAgent(agentCandidates[agentKey], attempt) {
			taskIDs = append(taskIDs, u.taskID)
			reasons = append(reasons, u.reason)
		}
	}
	if len(taskIDs) == 0 {
		return
	}
	if err := s.Queries.UpdateQueueWaitReasons(ctx, db.UpdateQueueWaitReasonsParams{
		TaskIds: taskIDs,
		Reasons: reasons,
	}); err != nil {
		slog.Warn("persist queue wait reasons failed",
			"error", err,
			"tasks", len(taskIDs),
		)
	}
}
