package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestQoderConnectionSecrets(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withVCSBox(t)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID)
	})
	body := map[string]any{"name": "Managed QCA", "base_url": "https://api.qoder.com/api/v1/cloud", "environment_id": "env_test", "qoder_token": "qca-test-secret", "github_tokens": map[string]string{"https://github.com/owner/repo": "github-test-secret"}}
	var view qoderSettingsView
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200).JSON(&view)
	if !view.Configured || !view.HasToken || len(view.Repositories) != 1 {
		t.Fatalf("unexpected view: %+v", view)
	}
	var encrypted string
	dbfx.QueryRow(t, `SELECT config_encrypted FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID).Scan(&encrypted)
	raw, _ := json.Marshal(view)
	for _, secret := range []string{"qca-test-secret", "github-test-secret"} {
		if strings.Contains(encrypted, secret) || strings.Contains(string(raw), secret) {
			t.Fatal("plaintext credential exposed")
		}
	}
	body["qoder_token"] = ""
	body["github_tokens"] = map[string]string{"https://github.com/owner/repo": ""}
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200)
	cfg, _, _, _, err := testHandler.readQoderSettings(context.Background(), testWorkspaceID)
	if err != nil || cfg.QoderToken != "qca-test-secret" || cfg.GitHubTokens["https://github.com/owner/repo"] != "github-test-secret" {
		t.Fatal("blank field did not retain credentials")
	}
	if !strings.HasPrefix(cfg.MulticaToken, "mdt_") {
		t.Fatal("managed bridge must use a daemon credential")
	}
	var credentialWorkspace, daemonID string
	var expiring bool
	dbfx.QueryRow(t, `SELECT workspace_id::text,daemon_id,expires_at>now() AND expires_at<now()+interval '2 hours' FROM daemon_token WHERE token_hash=$1`, auth.HashToken(cfg.MulticaToken)).Scan(&credentialWorkspace, &daemonID, &expiring)
	if credentialWorkspace != testWorkspaceID || daemonID != "qoder-managed-"+testWorkspaceID || !expiring {
		t.Fatal("unscoped or non-expiring service credential")
	}
	body["github_tokens"] = map[string]string{}
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200)
	cfg, _, _, _, err = testHandler.readQoderSettings(context.Background(), testWorkspaceID)
	if err != nil || len(cfg.GitHubTokens) != 0 {
		t.Fatal("removed repository retained credential")
	}
	testutil.Call(t, testHandler.StopQoderConnection, vcsHandlerRequest("POST", "/qoder/stop", nil, "")).Want(200).JSON(&view)
	var tokenCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM daemon_token d JOIN cloud_agent_connection c ON c.token_id=d.id AND c.provider='qoder_cloud' WHERE c.workspace_id=$1`, testWorkspaceID).Scan(&tokenCount)
	if tokenCount != 0 {
		t.Fatal("stopped credential still exists")
	}
	if view.Enabled {
		t.Fatal("connection did not stop")
	}
	runtimeID := dbfx.Runtime(t, "active QCA", testutil.Cols{"provider": "qoder_cloud", "runtime_mode": "cloud"})
	var revision int64
	dbfx.QueryRow(t, `SELECT revision FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID).Scan(&revision)
	if err := testHandler.qoderReady(context.Background(), testWorkspaceID, revision, runtimeID); err != nil {
		t.Fatal(err)
	}
	var visibility string
	dbfx.QueryRow(t, `SELECT visibility FROM agent_runtime WHERE id=$1`, runtimeID).Scan(&visibility)
	if visibility != "public" {
		t.Fatal("managed runtime is not available to workspace members")
	}
	agentID := dbfx.Agent(t, "active QCA agent", runtimeID)
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running"})
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(409)
	testutil.Call(t, testHandler.StopQoderConnection, vcsHandlerRequest("POST", "/qoder/stop", nil, "")).Want(409)

	body["base_url"] = "http://127.0.0.1:1"
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(400)
	body["base_url"] = "https://api.qoder.com/api/v1/cloud"
	body["multica_token"] = "injected"
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(400)
}

func TestQoderBaseURL(t *testing.T) {
	for raw, want := range map[string]string{
		"":                                       qoderAPIURL,
		"https://api.qoder.com/api/v1/cloud/":    qoderAPIURL,
		" https://api.qoder.com.cn/api/v1/cloud": "https://api.qoder.com.cn/api/v1/cloud",
	} {
		if got, ok := qoderBaseURL(raw); !ok || got != want {
			t.Fatalf("qoderBaseURL(%q) = %q, %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"https://api.qoder.com.cn.evil.test/api/v1/cloud", "http://api.qoder.com/api/v1/cloud", "https://api.qoder.com.cn/api/v1"} {
		if _, ok := qoderBaseURL(raw); ok {
			t.Fatalf("qoderBaseURL(%q) accepted", raw)
		}
	}
}

func TestQoderConnectionLimits(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withVCSBox(t)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID)
	})
	body := map[string]any{"name": "Managed QCA", "base_url": "https://api.qoder.com/api/v1/cloud", "environment_id": "env_test", "qoder_token": "qca-test-secret"}
	var view qoderSettingsView
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200).JSON(&view)
	if view.MaxConcurrency != defaultQoderConcurrency || view.RunTimeoutMinutes != defaultQoderRunTimeout {
		t.Fatalf("defaults not applied: %+v", view)
	}
	for _, invalid := range []map[string]any{{"max_concurrency": 11}, {"max_concurrency": -1}, {"run_timeout_minutes": 4}, {"run_timeout_minutes": 241}} {
		req := map[string]any{}
		for k, v := range body {
			req[k] = v
		}
		for k, v := range invalid {
			req[k] = v
		}
		testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", req, "")).Want(400)
	}
	body["base_url"] = "https://api.qoder.com.cn/api/v1/cloud/"
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200).JSON(&view)
	if view.BaseURL != "https://api.qoder.com.cn/api/v1/cloud" {
		t.Fatalf("China region not saved: %+v", view)
	}
	body["max_concurrency"] = 5
	body["run_timeout_minutes"] = 90
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200).JSON(&view)
	if view.MaxConcurrency != 5 || view.RunTimeoutMinutes != 90 {
		t.Fatalf("limits not saved: %+v", view)
	}
	// Omitted limits keep the saved values rather than resetting to defaults.
	delete(body, "max_concurrency")
	delete(body, "run_timeout_minutes")
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200).JSON(&view)
	if view.MaxConcurrency != 5 || view.RunTimeoutMinutes != 90 {
		t.Fatalf("limits reset on save: %+v", view)
	}
	// A journal holding concurrent runs blocks configuration changes.
	journal, err := testHandler.sealVCSSecret(`{"binding":"b","runs":[{"task_id":"t","phase":"prepared"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE cloud_agent_connection SET journal_encrypted=$2 WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID, journal); err != nil {
		t.Fatal(err)
	}
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(409)
}

func TestQoderSaveKeepsPausedConnectionPaused(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withVCSBox(t)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID)
	})
	body := map[string]any{"name": "QCA", "base_url": qoderAPIURL, "environment_id": "env_test", "qoder_token": "fixture-token"}
	var view qoderSettingsView
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200).JSON(&view)
	if !view.Enabled {
		t.Fatal("first connect must enable execution")
	}
	testutil.Call(t, testHandler.StopQoderConnection, vcsHandlerRequest("POST", "/qoder/stop", nil, "")).Want(200)

	body["max_concurrency"] = 3
	body["github_tokens"] = map[string]string{"https://github.com/owner/repo": "github-test-secret"}
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200).JSON(&view)
	var status string
	dbfx.QueryRow(t, `SELECT status FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID).Scan(&status)
	if view.Enabled || status != "stopped" || view.MaxConcurrency != 3 {
		t.Fatalf("saving a paused connection resumed it: enabled=%v status=%s view=%+v", view.Enabled, status, view)
	}

	testutil.Call(t, testHandler.ResumeQoderConnection, vcsHandlerRequest("POST", "/qoder/resume", body, "")).Want(200).JSON(&view)
	dbfx.QueryRow(t, `SELECT status FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID).Scan(&status)
	if !view.Enabled || status != "starting" {
		t.Fatalf("resume did not enable execution: enabled=%v status=%s", view.Enabled, status)
	}
}

