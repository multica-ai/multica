package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func cleanupClaudeConnection(t *testing.T) {
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1)`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID)
	})
}

func TestClaudeConnectionSecrets(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withVCSBox(t)
	cleanupClaudeConnection(t)
	body := map[string]any{"name": "Claude", "environment_id": "env_test", "anthropic_token": "sk-ant-test-secret", "vault_ids": []string{"vlt_test"}, "github_tokens": map[string]string{"https://github.com/owner/repo": "github-test-secret"}}
	var view claudeSettingsView
	testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", body, "")).Want(200).JSON(&view)
	if !view.Configured || !view.HasToken || len(view.Repositories) != 1 || strings.Join(view.VaultIDs, ",") != "vlt_test" {
		t.Fatalf("unexpected view: %+v", view)
	}
	var encrypted string
	dbfx.QueryRow(t, `SELECT config_encrypted FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, testWorkspaceID).Scan(&encrypted)
	raw, _ := json.Marshal(view)
	for _, secret := range []string{"sk-ant-test-secret", "github-test-secret"} {
		if strings.Contains(encrypted, secret) || strings.Contains(string(raw), secret) {
			t.Fatal("plaintext credential exposed")
		}
	}

	body["anthropic_token"] = ""
	body["github_tokens"] = map[string]string{"https://github.com/owner/repo": ""}
	testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", body, "")).Want(200)
	cfg, _, _, _, err := testHandler.readClaudeSettings(context.Background(), testWorkspaceID)
	if err != nil || cfg.AnthropicToken != "sk-ant-test-secret" || cfg.GitHubTokens["https://github.com/owner/repo"] != "github-test-secret" {
		t.Fatal("blank field did not retain credentials")
	}
	var daemonID string
	dbfx.QueryRow(t, `SELECT daemon_id FROM daemon_token WHERE token_hash=$1`, auth.HashToken(cfg.MulticaToken)).Scan(&daemonID)
	if daemonID != claudeManagedDaemonPrefix+testWorkspaceID {
		t.Fatalf("service credential daemon_id = %q", daemonID)
	}

	for name, bad := range map[string]map[string]any{
		"missing environment": {"name": "Claude", "environment_id": "", "anthropic_token": "k"},
		"foreign environment": {"name": "Claude", "environment_id": "qca_env", "anthropic_token": "k"},
		"concurrency":         {"name": "Claude", "environment_id": "env_test", "anthropic_token": "k", "max_concurrency": 99},
		"unknown field":       {"name": "Claude", "environment_id": "env_test", "anthropic_token": "k", "base_url": "https://evil.example"},
	} {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", bad, "")).Want(400)
		})
	}
}

func TestClaudeManagedRegistrationAndRevocation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withVCSBox(t)
	cleanupClaudeConnection(t)
	input := map[string]any{"name": "Claude", "environment_id": "env_test", "anthropic_token": "fixture-token"}
	testutil.Call(t, testHandler.SaveClaudeConnection, vcsHandlerRequest("PUT", "/claude-managed", input, "")).Want(200)
	cfg, _, _, _, err := testHandler.readClaudeSettings(context.Background(), testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	daemonID := claudeManagedDaemonPrefix + testWorkspaceID
	register := func(provider string) *testutil.Response {
		req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{"workspace_id": testWorkspaceID, "daemon_id": daemonID, "runtimes": []map[string]any{{"name": "Claude", "type": provider, "runtime_mode": "cloud", "status": "online"}}}, testWorkspaceID, daemonID)
		req.Header.Set("Authorization", "Bearer "+cfg.MulticaToken)
		return testutil.Call(t, testHandler.DaemonRegister, req)
	}
	register("qoder_cloud").Want(403)
	register("claude").Want(403)
	register("claude_managed").Want(200)

	testutil.Call(t, testHandler.StopClaudeConnection, vcsHandlerRequest("POST", "/claude-managed/stop", nil, "")).Want(200)
	req := newDaemonTokenRequest("POST", "/api/daemon/register", nil, testWorkspaceID, daemonID)
	req.Header.Set("Authorization", "Bearer "+cfg.MulticaToken)
	if allowed, err := testHandler.managedCredentialAllowed(req, testWorkspaceID, daemonID); err != nil || allowed {
		t.Fatalf("stopped credential accepted: %v %v", allowed, err)
	}
}
