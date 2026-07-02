package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
)

func createOpenIdeFixture(t *testing.T, ownerID string) (runtimeID, agentID, issueID string) {
	t.Helper()
	ctx := context.Background()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, 'open-ide-daemon', 'Open IDE Runtime', 'local', 'open_ide_provider', 'online',
		        'open ide test', '{}'::jsonb, $2, 'private', now())
		RETURNING id
	`, testWorkspaceID, ownerID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, runtime_provider, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'open-ide-agent-' || substr($2::text, 1, 8), '', 'local', '{}'::jsonb,
		        $2, 'open_ide_provider', 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_id, creator_type, number, position, project_id
		)
		VALUES (
			$1, 'Open IDE issue', 'in_progress', 'none', $2, 'member',
			(SELECT COALESCE(MAX(number), 83000) + 1 FROM issue WHERE workspace_id = $1),
			0,
			gen_random_uuid()
		)
		RETURNING id
	`, testWorkspaceID, ownerID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	return runtimeID, agentID, issueID
}

func seedOpenIdeTask(t *testing.T, agentID, runtimeID, issueID, status, workDir, createdOffset string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, work_dir, created_at
		)
		VALUES ($1, $2, $3, $4, 0, $5, now() + ($6::interval))
		RETURNING id
	`, agentID, runtimeID, issueID, status, workDir, createdOffset).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func attachOpenIdeGithubProject(t *testing.T, issueID, repoURL string) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'Open IDE Project')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	if _, err := testPool.Exec(context.Background(), `
		UPDATE issue SET project_id = $2 WHERE id = $1
	`, issueID, projectID); err != nil {
		t.Fatalf("attach issue project: %v", err)
	}

	var resourceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project_resource (
			project_id, workspace_id, resource_type, resource_ref, position
		)
		VALUES ($1, $2, 'github_repo', jsonb_build_object('url', $3::text), 0)
		RETURNING id
	`, projectID, testWorkspaceID, repoURL).Scan(&resourceID); err != nil {
		t.Fatalf("create project resource: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_resource WHERE id = $1`, resourceID)
	})
	return projectID
}

func TestOpenIssueInIde_CreatesCommandForRuntimeOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, agentID, issueID := createOpenIdeFixture(t, testUserID)
	taskID := seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\open-ide\workdir`, "-1 minute")

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/ide/open", map[string]any{"ide": "intellij_idea"})
	req = withURLParam(req, "id", issueID)
	testHandler.OpenIssueInIde(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("OpenIssueInIde: expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "queued" {
		t.Fatalf("status = %v, want queued", resp["status"])
	}
	if resp["task_id"] != taskID {
		t.Fatalf("task_id = %v, want %s", resp["task_id"], taskID)
	}
	if _, ok := resp["work_dir"]; ok {
		t.Fatalf("response leaked work_dir: %#v", resp)
	}

	var commandType, payload string
	if err := testPool.QueryRow(context.Background(), `
		SELECT command_type, payload::text
		FROM daemon_command
		WHERE task_id = $1
	`, taskID).Scan(&commandType, &payload); err != nil {
		t.Fatalf("load daemon command: %v", err)
	}
	if commandType != "open_intellij" {
		t.Fatalf("command_type = %q, want open_intellij", commandType)
	}
	if !strings.Contains(payload, "work_dir") {
		t.Fatalf("command payload missing work_dir: %s", payload)
	}
	if !strings.Contains(payload, "branch_name") {
		t.Fatalf("command payload missing branch_name: %s", payload)
	}
	if !strings.Contains(payload, strings.ReplaceAll(taskID, "-", "")[:8]) {
		t.Fatalf("command payload branch_name does not include task short id: %s", payload)
	}
}

func TestGetOpenIdeCommandStatusReturnsDaemonErrorForRequester(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, agentID, issueID := createOpenIdeFixture(t, testUserID)
	taskID := seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\open-ide\workdir`, "-1 minute")

	openW := httptest.NewRecorder()
	openReq := newRequest(http.MethodPost, "/api/issues/"+issueID+"/ide/open", map[string]any{"ide": "intellij_idea"})
	openReq = withURLParam(openReq, "id", issueID)
	testHandler.OpenIssueInIde(openW, openReq)
	if openW.Code != http.StatusAccepted {
		t.Fatalf("OpenIssueInIde: expected 202, got %d: %s", openW.Code, openW.Body.String())
	}

	var commandID string
	if err := testPool.QueryRow(context.Background(), `
		UPDATE daemon_command
		SET status = 'failed', error = 'exec: "idea": executable file not found in %PATH%', completed_at = now()
		WHERE task_id = $1
		RETURNING id
	`, taskID).Scan(&commandID); err != nil {
		t.Fatalf("mark command failed: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/issues/"+issueID+"/ide/commands/"+commandID, nil)
	req = withURLParam(req, "id", issueID)
	req = withURLParam(req, "commandId", commandID)
	testHandler.GetOpenIdeCommandStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetOpenIdeCommandStatus: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "failed" {
		t.Fatalf("status = %v, want failed", resp["status"])
	}
	if resp["error"] != `exec: "idea": executable file not found in %PATH%` {
		t.Fatalf("error = %v", resp["error"])
	}
	if _, ok := resp["payload"]; ok {
		t.Fatalf("response leaked command payload: %#v", resp)
	}
}

