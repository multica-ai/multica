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

	"github.com/jackc/pgx/v5/pgtype"
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
	squadID  string            // the squad the parent is assigned to
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
			[]string{uuidToString(childA.ID), uuidToString(childB.ID), uuidToString(parent.ID)})
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
		squadID:  squadID,
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

	// No sub-issue work starts on an under-specified plan.
	if got := issueStatus(t, fx.childA); got != "backlog" {
		t.Errorf("under-specified plan must start nothing; childA got %q", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 0 {
		t.Errorf("under-specified plan must enqueue nothing, got %d", got)
	}
	// CEREBRO-PATCH(orchestration-plan-refinement): FIR-2564 — leader-finishes-plan assertion.
	// The SQUAD LEADER (not the human) is dispatched on the parent to finish the
	// plan, with a comment that names the offending sub-issue.
	if got := countPendingTasksForAgent(t, uuidToString(fx.parent.ID), fx.leaderID); got != 1 {
		t.Errorf("squad leader should be dispatched once to finish the plan, got %d", got)
	}
	content := parentSystemCommentContent(t, uuidToString(fx.parent.ID))
	if !strings.Contains(strings.ToLower(content), "acceptance criteria") {
		t.Errorf("expected an acceptance-criteria explanation, got: %s", content)
	}
	if !strings.Contains(strings.ToLower(content), "squad leader") {
		t.Errorf("explanation should address the squad leader, got: %s", content)
	}
	if !strings.Contains(content, "#"+strconv.Itoa(int(fx.childA.Number))) {
		t.Errorf("explanation should name the offending sub-issue, got: %s", content)
	}
}

// TestOrchestrateDecomposesEmptyParent: a labeled, squad-assigned parent with NO
// sub-issues dispatches the SQUAD LEADER (not the human owner) to break it down,
// with a comment that explains there are no sub-issues yet.
func TestOrchestrateDecomposesEmptyParent(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	// Remove both sub-issues so the parent is labeled + squad-assigned but empty.
	for _, id := range []string{uuidToString(fx.childA.ID), uuidToString(fx.childB.ID)} {
		if _, err := testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id); err != nil {
			t.Fatalf("delete child: %v", err)
		}
	}

	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, fx.labelID.ID)

	// CEREBRO-PATCH(orchestration-decompose): FIR-2564 — empty-parent decomposition assertion.
	// The squad leader (not the owner) is dispatched on the parent to decompose it.
	if got := countPendingTasksForAgent(t, uuidToString(fx.parent.ID), fx.leaderID); got != 1 {
		t.Errorf("squad leader should be dispatched once to decompose the empty parent, got %d", got)
	}
	content := strings.ToLower(parentSystemCommentContent(t, uuidToString(fx.parent.ID)))
	if !strings.Contains(content, "no sub-issues") {
		t.Errorf("expected a 'no sub-issues yet' explanation, got: %s", content)
	}
	if !strings.Contains(content, "squad leader") {
		t.Errorf("explanation should address the squad leader, got: %s", content)
	}
}

