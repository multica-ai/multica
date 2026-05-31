package handler

// CEREBRO-PATCH(orchestration-cerebro): FIR-2564 — integration tests for the
// `orchestrate` label trigger. Cerebro-only test file living in the handler
// package; see orchestration_cerebro.go for the implementation.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	parent   db.Issue
	childA   db.Issue
	childB   db.Issue
	labelID  db.GetLabelParams // carries ID + WorkspaceID for convenience
	agentID  string            // the child worker agent
	leaderID string            // the squad leader = independent verifier
}

func createBacklogChild(t *testing.T, parentID, title string) db.Issue {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": title + " " + time.Now().Format(time.RFC3339Nano),
		// Acceptance criteria so the plan-adequacy precheck passes.
		"description":     "Acceptance criteria:\n- [ ] delivered as planned",
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

	// A second agent (sharing the test runtime) to be the squad leader =
	// independent verifier, distinct from the child worker agent.
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Handler Verifier Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		ON CONFLICT (workspace_id, name) DO UPDATE SET runtime_id = EXCLUDED.runtime_id
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&leaderID); err != nil {
		t.Fatalf("create verifier agent: %v", err)
	}

	// Squad led by the verifier agent; the parent is assigned to it (squad-only
	// gate). Created before the parent so we can assign it immediately.
	var squadID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		 VALUES ($1, $2, '', $3, $4) RETURNING id`,
		testWorkspaceID, "Orchestrate Fixture Squad "+time.Now().Format(time.RFC3339Nano), leaderID, testUserID,
	).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

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

	// Parent → squad (squad-only gate); both children → the worker agent.
	setIssueAssigneeDirect(t, parentResp.ID, "squad", squadID)
	for _, id := range []string{uuidToString(childA.ID), uuidToString(childB.ID)} {
		setIssueAssigneeDirect(t, id, "agent", agentID)
	}
	// Reload parent so AssigneeType=squad is populated for the gate checks.
	parent, _ = testHandler.Queries.GetIssue(ctx, parent.ID)
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
		parent:   parent,
		childA:   childA,
		childB:   childB,
		labelID:  db.GetLabelParams{ID: label.ID, WorkspaceID: parent.WorkspaceID},
		agentID:  agentID,
		leaderID: leaderID,
	}
}

// getOrCreateTestLabel returns a workspace label by name, creating it only if
// absent. The orchestration engine itself ensures the `orch-verified` /
// `orch-rejected` labels exist, so tests must not assume they can create them.
func getOrCreateTestLabel(t *testing.T, workspaceID db.GetLabelParams, name, color string) db.IssueLabel {
	t.Helper()
	ctx := context.Background()
	labels, err := testHandler.Queries.ListLabels(ctx, workspaceID.WorkspaceID)
	if err != nil {
		t.Fatalf("list labels: %v", err)
	}
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l.Name), name) {
			return l
		}
	}
	created, err := testHandler.Queries.CreateLabel(ctx, db.CreateLabelParams{
		WorkspaceID: workspaceID.WorkspaceID, Name: name, Color: color,
	})
	if err != nil {
		t.Fatalf("create label %q: %v", name, err)
	}
	return created
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

// CEREBRO-PATCH(orchestration-verify-gate): FIR-2564 — squad-gate + plan-adequacy tests.
// TestOrchestrateRefusesNonSquadParent: the label only triggers when the parent
// is assigned to a squad. An agent-assigned parent starts nothing and is told to
// assign a squad.
func TestOrchestrateRefusesNonSquadParent(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	// Re-assign the parent away from the squad, to an agent.
	setIssueAssigneeDirect(t, uuidToString(fx.parent.ID), "agent", fx.agentID)
	parent, _ := testHandler.Queries.GetIssue(ctx, fx.parent.ID)

	testHandler.maybeStartOrchestrationOnLabel(ctx, parent, fx.labelID.ID)

	if got := issueStatus(t, fx.childA); got != "backlog" {
		t.Errorf("non-squad parent must start nothing; childA got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 0 {
		t.Errorf("non-squad parent must enqueue nothing, got %d", got)
	}
	content := parentSystemCommentContent(t, uuidToString(fx.parent.ID))
	if !strings.Contains(strings.ToLower(content), "squad") {
		t.Errorf("expected a squad-required explanation, got: %s", content)
	}
}

// TestOrchestrateRefusesPlanWithoutCriteria: the plan-adequacy precheck refuses a
// plan whose sub-issues lack acceptance criteria, naming the offenders.
func TestOrchestrateRefusesPlanWithoutCriteria(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	// Strip childA's acceptance criteria.
	if _, err := testPool.Exec(ctx,
		`UPDATE issue SET description = 'no criteria here' WHERE id = $1`, fx.childA.ID); err != nil {
		t.Fatalf("clear childA criteria: %v", err)
	}

	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, fx.labelID.ID)

	if got := issueStatus(t, fx.childA); got != "backlog" {
		t.Errorf("under-specified plan must start nothing; childA got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 0 {
		t.Errorf("under-specified plan must enqueue nothing, got %d", got)
	}
	content := parentSystemCommentContent(t, uuidToString(fx.parent.ID))
	if !strings.Contains(strings.ToLower(content), "acceptance criteria") {
		t.Errorf("expected an acceptance-criteria explanation, got: %s", content)
	}
	if !strings.Contains(content, "#"+strconv.Itoa(int(fx.childA.Number))) {
		t.Errorf("explanation should name the offending sub-issue, got: %s", content)
	}
}

// TestOrchestrateGateHoldsUntilVerified: childA reaching `done` is NOT enough to
// release childB — the verification gate holds it until childA is independently
// verified (`orch-verified`). The done-but-unverified child triggers a
// verification request comment; once verified, childB is promoted and enqueued.
func TestOrchestrateGateHoldsUntilVerified(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, fx.labelID.ID)
	commentsBefore := countSystemCommentsOn(t, uuidToString(fx.parent.ID))

	// Mark childA done and fire the advance hook with a realistic prev/next.
	prevA, _ := testHandler.Queries.GetIssue(ctx, fx.childA.ID)
	doneA, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: fx.childA.ID, Status: "done", WorkspaceID: fx.parent.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("mark childA done: %v", err)
	}
	testHandler.advanceOrchestrationOnChildDone(ctx, prevA, doneA)

	// Gate: childB must NOT be released on a bare `done`.
	if got := issueStatus(t, fx.childB); got != "backlog" {
		t.Errorf("childB must stay backlog while blocker is done-but-unverified, got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childB.ID), fx.agentID); got != 0 {
		t.Errorf("childB must not be enqueued before verification, got %d", got)
	}
	// The independent verifier (squad leader, NOT the worker) was dispatched on
	// childA, and a verification note was posted on the parent.
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.leaderID); got != 1 {
		t.Errorf("independent verifier (leader) should have 1 task on childA, got %d", got)
	}
	if got := countSystemCommentsOn(t, uuidToString(fx.parent.ID)); got <= commentsBefore {
		t.Errorf("expected a verification request comment, count did not grow (%d)", got)
	}

	// Independent verifier passes: attach `orch-verified` to childA.
	verified := getOrCreateTestLabel(t, fx.labelID, "orch-verified", "#16a34a")
	if err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID: fx.childA.ID, LabelID: verified.ID, WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("attach verified label: %v", err)
	}
	// Reload childA so ParentIssueID is populated, then fire the verdict hook.
	verifiedA, _ := testHandler.Queries.GetIssue(ctx, fx.childA.ID)
	testHandler.maybeStartOrchestrationOnLabel(ctx, verifiedA, verified.ID)

	if got := issueStatus(t, fx.childB); got != "todo" {
		t.Errorf("childB should be promoted after blocker verified, got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childB.ID), fx.agentID); got != 1 {
		t.Errorf("childB should have 1 enqueued task after verification, got %d", got)
	}
}

// TestOrchestrateRejectReopensChild: attaching `orch-rejected` to a done child
// re-opens it (back to todo) and re-dispatches its worker, so failed work is
// redone instead of silently releasing dependents.
func TestOrchestrateRejectReopensChild(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, fx.labelID.ID)

	// childA is done (claimed) but fails verification.
	if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: fx.childA.ID, Status: "done", WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("mark childA done: %v", err)
	}
	// Clear the first-wave task so the re-dispatch count is unambiguous.
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.childA.ID)

	rejected := getOrCreateTestLabel(t, fx.labelID, "orch-rejected", "#dc2626")
	if err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID: fx.childA.ID, LabelID: rejected.ID, WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("attach rejected label: %v", err)
	}
	rejectedA, _ := testHandler.Queries.GetIssue(ctx, fx.childA.ID)
	testHandler.maybeStartOrchestrationOnLabel(ctx, rejectedA, rejected.ID)

	if got := issueStatus(t, fx.childA); got != "todo" {
		t.Errorf("rejected childA should be re-opened to todo, got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 1 {
		t.Errorf("rejected childA should be re-dispatched once, got %d", got)
	}
	// Dependents stay blocked — childA is no longer done.
	if got := issueStatus(t, fx.childB); got != "backlog" {
		t.Errorf("childB must stay backlog while childA reworks, got %q", got)
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

// CEREBRO-PATCH(orchestration-cerebro): FIR-2564 squad path test.
// TestOrchestrateStartsSquadChild verifies the primary path: a sub-issue
// assigned to a SQUAD is promoted out of backlog and the squad LEADER is woken
// when the parent gets the `orchestrate` label.
func TestOrchestrateStartsSquadChild(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Leader = the seeded "Handler Test Agent" (has a runtime).
	var leaderID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&leaderID); err != nil {
		t.Fatalf("locate test agent: %v", err)
	}
	var squadID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		 VALUES ($1, $2, '', $3, $4) RETURNING id`,
		testWorkspaceID, "Orchestrate Squad", leaderID, testUserID,
	).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	// Parent (active) + one squad-assigned backlog child.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "orch squad parent " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: %d %s", w.Code, w.Body.String())
	}
	parentResp := decodeIssue(t, w)
	// Parent assigned to the squad (squad-only gate).
	setIssueAssigneeDirect(t, parentResp.ID, "squad", squadID)
	parent, err := testHandler.Queries.GetIssue(ctx, parseUUID(parentResp.ID))
	if err != nil {
		t.Fatalf("reload parent: %v", err)
	}
	child := createBacklogChild(t, parentResp.ID, "squad child")
	setIssueAssigneeDirect(t, uuidToString(child.ID), "squad", squadID)
	child, _ = testHandler.Queries.GetIssue(ctx, child.ID)

	label, err := testHandler.Queries.CreateLabel(ctx, db.CreateLabelParams{
		WorkspaceID: parent.WorkspaceID, Name: "orchestrate", Color: "#a855f7",
	})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, child.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, child.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM issue_label WHERE id = $1`, label.ID)
	})

	testHandler.maybeStartOrchestrationOnLabel(ctx, parent, label.ID)

	got, err := testHandler.Queries.GetIssue(ctx, child.ID)
	if err != nil {
		t.Fatalf("reload child: %v", err)
	}
	if got.Status != "todo" {
		t.Errorf("squad child should be promoted to todo, got %q", got.Status)
	}
	if n := countPendingTasksForAgent(t, uuidToString(child.ID), leaderID); n != 1 {
		t.Errorf("squad leader should have 1 pending task, got %d", n)
	}
}
