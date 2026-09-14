package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The queue door for Triage (MUL-7189 §2.3).
//
// An issue in Triage produces no execution run. That rule cannot be enforced at
// the call sites: at least five paths enqueue without looking at status at all
// (comment and mention dispatch, quick action, manual rerun, auto-retry), and
// dispatchIssueRun discards the enqueue error entirely, so a missed call site
// would fail silently. So the check sits at the last point every issue-linked
// task passes through — immediately before the INSERT — and the upstream
// short-circuits exist only to make previews and dispatch reasons correct.
//
// Exactly two enqueue paths are exempt, and both are Triage's own:
//
//   - the Triage run itself, the one run an issue in Triage is allowed;
//   - accept, which moves the issue out of Triage in the same transaction that
//     enqueues the execution run, so it passes this check anyway.
//
// Both belong to sub-issues that are not built yet.

// ErrIssueInTriage is returned by every issue enqueue path when the issue is
// waiting in Triage. Handlers render it as 403 issue_in_triage; background
// callers log it and drop the trigger.
var ErrIssueInTriage = errors.New("the issue is in triage, so it does not run")

// guardIssueNotInTriage refuses an enqueue for an issue in Triage. q is the
// caller's own query handle so a transaction-scoped enqueue reads the status
// its own transaction wrote — the deferred-channel path creates the issue and
// its task together, and an uncommitted Triage issue must still be seen.
//
// A missing issue is not in Triage: the enqueue proceeds and fails (or not) on
// its own terms. A read error refuses, because a run that cannot be shown to be
// allowed must not start.
func guardIssueNotInTriage(ctx context.Context, q *db.Queries, issueID pgtype.UUID) error {
	if !issueID.Valid {
		return nil
	}
	row, err := q.GetIssueGCStatus(ctx, issueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("triage guard: load issue status: %w", err)
	}
	if row.Status == issuestatus.Triage {
		return ErrIssueInTriage
	}
	return nil
}

// TriageContextType marks a task as a Triage run in its context JSONB
// (MUL-7189 §3.6). Triage runs carry no retry budget and no resumable session:
// a fresh triage of the current entry is always the right recovery, and a later
// execution run on the accepted issue must not inherit the triage conversation.
const TriageContextType = "triage"

// isTriageTask reports whether a queued task is a Triage run. The marker lives
// in the task's context rather than a column, so a task written before Triage
// existed reads as false.
func isTriageTask(t db.AgentTaskQueue) bool {
	if len(t.Context) == 0 {
		return false
	}
	var payload struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(t.Context, &payload) == nil && payload.Type == TriageContextType
}
