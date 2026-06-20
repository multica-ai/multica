package handler

// CEREBRO-PATCH(daemon-repo-toolpolicy): handler test for the tool-policy repo resolver (FIR-2505).
//
// Verifies CheckDaemonRepoCapability resolves repo access through the tool-policy
// chain (FIR-2505): a repo with no policy is allowed (Base=Allow — repos are not
// locked by default), an agent-level Deny on a specific repo blocks that repo
// only, and the decision is scoped to the exact repo URL. Skips when no DB.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestCheckDaemonRepoCapability_ResolvesViaToolPolicy(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	orig := testHandler.CerebroQueries
	testHandler.CerebroQueries = cerebrodb.New(testPool)
	t.Cleanup(func() { testHandler.CerebroQueries = orig })

	ctx := context.Background()
	const agentID = "000000aa-0000-0000-0000-0000000000aa"
	const lockedRepo = "github.com/firtal-group/locked-repo"
	const openRepo = "github.com/firtal-group/open-repo"

	clear := func() {
		testPool.Exec(ctx,
			`DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key = 'repo.checkout'`,
			testWorkspaceID)
	}
	clear()
	t.Cleanup(clear)

	// Agent-level Deny on one repo only.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, resource_pattern, setting)
		VALUES ($1, 'repo.checkout', 'agent', $2, $3, 'deny')
	`, testWorkspaceID, agentID, lockedRepo); err != nil {
		t.Fatalf("seed deny policy: %v", err)
	}

	check := func(url string) map[string]any {
		t.Helper()
		body := map[string]string{"url": url, "capability": "repo.checkout", "agent_id": agentID}
		req := withURLParams(
			newRequest(http.MethodPost, "/api/daemon/workspaces/"+testWorkspaceID+"/repo/check", body),
			"workspaceId", testWorkspaceID,
		)
		w := httptest.NewRecorder()
		testHandler.CheckDaemonRepoCapability(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d for %s: %s", w.Code, url, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	// The denied repo is blocked for this agent.
	if r := check(lockedRepo); r["allowed"] != false || r["decision"] != "deny" {
		t.Fatalf("locked repo: got allowed=%v decision=%v, want false/deny", r["allowed"], r["decision"])
	}

	// A repo with no policy resolves to the Base default (Allow) — repos are not
	// locked by default; the admin tightens specific repos in the screen. This
	// also proves the Deny is scoped to its exact URL and does not bleed across.
	if r := check(openRepo); r["allowed"] != true {
		t.Fatalf("unconfigured repo: got allowed=%v decision=%v, want true/allow", r["allowed"], r["decision"])
	}
}

// CEREBRO-PATCH(repo-action-condition): handler test for action-scoped repo Conditions (FIR-1609).
//
// TestCheckDaemonRepoCapability_ActionScopedCondition proves the gate derives the
// action verb from the capability (FIR-1609) so an action-scoped Condition bites
// here. Before the gate set RequestContext.Action, ctx.Action was "" and any
// action-scoped row never matched — an action-scoped Deny silently failed OPEN.
func TestCheckDaemonRepoCapability_ActionScopedCondition(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	orig := testHandler.CerebroQueries
	testHandler.CerebroQueries = cerebrodb.New(testPool)
	t.Cleanup(func() { testHandler.CerebroQueries = orig })

	ctx := context.Background()
	const agentID = "000000bb-0000-0000-0000-0000000000bb"
	const checkoutDeniedRepo = "github.com/firtal-group/checkout-denied"
	const pushDeniedRepo = "github.com/firtal-group/push-denied"

	clear := func() {
		testPool.Exec(ctx,
			`DELETE FROM cerebro_tool_policy WHERE workspace_id = $1 AND tool_key IN ('repo.checkout','repo.push')`,
			testWorkspaceID)
	}
	clear()
	t.Cleanup(clear)

	// A checkout Deny gated to the "checkout" action — matches a checkout request.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, resource_pattern, setting, conditions)
		VALUES ($1, 'repo.checkout', 'agent', $2, $3, 'deny', '{"actions":["checkout"]}')
	`, testWorkspaceID, agentID, checkoutDeniedRepo); err != nil {
		t.Fatalf("seed checkout-scoped deny: %v", err)
	}
	// A checkout Deny gated to the "push" action — must NOT bite a checkout request.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_tool_policy (workspace_id, tool_key, layer, subject_id, resource_pattern, setting, conditions)
		VALUES ($1, 'repo.checkout', 'agent', $2, $3, 'deny', '{"actions":["push"]}')
	`, testWorkspaceID, agentID, pushDeniedRepo); err != nil {
		t.Fatalf("seed push-scoped deny: %v", err)
	}

	check := func(url, capability string) map[string]any {
		t.Helper()
		body := map[string]string{"url": url, "capability": capability, "agent_id": agentID}
		req := withURLParams(
			newRequest(http.MethodPost, "/api/daemon/workspaces/"+testWorkspaceID+"/repo/check", body),
			"workspaceId", testWorkspaceID,
		)
		w := httptest.NewRecorder()
		testHandler.CheckDaemonRepoCapability(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d for %s: %s", w.Code, url, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	// Action matches ("checkout") → the conditioned Deny applies → blocked.
	if r := check(checkoutDeniedRepo, "repo.checkout"); r["allowed"] != false || r["decision"] != "deny" {
		t.Fatalf("checkout-scoped deny on checkout: got allowed=%v decision=%v, want false/deny", r["allowed"], r["decision"])
	}

	// Action mismatches ("checkout" vs the row's "push") → condition not met → the
	// Deny drops → falls through to Base Allow. This proves the action term is
	// actually evaluated against ctx.Action, not ignored.
	if r := check(pushDeniedRepo, "repo.checkout"); r["allowed"] != true {
		t.Fatalf("push-scoped deny on checkout: got allowed=%v decision=%v, want true/allow", r["allowed"], r["decision"])
	}
}
