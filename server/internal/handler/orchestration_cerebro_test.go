package handler

// CEREBRO-PATCH(orchestration-cerebro): FIR-2564 — integration tests for the
// `orchestrate` label trigger. Cerebro-only test file living in the handler
// package; see orchestration_cerebro.go for the implementation.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func decodeIssue(t *testing.T, w *httptest.ResponseRecorder) IssueResponse {
	t.Helper()
	var resp IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	return resp
}

// orchestrationFixture builds a parent with two agent-assigned children where
// childA blocks childB, plus an `orchestrate` label attached to the parent.
// Everything is created in `backlog` so no enqueue fires at setup — the test
// drives the trigger explicitly.
type orchestrationFixture struct {
	parent  db.Issue
	childA  db.Issue
	childB  db.Issue
	labelID db.GetLabelParams // carries ID + WorkspaceID for convenience
	agentID string
}

func createBacklogChild(t *testing.T, parentID, title string) db.Issue {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           title + " " + time.Now().Format(time.RFC3339Nano),
		"status":          "backlog",
		"parent_issue_id": parentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeIssue(t, w)
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(resp.ID))
	if err != nil {
		t.Fatalf("reload child: %v", err)
	}
	return issue
}

func newOrchestrationFixture(t *testing.T) orchestrationFixture {
	t.Helper()
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("locate test agent: %v", err)
	}

	// Parent, active so the engine is allowed to drive it.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "orchestrate parent " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	parentResp := decodeIssue(t, w)
	parent, err := testHandler.Queries.GetIssue(ctx, parseUUID(parentResp.ID))
	if err != nil {
		t.Fatalf("reload parent: %v", err)
	}

	childA := createBacklogChild(t, parentResp.ID, "child A")
	childB := createBacklogChild(t, parentResp.ID, "child B")

	// Assign both children to the test agent (direct write, no trigger).
	for _, id := range []string{parentResp.ID, uuidToString(childA.ID), uuidToString(childB.ID)} {
		setIssueAssigneeDirect(t, id, "agent", agentID)
	}
	// Reload children so AssigneeID is populated for the readiness checks.
	childA, _ = testHandler.Queries.GetIssue(ctx, childA.ID)
	childB, _ = testHandler.Queries.GetIssue(ctx, childB.ID)

	// childA blocks childB.
	if err := testHandler.Queries.CreateBlocksEdge(ctx, db.CreateBlocksEdgeParams{
		IssueID:          childA.ID,
		DependsOnIssueID: childB.ID,
		WorkspaceID:      parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("create blocks edge: %v", err)
	}

	// `orchestrate` label, attached to the parent.
	label, err := testHandler.Queries.CreateLabel(ctx, db.CreateLabelParams{
		WorkspaceID: parent.WorkspaceID,
		Name:        "orchestrate",
		Color:       "#ec4899",
	})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}
	if err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID:     parent.ID,
		LabelID:     label.ID,
		WorkspaceID: parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("attach label: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = ANY($1)`,
			[]string{uuidToString(childA.ID), uuidToString(childB.ID)})
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, childA.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, childB.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parent.ID)
		testPool.Exec(ctx, `DELETE FROM issue_label WHERE id = $1`, label.ID)
	})

	return orchestrationFixture{
		parent:  parent,
		childA:  childA,
		childB:  childB,
		labelID: db.GetLabelParams{ID: label.ID, WorkspaceID: parent.WorkspaceID},
		agentID: agentID,
	}
}

func issueStatus(t *testing.T, issueID db.Issue) string {
	t.Helper()
	got, err := testHandler.Queries.GetIssue(context.Background(), issueID.ID)
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	return got.Status
}

// TestOrchestrateLabelStartsFirstWave: attaching `orchestrate` promotes and
// enqueues childA (no blockers) but leaves childB (blocked by A) untouched,
// and posts a wave-plan summary comment on the parent.
func TestOrchestrateLabelStartsFirstWave(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, fx.labelID.ID)

	if got := issueStatus(t, fx.childA); got != "todo" {
		t.Errorf("childA should be promoted to todo, got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 1 {
		t.Errorf("childA should have 1 enqueued task, got %d", got)
	}
	if got := issueStatus(t, fx.childB); got != "backlog" {
		t.Errorf("childB is blocked and must stay backlog, got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childB.ID), fx.agentID); got != 0 {
		t.Errorf("childB must not be enqueued while blocked, got %d", got)
	}
	content := parentSystemCommentContent(t, uuidToString(fx.parent.ID))
	if !strings.Contains(content, "Wave") {
		t.Errorf("summary comment should describe waves, got: %s", content)
	}
}

// TestOrchestrateAdvancesOnChildDone: once childA is done, the child-done
// entrypoint starts childB.
func TestOrchestrateAdvancesOnChildDone(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, fx.labelID.ID)

	// Mark childA done and fire the advance hook with a realistic prev/next.
	prevA, _ := testHandler.Queries.GetIssue(ctx, fx.childA.ID)
	doneA, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: fx.childA.ID, Status: "done", WorkspaceID: fx.parent.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("mark childA done: %v", err)
	}
	testHandler.advanceOrchestrationOnChildDone(ctx, prevA, doneA)

	if got := issueStatus(t, fx.childB); got != "todo" {
		t.Errorf("childB should be promoted after blocker done, got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childB.ID), fx.agentID); got != 1 {
		t.Errorf("childB should have 1 enqueued task after advance, got %d", got)
	}
}

// TestOrchestrateRejectsCycle: a mutual block between children is rejected with
// an explanatory comment and nothing is started.
func TestOrchestrateRejectsCycle(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	// Add the reverse edge so childA <-> childB form a cycle.
	if err := testHandler.Queries.CreateBlocksEdge(ctx, db.CreateBlocksEdgeParams{
		IssueID:          fx.childB.ID,
		DependsOnIssueID: fx.childA.ID,
		WorkspaceID:      fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("create reverse edge: %v", err)
	}

	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, fx.labelID.ID)

	if got := issueStatus(t, fx.childA); got != "backlog" {
		t.Errorf("cycle must start nothing; childA got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 0 {
		t.Errorf("cycle must enqueue nothing, got %d for childA", got)
	}
	content := parentSystemCommentContent(t, uuidToString(fx.parent.ID))
	if !strings.Contains(strings.ToLower(content), "circular") {
		t.Errorf("expected a circular-dependency explanation, got: %s", content)
	}
}

// TestOrchestrateNonOrchestrateLabelInert: attaching a differently-named label
// must not start anything.
func TestOrchestrateNonOrchestrateLabelInert(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	other, err := testHandler.Queries.CreateLabel(ctx, db.CreateLabelParams{
		WorkspaceID: fx.parent.WorkspaceID, Name: "push", Color: "#000000",
	})
	if err != nil {
		t.Fatalf("create other label: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue_label WHERE id = $1`, other.ID) })

	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, other.ID)

	if got := issueStatus(t, fx.childA); got != "backlog" {
		t.Errorf("non-orchestrate label must be inert; childA got %q", got)
	}
	if got := countSystemCommentsOn(t, uuidToString(fx.parent.ID)); got != 0 {
		t.Errorf("non-orchestrate label must post no comment, got %d", got)
	}
}
