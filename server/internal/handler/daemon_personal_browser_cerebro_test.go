package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPersonalBrowserFeatureEnabled_UsesPersonalOverrideWhenWorkspaceIsUnset(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	var ownerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Personal Browser Owner', 'personal-browser-owner@multica.test')
		RETURNING id
	`).Scan(&ownerID); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID)
	})

	if _, err := testPool.Exec(ctx, `
		DELETE FROM cerebro_feature_flags
		WHERE workspace_id = $1 AND flag_key = $2
	`, testWorkspaceID, personalBrowserFeatureFlag); err != nil {
		t.Fatalf("clear browser flags: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			DELETE FROM cerebro_feature_flags
			WHERE workspace_id = $1 AND flag_key = $2
		`, testWorkspaceID, personalBrowserFeatureFlag)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
		VALUES ($1, $2, $3, true)
	`, testWorkspaceID, ownerID, personalBrowserFeatureFlag); err != nil {
		t.Fatalf("seed personal browser flag: %v", err)
	}

	env := testHandler.withPersonalBrowserEnv(
		ctx,
		nil,
		parseUUID(testWorkspaceID),
		parseUUID(ownerID),
	)
	if env[personalBrowserGrantEnv] != "1" {
		t.Fatalf("personal browser env = %q, want 1", env[personalBrowserGrantEnv])
	}
}

func TestDaemonClaimWiresPersonalBrowserEnvironment(t *testing.T) {
	src, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon.go: %v", err)
	}

	required := "customEnv = h.withPersonalBrowserEnv(r.Context(), customEnv, agent.WorkspaceID, agent.OwnerID)"
	if !strings.Contains(string(src), required) {
		t.Fatalf("daemon.go is missing personal-browser claim wiring %q", required)
	}
}

func TestAuthorizePersonalBrowser_RequiresFeatureAndPermission(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	var ownerID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Personal Browser Authorization Owner', 'personal-browser-authorization-owner@multica.test')
		RETURNING id
	`).Scan(&ownerID); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, ownerID); err != nil {
		t.Fatalf("add owner as member: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'personal-browser-authorization-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, handlerTestRuntimeID(t), ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key = $2`, testWorkspaceID, personalBrowserToolKey)
		testPool.Exec(context.Background(), `DELETE FROM cerebro_feature_flags WHERE workspace_id = $1 AND flag_key = $2`, testWorkspaceID, personalBrowserFeatureFlag)
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID)
	})

	if _, err := testPool.Exec(ctx, `
		DELETE FROM cerebro_feature_flags
		WHERE workspace_id = $1 AND flag_key = $2
	`, testWorkspaceID, personalBrowserFeatureFlag); err != nil {
		t.Fatalf("clear browser flags: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		DELETE FROM cerebro_tool_policy
		WHERE workspace_id = $1 AND tool_key = $2
	`, testWorkspaceID, personalBrowserToolKey); err != nil {
		t.Fatalf("clear browser policy: %v", err)
	}
	authorize := func(host string) map[string]any {
		t.Helper()
		req := newRequest(http.MethodPost, "/api/cerebro/personal-browser/authorize", map[string]string{
			"host": host, "action": "open-tab",
		})
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		req.Header.Set("X-User-ID", ownerID)
		w := httptest.NewRecorder()
		testHandler.AuthorizePersonalBrowser(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("authorize status %d: %s", w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode authorize response: %v", err)
		}
		return result
	}

	if result := authorize("finance.firtal.com"); result["allowed"] != false || result["reason"] != "Browser feature is disabled" {
		t.Fatalf("feature off: got allowed=%v reason=%v, want false/Browser feature is disabled", result["allowed"], result["reason"])
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
		VALUES ($1, $2, $3, true)
	`, testWorkspaceID, ownerID, personalBrowserFeatureFlag); err != nil {
		t.Fatalf("enable browser feature: %v", err)
	}
	if result := authorize("finance.firtal.com"); result["allowed"] != false || result["reason"] != "tools:personal-browser is not granted for this agent" {
		t.Fatalf("feature on + no permission: got allowed=%v reason=%v, want the missing-permission error", result["allowed"], result["reason"])
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, resource_pattern, setting, conditions)
		VALUES ($1, $2, 'agent', $3, '', 'allow', '{"host_allowlist":["finance.firtal.com"]}')
	`, testWorkspaceID, personalBrowserToolKey, agentID); err != nil {
		t.Fatalf("seed browser permission: %v", err)
	}
	if result := authorize("finance.firtal.com"); result["allowed"] != true || result["decision"] != "allow" {
		t.Fatalf("feature on + permission allow: got allowed=%v decision=%v, want true/allow", result["allowed"], result["decision"])
	}
	if result := authorize("example.com"); result["allowed"] != false || result["decision"] != "deny" {
		t.Fatalf("permission host mismatch: got allowed=%v decision=%v, want false/deny", result["allowed"], result["decision"])
	}
}