// TestOrchestrationLeaderDispatchCarriesHumanOrigin: the engine's leader dispatch
// must stamp the issue's human owner as original_user_id, so the leader can wake a
// downstream verifier/QA agent — without it the handoff is saved but never started
// and the flow stalls one step short.
func TestOrchestrationLeaderDispatchCarriesHumanOrigin(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	// CEREBRO-PATCH(orchestration-handoff-provenance): FIR-2564 — resolver finds the
	// human creator, and the dispatched leader task carries it as original_user_id.
	actor, ok := testHandler.resolveOrchestrationActor(ctx, fx.parent)
	if !ok || uuidToString(actor) != testUserID {
		t.Fatalf("resolveOrchestrationActor = %s ok=%v; want %s", uuidToString(actor), ok, testUserID)
	}

	testHandler.enqueueOrchestrationLeaderTask(ctx, fx.parent, pgtype.UUID{})

	var originalUserID string
	if err := testPool.QueryRow(ctx,
		`SELECT original_user_id::text FROM agent_task_queue
		   WHERE issue_id = $1 AND agent_id = $2
		   ORDER BY created_at DESC LIMIT 1`,
		uuidToString(fx.parent.ID), fx.leaderID,
	).Scan(&originalUserID); err != nil {
		t.Fatalf("load leader task: %v", err)
	}
	if originalUserID != testUserID {
		t.Errorf("leader task original_user_id = %s, want %s", originalUserID, testUserID)
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

// CEREBRO-PATCH(orchestration-verifier-fallback): FIR-2564 — regression test.
// TestOrchestrateVerifierFallsBackToReviewerWhenLeaderIsWorker: when the squad
// leader is also the agent that built a sub-issue (the common small-squad case
// where the leader assigns the work to herself), she cannot independently judge
// her own deliverable. The engine must fall back to an independent squad member
// — here a Reviewer — instead of stranding the run on the human-label gate.
// FIR-2564 regression: this is exactly why the reminder orchestration stalled
// one step before the goal.
func TestOrchestrateVerifierFallsBackToReviewerWhenLeaderIsWorker(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	// A third agent that is the squad's dedicated Reviewer = independent verifier.
	var reviewerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Handler Reviewer Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		ON CONFLICT (workspace_id, name) DO UPDATE SET runtime_id = EXCLUDED.runtime_id
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&reviewerID); err != nil {
		t.Fatalf("create reviewer agent: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO squad_member (squad_id, member_type, member_id, role) VALUES ($1, 'agent', $2, 'Reviewer')`,
		fx.squadID, reviewerID); err != nil {
		t.Fatalf("add reviewer member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.childA.ID)
		testPool.Exec(context.Background(), `DELETE FROM squad_member WHERE squad_id = $1 AND member_id = $2`, fx.squadID, reviewerID)
	})

	// Make the leader the worker on childA (leader builds and owns the sub-issue).
	setIssueAssigneeDirect(t, uuidToString(fx.childA.ID), "agent", fx.leaderID)
	childA, _ := testHandler.Queries.GetIssue(ctx, fx.childA.ID)

	// childA reports done → fire the verification request.
	prevA := childA
	doneA, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: childA.ID, Status: "done", WorkspaceID: fx.parent.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("mark childA done: %v", err)
	}
	testHandler.advanceOrchestrationOnChildDone(ctx, prevA, doneA)

	// The Reviewer — not the leader (who did the work), not the human gate —
	// must be dispatched to verify.
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), reviewerID); got != 1 {
		t.Errorf("independent reviewer should have 1 verify task on childA, got %d", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.leaderID); got != 0 {
		t.Errorf("leader (the worker) must NOT verify her own work, got %d tasks", got)
	}
	// Gate still holds: childB stays blocked until childA is actually verified.
	if got := issueStatus(t, fx.childB); got != "backlog" {
		t.Errorf("childB must stay backlog until childA is verified, got %q", got)
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
// CEREBRO-PATCH(orchestration-comments-eval): FIR-2564 — dedup/attribution + full-eval tests.
// TestOrchestrateCommentDedupAndAttribution: an identical Orchestrator comment
// posted twice is written once, and every engine comment is attributed.
func TestOrchestrateCommentDedupAndAttribution(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	testHandler.postOrchestrationComment(ctx, fx.parent, "duplicate note from a doubly-fired action")
	testHandler.postOrchestrationComment(ctx, fx.parent, "duplicate note from a doubly-fired action")

	if got := countSystemCommentsOn(t, uuidToString(fx.parent.ID)); got != 1 {
		t.Errorf("identical Orchestrator comment should be deduped to 1, got %d", got)
	}
	content := parentSystemCommentContent(t, uuidToString(fx.parent.ID))
	if !strings.Contains(content, "Orchestrator") {
		t.Errorf("engine comment should be attributed to the Orchestrator, got: %s", content)
	}
}

// TestOrchestrateFullDeliveryEval: once every sub-issue is verified-done, the
// orchestrator requests a full-delivery evaluation from the squad leader — once.
func TestOrchestrateFullDeliveryEval(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	verified := getOrCreateTestLabel(t, fx.labelID, "orch-verified", "#16a34a")
	for _, ch := range []db.Issue{fx.childA, fx.childB} {
		if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID: ch.ID, Status: "done", WorkspaceID: fx.parent.WorkspaceID,
		}); err != nil {
			t.Fatalf("mark child done: %v", err)
		}
		if err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
			IssueID: ch.ID, LabelID: verified.ID, WorkspaceID: fx.parent.WorkspaceID,
		}); err != nil {
			t.Fatalf("attach verified: %v", err)
		}
	}

	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, fx.labelID.ID)

	if !testHandler.issueHasLabelNamed(ctx, fx.parent, "orch-eval-done") {
		t.Errorf("parent should be marked orch-eval-done once full-delivery eval is requested")
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.parent.ID), fx.leaderID); got != 1 {
		t.Errorf("squad leader should have 1 full-delivery-eval task, got %d", got)
	}

	// Fires exactly once: a second trigger must not enqueue a second eval task.
	testHandler.maybeStartOrchestrationOnLabel(ctx, fx.parent, fx.labelID.ID)
	if got := countPendingTasksForAgent(t, uuidToString(fx.parent.ID), fx.leaderID); got != 1 {
		t.Errorf("full-delivery eval must fire exactly once, got %d tasks", got)
	}
}

