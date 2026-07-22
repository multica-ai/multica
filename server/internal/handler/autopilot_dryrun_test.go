package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/dispatch"
)

// dispatchPlanResponse mirrors the JSON the dry-run endpoint returns. Only the
// fields the tests assert are typed; the rest ride along so a future additive
// field does not break these tests.
type dispatchPlanResponse struct {
	AutopilotID      string         `json:"autopilot_id"`
	ExecutionMode    string         `json:"execution_mode"`
	AssigneeType     string         `json:"assignee_type"`
	Source           string         `json:"source"`
	DryRun           bool           `json:"dry_run"`
	Skipped          bool           `json:"skipped"`
	Reason           string         `json:"reason,omitempty"`
	ReasonCode       string         `json:"reason_code,omitempty"`
	Leader           *planAgentJSON `json:"leader,omitempty"`
	Ready            bool           `json:"ready"`
	ReadinessReason  string         `json:"readiness_reason,omitempty"`
	IssueTitle       string         `json:"issue_title,omitempty"`
	IssueDescription string         `json:"issue_description,omitempty"`
	TaskPrompt       string         `json:"task_prompt,omitempty"`
}

type planAgentJSON struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RuntimeID     string `json:"runtime_id,omitempty"`
	Archived      bool   `json:"archived"`
	SquadResolved bool   `json:"squad_resolved"`
}

// createDryRunAutopilot creates an autopilot via the API as the workspace owner
// (testUserID) with the requested mode/assignee and returns its id. Shared
// setup for the dry-run handler tests.
func createDryRunAutopilot(t *testing.T, body map[string]any) string {
	t.Helper()
	if body["title"] == nil {
		body["title"] = "dryrun ap"
	}
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, body)
	testHandler.CreateAutopilot(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ap AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&ap); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, ap.ID)
		testPool.Exec(ctx, `DELETE FROM autopilot_trigger WHERE autopilot_id = $1`, ap.ID)
		testPool.Exec(ctx, `DELETE FROM autopilot_collaborator WHERE autopilot_id = $1`, ap.ID)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})
	return ap.ID
}

// callDryRunTrigger POSTs /trigger?dry_run=true as the given user (empty =
// workspace owner) and decodes the dispatch plan. It asserts the endpoint
// returned 200 and carried DryRun=true.
func callDryRunTrigger(t *testing.T, userID, apID string) dispatchPlanResponse {
	t.Helper()
	w := httptest.NewRecorder()
	path := "/api/autopilots/" + apID + "/trigger?workspace_id=" + testWorkspaceID + "&dry_run=true"
	var r *http.Request
	if userID == "" {
		r = newRequest("POST", path, nil)
	} else {
		r = newRequestAs(userID, "POST", path, nil)
	}
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("dry-run trigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var plan dispatchPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if !plan.DryRun {
		t.Fatalf("plan.DryRun = false, want true (must distinguish from a real run response)")
	}
	return plan
}

// assertNoDispatchSideEffects fails the test if a dry-run call left behind any
// autopilot_run row for this autopilot. The dispatch flow's first write is
// always the autopilot_run row (the unit of attribution/audit), so zero runs
// implies zero downstream issue/task creation - this is the core WS-749
// guarantee: zero persistent side effects.
func assertNoDispatchSideEffects(t *testing.T, apID string) {
	t.Helper()
	ctx := context.Background()
	var runs int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, apID).Scan(&runs); err != nil {
		t.Fatalf("count autopilot_run: %v", err)
	}
	if runs != 0 {
		t.Fatalf("dry-run must not persist autopilot_run rows; got %d", runs)
	}
}

