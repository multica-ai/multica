package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

func TestPluginIssueListHonorsCallbackIssueScope(t *testing.T) {
	withCallbackTokens(t)
	installationID := installPluginForAction(t, []string{"issues:read"})
	granted := dbfx.Issue(t, "plugin-list callback granted", testutil.Cols{"status": "todo"})
	_ = dbfx.Issue(t, "plugin-list callback other", testutil.Cols{"status": "todo"})

	token := issueCallbackToken(t, installationID, service.HookActor{Type: "member", ID: parseUUID(testUserID)})
	unscopedRecorder := httptest.NewRecorder()
	testHandler.ListPluginIssues(unscopedRecorder, callbackRequest(token, http.MethodGet, "/v1/issues", nil, nil))
	if unscopedRecorder.Code != http.StatusOK {
		t.Fatalf("unscoped callback list status=%d body=%s", unscopedRecorder.Code, unscopedRecorder.Body.String())
	}
	var all publicapiv1.IssueListResponse
	if err := json.Unmarshal(unscopedRecorder.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode unscoped callback list: %v", err)
	}
	if len(all.Issues) < 2 {
		t.Fatalf("unscoped callback list returned %d issues, want the workspace collection", len(all.Issues))
	}

	installation, err := testHandler.PluginService.InstallationForWorkspace(context.Background(), parseUUID(testWorkspaceID), installationID)
	if err != nil {
		t.Fatalf("load installation: %v", err)
	}
	scopedToken, err := testHandler.PluginService.Callbacks.Issue(context.Background(), service.HookInvocation{
		Installation: installation,
		Hook:         plugincontract.Hook{Key: "summarize"},
		Trigger:      plugincontract.TriggerManual,
		Actor:        service.HookActor{Type: "member", ID: parseUUID(testUserID)},
		IssueID:      parseUUID(granted),
	})
	if err != nil {
		t.Fatalf("issue scoped callback token: %v", err)
	}
	scopedRecorder := httptest.NewRecorder()
	testHandler.ListPluginIssues(scopedRecorder, callbackRequest(scopedToken, http.MethodGet, "/v1/issues?limit=50", nil, nil))
	if scopedRecorder.Code != http.StatusOK {
		t.Fatalf("scoped callback list status=%d body=%s", scopedRecorder.Code, scopedRecorder.Body.String())
	}
	var one publicapiv1.IssueListResponse
	if err := json.Unmarshal(scopedRecorder.Body.Bytes(), &one); err != nil {
		t.Fatalf("decode scoped callback list: %v", err)
	}
	if len(one.Issues) != 1 || one.Issues[0].ID != granted || one.NextCursor != "" {
		t.Fatalf("scoped callback list = %+v, want only %s", one, granted)
	}
}

