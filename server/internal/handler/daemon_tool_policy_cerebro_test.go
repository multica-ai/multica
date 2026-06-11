package handler

// CEREBRO-PATCH(daemon-tool-policy-cerebro): handler test for the local-runtime
// per-tool resolver (TECH-3173).
//
// Proves the staged-rollout contract end to end through the HTTP seam: OFF never
// blocks even a denied tool (no behaviour change), OBSERVE resolves and flags
// would_block but still allows (dry run), and ENFORCE acts on the verdict
// (denied tool blocked, unconfigured tool allowed). The verdict itself comes
// from the same cerebro_tool_policy chain the gateway gate uses. Skips when no DB.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/localtoolpolicy"
)

func TestResolveDaemonToolPolicy_StagedRollout(t *testing.T) {
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

	clear := func() {
		testPool.Exec(ctx,
			`DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key IN ('tools:Bash','tools:Read')`,
			testWorkspaceID)
	}
	clear()
	t.Cleanup(clear)

	// Stage is driven by workspace settings (TECH-3173), not an env var: seed the
	// two workspace-level feature-flag rows (all-zero sentinel user_id) the way
	// the Settings UI writes them, then map the desired Mode onto the pair.
	clearFlags := func() {
		testPool.Exec(ctx, `
			DELETE FROM cerebro_feature_flags
			WHERE workspace_id = $1
			  AND flag_key IN ('cerebro_local_tool_policy','cerebro_local_tool_policy_enforce')
		`, testWorkspaceID)
	}
	clearFlags()
	t.Cleanup(clearFlags)
	setMode := func(mode localtoolpolicy.Mode) {
		t.Helper()
		enabled := mode == localtoolpolicy.ModeObserve || mode == localtoolpolicy.ModeEnforce
		enforce := mode == localtoolpolicy.ModeEnforce
		upsert := func(key string, on bool) {
			if _, err := testPool.Exec(ctx, `
				INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
				VALUES ($1, '00000000-0000-0000-0000-000000000000', $2, $3)
				ON CONFLICT (workspace_id, user_id, flag_key) DO UPDATE SET enabled = EXCLUDED.enabled
			`, testWorkspaceID, key, on); err != nil {
				t.Fatalf("seed flag %s: %v", key, err)
			}
		}
		upsert("cerebro_local_tool_policy", enabled)
		upsert("cerebro_local_tool_policy_enforce", enforce)
	}

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
		body := map[string]any{"tool_name": tool, "agent_id": agentID}
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

	// OFF (default): even the denied tool is allowed — spawning is unchanged.
	setMode(localtoolpolicy.ModeOff)
	if r := resolve(deniedTool); r["allowed"] != true || r["decision"] != "allow" {
		t.Fatalf("off/denied: got allowed=%v decision=%v, want true/allow", r["allowed"], r["decision"])
	}

	// OBSERVE: the denied tool is STILL allowed (dry run never blocks) but the
	// response flags that an enforce would have blocked it.
	setMode(localtoolpolicy.ModeObserve)
	if r := resolve(deniedTool); r["allowed"] != true || r["would_block"] != true || r["observed"] != "deny" {
		t.Fatalf("observe/denied: got allowed=%v would_block=%v observed=%v, want true/true/deny",
			r["allowed"], r["would_block"], r["observed"])
	}

	// ENFORCE: the denied tool is blocked.
	setMode(localtoolpolicy.ModeEnforce)
	if r := resolve(deniedTool); r["allowed"] != false || r["decision"] != "deny" {
		t.Fatalf("enforce/denied: got allowed=%v decision=%v, want false/deny", r["allowed"], r["decision"])
	}

	// ENFORCE: an unconfigured tool resolves to the Base default (Allow) — tools
	// are not locked by default; the admin tightens specific tools in the screen.
	if r := resolve(openTool); r["allowed"] != true || r["decision"] != "allow" {
		t.Fatalf("enforce/unconfigured: got allowed=%v decision=%v, want true/allow", r["allowed"], r["decision"])
	}
}