func TestQoderManagedRegistrationAndRevocation(t *testing.T) {
	withVCSBox(t)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID)
	})
	input := map[string]any{"name": "QCA", "base_url": qoderAPIURL, "environment_id": "env_test", "qoder_token": "fixture-token"}
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", input, "")).Want(200)
	cfg, _, _, _, err := testHandler.readQoderSettings(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	daemonID := "qoder-managed-" + testWorkspaceID
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{"workspace_id": testWorkspaceID, "daemon_id": daemonID, "runtimes": []map[string]any{{"name": "QCA", "type": "qoder_cloud", "runtime_mode": "cloud", "status": "online"}}}, testWorkspaceID, daemonID)
	req.Header.Set("Authorization", "Bearer "+cfg.MulticaToken)
	testutil.Call(t, testHandler.DaemonRegister, req).Want(200)
	testutil.Call(t, testHandler.StopQoderConnection, vcsHandlerRequest("POST", "/qoder/stop", nil, "")).Want(200)
	allowed, err := testHandler.qoderCredentialAllowed(req, testWorkspaceID, daemonID)
	if err != nil || allowed {
		t.Fatalf("stopped credential accepted: %v %v", allowed, err)
	}
}

func TestQoderRetirementCancelsRemoteAndClearsPrompt(t *testing.T) {
	withVCSBox(t)
	ctx := context.Background()
	cancelled := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			cancelled++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"idle"}`))
	}))
	defer remote.Close()
	runtimeID := dbfx.Runtime(t, "retired QCA", testutil.Cols{"provider": "qoder_cloud", "runtime_mode": "cloud"})
	agentID := dbfx.Agent(t, "retired agent", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "running"})
	cfg, _ := json.Marshal(qoderSettings{BaseURL: remote.URL, QoderToken: "fixture-secret"})
	encrypted, err := testHandler.sealVCSSecret(string(cfg))
	if err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(map[string]any{"run": map[string]any{"task_id": taskID, "session_id": "sess_fixture", "prompt": "private issue content"}})
	journal, err := testHandler.sealVCSSecret(string(state))
	if err != nil {
		t.Fatal(err)
	}
	_, err = testPool.Exec(ctx, `INSERT INTO cloud_agent_connection(workspace_id,provider,owner_id,token_id,config_encrypted,enabled,journal_encrypted) VALUES($1,'qoder_cloud',$2,$2,$3,false,$4)`, testWorkspaceID, testUserID, encrypted, journal)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID)
	})
	if err = testHandler.cleanupQoderJournal(ctx, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	var remaining, status string
	dbfx.QueryRow(t, `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID).Scan(&remaining)
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status)
	if cancelled != 1 || remaining != "" || status != "failed" {
		t.Fatalf("retirement: cancels=%d journal cleared=%v status=%s", cancelled, remaining == "", status)
	}
}

