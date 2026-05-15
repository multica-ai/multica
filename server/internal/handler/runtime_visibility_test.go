package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestCanUseRuntimeForAgent_Pure exercises the pure predicate behind the
// CreateAgent / UpdateAgent runtime gate. The truth table mirrors the issue
// (MUL-2062) acceptance criteria: workspace owner / admin can use any
// runtime, runtime owners can use their own runtime regardless of
// visibility, and any member can use a public runtime; everyone else gets
// denied for a private runtime owned by someone else.
func TestCanUseRuntimeForAgent_Pure(t *testing.T) {
	ownerUserID := "11111111-1111-1111-1111-111111111111"
	otherUserID := "22222222-2222-2222-2222-222222222222"

	privateRT := db.AgentRuntime{
		OwnerID:    util.MustParseUUID(ownerUserID),
		Visibility: "private",
	}
	publicRT := db.AgentRuntime{
		OwnerID:    util.MustParseUUID(ownerUserID),
		Visibility: "public",
	}

	cases := []struct {
		name   string
		userID string
		role   string
		rt     db.AgentRuntime
		want   bool
	}{
		// workspace owner / admin override
		{"workspace owner on private runtime owned by another", otherUserID, "owner", privateRT, true},
		{"workspace admin on private runtime owned by another", otherUserID, "admin", privateRT, true},
		// runtime owner
		{"runtime owner on own private runtime", ownerUserID, "member", privateRT, true},
		{"runtime owner on own public runtime", ownerUserID, "member", publicRT, true},
		// public runtime allows anyone in workspace
		{"plain member on someone else's public runtime", otherUserID, "member", publicRT, true},
		// the hole the issue closes
		{"plain member on someone else's private runtime", otherUserID, "member", privateRT, false},
		{"plain member with empty role on private runtime", otherUserID, "", privateRT, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			member := db.Member{
				UserID: util.MustParseUUID(tc.userID),
				Role:   tc.role,
			}
			got := canUseRuntimeForAgent(member, tc.rt)
			if got != tc.want {
				t.Fatalf("canUseRuntimeForAgent(role=%s, visibility=%s, owner=%s, caller=%s) = %v; want %v",
					tc.role, tc.rt.Visibility, ownerUserID, tc.userID, got, tc.want)
			}
		})
	}
}

