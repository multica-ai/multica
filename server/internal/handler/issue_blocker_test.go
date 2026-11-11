package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func blockerTestIssue(t *testing.T, body map[string]any) IssueResponse {
	t.Helper()
	if body["title"] == nil {
		body["title"] = "blocker API " + uuid.NewString()
	}
	var out IssueResponse
	testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)).Want(http.StatusCreated).JSON(&out)
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/issues/"+out.ID, nil), "id", out.ID)
		testutil.Call(t, testHandler.DeleteIssue, req).Want(http.StatusNoContent)
	})
	return out
}

func TestIssueBlockerReleaseKeepsWorkflowBrief(t *testing.T) {
	seedTestCatalog(t)
	agent := seededReadyAgentID(t)
	project := deliveryProject(t, "Blocked delivery "+workflowTestSuffix(), agent)
	prerequisite := blockerTestIssue(t, map[string]any{"status": "todo"})
	issue := blockerTestIssue(t, map[string]any{"status": "in_progress", "project_id": project, "blocked_by": []string{prerequisite.ID}})
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue.ID); n != 0 {
		t.Fatalf("blocked handoff queued %d runs", n)
	}
	updateIssueForTest(t, prerequisite.ID, map[string]any{"status": "done"}).Want(200)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	if err := svc.TickWorkspaces(context.Background(), parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	rule, err := testHandler.Queries.GetSystemWakeup(context.Background(), db.GetSystemWakeupParams{IssueID: parseUUID(issue.ID), SystemRule: pgtype.Text{String: service.SystemRuleBlockersDone, Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.Queries.GetAgentTask(context.Background(), rule.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.WorkflowStep.String != "in_progress" {
		t.Fatalf("wakeup workflow step = %s", task.WorkflowStep.String)
	}
	if brief := claimBriefForTest(t, issue.ID, task); !strings.Contains(brief, "Implement it and open a PR.") {
		t.Fatalf("missing handoff instructions: %s", brief)
	}
}

func TestIssueBlockerAtomicAPIAndFilter(t *testing.T) {
	a := blockerTestIssue(t, map[string]any{"status": "todo"})
	b := blockerTestIssue(t, map[string]any{"status": "todo", "blocked_by": []string{a.ID}, "assignee_type": "agent", "assignee_id": handlerTestAgentID(t)})
	if b.BlockedCount != 1 || len(b.BlockedBy) != 1 || b.BlockedBy[0].Via != "manual" {
		t.Fatalf("missing blockers: %+v", b)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", b.ID); n != 0 {
		t.Fatalf("blocked create queued %d tasks", n)
	}
	// The rejected cycle rolls back the status write as well as the edge.
	req := withURLParam(newRequest("PUT", "/api/issues/"+a.ID, map[string]any{"status": "in_review", "blocked_by": []string{b.ID}}), "id", a.ID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusBadRequest)
	var status string
	dbfx.QueryRow(t, "SELECT status FROM issue WHERE id=$1", a.ID).Scan(&status)
	if status != "todo" {
		t.Fatalf("rejected edge changed status: %s", status)
	}
	var list struct {
		Issues []IssueResponse `json:"issues"`
		Total  int             `json:"total"`
	}
	testutil.Call(t, testHandler.ListIssues, newRequest("GET", "/api/issues?workspace_id="+testWorkspaceID+"&blocked=true", nil)).Want(200).JSON(&list)
	found := false
	for _, row := range list.Issues {
		if row.ID == b.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("blocked list omitted waiting issue")
	}
	// Parent edits must also validate existing manual edges.
	req = withURLParam(newRequest("PUT", "/api/issues/"+b.ID, map[string]any{"parent_issue_id": a.ID}), "id", b.ID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusBadRequest)
}

func TestIssueBlockerRejectsMissingReferenceAtomically(t *testing.T) {
	title := "missing blocker " + uuid.NewString()
	testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": title, "blocked_by": []string{uuid.NewString()}})).Want(400)
	if n := dbfx.Count(t, "SELECT count(*) FROM issue WHERE workspace_id=$1 AND title=$2", testWorkspaceID, title); n != 0 {
		t.Fatalf("failed create left %d issues", n)
	}
	a := blockerTestIssue(t, map[string]any{})
	req := withURLParam(newRequest("PUT", "/api/issues/"+a.ID, map[string]any{"blocked_by": []string{"malformed"}}), "id", a.ID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(400)
}

func TestIssueBlockerPrivateReferencesStayPrivate(t *testing.T) {
	f := newPrivateProjectFixture(t)
	ctx := context.Background()
	if err := service.AddIssueBlocker(ctx, testHandler.Queries, parseUUID(testWorkspaceID), parseUUID(f.publicIssue), parseUUID(f.secretIssue), "member", parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dbfx.Exec(t, "DELETE FROM issue_blocker WHERE issue_id=$1", f.publicIssue)
		cleanupChildDoneIssue(f.publicIssue)
	})
	get := func(user string) IssueResponse {
		t.Helper()
		var issue IssueResponse
		req := withURLParam(asUser(newRequest("GET", "/api/issues/"+f.publicIssue, nil), user), "id", f.publicIssue)
		testutil.Call(t, testHandler.GetIssue, req).Want(200).JSON(&issue)
		return issue
	}
	owner := get(testUserID)
	if len(owner.BlockedBy) != 1 || owner.BlockedBy[0].ID != f.secretIssue {
		t.Fatalf("authorized owner lost the reference: %+v", owner.BlockedBy)
	}
	outsider := get(f.outsider)
	if len(outsider.BlockedBy) != 0 || outsider.BlockedCount != 1 {
		t.Fatalf("outsider projection = %+v", outsider)
	}
	publisher := *testHandler
	publisher.Bus = events.New()
	var broadcast string
	publisher.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) { raw, _ := json.Marshal(e.Payload); broadcast = string(raw) })
	update := withURLParam(newRequest("PUT", "/api/issues/"+f.publicIssue, map[string]any{"title": f.marker + " updated public issue"}), "id", f.publicIssue)
	testutil.Call(t, publisher.UpdateIssue, update).Want(200)
	if broadcast == "" || strings.Contains(broadcast, f.secretIssue) || strings.Contains(broadcast, f.marker+" secret issue") {
		t.Fatalf("public broadcast leaked or was missing: %s", broadcast)
	}
	req := withURLParam(asUser(newRequest("PUT", "/api/issues/"+f.publicIssue, map[string]any{"blocked_by": []string{f.secretIssue}}), f.outsider), "id", f.publicIssue)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(400)
	dbfx.Exec(t, "UPDATE issue SET assignee_type='member',assignee_id=$2 WHERE id=$1", f.publicIssue, f.outsider)
	dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", f.secretIssue)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	if err := svc.TickWorkspaces(ctx, parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	var details string
	dbfx.QueryRow(t, "SELECT details::text FROM inbox_item WHERE issue_id=$1 AND type='blockers_done'", f.publicIssue).Scan(&details)
	if strings.Contains(details, f.secretIssue) || strings.Contains(details, f.marker+" secret issue") {
		t.Fatalf("inbox leaked a private prerequisite: %s", details)
	}
	var activity string
	dbfx.QueryRow(t, "SELECT details::text FROM activity_log WHERE issue_id=$1 AND action='wakeup_triggered' ORDER BY created_at DESC LIMIT 1", f.publicIssue).Scan(&activity)
	if strings.Contains(activity, f.secretIssue) || strings.Contains(activity, f.marker+" secret issue") {
		t.Fatalf("activity leaked a private prerequisite: %s", activity)
	}
}

func TestIssueBlockerDeletingStagePredecessorReleasesWaitingIssue(t *testing.T) {
	parent := blockerTestIssue(t, map[string]any{"status": "todo"})
	earlier := dbfx.Issue(t, "deleted stage predecessor", testutil.Cols{"parent_issue_id": parent.ID, "stage": 1})
	waiting := blockerTestIssue(t, map[string]any{"status": "todo", "parent_issue_id": parent.ID, "stage": 2, "assignee_type": "agent", "assignee_id": handlerTestAgentID(t)})
	if waiting.BlockedCount != 1 {
		t.Fatalf("later stage was not waiting: %+v", waiting)
	}
	req := withURLParam(newRequest("DELETE", "/api/issues/"+earlier, nil), "id", earlier)
	testutil.Call(t, testHandler.DeleteIssue, req).Want(204)
	svc := service.IssueWakeupService{Tasks: testHandler.TaskService}
	if err := svc.TickWorkspaces(context.Background(), parseUUID(testWorkspaceID)); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND context->>'wakeup_system'='blockers_done'", waiting.ID); n != 1 {
		t.Fatalf("deleting predecessor queued %d runs", n)
	}
	var current IssueResponse
	req = withURLParam(newRequest("GET", "/api/issues/"+waiting.ID, nil), "id", waiting.ID)
	testutil.Call(t, testHandler.GetIssue, req).Want(200).JSON(&current)
	if current.BlockedCount != 0 || current.Status != "todo" {
		t.Fatalf("release changed status or left a blocker: %+v", current)
	}
}
