package cerebrodb

// FIR-3901 — "dead" failed runs: a run that failed and that nothing is going to
// pick up again on its own. These drive the red failed bar on an issue and the
// red pip in the inbox.
//
// A failed task counts as dead when ALL of these hold:
//
//   1. It has no descendant retry (agent_task_queue.parent_task_id). The
//      auto-retry path (TaskService.MaybeRetryFailedTask → CreateRetryTask)
//      creates that descendant in the same failure handler, so its absence is
//      the authoritative "no automatic retry happened" signal.
//   2. It is the issue's newest run. A newer run — a manual rerun, a follow-up
//      comment, a wakeup, another agent's @-mention — means the thread moved on
//      and the old failure is no longer the thing the user has to act on.
//
//      FIR-4073 narrowed this from "no newer run by the SAME agent, created
//      after this one finished" to "no newer run on the issue at all". Both
//      halves of the old predicate kept stale bars alive:
//        - Keying on agent_id meant a second agent's successful run left the
//          first agent's failure on screen indefinitely.
//        - Comparing n.created_at against t.completed_at missed the common
//          overlap case: comment while an agent is still working → the new run
//          is enqueued BEFORE the old one fails, so it never counted as
//          "newer" and the red bar outlived a run that had already succeeded.
//      Comparing created_at to created_at asks the question the user actually
//      means — "is this still the latest run?" — and is immune to overlap.
//   3. It settled at least DeadFailedGraceSeconds ago. The retry row lands
//      within milliseconds of the failure, so without this grace window every
//      auto-retried failure would flash red for one poll cycle before its
//      descendant appears. Cheaper than reconciling the flash client-side.
//   4. It settled inside DeadFailedWindowHours. Past that the failure is
//      history, not a to-do — the bar must not become an archive.
//
// resume_possible mirrors the three gates the daemon actually applies when it
// decides whether to resume a conversation, so the UI can disable the Fortsæt
// button instead of silently degrading to a blank session:
//
//   - session_id IS NOT NULL          (handler/daemon.go: GetLastTaskSession)
//   - failure_reason not poisoned     (same query's blacklist)
//   - the recorded runtime is online  (handler/daemon.go: prior.RuntimeID == task.RuntimeID)
//
// The workdir gate (daemon.gateResumeToReusedWorkdir) cannot be evaluated
// server-side — the daemon only knows whether it reused the folder at claim
// time — so resume_possible is a necessary, not sufficient, condition. It
// removes the three failure modes we can see; it does not promise the folder
// still exists on the machine.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// DeadFailedGraceSeconds is how long a failed task must have been settled
// before it may show as a dead failure. Covers the auto-retry write.
const DeadFailedGraceSeconds = 60

// DeadFailedWindowHours bounds how far back a dead failure stays actionable.
const DeadFailedWindowHours = 48

// Poisoned failure reasons — resuming these replays the same broken state.
// Keep in sync with GetLastTaskSession's NOT IN list and daemon/poisoned.go.
var deadFailedPoisonedReasons = []string{
	"iteration_limit",
	"agent_fallback_message",
	"api_invalid_request",
	"codex_semantic_inactivity",
}

// DeadFailedTask is one unresolved failed run.
type DeadFailedTask struct {
	ID             pgtype.UUID        `json:"id"`
	AgentID        pgtype.UUID        `json:"agent_id"`
	IssueID        pgtype.UUID        `json:"issue_id"`
	ParentIssueID  pgtype.UUID        `json:"parent_issue_id"`
	RuntimeID      pgtype.UUID        `json:"runtime_id"`
	FailureReason  pgtype.Text        `json:"failure_reason"`
	Error          pgtype.Text        `json:"error"`
	CompletedAt    pgtype.Timestamptz `json:"completed_at"`
	Attempt        int32              `json:"attempt"`
	MaxAttempts    int32              `json:"max_attempts"`
	ResumePossible bool               `json:"resume_possible"`
	HasSession     bool               `json:"has_session"`
	RuntimeOnline  bool               `json:"runtime_online"`
	RuntimeName    pgtype.Text        `json:"runtime_name"`
	// FIR-4073 — the run's machine is paused right now (rate limit, quota cap,
	// expired key). The run did not "fail" in any sense the user should act on:
	// the pause suspended it and the unpause sweeper resumes it at UnpauseAt.
	// Surfaced so the alert reads grey "waiting" instead of red "failed", and
	// so the routine pause no longer needs an issue comment to explain itself.
	// UnpauseAt is NULL when the auto-pause circuit breaker gave up, i.e. the
	// pause really does need a human.
	RuntimePaused bool               `json:"runtime_paused"`
	UnpauseAt     pgtype.Timestamptz `json:"unpause_at"`
}