func TestQoderStepLeaseReleasesDatabaseConnection(t *testing.T) {
	withVCSBox(t)
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, testWorkspaceID)
		_, _ = testPool.Exec(ctx, `DELETE FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID)
	})
	input := map[string]any{"name": "QCA", "base_url": qoderAPIURL, "environment_id": "env_test", "qoder_token": "fixture-token"}
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", input, "")).Want(200)
	var revision int64
	dbfx.QueryRow(t, `SELECT revision FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, testWorkspaceID).Scan(&revision)
	baseline := testPool.Stat().AcquiredConns()
	release, err := testHandler.acquireQoderStep(ctx, testWorkspaceID, revision)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if testPool.Stat().AcquiredConns() != baseline {
		t.Fatal("step retained a database connection")
	}
	testutil.Call(t, testHandler.StopQoderConnection, vcsHandlerRequest("POST", "/qoder/stop", nil, "")).Want(409)
	release()
	testutil.Call(t, testHandler.StopQoderConnection, vcsHandlerRequest("POST", "/qoder/stop", nil, "")).Want(200)
}

func TestQoderQuickCreateUnsupported(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, metadata := range []string{`{}`, `{"cli_version":"99.0.0"}`} {
		runtimeID := dbfx.Runtime(t, "Qoder issue-only runtime", testutil.Cols{"provider": "qoder_cloud", "runtime_mode": "cloud", "metadata": metadata})
		status, payload := testHandler.checkQuickCreateDaemonVersion(context.Background(), "issue", parseUUID(runtimeID))
		if status != http.StatusUnprocessableEntity || payload["code"] != "agent_unavailable" {
			t.Fatalf("Qoder quick-create should be unavailable, got %d: %v", status, payload)
		}
		if !strings.Contains(payload["reason"].(string), "create the issue manually") {
			t.Fatalf("missing actionable issue-only explanation: %v", payload)
		}
	}
}