func TestGetOpenIdeCommandStatusRejectsOtherRequester(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, agentID, issueID := createOpenIdeFixture(t, testUserID)
	taskID := seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\open-ide\workdir`, "-1 minute")

	openW := httptest.NewRecorder()
	openReq := newRequest(http.MethodPost, "/api/issues/"+issueID+"/ide/open", map[string]any{"ide": "intellij_idea"})
	openReq = withURLParam(openReq, "id", issueID)
	testHandler.OpenIssueInIde(openW, openReq)
	if openW.Code != http.StatusAccepted {
		t.Fatalf("OpenIssueInIde: expected 202, got %d: %s", openW.Code, openW.Body.String())
	}

	var commandID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM daemon_command WHERE task_id = $1
	`, taskID).Scan(&commandID); err != nil {
		t.Fatalf("load command id: %v", err)
	}

	otherUserID := createRuntimeVisibilityAdmin(t)
	w := httptest.NewRecorder()
	req := newRequestAs(otherUserID, http.MethodGet, "/api/issues/"+issueID+"/ide/commands/"+commandID, nil)
	req = withURLParam(req, "id", issueID)
	req = withURLParam(req, "commandId", commandID)
	testHandler.GetOpenIdeCommandStatus(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetOpenIdeCommandStatus as other requester: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenIssueInIde_TaskIDOverridesLatestIssueTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, agentID, issueID := createOpenIdeFixture(t, testUserID)
	targetID := seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\target\workdir`, "-5 minutes")
	seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\latest\workdir`, "-1 minute")

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/ide/open", map[string]any{
		"ide":     "intellij_idea",
		"task_id": targetID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.OpenIssueInIde(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("OpenIssueInIde: expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TaskID != targetID {
		t.Fatalf("task_id = %s, want explicitly requested task %s", resp.TaskID, targetID)
	}
}