// deadFailedSelect is the shared projection + dead-run predicate. The caller
// appends its own scope filter and ordering.
const deadFailedSelect = `
SELECT
    t.id,
    t.agent_id,
    t.issue_id,
    i.parent_issue_id,
    t.runtime_id,
    t.failure_reason,
    t.error,
    t.completed_at,
    t.attempt,
    t.max_attempts,
    (t.session_id IS NOT NULL)                                   AS has_session,
    COALESCE(rt.status = 'online', FALSE)                        AS runtime_online,
    rt.name                                                      AS runtime_name,
    (rt.paused_at IS NOT NULL)                                   AS runtime_paused,
    rt.unpause_at                                                AS unpause_at,
    (
        t.session_id IS NOT NULL
        AND COALESCE(t.failure_reason, '') <> ALL($2::text[])
        AND COALESCE(rt.status = 'online', FALSE)
    )                                                            AS resume_possible
FROM agent_task_queue t
JOIN issue i ON i.id = t.issue_id
LEFT JOIN agent_runtime rt ON rt.id = t.runtime_id
WHERE t.status = 'failed'
  AND t.issue_id IS NOT NULL
  AND t.completed_at IS NOT NULL
  AND t.completed_at <  NOW() - MAKE_INTERVAL(secs => $3::int)
  AND t.completed_at >= NOW() - MAKE_INTERVAL(hours => $4::int)
  AND NOT EXISTS (
        SELECT 1 FROM agent_task_queue d WHERE d.parent_task_id = t.id
      )
  AND NOT EXISTS (
        SELECT 1 FROM agent_task_queue n
        WHERE n.issue_id = t.issue_id
          AND n.id <> t.id
          AND n.created_at > t.created_at
      )
`

func scanDeadFailed(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}) ([]DeadFailedTask, error) {
	defer rows.Close()
	out := []DeadFailedTask{}
	for rows.Next() {
		var t DeadFailedTask
		if err := rows.Scan(
			&t.ID, &t.AgentID, &t.IssueID, &t.ParentIssueID, &t.RuntimeID,
			&t.FailureReason, &t.Error, &t.CompletedAt, &t.Attempt, &t.MaxAttempts,
			&t.HasSession, &t.RuntimeOnline, &t.RuntimeName,
			&t.RuntimePaused, &t.UnpauseAt, &t.ResumePossible,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListDeadFailedTasksForIssue returns the unresolved failed runs on one issue,
// newest first. Drives the red failed bar at the top of the issue.
func (q *Queries) ListDeadFailedTasksForIssue(ctx context.Context, issueID pgtype.UUID) ([]DeadFailedTask, error) {
	rows, err := q.db.Query(ctx,
		deadFailedSelect+` AND t.issue_id = $1 ORDER BY t.completed_at DESC`,
		issueID, deadFailedPoisonedReasons, DeadFailedGraceSeconds, DeadFailedWindowHours)
	if err != nil {
		return nil, err
	}
	return scanDeadFailed(rows)
}

// ListDeadFailedIssueTasksInWorkspace returns every unresolved failed run in a
// workspace. Drives the red pip on inbox rows.
func (q *Queries) ListDeadFailedIssueTasksInWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]DeadFailedTask, error) {
	rows, err := q.db.Query(ctx,
		deadFailedSelect+` AND i.workspace_id = $1 ORDER BY t.completed_at DESC`,
		workspaceID, deadFailedPoisonedReasons, DeadFailedGraceSeconds, DeadFailedWindowHours)
	if err != nil {
		return nil, err
	}
	return scanDeadFailed(rows)
}