// CEREBRO-PATCH(orchestration-watchdog): FIR-2564 — stall watchdog tests.
// TestOrchestrationWatchdog_RecoversStalledChild: a started sub-issue whose run
// hung (in-flight, no active task) is flagged and re-dispatched once; once a run
// is active again the marker clears and it is not re-dispatched twice.
func TestOrchestrationWatchdog_RecoversStalledChild(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: fx.childA.ID, Status: "in_progress", WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("set in_progress: %v", err)
	}
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.childA.ID)

	wd := NewOrchestrationWatchdog(testHandler)
	wd.stallAfter = -time.Hour // any in-flight child counts as stale

	wd.sweepOnce(ctx, time.Now())

	if !testHandler.issueHasLabelNamed(ctx, fx.childA, "orch-stalled") {
		t.Errorf("stalled child should be flagged orch-stalled")
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 1 {
		t.Errorf("stalled child should be re-dispatched once, got %d", got)
	}

	// A run is active now -> next sweep clears the marker and does not re-dispatch.
	wd.sweepOnce(ctx, time.Now())
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 1 {
		t.Errorf("watchdog must not double-redispatch, got %d", got)
	}
	if testHandler.issueHasLabelNamed(ctx, fx.childA, "orch-stalled") {
		t.Errorf("marker should clear once a run is active again")
	}
}

// CEREBRO-PATCH(orchestration-stalled-handoff): FIR-2687 — stalled-worker takeover tests.
// TestOrchestrationWatchdog_HandsOffPersistentStallToLeader: a child still stalled
// after a prior re-dispatch (carries orch-stalled, no active task) is handed to the
// squad leader (backup builder) to finish — NOT re-dispatched to the same worker and
// NOT frozen. The child is marked orch-handed-off and a note is posted on the parent.
func TestOrchestrationWatchdog_HandsOffPersistentStallToLeader(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: fx.childA.ID, Status: "in_progress", WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("set in_progress: %v", err)
	}
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.childA.ID)
	stalled := getOrCreateTestLabel(t, fx.labelID, "orch-stalled", "#f59e0b")
	testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID: fx.childA.ID, LabelID: stalled.ID, WorkspaceID: fx.parent.WorkspaceID,
	})

	wd := NewOrchestrationWatchdog(testHandler)
	wd.stallAfter = -time.Hour
	before := countSystemCommentsOn(t, uuidToString(fx.parent.ID))

	wd.sweepOnce(ctx, time.Now())

	// The original worker is NOT re-dispatched again...
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 0 {
		t.Errorf("persistent stall must not re-dispatch the original worker, got %d", got)
	}
	// ...the squad leader (backup builder) is dispatched on the child instead...
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.leaderID); got != 1 {
		t.Errorf("squad leader should be handed the stalled child once, got %d", got)
	}
	// ...the child is marked handed-off so the next sweep escalates, not re-hands...
	if !testHandler.issueHasLabelNamed(ctx, fx.childA, "orch-handed-off") {
		t.Errorf("handed-off child should carry the orch-handed-off marker")
	}
	// ...and a hand-off note is posted on the parent.
	if countSystemCommentsOn(t, uuidToString(fx.parent.ID)) <= before {
		t.Errorf("hand-off should post a note on the parent")
	}
}

