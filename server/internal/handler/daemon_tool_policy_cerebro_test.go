package handler

// CEREBRO-PATCH(daemon-tool-policy-cerebro): handler test for the local-runtime
// per-tool resolver (TECH-3173).
//
// Proves the always-enforced contract end to end through the HTTP seam: a denied
// tool is blocked and an unconfigured tool is allowed. The verdict itself comes
// from the same cerebro_tool_policy chain the gateway gate uses. Skips when no DB.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestResolveDaemonToolPolicy_AlwaysEnforced(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	orig := testHandler.CerebroQueries
	testHandler.CerebroQueries = cerebrodb.New(testPool)
	t.Cleanup(func() { testHandler.CerebroQueries = orig })

	ctx := context.Background()
	const deniedTool = "Bash"
	const openTool = "Read"

	// A real agent row so GetAgent resolves the runtime + owner the chain keys
	// on. Enforce fails closed when the agent is unknown (by design), so the
	// policy paths must be exercised against an agent that actually exists.
	var ownerID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Local Tool Policy Owner', 'local-tool-policy-owner@multica.test')
		RETURNING id
	`).Scan(&ownerID); err != nil {
		t.Fatalf("create owner user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = 'local-tool-policy-owner@multica.test'`)
	})
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
		VALUES ($1, 'local-tool-policy-test-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, handlerTestRuntimeID(t), ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'running', 0)
		RETURNING id
	`, agentID, handlerTestRuntimeID(t)).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	// The real claim path (cerebroEffectiveToolsForClaim) snapshots CAPABILITY KEYS
	// into the mandate — built-ins as "tools:<Name>", because the daemon capability
	// snapshot reports them under the "tools" kind (verified in
	// TestLocalTaskMandateStoresCapabilityKeyNotBareToolName). The hook posts the
	// bare Claude name, and Authorize canonicalises it through PolicyToolKey to
	// match, exactly as the policy-chain resolve does. Seed the realistic key.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_task_mandate (task_id, workspace_id, agent_id, allowed_tools, expires_at)
		VALUES ($1, $2, $3, '["tools:Read"]'::jsonb, now() + interval '1 hour')
	`, taskID, testWorkspaceID, agentID); err != nil {
		t.Fatalf("issue task mandate: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	clear := func() {
		testPool.Exec(ctx,
			`DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key IN ('tools:Bash','tools:Read')`,
			testWorkspaceID)
	}
	clear()
	t.Cleanup(clear)

	// CEREBRO-PATCH(daemon-tool-policy-cerebro): TECH-2563 — seed the Deny under the
	// canonical capability key "tools:Bash", which is what the permissions screen
	// actually writes (cerebro_capability.capability_key, source runtime_report).
	// The resolver receives the bare Claude name "Bash" and must canonicalise it to
	// match; seeding the bare "Bash" (as before) hid the real-world key mismatch.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, resource_pattern, setting)
		VALUES ($1, 'tools:Bash', 'agent', $2, '', 'deny')
	`, testWorkspaceID, agentID); err != nil {
		t.Fatalf("seed deny policy: %v", err)
	}

	resolve := func(tool string) map[string]any {
		t.Helper()
		body := map[string]any{"tool_name": tool, "agent_id": agentID, "task_id": taskID}
		req := withURLParams(
			newRequest(http.MethodPost, "/api/daemon/workspaces/"+testWorkspaceID+"/tool-policy/resolve", body),
			"workspaceId", testWorkspaceID,
		)
		w := httptest.NewRecorder()
		testHandler.ResolveDaemonToolPolicy(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d for %s: %s", w.Code, tool, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	// The denied tool is blocked with no rollout switch.
	if r := resolve(deniedTool); r["allowed"] != false || r["decision"] != "deny" {
		t.Fatalf("enforce/denied: got allowed=%v decision=%v, want false/deny", r["allowed"], r["decision"])
	}

	// Opening the policy after claim must not expand the immutable run contract:
	// Bash was outside the initial intersection, so the same running task remains
	// denied by its Task Mandate even after the later rule becomes Allow.
	if _, err := testPool.Exec(ctx, `
		UPDATE cerebro_tool_policy SET setting='allow'
		WHERE workspace_id=$1 AND tool_key='tools:Bash' AND layer='agent' AND subject_id=$2
	`, testWorkspaceID, agentID); err != nil {
		t.Fatalf("open Bash policy after claim: %v", err)
	}
	if r := resolve(deniedTool); r["allowed"] != false || r["decision"] != "deny" {
		t.Fatalf("mandate expanded after policy opened: got allowed=%v decision=%v, want false/deny", r["allowed"], r["decision"])
	}
	var mandateDenials int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cerebro_access_decision_ledger
		WHERE agent_id = $1
		  AND task_id = $2
		  AND observed_tool_name = $3
		  AND reason = 'task mandate denied the call'
	`, agentID, taskID, deniedTool).Scan(&mandateDenials); err != nil {
		t.Fatalf("read task mandate denial observation: %v", err)
	}
	if mandateDenials < 1 {
		t.Fatal("local Task Mandate denial was not recorded for Capabilities drift")
	}
	observed, err := testHandler.CerebroQueries.ListAgentObservedToolUsage(ctx, cerebrodb.ListAgentObservedToolUsageParams{
		AgentID:    parseUUID(agentID),
		WindowDays: observedAccessWindowDays,
	})
	if err != nil {
		t.Fatalf("read Capabilities observed access: %v", err)
	}
	visible := false
	for _, tool := range observed {
		if tool.Tool == deniedTool && tool.MandateDenials >= 1 {
			visible = true
		}
	}
	if !visible {
		t.Fatalf("Task Mandate denial is missing from Capabilities observed access: %+v", observed)
	}

	// ENFORCE: an unconfigured tool resolves to the Base default (Allow) — tools
	// are not locked by default; the admin tightens specific tools in the screen.
	if r := resolve(openTool); r["allowed"] != true || r["decision"] != "allow" {
		t.Fatalf("enforce/unconfigured: got allowed=%v decision=%v, want true/allow", r["allowed"], r["decision"])
	}
}

