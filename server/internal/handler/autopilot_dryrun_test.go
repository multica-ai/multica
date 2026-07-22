package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// insertDryRunTestAutopilot creates an autopilot row assigned to the given
// agent with the requested execution mode and optional issue-title template,
// then registers cascade cleanup. Used by the dry-run tests so they can vary
// mode/template without going through the create endpoint.
func insertDryRunTestAutopilot(t *testing.T, agentID, title, mode, titleTemplate string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO autopilot (
			workspace_id, title, description, assignee_type, assignee_id,
			status, execution_mode, issue_title_template, created_by_type, created_by_id
		)
		VALUES ($1, $2, 'do the thing', 'agent', $3, 'active', $4, NULLIF($5, '')::text, 'member', $6)
		RETURNING id
	`, testWorkspaceID, title, agentID, mode, titleTemplate, testUserID).Scan(&id); err != nil {
		t.Fatalf("failed to insert test autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE autopilot_id = $1`, id)
		testPool.Exec(context.Background(), `DELETE FROM autopilot_trigger WHERE autopilot_id = $1`, id)
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, id)
	})
	return id
}

// insertAgentWithOfflineRuntime creates a workspace-visible agent bound to a
// runtime whose status is 'offline', so AgentReadiness reports "agent runtime
// is offline" and a run_only dispatch is blocked. agent.runtime_id is NOT NULL
// with a FK to agent_runtime, so "not ready" must come from an offline bound
// runtime, not a NULL one. Used to exercise the readiness-blocked branch of
// the dry-run plan and the real-trigger skipped-run path.
func insertAgentWithOfflineRuntime(t *testing.T, name string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'offline', $4, '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, name+" runtime", name+"-offline", name+" offline runtime", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("failed to create offline agent_runtime: %v", err)
	}
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 1, $4, '', '{}'::jsonb, '[]'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("failed to create offline-runtime agent: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("failed to seed workspace invocation target: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return agentID
}

