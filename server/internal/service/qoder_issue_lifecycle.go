package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type qoderIssueChange struct {
	issue    db.Issue
	previous string
	activity wakeupActivity
}

// syncQoderIssue runs inside the task transition transaction. Only a status
// this run took ownership of can advance to review. A revision mismatch also
// protects edits away and back, reassignment, and concurrent human decisions.
func syncQoderIssue(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, completing bool) (*qoderIssueChange, error) {
	if !task.IssueID.Valid || !task.RuntimeID.Valid {
		return nil, nil
	}
	issue, err := q.LockQoderTaskIssue(ctx, task.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	target := "in_progress"
	if completing {
		var marker struct {
			Revision int64 `json:"qoder_issue_revision"`
		}
		if err := json.Unmarshal(task.Context, &marker); err != nil {
			return nil, nil
		}
		if marker.Revision == 0 || marker.Revision != issue.Revision || issue.Status != "in_progress" {
			return nil, nil
		}
		target = "in_review"
	} else {
		// Clear any caller-provided marker before deciding whether to own this run.
		if err := q.SetQoderIssueRevision(ctx, db.SetQoderIssueRevisionParams{TaskID: task.ID, Revision: 0}); err != nil {
			return nil, err
		}
		// Custom workflow columns, triage, review and terminal issues stay untouched.
		if issue.TriageState.Valid || (issue.Status != "todo" && issue.Status != "backlog") {
			return nil, nil
		}
	}
	updated, err := q.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: issue.ID, WorkspaceID: issue.WorkspaceID, Status: target, SourceTaskID: task.ID,
	})
	if err != nil {
		return nil, err
	}
	if !completing {
		if err := q.SetQoderIssueRevision(ctx, db.SetQoderIssueRevisionParams{TaskID: task.ID, Revision: updated.Revision}); err != nil {
			return nil, err
		}
	}
	details, _ := json.Marshal(map[string]any{"from": issue.Status, "to": target, "source": "qoder_cloud", "task_id": util.UUIDToString(task.ID)})
	activity, err := q.CreateActivity(ctx, db.CreateActivityParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID,
		ActorType: util.StrToText("agent"), ActorID: task.AgentID, Action: "status_changed", Details: details,
	})
	if err != nil {
		return nil, err
	}
	return &qoderIssueChange{issue: updated, previous: issue.Status, activity: wakeupActivity{row: activity, actorType: "agent", actorID: util.UUIDToString(task.AgentID), details: details}}, nil
}

func (s *TaskService) publishQoderIssueChange(ctx context.Context, change *qoderIssueChange) {
	if change == nil || s.Bus == nil {
		return
	}
	s.broadcastIssueUpdated(ctx, change.issue, change.previous)
	(&IssueWakeupService{Tasks: s}).publishWakeupActivities(change.activity)
}
