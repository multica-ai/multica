package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestEnqueueTaskForIssueAutopilotPreservesTriggerOwnerAfterRunCloses(t *testing.T) {
	for _, status := range []string{"issue_created", "running", "completed", "failed", "skipped"} {
		t.Run(status, func(t *testing.T) {
			fx, autopilotOwner := newPrincipalFixture(t)
			triggerOwner := fx.member(t, "handoff-trigger-owner")
			agentID := fx.privateAgentOwnedBy(t, triggerOwner, "handoff")
			autopilotID, triggerID := fx.autopilotWithTrigger(t, agentID, autopilotOwner, triggerOwner)
			fx.Insert(t, "autopilot_rule_version", testutil.Cols{
				"autopilot_id": autopilotID, "workspace_id": fx.WorkspaceID,
				"published_by_type": "member", "published_by_id": autopilotOwner,
			})
			issueID := fx.Issue(t, "autopilot handoff", testutil.Cols{
				"assignee_type": "agent", "assignee_id": agentID,
				"origin_type": "autopilot", "origin_id": autopilotID,
				"status": "in_review",
			})
			fx.Insert(t, "autopilot_run", testutil.Cols{
				"autopilot_id": autopilotID, "trigger_id": triggerID,
				"issue_id": issueID, "source": "schedule", "status": status,
			})
			ctx := context.Background()
			issue, err := fx.q.GetIssue(ctx, util.MustParseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			task, err := fx.svc.TaskSvc.EnqueueTaskForIssue(ctx, issue)
			if err != nil {
				t.Fatalf("enqueue handoff: %v", err)
			}
			fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
			stored, err := fx.q.GetAgentTask(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.OriginatorSource.String != string(attribution.SourceTriggerOwner) {
				t.Errorf("source = %q, want trigger_owner", stored.OriginatorSource.String)
			}
			if got := util.UUIDToString(stored.OriginatorUserID); got != triggerOwner {
				t.Errorf("originator = %q, want trigger creator %q", got, triggerOwner)
			}
			if got := util.UUIDToString(stored.AccountableUserID); got != triggerOwner {
				t.Errorf("accountable = %q, want trigger creator %q", got, triggerOwner)
			}
			if status == "completed" || status == "failed" || status == "skipped" {
				if _, err := fx.q.GetAutopilotRunByIssue(ctx, issue.ID); !errors.Is(err, pgx.ErrNoRows) {
					t.Errorf("active-run lookup must exclude %s runs, got %v", status, err)
				}
			}
		})
	}
}

func TestAutopilotIssueAttributionUsesLatestMatchingRun(t *testing.T) {
	fx, owner := newPrincipalFixture(t)
	triggerOwner := fx.member(t, "latest-trigger-owner")
	agentID := fx.privateAgentOwnedBy(t, owner, "latest")
	autopilotID, oldTriggerID := fx.autopilotWithTrigger(t, agentID, owner, owner)
	triggerID := fx.trigger(t, autopilotID, "member", triggerOwner)
	issueID := fx.Issue(t, "latest autopilot run", testutil.Cols{
		"origin_type": "autopilot", "origin_id": autopilotID,
	})
	createdAt := time.Now().Add(-time.Hour)
	for i, id := range []string{oldTriggerID, triggerID} {
		fx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": autopilotID, "trigger_id": id, "issue_id": issueID,
			"source": "schedule", "status": "completed",
			"created_at": createdAt.Add(time.Duration(i) * time.Minute),
		})
	}
	// A newer run from another autopilot must not supply this issue's principal.
	otherAutopilotID, otherTriggerID := fx.autopilotWithTrigger(t, agentID, owner, owner)
	fx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": otherAutopilotID, "trigger_id": otherTriggerID, "issue_id": issueID,
		"source": "schedule", "status": "completed", "created_at": createdAt.Add(2 * time.Minute),
	})
	ctx := context.Background()
	issue, err := fx.q.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	got := fx.svc.TaskSvc.attributionForIssueTask(ctx, issue, pgtype.UUID{}, attribution.SourceCommentSource, pgtype.UUID{})
	if got.Source != attribution.SourceTriggerOwner || util.UUIDToString(got.UserID) != triggerOwner {
		t.Fatalf("attribution = %+v, want latest matching trigger owner %s", got, triggerOwner)
	}
	otherWorkspaceID := fx.Workspace(t, "foreign attribution workspace", "foreign-attribution-workspace")
	_, err = fx.q.GetLatestAutopilotRunForIssueAttribution(ctx, db.GetLatestAutopilotRunForIssueAttributionParams{
		IssueID: issue.ID, AutopilotID: issue.OriginID, WorkspaceID: util.MustParseUUID(otherWorkspaceID),
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign workspace lookup = %v, want no rows", err)
	}
	fx.Exec(t, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, fx.WorkspaceID, triggerOwner)
	got = fx.svc.TaskSvc.attributionForIssueTask(ctx, issue, pgtype.UUID{}, attribution.SourceCommentSource, pgtype.UUID{})
	if got.UserID.Valid {
		t.Fatalf("removed trigger creator still authorizes handoff: %+v", got)
	}
	_, err = fx.q.GetLatestAutopilotRunForIssueAttribution(ctx, db.GetLatestAutopilotRunForIssueAttributionParams{
		IssueID: util.MustParseUUID(fx.Issue(t, "unlinked issue")), AutopilotID: issue.OriginID,
		WorkspaceID: issue.WorkspaceID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unlinked issue lookup = %v, want no rows", err)
	}
}