func TestPluginIssueTaskCreatesAttributedSafeTask(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "tasks:write"})
	issueID := dbfx.Issue(t, "plugin task creation", testutil.Cols{
		"status": "in_progress", "priority": "low",
	})
	agentID := createHandlerTestAgent(t, "Plugin Task Target", nil)

	recorder := httptest.NewRecorder()
	testHandler.CreatePluginIssueTask(recorder, pluginActionRequest(http.MethodPost, "/v1/tasks", installationID,
		map[string]any{
			"agent_id":     agentID,
			"handoff_note": "run during the idle window",
			"priority":     "high",
		}, map[string]string{"issue_ref": issueID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create plugin task status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var task publicapiv1.IssueTask
	if err := json.Unmarshal(recorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("decode plugin task: %v", err)
	}
	if task.ID == "" || task.IssueID != issueID || task.AgentID != agentID || task.RuntimeID == "" ||
		task.Status != "queued" || task.Priority != "high" || task.CreatedAt == "" {
		t.Fatalf("unexpected plugin task response: %+v", task)
	}
	var raw map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw plugin task: %v", err)
	}
	for _, forbidden := range []string{"result", "error", "failure_reason", "runtime_config", "api_key"} {
		if _, leaked := raw[forbidden]; leaked {
			t.Fatalf("plugin task response leaked %q: %s", forbidden, recorder.Body.String())
		}
	}

	var originatorSource, evidenceKind, evidenceRef string
	dbfx.QueryRow(t, `
		SELECT originator_source, trigger_evidence_kind, trigger_evidence_ref_id::text
		FROM agent_task_queue WHERE id = $1
	`, task.ID).Scan(&originatorSource, &evidenceKind, &evidenceRef)
	if originatorSource != "direct_human" || evidenceKind != "plugin_installation" || evidenceRef != installationID {
		t.Fatalf("task attribution = %q/%q/%s, want direct_human/plugin_installation/%s",
			originatorSource, evidenceKind, evidenceRef, installationID)
	}

	duplicate := httptest.NewRecorder()
	testHandler.CreatePluginIssueTask(duplicate, pluginActionRequest(http.MethodPost, "/v1/tasks", installationID,
		map[string]any{"agent_id": agentID}, map[string]string{"issue_ref": issueID}))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate plugin task status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	var problem publicapiv1.Problem
	if err := json.Unmarshal(duplicate.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode duplicate problem: %v", err)
	}
	if problem.Code != "duplicate_pending_task" {
		t.Fatalf("duplicate problem code = %q", problem.Code)
	}
}

func TestPluginIssueTaskRequiresWriteScopeAndValidAgent(t *testing.T) {
	readOnly := installPluginForAction(t, []string{"issues:read"})
	issueID := dbfx.Issue(t, "plugin task scope", testutil.Cols{"status": "todo"})
	agentID := createHandlerTestAgent(t, "Plugin Task Scope Target", nil)

	scopeDenied := httptest.NewRecorder()
	testHandler.CreatePluginIssueTask(scopeDenied, pluginActionRequest(http.MethodPost, "/v1/tasks", readOnly,
		map[string]any{"agent_id": agentID}, map[string]string{"issue_ref": issueID}))
	if scopeDenied.Code != http.StatusForbidden {
		t.Fatalf("scope denied status=%d body=%s", scopeDenied.Code, scopeDenied.Body.String())
	}

	installationID := installPluginForAction(t, []string{"issues:read", "tasks:write"})
	noRuntimeAgent := dbfx.Agent(t, "Plugin Task No Runtime", "")
	noRuntime := httptest.NewRecorder()
	testHandler.CreatePluginIssueTask(noRuntime, pluginActionRequest(http.MethodPost, "/v1/tasks", installationID,
		map[string]any{"agent_id": noRuntimeAgent}, map[string]string{"issue_ref": issueID}))
	if noRuntime.Code != http.StatusBadRequest {
		t.Fatalf("no-runtime status=%d body=%s", noRuntime.Code, noRuntime.Body.String())
	}

	archivedAgent := createHandlerTestAgent(t, "Plugin Task Archived", nil)
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, archivedAgent)
	archived := httptest.NewRecorder()
	testHandler.CreatePluginIssueTask(archived, pluginActionRequest(http.MethodPost, "/v1/tasks", installationID,
		map[string]any{"agent_id": archivedAgent}, map[string]string{"issue_ref": issueID}))
	if archived.Code != http.StatusBadRequest {
		t.Fatalf("archived status=%d body=%s", archived.Code, archived.Body.String())
	}

	var foreignOwnerID, foreignWorkspaceID, foreignAgentID string
	dbfx.QueryRow(t, `
		INSERT INTO "user" (name, email) VALUES ('Plugin Foreign Agent Owner', $1) RETURNING id
	`, "plugin-foreign-agent-"+testUserID+"@multica.test").Scan(&foreignOwnerID)
	dbfx.QueryRow(t, `
		INSERT INTO workspace (name, slug) VALUES ('Plugin Foreign Agents', $1) RETURNING id
	`, "plugin-foreign-agents-"+testUserID).Scan(&foreignWorkspaceID)
	dbfx.Exec(t, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, foreignWorkspaceID, foreignOwnerID)
	dbfx.QueryRow(t, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, visibility, permission_mode, owner_id)
		VALUES ($1, 'Plugin Foreign Target', 'cloud', '{}'::jsonb, 'workspace', 'public_to', $2)
		RETURNING id
	`, foreignWorkspaceID, foreignOwnerID).Scan(&foreignAgentID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, foreignAgentID)
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1`, foreignWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, foreignOwnerID)
	})

	crossWorkspace := httptest.NewRecorder()
	testHandler.CreatePluginIssueTask(crossWorkspace, pluginActionRequest(http.MethodPost, "/v1/tasks", installationID,
		map[string]any{"agent_id": foreignAgentID}, map[string]string{"issue_ref": issueID}))
	if crossWorkspace.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace agent status=%d body=%s", crossWorkspace.Code, crossWorkspace.Body.String())
	}
}