// TestDryRunAutopilot_CreateIssue previews a create_issue dispatch and asserts
// the plan reports a ready leader plus the rendered title/description, with no
// run row written.
func TestDryRunAutopilot_CreateIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "dryrun-createissue-agent", nil)
	apID := createDryRunAutopilot(t, map[string]any{
		"title":                "dryrun create_issue",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"description":          "watch the dashboard",
		"issue_title_template": "nightly report {{date}}",
	})

	plan := callDryRunTrigger(t, "", apID)

	if plan.ExecutionMode != "create_issue" {
		t.Fatalf("execution_mode = %q, want create_issue", plan.ExecutionMode)
	}
	if plan.Skipped {
		t.Fatalf("expected dispatch to proceed, got skipped: %s (%s)", plan.Reason, plan.ReasonCode)
	}
	if !plan.Ready {
		t.Fatalf("expected ready leader, got not-ready: %s", plan.ReadinessReason)
	}
	if plan.Leader == nil || plan.Leader.ID != agentID {
		t.Fatalf("leader not resolved to the assignee agent: %+v", plan.Leader)
	}
	if plan.Leader.SquadResolved {
		t.Fatalf("direct agent assignee must not report squad_resolved")
	}
	if plan.TaskPrompt != "" {
		t.Fatalf("create_issue must not populate task_prompt: %q", plan.TaskPrompt)
	}
	if !strings.Contains(plan.IssueTitle, "nightly report") {
		t.Fatalf("issue title not rendered from template: %q", plan.IssueTitle)
	}
	if !strings.Contains(plan.IssueDescription, "watch the dashboard") {
		t.Fatalf("issue description must preserve autopilot description: %q", plan.IssueDescription)
	}
	assertNoDispatchSideEffects(t, apID)
}

// TestDryRunAutopilot_RunOnly previews a run_only dispatch and asserts the plan
// surfaces the autopilot description as the task prompt, with no issue fields
// and no persistence.
func TestDryRunAutopilot_RunOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "dryrun-runonly-agent", nil)
	apID := createDryRunAutopilot(t, map[string]any{
		"title":          "dryrun run_only",
		"assignee_id":    agentID,
		"execution_mode": "run_only",
		"description":    "summarize today's PRs",
	})

	plan := callDryRunTrigger(t, "", apID)

	if plan.ExecutionMode != "run_only" {
		t.Fatalf("execution_mode = %q, want run_only", plan.ExecutionMode)
	}
	if plan.Skipped {
		t.Fatalf("expected dispatch to proceed, got skipped: %s (%s)", plan.Reason, plan.ReasonCode)
	}
	if plan.TaskPrompt != "summarize today's PRs" {
		t.Fatalf("task_prompt = %q, want the autopilot description", plan.TaskPrompt)
	}
	if plan.IssueTitle != "" || plan.IssueDescription != "" {
		t.Fatalf("run_only must not populate issue fields: title=%q desc=%q", plan.IssueTitle, plan.IssueDescription)
	}
	assertNoDispatchSideEffects(t, apID)
}

// TestDryRunAutopilot_SquadAssigned previews a squad-assigned autopilot and
// asserts the leader is resolved through the squad (SquadResolved=true) and
// points at the squad's leader agent.
func TestDryRunAutopilot_SquadAssigned(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "dryrun-squad-leader", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'dryrun squad', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID) })

	apID := createDryRunAutopilot(t, map[string]any{
		"title":          "dryrun squad ap",
		"assignee_type":  "squad",
		"assignee_id":    squadID,
		"execution_mode": "create_issue",
	})

	plan := callDryRunTrigger(t, "", apID)

	if plan.AssigneeType != "squad" {
		t.Fatalf("assignee_type = %q, want squad", plan.AssigneeType)
	}
	if plan.Skipped {
		t.Fatalf("expected squad dispatch to proceed, got skipped: %s (%s)", plan.Reason, plan.ReasonCode)
	}
	if plan.Leader == nil {
		t.Fatalf("squad leader must be resolved")
	}
	if !plan.Leader.SquadResolved {
		t.Fatalf("squad autopilot must report squad_resolved=true")
	}
	if plan.Leader.ID != leaderID {
		t.Fatalf("leader id = %q, want squad leader %q", plan.Leader.ID, leaderID)
	}
	assertNoDispatchSideEffects(t, apID)
}

// TestDryRunAutopilot_NotReadyArchivedAgent pins the readiness-blocked path:
// an archived agent is not ready, so the plan reports Skipped=true with a
// target_unavailable reason code, and still writes nothing.
func TestDryRunAutopilot_NotReadyArchivedAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Start from a ready, workspace-invocable agent (so the invocation gate
	// would pass) and archive it, isolating the readiness failure. The autopilot
	// is created first while the agent is still active (CreateAutopilot may
	// reject an archived assignee), then the agent is archived for the preview.
	agentID := createHandlerTestAgent(t, "dryrun-archived-agent", nil)
	apID := createDryRunAutopilot(t, map[string]any{
		"title":          "dryrun archived ap",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
	})
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, agentID); err != nil {
		t.Fatalf("archive agent: %v", err)
	}

	plan := callDryRunTrigger(t, "", apID)

	if !plan.Skipped {
		t.Fatalf("expected skip for archived agent, got proceed")
	}
	if plan.ReasonCode != string(dispatch.ReasonTargetUnavailable) {
		t.Fatalf("reason_code = %q, want %q", plan.ReasonCode, dispatch.ReasonTargetUnavailable)
	}
	if plan.Ready {
		t.Fatalf("expected Ready=false for archived agent")
	}
	if plan.Leader == nil {
		t.Fatalf("leader should still be resolved so the plan names what is blocking")
	}
	if !plan.Leader.Archived {
		t.Fatalf("leader must report archived=true")
	}
	assertNoDispatchSideEffects(t, apID)
}