func TestOpenIssueInIde_AppendsGithubRepoCheckoutDirectory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, agentID, issueID := createOpenIdeFixture(t, testUserID)
	taskID := seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\open-ide\workdir`, "-1 minute")
	attachOpenIdeGithubProject(t, issueID, "https://github.com/syu1112/multica.git")

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/ide/open", map[string]any{
		"ide":     "intellij_idea",
		"task_id": taskID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.OpenIssueInIde(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("OpenIssueInIde: expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var payload string
	if err := testPool.QueryRow(context.Background(), `
		SELECT payload::text
		FROM daemon_command
		WHERE task_id = $1
	`, taskID).Scan(&payload); err != nil {
		t.Fatalf("load daemon command: %v", err)
	}
	if !strings.Contains(payload, `open-ide\\workdir\\multica`) {
		t.Fatalf("payload did not target repo checkout directory: %s", payload)
	}
}

func TestOpenIssueInIde_RejectsWorkspaceAdminForOtherRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, agentID, issueID := createOpenIdeFixture(t, testUserID)
	seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\open-ide\workdir`, "-1 minute")
	adminUserID := createRuntimeVisibilityAdmin(t)

	w := httptest.NewRecorder()
	req := newRequestAs(adminUserID, http.MethodPost, "/api/issues/"+issueID+"/ide/open", map[string]any{"ide": "intellij_idea"})
	req = withURLParam(req, "id", issueID)
	testHandler.OpenIssueInIde(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("OpenIssueInIde as workspace admin: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenIssueInIde_UsesLatestEligibleTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, agentID, issueID := createOpenIdeFixture(t, testUserID)
	olderID := seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\older\workdir`, "-5 minutes")
	seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", "", "-1 minute")

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/ide/open", map[string]any{"ide": "intellij_idea"})
	req = withURLParam(req, "id", issueID)
	testHandler.OpenIssueInIde(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("OpenIssueInIde: expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TaskID != olderID {
		t.Fatalf("task_id = %s, want latest eligible task %s", resp.TaskID, olderID)
	}
}

func TestClaimDaemonCommandsAllowsRuntimeOwnerToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, agentID, issueID := createOpenIdeFixture(t, testUserID)
	taskID := seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\open-ide\workdir`, "-1 minute")

	openW := httptest.NewRecorder()
	openReq := newRequest(http.MethodPost, "/api/issues/"+issueID+"/ide/open", map[string]any{"ide": "intellij_idea"})
	openReq = withURLParam(openReq, "id", issueID)
	testHandler.OpenIssueInIde(openW, openReq)
	if openW.Code != http.StatusAccepted {
		t.Fatalf("OpenIssueInIde: expected 202, got %d: %s", openW.Code, openW.Body.String())
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/daemon/commands/claim", map[string]any{
		"daemon_id": "open-ide-daemon",
		"limit":     1,
	})
	testHandler.ClaimDaemonCommands(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimDaemonCommands: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Commands []daemonCommandResponse `json:"commands"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Commands) != 1 {
		t.Fatalf("commands len = %d, want 1; body=%s", len(resp.Commands), w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM daemon_command WHERE task_id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("load command status: %v", err)
	}
	if status != "claimed" {
		t.Fatalf("status = %s, want claimed", status)
	}
}

func TestClaimDaemonCommandsRejectsDaemonMismatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/daemon/commands/claim", map[string]any{
		"daemon_id": "other-daemon",
		"limit":     1,
	})
	req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, "open-ide-daemon"))
	testHandler.ClaimDaemonCommands(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("ClaimDaemonCommands daemon mismatch: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompleteDaemonCommandAllowsRuntimeOwnerToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, agentID, issueID := createOpenIdeFixture(t, testUserID)
	taskID := seedOpenIdeTask(t, agentID, runtimeID, issueID, "completed", `C:\Users\imshe\multica_workspaces\open-ide\workdir`, "-1 minute")

	openW := httptest.NewRecorder()
	openReq := newRequest(http.MethodPost, "/api/issues/"+issueID+"/ide/open", map[string]any{"ide": "intellij_idea"})
	openReq = withURLParam(openReq, "id", issueID)
	testHandler.OpenIssueInIde(openW, openReq)
	if openW.Code != http.StatusAccepted {
		t.Fatalf("OpenIssueInIde: expected 202, got %d: %s", openW.Code, openW.Body.String())
	}

	claimW := httptest.NewRecorder()
	claimReq := newRequest(http.MethodPost, "/api/daemon/commands/claim", map[string]any{
		"daemon_id": "open-ide-daemon",
		"limit":     1,
	})
	testHandler.ClaimDaemonCommands(claimW, claimReq)
	if claimW.Code != http.StatusOK {
		t.Fatalf("ClaimDaemonCommands: expected 200, got %d: %s", claimW.Code, claimW.Body.String())
	}

	var commandID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM daemon_command WHERE task_id = $1`, taskID).Scan(&commandID); err != nil {
		t.Fatalf("load command id: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/daemon/commands/"+commandID+"/complete", map[string]any{
		"daemon_id": "open-ide-daemon",
		"status":    "completed",
	})
	req = withURLParam(req, "commandId", commandID)
	testHandler.CompleteDaemonCommand(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteDaemonCommand: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM daemon_command WHERE id = $1`, commandID).Scan(&status); err != nil {
		t.Fatalf("load command status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("status = %s, want completed", status)
	}
}