// runtimeVisibilityFixture builds the three-actor world the gate needs to
// exercise: a private runtime owned by a non-admin member, a separate plain
// member in the same workspace, and the workspace owner (testUserID). The
// runtime is registered through agent_runtime directly so the test doesn't
// depend on the daemon-registration code path. Returns runtime id, runtime
// owner user id, and the plain member's user id.
func runtimeVisibilityFixture(t *testing.T) (runtimeID, runtimeOwnerID, plainMemberID string) {
	t.Helper()
	ctx := context.Background()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Runtime Owner', 'runtime-owner@multica.test')
		RETURNING id
	`).Scan(&runtimeOwnerID); err != nil {
		t.Fatalf("create runtime owner user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = 'runtime-owner@multica.test'`)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, runtimeOwnerID); err != nil {
		t.Fatalf("add runtime owner as member: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Plain Runtime Member', 'plain-runtime-member@multica.test')
		RETURNING id
	`).Scan(&plainMemberID); err != nil {
		t.Fatalf("create plain member user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM "user" WHERE email = 'plain-runtime-member@multica.test'`)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, plainMemberID); err != nil {
		t.Fatalf("add plain member: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, 'Visibility Test Runtime', 'cloud', 'visibility_test_provider', 'online', 'visibility test', '{}'::jsonb, $2, 'private', now())
		RETURNING id
	`, testWorkspaceID, runtimeOwnerID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	return runtimeID, runtimeOwnerID, plainMemberID
}

// TestCreateAgent_RejectsPrivateRuntimeForNonOwner walks the gate end-to-end:
// the runtime is private and owned by a non-admin member, so a workspace
// owner and the runtime owner can both create agents on it, but a plain
// workspace member cannot.
func TestCreateAgent_RejectsPrivateRuntimeForNonOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'runtime-visibility-test-%'`,
			testWorkspaceID)
	})

	body := func(name string) map[string]any {
		return map[string]any{
			"name":                 name,
			"description":          "",
			"runtime_id":           runtimeID,
			"visibility":           "private",
			"max_concurrent_tasks": 1,
		}
	}

	// Workspace owner (testUserID): allowed via admin override even though
	// the runtime is private and owned by someone else.
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body("runtime-visibility-test-admin")))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent as workspace owner: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Runtime owner: allowed because they own the runtime.
	w = httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(runtimeOwnerID, http.MethodPost, "/api/agents", body("runtime-visibility-test-runtime-owner")))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent as runtime owner: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Plain member: this is the hole MUL-2062 closes — must be 403.
	w = httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(plainMemberID, http.MethodPost, "/api/agents", body("runtime-visibility-test-plain-member")))
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateAgent as plain member on private runtime: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateAgent_AllowsPublicRuntimeForPlainMember verifies the "public"
// half of the visibility predicate: once the runtime owner flips it to
// public, any workspace member can create agents on it.
func TestCreateAgent_AllowsPublicRuntimeForPlainMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, _, plainMemberID := runtimeVisibilityFixture(t)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID,
	); err != nil {
		t.Fatalf("flip runtime to public: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = 'runtime-visibility-test-public-runtime'`,
			testWorkspaceID)
	})

	body := map[string]any{
		"name":                 "runtime-visibility-test-public-runtime",
		"description":          "",
		"runtime_id":           runtimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(plainMemberID, http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent as plain member on public runtime: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateAgent_RejectsRebindToPrivateRuntime is the regression for the
// "update can bypass create" backdoor — without this gate a plain member
// could create an agent on a public runtime, then re-bind it onto someone
// else's private runtime via UpdateAgent.
func TestUpdateAgent_RejectsRebindToPrivateRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	privateRuntimeID, _, plainMemberID := runtimeVisibilityFixture(t)

	ctx := context.Background()
	// Create a public runtime that the plain member can legitimately own
	// an agent on, then we try to move the agent onto the private runtime.
	var publicRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, 'Public Runtime', 'cloud', 'visibility_test_public_provider', 'online', 'public', '{}'::jsonb, $2, 'public', now())
		RETURNING id
	`, testWorkspaceID, plainMemberID).Scan(&publicRuntimeID); err != nil {
		t.Fatalf("create public runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, publicRuntimeID)
	})

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'rebind-test-agent', '', 'cloud', '{}'::jsonb,
		        $2, 'private', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, publicRuntimeID, plainMemberID).Scan(&agentID); err != nil {
		t.Fatalf("create agent on public runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	body := map[string]any{
		"runtime_id": privateRuntimeID,
	}
	w := httptest.NewRecorder()
	req := newRequestAs(plainMemberID, http.MethodPut, "/api/agents/"+agentID, body)
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateAgent rebinding to private runtime: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateAgentRuntime_CombinedPatchAppliesBoth pins the post-review
// invariant that a PATCH carrying BOTH `timezone` and `visibility` runs
// both mutations. Before the fix, the timezone branch returned early on
// a tz no-op (`tz == rt.Timezone`) and the visibility branch was never
// reached, silently dropping half of a legitimate request.
func TestUpdateAgentRuntime_CombinedPatchAppliesBoth(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, _ := runtimeVisibilityFixture(t)

	// Use the runtime's CURRENT timezone (UTC by default) so the timezone
	// branch is a no-op — that's the exact case the old short-circuit hit.
	w := httptest.NewRecorder()
	req := newRequestAs(runtimeOwnerID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{
		"timezone":   "UTC",
		"visibility": "public",
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH timezone+visibility (tz no-op): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Visibility != "public" {
		t.Fatalf("combined patch dropped visibility: got %q, want public", resp.Visibility)
	}
}

// TestUpdateAgentRuntime_InvalidVisibilityDoesNotMutateTimezone pins the
// other half of the ordering fix: when visibility is invalid the handler
// must 400 BEFORE running the timezone mutation, so a malformed request
// can't leave the row half-updated (timezone written, visibility rejected).
func TestUpdateAgentRuntime_InvalidVisibilityDoesNotMutateTimezone(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, _ := runtimeVisibilityFixture(t)

	ctx := context.Background()
	// Snapshot the timezone before the request so we can assert it didn't
	// move. The fixture inserts the runtime with the table default (UTC),
	// but we read instead of hard-coding so future fixture changes don't
	// silently make the test pass for the wrong reason.
	var beforeTZ string
	if err := testPool.QueryRow(ctx,
		`SELECT timezone FROM agent_runtime WHERE id = $1`, runtimeID,
	).Scan(&beforeTZ); err != nil {
		t.Fatalf("snapshot timezone: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequestAs(runtimeOwnerID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{
		"timezone":   "Asia/Tokyo",
		"visibility": "everyone",
	})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PATCH with invalid visibility: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var afterTZ string
	if err := testPool.QueryRow(ctx,
		`SELECT timezone FROM agent_runtime WHERE id = $1`, runtimeID,
	).Scan(&afterTZ); err != nil {
		t.Fatalf("re-read timezone: %v", err)
	}
	if afterTZ != beforeTZ {
		t.Fatalf("400 on visibility must not mutate timezone: before=%q after=%q", beforeTZ, afterTZ)
	}
}

// TestUpdateAgentRuntime_VisibilityToggle covers the PATCH endpoint:
// runtime owner / workspace admin can flip private↔public; plain members
// cannot; an unknown value is rejected with 400.
func TestUpdateAgentRuntime_VisibilityToggle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, runtimeOwnerID, plainMemberID := runtimeVisibilityFixture(t)

	patch := func(actorID string, visibility string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newRequestAs(actorID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{
			"visibility": visibility,
		})
		req = withURLParam(req, "runtimeId", runtimeID)
		testHandler.UpdateAgentRuntime(w, req)
		return w
	}

	// Runtime owner flips private → public.
	if w := patch(runtimeOwnerID, "public"); w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentRuntime as runtime owner → public: expected 200, got %d: %s", w.Code, w.Body.String())
	} else {
		var resp AgentRuntimeResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Visibility != "public" {
			t.Fatalf("expected visibility=public, got %q", resp.Visibility)
		}
	}

	// Workspace owner (testUserID) flips it back.
	if w := patch(testUserID, "private"); w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentRuntime as workspace owner → private: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Plain member: forbidden, regardless of intent.
	if w := patch(plainMemberID, "public"); w.Code != http.StatusForbidden {
		t.Fatalf("UpdateAgentRuntime as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Bad value from the owner: 400.
	if w := patch(runtimeOwnerID, "everyone"); w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgentRuntime with invalid visibility: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