// countRows returns the row count for a table filtered by autopilot_id, used
// to assert the dry-run left no run/issue/task behind.
func countRows(t *testing.T, table, autopilotID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE autopilot_id = $1`, autopilotID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// dispatchPlan is the test-side mirror of service.DispatchPlan. Decoding into
// a local struct keeps the test independent of the service package's exact
// field set while still asserting the contract the API exposes.
type dispatchPlan struct {
	Allowed             bool   `json:"allowed"`
	ExecutionMode       string `json:"execution_mode"`
	Source              string `json:"source"`
	AssigneeType        string `json:"assignee_type"`
	AgentID             string `json:"agent_id"`
	AgentName           string `json:"agent_name"`
	AgentReady          bool   `json:"agent_ready"`
	ReadinessReason     string `json:"readiness_reason"`
	InvokeAllowed       bool   `json:"invoke_allowed"`
	SkipReason          string `json:"skip_reason"`
	ReasonCode          string `json:"reason_code"`
	RenderedTitle       string `json:"rendered_title"`
	RenderedDescription string `json:"rendered_description"`
	TaskSummary         string `json:"task_summary"`
}

func callDryRun(t *testing.T, apID string) dispatchPlan {
	t.Helper()
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilots/"+apID+"/trigger?dry_run=true&workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("dry-run: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var plan dispatchPlan
	if err := json.NewDecoder(w.Body).Decode(&plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	return plan
}

// TestTriggerAutopilot_DryRun_CreateIssue exercises the happy path for a
// create_issue autopilot assigned to a ready agent: the plan reports allowed,
// a rendered title, and readiness - and crucially creates no run/issue/task.
func TestTriggerAutopilot_DryRun_CreateIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "dryrun-createissue-agent", nil)
	apID := insertDryRunTestAutopilot(t, agentID, "Daily Report {{date}}", "create_issue", "Daily Report {{date}}")

	plan := callDryRun(t, apID)

	if !plan.Allowed {
		t.Fatalf("expected allowed=true, got skip_reason=%q reason_code=%q", plan.SkipReason, plan.ReasonCode)
	}
	if plan.ExecutionMode != "create_issue" {
		t.Fatalf("execution_mode: got %q want create_issue", plan.ExecutionMode)
	}
	if plan.AgentID == "" || plan.AgentName == "" {
		t.Fatalf("expected resolved agent, got id=%q name=%q", plan.AgentID, plan.AgentName)
	}
	if !plan.AgentReady {
		t.Fatalf("expected agent_ready=true, reason=%q", plan.ReadinessReason)
	}
	if !plan.InvokeAllowed {
		t.Fatalf("expected invoke_allowed=true")
	}
	if plan.RenderedTitle == "" || plan.RenderedTitle == "Daily Report {{date}}" {
		t.Fatalf("expected {{date}} interpolated, got %q", plan.RenderedTitle)
	}
	if plan.RenderedDescription == "" {
		t.Fatalf("expected rendered_description non-empty")
	}
	if plan.TaskSummary != "" {
		t.Fatalf("run_only-only field task_summary should be empty for create_issue, got %q", plan.TaskSummary)
	}

	// Zero side effects: no run, no issue, no task.
	if n := countRows(t, "autopilot_run", apID); n != 0 {
		t.Fatalf("dry-run created %d autopilot_run rows", n)
	}
	if n := countAutopilotIssues(t, apID); n != 0 {
		t.Fatalf("dry-run created %d issues", n)
	}
	if n := countAutopilotTasks(t, apID); n != 0 {
		t.Fatalf("dry-run created %d agent_task_queue rows", n)
	}
}

// TestTriggerAutopilot_DryRun_RunOnly verifies the run_only path renders a
// task summary instead of a title/description, and still creates nothing.
func TestTriggerAutopilot_DryRun_RunOnly(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "dryrun-runonly-agent", nil)
	apID := insertDryRunTestAutopilot(t, agentID, "Nightly Cleanup", "run_only", "")

	plan := callDryRun(t, apID)

	if !plan.Allowed {
		t.Fatalf("expected allowed=true, got skip_reason=%q", plan.SkipReason)
	}
	if plan.ExecutionMode != "run_only" {
		t.Fatalf("execution_mode: got %q want run_only", plan.ExecutionMode)
	}
	if plan.TaskSummary == "" {
		t.Fatalf("expected task_summary non-empty")
	}
	if plan.RenderedTitle != "" || plan.RenderedDescription != "" {
		t.Fatalf("create_issue-only fields should be empty for run_only, got title=%q desc=%q", plan.RenderedTitle, plan.RenderedDescription)
	}
	if n := countRows(t, "autopilot_run", apID); n != 0 {
		t.Fatalf("dry-run created %d autopilot_run rows", n)
	}
	if n := countAutopilotTasks(t, apID); n != 0 {
		t.Fatalf("dry-run created %d agent_task_queue rows", n)
	}
}

// TestTriggerAutopilot_DryRun_ReadinessBlocked verifies a run_only autopilot
// whose agent's runtime is offline reports allowed=false with a typed reason
// and still creates nothing. This is the case users most need to catch before a
// real trigger silently skips.
func TestTriggerAutopilot_DryRun_ReadinessBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := insertAgentWithOfflineRuntime(t, "dryrun-offline-agent")
	apID := insertDryRunTestAutopilot(t, agentID, "Blocked Run", "run_only", "")

	plan := callDryRun(t, apID)

	if plan.Allowed {
		t.Fatalf("expected allowed=false for no-runtime run_only agent")
	}
	if plan.AgentReady {
		t.Fatalf("expected agent_ready=false")
	}
	if plan.ReadinessReason == "" {
		t.Fatalf("expected readiness_reason non-empty")
	}
	if plan.SkipReason == "" {
		t.Fatalf("expected skip_reason non-empty")
	}
	if plan.ReasonCode == "" {
		t.Fatalf("expected typed reason_code non-empty")
	}
	if n := countRows(t, "autopilot_run", apID); n != 0 {
		t.Fatalf("dry-run created %d autopilot_run rows for a blocked plan", n)
	}
}

// TestTriggerAutopilot_DryRun_AuthorizationMatchesRealTrigger guards that the
// dry-run path reuses requireAutopilotWrite: a plain member who cannot write
// the autopilot gets 403 on dry-run just as they would on a real trigger.
func TestTriggerAutopilot_DryRun_AuthorizationMatchesRealTrigger(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	apID := createAutopilotAs(t, "", "dryrun-auth-owner")
	stranger := createPlainMember(t, "dryrun-auth-stranger@multica.test")

	w := httptest.NewRecorder()
	r := newRequestAs(stranger, "POST", "/api/autopilots/"+apID+"/trigger?dry_run=true&workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("dry-run by stranger: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestTriggerAutopilot_RealTriggerUnaffectedByDryRunBranch is the regression
// guard: POSTing /trigger WITHOUT dry_run must still dispatch (create a run)
// rather than return a plan. Uses a run_only + offline-runtime autopilot so the
// real dispatch records a skipped run without the heavier issue/task fan-out,
// keeping the test focused on "the branch did not swallow the real path".
func TestTriggerAutopilot_RealTriggerUnaffectedByDryRunBranch(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := insertAgentWithOfflineRuntime(t, "realtrigger-offline-agent")
	apID := insertDryRunTestAutopilot(t, agentID, "Real Trigger Regression", "run_only", "")

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/autopilots/"+apID+"/trigger?workspace_id="+testWorkspaceID, nil)
	r = withURLParam(r, "id", apID)
	testHandler.TriggerAutopilot(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("real trigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Response must be a run (has id + status), not a dispatch plan (has allowed).
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["allowed"]; ok {
		t.Fatalf("real trigger returned a dispatch plan, not a run: %v", resp)
	}
	if resp["id"] == nil || resp["status"] == nil {
		t.Fatalf("real trigger response missing run id/status: %v", resp)
	}
	if n := countRows(t, "autopilot_run", apID); n != 1 {
		t.Fatalf("real trigger: expected 1 autopilot_run row, got %d", n)
	}
}

// countAutopilotIssues counts issues whose origin_id is the autopilot - the
// marker dispatchCreateIssue stamps via origin_type='autopilot'.
func countAutopilotIssues(t *testing.T, autopilotID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM issue WHERE origin_type = 'autopilot' AND origin_id = $1`, autopilotID).Scan(&n); err != nil {
		t.Fatalf("count autopilot issues: %v", err)
	}
	return n
}

// countAutopilotTasks counts agent_task_queue rows whose autopilot_run_id belongs
// to a run of this autopilot - the link dispatchCreateIssue/dispatchRunOnly
// stamps when it enqueues the assignee task. agent_task_queue has no
// autopilot_id column (it links via autopilot_run_id), so this joins through
// autopilot_run. A dry-run must leave zero.
func countAutopilotTasks(t *testing.T, autopilotID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE autopilot_id = $1)`, autopilotID).Scan(&n); err != nil {
		t.Fatalf("count autopilot tasks: %v", err)
	}
	return n
}
