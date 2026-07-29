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
//   2. No later task exists for the same (issue_id, agent_id) pair. A newer run
//      — a manual rerun, a follow-up comment, a wakeup — means the thread moved
//      on and the old failure is no longer the thing the user has to act on.
//      Keying on the agent (not just the issue) keeps a parallel @-mention
//      agent's run from silently clearing another agent's failure.
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
          AND n.agent_id = t.agent_id
          AND n.id <> t.id
          AND n.created_at > t.completed_at
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
			&t.HasSession, &t.RuntimeOnline, &t.RuntimeName, &t.ResumePossible,
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