func TestPluginIssueTaskInstallTokenFailsClosedWithoutInstallerMembership(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "tasks:write"})
	issueID := dbfx.Issue(t, "plugin task install token", testutil.Cols{"status": "todo"})
	agentID := createHandlerTestAgent(t, "Plugin Install Token Target", nil)
	token, err := testHandler.PluginService.IssueInstallToken(context.Background(), parseUUID(installationID))
	if err != nil {
		t.Fatalf("issue install token: %v", err)
	}

	allowed := httptest.NewRecorder()
	testHandler.CreatePluginIssueTask(allowed, pluginInstallTokenRequest(http.MethodPost, "/v1/tasks", token,
		map[string]any{"agent_id": agentID, "priority": "low"}, map[string]string{"issue_ref": issueID}))
	if allowed.Code != http.StatusCreated {
		t.Fatalf("install-token task status=%d body=%s", allowed.Code, allowed.Body.String())
	}

	revokedInstallationID := installPluginForAction(t, []string{"issues:read", "tasks:write"})
	revokedIssueID := dbfx.Issue(t, "plugin task revoked installer", testutil.Cols{"status": "todo"})
	revokedToken, err := testHandler.PluginService.IssueInstallToken(context.Background(), parseUUID(revokedInstallationID))
	if err != nil {
		t.Fatalf("issue revoked install token: %v", err)
	}
	var temporaryUserID string
	dbfx.QueryRow(t, `INSERT INTO "user" (name, email) VALUES ('Temporary Installer', $1) RETURNING id`,
		"temporary-installer-"+testUserID+"@multica.test").Scan(&temporaryUserID)
	dbfx.Exec(t, `UPDATE plugin_installation SET installed_by = $1 WHERE id = $2`, temporaryUserID, revokedInstallationID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, temporaryUserID)
	})

	revoked := httptest.NewRecorder()
	testHandler.CreatePluginIssueTask(revoked, pluginInstallTokenRequest(http.MethodPost, "/v1/tasks", revokedToken,
		map[string]any{"agent_id": agentID}, map[string]string{"issue_ref": revokedIssueID}))
	if revoked.Code != http.StatusForbidden {
		t.Fatalf("revoked installer status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	var problem publicapiv1.Problem
	if err := json.Unmarshal(revoked.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode revoked installer problem: %v", err)
	}
	if problem.Code != "installer_membership_revoked" {
		t.Fatalf("revoked installer problem code = %q", problem.Code)
	}
}

func TestPluginIssueTaskCallbackRequiresAMemberActor(t *testing.T) {
	withCallbackTokens(t)
	installationID := installPluginForAction(t, []string{"issues:read", "tasks:write"})
	issueID := dbfx.Issue(t, "plugin task callback", testutil.Cols{"status": "todo"})
	agentID := createHandlerTestAgent(t, "Plugin Callback Task Target", nil)
	token := issueCallbackToken(t, installationID, service.HookActor{Type: "plugin", ID: parseUUID(installationID)})

	recorder := httptest.NewRecorder()
	testHandler.CreatePluginIssueTask(recorder, callbackRequest(token, http.MethodPost, "/v1/tasks",
		map[string]any{"agent_id": agentID}, map[string]string{"issue_ref": issueID}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("plugin callback task status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var problem publicapiv1.Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode callback problem: %v", err)
	}
	if problem.Code != "member_required" {
		t.Fatalf("callback problem code = %q", problem.Code)
	}
}
