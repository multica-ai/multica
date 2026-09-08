package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestAutoAdvanceIssueStatusLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	bootstrap := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()

	ownerID := bootstrap.User(t,
		fmt.Sprintf("auto-advance-owner-%d", suffix),
		fmt.Sprintf("auto-advance-owner-%d@example.com", suffix),
	)
	workspaceIDStr := bootstrap.Workspace(t,
		fmt.Sprintf("auto-advance-ws-%d", suffix),
		fmt.Sprintf("auto-advance-ws-%d", suffix),
	)
	wsUUID := util.MustParseUUID(workspaceIDStr)

	fx := testutil.New(pool, workspaceIDStr, ownerID)
	fx.Member(t, workspaceIDStr, ownerID, "owner")
	agentIDStr := fx.Agent(t, "Auto Advance Agent", "")
	agentUUID := util.MustParseUUID(agentIDStr)

	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	// 1. Create an issue in 'todo'
	issue, err := queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: wsUUID,
		Title:       "Auto Advance Test Issue",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   util.MustParseUUID(ownerID),
	})
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	taskID := dbid.NewV7()
	taskQueue := db.AgentTaskQueue{
		ID:      taskID,
		IssueID: issue.ID,
		AgentID: agentUUID,
		Status:  "running",
	}

	// 2. Test autoAdvanceIssueOnTaskStart: todo -> in_progress
	svc.autoAdvanceIssueOnTaskStart(ctx, taskQueue)
	afterStart, err := queries.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if afterStart.Status != "in_progress" {
		t.Fatalf("expected issue status in_progress on task start, got %s", afterStart.Status)
	}

	// 3. Test autoAdvanceIssueOnTaskComplete: in_progress -> in_review
	svc.autoAdvanceIssueOnTaskComplete(ctx, taskQueue)
	afterComplete, err := queries.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if afterComplete.Status != "in_review" {
		t.Fatalf("expected issue status in_review on task complete, got %s", afterComplete.Status)
	}

	// 4. Test autoAdvanceIssueOnTaskFail: set back to in_progress, then fail -> blocked
	_, err = queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          issue.ID,
		Status:      "in_progress",
		WorkspaceID: wsUUID,
	})
	if err != nil {
		t.Fatalf("UpdateIssueStatus failed: %v", err)
	}

	svc.autoAdvanceIssueOnTaskFail(ctx, taskQueue, "provider_quota_limit")
	afterFail, err := queries.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if afterFail.Status != "blocked" {
		t.Fatalf("expected issue status blocked on task failure, got %s", afterFail.Status)
	}

	// 5. Test autoAdvanceIssueOnTaskCancel: set back to in_progress, then cancel -> todo
	_, err = queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          issue.ID,
		Status:      "in_progress",
		WorkspaceID: wsUUID,
	})
	if err != nil {
		t.Fatalf("UpdateIssueStatus failed: %v", err)
	}

	svc.autoAdvanceIssueOnTaskCancel(ctx, taskQueue)
	afterCancel, err := queries.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if afterCancel.Status != "todo" {
		t.Fatalf("expected issue status todo on task cancel, got %s", afterCancel.Status)
	}

	// 6. Test terminal preservation: done / cancelled never overwritten by task completion or failure
	_, err = queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          issue.ID,
		Status:      "done",
		WorkspaceID: wsUUID,
	})
	if err != nil {
		t.Fatalf("UpdateIssueStatus failed: %v", err)
	}

	svc.autoAdvanceIssueOnTaskComplete(ctx, taskQueue)
	afterDoneComplete, err := queries.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if afterDoneComplete.Status != "done" {
		t.Fatalf("expected issue status done to be preserved, got %s", afterDoneComplete.Status)
	}

	t.Logf("SUCCESS: Auto-advance lifecycle verified for issue %s across all states", util.UUIDToString(issue.ID))
}