// TestDryRunAutopilot_PausedAutopilotAllowed proves the dry-run path skips the
// active-status gate a real trigger enforces: a paused autopilot still returns
// a 200 plan rather than a 400 "autopilot is not active".
func TestDryRunAutopilot_PausedAutopilotAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "dryrun-paused-agent", nil)
	apID := createDryRunAutopilot(t, map[string]any{
		"title":          "dryrun paused ap",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
	})
	if _, err := testPool.Exec(ctx, `UPDATE autopilot SET status = 'paused' WHERE id = $1`, apID); err != nil {
		t.Fatalf("pause autopilot: %v", err)
	}

	plan := callDryRunTrigger(t, "", apID)
	if plan.Skipped {
		t.Fatalf("paused-but-ready autopilot should still preview a proceed plan: %s", plan.Reason)
	}
	assertNoDispatchSideEffects(t, apID)
}

// TestDryRunAutopilot_StrangerForbidden enforces that dry-run reuses the real
// trigger's auth: a workspace member who is neither creator nor collaborator
// gets 403, not a free preview.
func TestDryRunAutopilot_StrangerForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "dryrun-stranger-agent", nil)
	apID := createDryRunAutopilot(t, map[string]any{
		"title":          "dryrun stranger ap",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
	})
	stranger := createPlainMember(t, "dryrun-stranger@example.com")

	w := httptest.NewRecorder()
	r := newRequestAs(stranger, "POST", "/api/autopilots/"+apID+"/trigger?workspace_id="+testWorkspaceID+"&dry_run=true", nil)
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("stranger dry-run: expected 403, got %d: %s", w.Code, w.Body.String())
	}
	assertNoDispatchSideEffects(t, apID)
}

// TestDryRunAutopilot_InvalidBoolean rejects a malformed dry_run query value
// with 400 rather than silently treating it as false (and dispatching for
// real) - a typo must never trigger a real run.
func TestDryRunAutopilot_InvalidBoolean(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "dryrun-badbool-agent", nil)
	apID := createDryRunAutopilot(t, map[string]any{
		"title":          "dryrun badbool ap",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
	})

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilots/"+apID+"/trigger?workspace_id="+testWorkspaceID+"&dry_run=notabool", nil)
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid dry_run boolean: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	assertNoDispatchSideEffects(t, apID)
}

// TestDryRunAutopilot_NoPersistenceVsRealTrigger is the integration proof for
// WS-749: a dry-run on a ready create_issue autopilot writes zero rows, while a
// real trigger on the same autopilot writes exactly one autopilot_run. The
// contrast is what proves the dry-run branch is genuinely side-effect-free
// rather than merely returning early on a path that never persisted anyway.
func TestDryRunAutopilot_NoPersistenceVsRealTrigger(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "dryrun-contrast-agent", nil)
	apID := createDryRunAutopilot(t, map[string]any{
		"title":          "dryrun contrast ap",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
	})

	// Dry-run: zero persistence.
	callDryRunTrigger(t, "", apID)
	assertNoDispatchSideEffects(t, apID)

	// Real trigger: exactly one run row, and an issue created.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilots/"+apID+"/trigger?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("real trigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var runs int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, apID).Scan(&runs); err != nil {
		t.Fatalf("count runs after real trigger: %v", err)
	}
	if runs != 1 {
		t.Fatalf("real trigger must persist exactly 1 autopilot_run; got %d", runs)
	}
	var issues int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id = $1 AND title LIKE 'dryrun contrast ap%'`, testWorkspaceID).Scan(&issues); err != nil {
		t.Fatalf("count issues after real trigger: %v", err)
	}
	if issues < 1 {
		t.Fatalf("real trigger must create at least 1 issue; got %d", issues)
	}
}
