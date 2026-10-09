package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestQoderConnectionSecrets(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withVCSBox(t)
	t.Setenv("MULTICA_QODER_STATE_DIR", t.TempDir())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE id IN (SELECT token_id FROM qoder_connection WHERE workspace_id=$1)`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM qoder_connection WHERE workspace_id=$1`, testWorkspaceID)
	})
	body := map[string]any{"name": "Managed QCA", "base_url": "https://api.qoder.com/api/v1/cloud", "environment_id": "env_test", "qoder_token": "qca-test-secret", "github_tokens": map[string]string{"https://github.com/owner/repo": "github-test-secret"}}
	var view qoderSettingsView
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200).JSON(&view)
	if !view.Configured || !view.HasToken || len(view.Repositories) != 1 {
		t.Fatalf("unexpected view: %+v", view)
	}
	var encrypted string
	dbfx.QueryRow(t, `SELECT config_encrypted FROM qoder_connection WHERE workspace_id=$1`, testWorkspaceID).Scan(&encrypted)
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
	body["github_tokens"] = map[string]string{}
	testutil.Call(t, testHandler.SaveQoderConnection, vcsHandlerRequest("PUT", "/qoder", body, "")).Want(200)
	cfg, _, _, _, err = testHandler.readQoderSettings(context.Background(), testWorkspaceID)
	if err != nil || len(cfg.GitHubTokens) != 0 {
		t.Fatal("removed repository retained credential")
	}
	testutil.Call(t, testHandler.StopQoderConnection, vcsHandlerRequest("POST", "/qoder/stop", nil, "")).Want(200).JSON(&view)
	if view.Enabled {
		t.Fatal("connection did not stop")
	}
	runtimeID := dbfx.Runtime(t, "active QCA", testutil.Cols{"provider": "qoder_cloud", "runtime_mode": "cloud"})
	var revision int64
	dbfx.QueryRow(t, `SELECT revision FROM qoder_connection WHERE workspace_id=$1`, testWorkspaceID).Scan(&revision)
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