func TestResolveDaemonToolPolicy_NormalizesCursorHookNames(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	orig := testHandler.CerebroQueries
	testHandler.CerebroQueries = cerebrodb.New(testPool)
	t.Cleanup(func() { testHandler.CerebroQueries = orig })

	ctx := context.Background()
	var runtimeID, ownerID, agentID, taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, 'cursor-tool-policy-runtime', 'local', 'cursor', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create Cursor runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Cursor Tool Policy Owner', 'cursor-tool-policy-owner@multica.test')
		RETURNING id
	`).Scan(&ownerID); err != nil {
		t.Fatalf("create owner user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = 'cursor-tool-policy-owner@multica.test'`)
	})
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
		VALUES ($1, 'cursor-tool-policy-test-agent', '', 'local', '{}'::jsonb,
		        $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create Cursor agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'running', 0)
		RETURNING id
	`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_task_mandate (task_id, workspace_id, agent_id, allowed_tools, expires_at)
		VALUES ($1, $2, $3, '["tools:run_terminal_cmd","tools:read_file","tools:Task"]'::jsonb, now() + interval '1 hour')
	`, taskID, testWorkspaceID, agentID); err != nil {
		t.Fatalf("issue task mandate: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	resolve := func(tool string, args map[string]any) map[string]any {
		t.Helper()
		body := map[string]any{"tool_name": tool, "agent_id": agentID, "task_id": taskID, "args": args}
		req := withURLParams(
			newRequest(http.MethodPost, "/api/daemon/workspaces/"+testWorkspaceID+"/tool-policy/resolve", body),
			"workspaceId", testWorkspaceID,
		)
		w := httptest.NewRecorder()
		testHandler.ResolveDaemonToolPolicy(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d for %s: %s", w.Code, tool, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	for _, tool := range []string{"Shell", "Read", "Task"} {
		if r := resolve(tool, map[string]any{"command": "pwd"}); r["allowed"] != true || r["decision"] != "allow" {
			t.Errorf("Cursor %s: got allowed=%v decision=%v, want true/allow", tool, r["allowed"], r["decision"])
		}
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, resource_pattern, setting)
		VALUES ($1, 'tools:run_terminal_cmd', 'agent', $2, 'curl', 'deny')
	`, testWorkspaceID, agentID); err != nil {
		t.Fatalf("seed Cursor command deny: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			DELETE FROM cerebro_tool_policy
			WHERE workspace_id=$1 AND tool_key='tools:run_terminal_cmd' AND subject_id=$2
		`, testWorkspaceID, agentID)
	})
	if r := resolve("Shell", map[string]any{"command": "curl https://example.com"}); r["allowed"] != false || r["decision"] != "deny" {
		t.Fatalf("Cursor Shell curl: got allowed=%v decision=%v, want false/deny", r["allowed"], r["decision"])
	}
}
