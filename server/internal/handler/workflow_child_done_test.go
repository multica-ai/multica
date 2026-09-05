package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

type childDoneWorkflowFixture struct {
	parentID string
	stage1ID string
	stage2ID string
}

func newChildDoneWorkflowFixture(t *testing.T, withWorkflow bool) childDoneWorkflowFixture {
	t.Helper()
	parentID := dbfx.Issue(t, "workflow child-done parent "+time.Now().Format(time.RFC3339Nano), testutil.Cols{"status": "in_progress"})
	stage1ID := dbfx.Issue(t, "workflow stage 1", testutil.Cols{
		"parent_issue_id": parentID, "stage": 1, "status": "backlog",
	})
	stage2ID := dbfx.Issue(t, "workflow stage 2", testutil.Cols{
		"parent_issue_id": parentID, "stage": 2, "status": "backlog",
	})
	fx := childDoneWorkflowFixture{parentID: parentID, stage1ID: stage1ID, stage2ID: stage2ID}
	if withWorkflow {
		fx.start(t)
	}
	return fx
}
func (f childDoneWorkflowFixture) start(t *testing.T) {
	t.Helper()
	workspaceID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := util.ParseUUID(testUserID)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := testHandler.WorkflowService.CreateDefinition(context.Background(), service.CreateWorkflowDefinitionParams{
		WorkspaceID: workspaceID,
		Name:        "Child done workflow " + time.Now().Format(time.RFC3339Nano),
		Definition:  []byte(`{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"test","name":"Test"}]}`),
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	parentID, _ := util.ParseUUID(f.parentID)
	if _, err := testHandler.WorkflowService.Start(context.Background(), service.StartWorkflowParams{
		WorkspaceID: workspaceID, IssueID: parentID, DefinitionID: definition.ID,
		Actor: service.WorkflowActor{Type: "member", ID: userID},
	}); err != nil {
		t.Fatal(err)
	}
}

func workflowRunForIssue(t *testing.T, issueID string) (status string, stage int32) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(), `SELECT status,current_stage FROM workflow_run WHERE issue_id=$1 ORDER BY created_at DESC LIMIT 1`, issueID).Scan(&status, &stage); err != nil {
		t.Fatal(err)
	}
	return status, stage
}
func TestChildDoneLegacyStagePathRemainsAgentDriven(t *testing.T) {
	fx := newChildDoneWorkflowFixture(t, false)
	updateChildStatus(t, fx.stage1ID, "done")
	assertWorkflowIssueStatusHandler(t, fx.stage2ID, "backlog")
	content := parentSystemCommentContent(t, fx.parentID)
	if !strings.Contains(content, "Stage 2 is next") {
		t.Fatalf("legacy staged comment changed: %q", content)
	}
}

func TestChildDoneActiveWorkflowAdvancesStageServerSide(t *testing.T) {
	fx := newChildDoneWorkflowFixture(t, true)
	updateChildStatus(t, fx.stage1ID, "done")
	assertWorkflowIssueStatusHandler(t, fx.stage2ID, "todo")
	status, stage := workflowRunForIssue(t, fx.parentID)
	if status != "running" || stage != 2 {
		t.Fatalf("run = %s stage %d, want running stage 2", status, stage)
	}
	content := parentSystemCommentContent(t, fx.parentID)
	if !strings.Contains(content, "advanced automatically to Stage 2") {
		t.Fatalf("workflow progress comment = %q", content)
	}
	if strings.Contains(content, "decide") || strings.Contains(content, "Stage 2 is next") {
		t.Fatalf("workflow path leaked legacy topology instruction: %q", content)
	}
}
func TestChildDoneActiveWorkflowAdvancesBeforeHumanParentGuard(t *testing.T) {
	fx := newChildDoneWorkflowFixture(t, true)
	setIssueAssigneeDirect(t, fx.parentID, "member", testUserID)
	updateChildStatus(t, fx.stage1ID, "done")
	assertWorkflowIssueStatusHandler(t, fx.stage2ID, "todo")
	status, stage := workflowRunForIssue(t, fx.parentID)
	if status != "running" || stage != 2 {
		t.Fatalf("run = %s stage %d, want running stage 2", status, stage)
	}
	if got := countSystemCommentsOn(t, fx.parentID); got != 0 {
		t.Fatalf("human parent received %d workflow progress comments, want 0", got)
	}
}

func assertWorkflowIssueStatusHandler(t *testing.T, issueID, want string) {
	t.Helper()
	var got string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id=$1`, issueID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("issue %s status = %q, want %q", issueID, got, want)
	}
}
func startChildDoneWorkflowRaw(t *testing.T, parentID, raw string) {
	t.Helper()
	workspaceID, err := util.ParseUUID(testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := util.ParseUUID(testUserID)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := testHandler.WorkflowService.CreateDefinition(context.Background(), service.CreateWorkflowDefinitionParams{
		WorkspaceID: workspaceID,
		Name:        "Child done edge " + time.Now().Format(time.RFC3339Nano),
		Definition:  []byte(raw),
		CreatedBy:   userID,
	})
	if err != nil {
		t.Fatal(err)
	}
	parentUUID, err := util.ParseUUID(parentID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.WorkflowService.Start(context.Background(), service.StartWorkflowParams{
		WorkspaceID: workspaceID, IssueID: parentUUID, DefinitionID: definition.ID,
		Actor: service.WorkflowActor{Type: "member", ID: userID},
	}); err != nil {
		t.Fatal(err)
	}
}
func TestChildDoneWorkflowBlocksWhenNextStageMissing(t *testing.T) {
	parentID := dbfx.Issue(t, "workflow blocked parent", testutil.Cols{"status": "in_progress"})
	stage1ID := dbfx.Issue(t, "workflow blocked stage 1", testutil.Cols{
		"parent_issue_id": parentID, "stage": 1, "status": "backlog",
	})
	startChildDoneWorkflowRaw(t, parentID, `{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"test","name":"Test"}]}`)
	updateChildStatus(t, stage1ID, "done")
	status, stage := workflowRunForIssue(t, parentID)
	if status != "blocked_materialization" || stage != 2 {
		t.Fatalf("run = %s stage %d, want blocked_materialization stage 2", status, stage)
	}
	content := parentSystemCommentContent(t, parentID)
	if !strings.Contains(content, "requires Stage 2") || !strings.Contains(content, "resume the workflow") {
		t.Fatalf("blocked workflow comment = %q", content)
	}
}

func TestChildDoneWorkflowFinalStageMovesParentToReview(t *testing.T) {
	parentID := dbfx.Issue(t, "workflow final parent", testutil.Cols{"status": "in_progress"})
	stage1ID := dbfx.Issue(t, "workflow final stage", testutil.Cols{
		"parent_issue_id": parentID, "stage": 1, "status": "backlog",
	})
	startChildDoneWorkflowRaw(t, parentID, `{"schema_version":1,"stages":[{"key":"build","name":"Build"}]}`)
	updateChildStatus(t, stage1ID, "done")
	status, _ := workflowRunForIssue(t, parentID)
	if status != "completed_pending_review" {
		t.Fatalf("run status = %q, want completed_pending_review", status)
	}
	assertWorkflowIssueStatusHandler(t, parentID, "in_review")
	content := parentSystemCommentContent(t, parentID)
	if !strings.Contains(content, "final declared workflow stage") || strings.Contains(content, "Decide whether") {
		t.Fatalf("final workflow comment = %q", content)
	}
}