// TestOrchestrationWatchdog_EscalatesAfterHandoffStillStalled: once a child has
// already been re-dispatched AND handed to the leader (carries both markers) and
// is STILL stalled, the watchdog escalates to a human and does not dispatch the
// leader again — the recovery ladder is bounded, no infinite loop.
func TestOrchestrationWatchdog_EscalatesAfterHandoffStillStalled(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: fx.childA.ID, Status: "in_progress", WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("set in_progress: %v", err)
	}
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.childA.ID)
	stalled := getOrCreateTestLabel(t, fx.labelID, "orch-stalled", "#f59e0b")
	handedOff := getOrCreateTestLabel(t, fx.labelID, "orch-handed-off", "#8b5cf6")
	for _, l := range []db.IssueLabel{stalled, handedOff} {
		testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
			IssueID: fx.childA.ID, LabelID: l.ID, WorkspaceID: fx.parent.WorkspaceID,
		})
	}

	wd := NewOrchestrationWatchdog(testHandler)
	wd.stallAfter = -time.Hour
	before := countSystemCommentsOn(t, uuidToString(fx.parent.ID))

	wd.sweepOnce(ctx, time.Now())

	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.agentID); got != 0 {
		t.Errorf("post-handoff stall must not re-dispatch the worker, got %d", got)
	}
	if got := countPendingTasksForAgent(t, uuidToString(fx.childA.ID), fx.leaderID); got != 0 {
		t.Errorf("post-handoff stall must not re-dispatch the leader again, got %d", got)
	}
	if countSystemCommentsOn(t, uuidToString(fx.parent.ID)) <= before {
		t.Errorf("post-handoff persistent stall should post a human-escalation comment")
	}
}

// CEREBRO-PATCH(orchestration-thread): FIR-2564 — in_review handling tests.
// TestOrchestrateVerifyCompletesInReviewChild: workers often finish into
// in_review (not done). A PASS verdict completes it to done and releases
// dependents.
func TestOrchestrateVerifyCompletesInReviewChild(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: fx.childA.ID, Status: "in_review", WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("set in_review: %v", err)
	}
	verified := getOrCreateTestLabel(t, fx.labelID, "orch-verified", "#16a34a")
	if err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID: fx.childA.ID, LabelID: verified.ID, WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("attach verified: %v", err)
	}
	verifiedA, _ := testHandler.Queries.GetIssue(ctx, fx.childA.ID)

	testHandler.advanceOnChildVerified(ctx, verifiedA)

	if got := issueStatus(t, fx.childA); got != "done" {
		t.Errorf("a verified in_review child should be completed to done, got %q", got)
	}
	if got := issueStatus(t, fx.childB); got != "todo" {
		t.Errorf("childB should release after A is verified+done, got %q", got)
	}
}

// TestOrchestrationWatchdog_NudgesInReviewVerification: an in_review sub-issue
// (worker finished, verification never fired) is nudged to verification, NOT
// treated as stalled.
func TestOrchestrationWatchdog_NudgesInReviewVerification(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: fx.childA.ID, Status: "in_review", WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("set in_review: %v", err)
	}
	testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.childA.ID)

	wd := NewOrchestrationWatchdog(testHandler)
	wd.stallAfter = -time.Hour
	before := countSystemCommentsOn(t, uuidToString(fx.parent.ID))

	wd.sweepOnce(ctx, time.Now())

	if testHandler.issueHasLabelNamed(ctx, fx.childA, "orch-stalled") {
		t.Errorf("in_review child must NOT be flagged stalled")
	}
	if countSystemCommentsOn(t, uuidToString(fx.parent.ID)) <= before {
		t.Errorf("watchdog should nudge verification (post a comment) for an in_review child")
	}
}

// CEREBRO-PATCH(orchestration-status-reconcile): FIR-2564 — verified ⟹ done.
// TestOrchestrationWatchdog_ReconcilesVerifiedToDone: a verified sub-issue that
// got bumped back to a non-done status (e.g. a paused run) is reconciled to done
// so status matches reality.
func TestOrchestrationWatchdog_ReconcilesVerifiedToDone(t *testing.T) {
	fx := newOrchestrationFixture(t)
	ctx := context.Background()

	if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: fx.childA.ID, Status: "in_progress", WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("set in_progress: %v", err)
	}
	verified := getOrCreateTestLabel(t, fx.labelID, "orch-verified", "#16a34a")
	if err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID: fx.childA.ID, LabelID: verified.ID, WorkspaceID: fx.parent.WorkspaceID,
	}); err != nil {
		t.Fatalf("attach verified: %v", err)
	}

	NewOrchestrationWatchdog(testHandler).sweepOnce(ctx, time.Now())

	if got := issueStatus(t, fx.childA); got != "done" {
		t.Errorf("a verified-but-not-done child should be reconciled to done, got %q", got)
	}
}

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
