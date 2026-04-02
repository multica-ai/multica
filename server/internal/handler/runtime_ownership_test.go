package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDaemonRegisterSetsOwnerID verifies that DaemonRegister populates owner_id
// from the authenticated user (X-User-ID header).
func TestDaemonRegisterSetsOwnerID(t *testing.T) {
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/daemon/register", DaemonRegisterRequest{
		WorkspaceID: testWorkspaceID,
		DaemonID:    "ownership-test-daemon",
		DeviceName:  "test-machine",
		Runtimes: []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Version string `json:"version"`
			Status  string `json:"status"`
		}{
			{Name: "Claude", Type: "claude", Version: "1.0", Status: "online"},
		},
	})
	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DaemonRegister: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Runtimes []AgentRuntimeResponse `json:"runtimes"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Runtimes) == 0 {
		t.Fatal("DaemonRegister: expected at least 1 runtime in response")
	}

	rt := resp.Runtimes[0]
	if rt.OwnerID == nil || *rt.OwnerID != testUserID {
		t.Fatalf("expected owner_id=%s, got %v", testUserID, rt.OwnerID)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, rt.ID)
	})
}

// TestRuntimeResponseIncludesVisibility verifies that runtime responses contain
// the visibility field, defaulting to "workspace".
func TestRuntimeResponseIncludesVisibility(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/daemon/register", DaemonRegisterRequest{
		WorkspaceID: testWorkspaceID,
		DaemonID:    "visibility-resp-daemon",
		DeviceName:  "vis-machine",
		Runtimes: []struct {
			Name    string `json:"name"`
			Type    string `json:"type"`
			Version string `json:"version"`
			Status  string `json:"status"`
		}{
			{Name: "Codex", Type: "codex", Version: "2.0", Status: "online"},
		},
	})
	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DaemonRegister: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Runtimes []AgentRuntimeResponse `json:"runtimes"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Runtimes) == 0 {
		t.Fatal("expected at least 1 runtime")
	}

	rt := resp.Runtimes[0]
	if rt.Visibility != "workspace" {
		t.Fatalf("expected default visibility 'workspace', got '%s'", rt.Visibility)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, rt.ID)
	})
}

// TestListRuntimesFiltersPrivate verifies that ListAgentRuntimes hides private
// runtimes from non-owners while still showing them to the owner.
func TestListRuntimesFiltersPrivate(t *testing.T) {
	ctx := context.Background()

	// Create user2 as a member of the test workspace.
	var user2ID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('runtime-vis-user2', 'runtime-vis-user2@multica.ai')
		RETURNING id
	`).Scan(&user2ID)
	if err != nil {
		t.Fatalf("create user2: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, user2ID)
	})

	_, err = testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, user2ID)
	if err != nil {
		t.Fatalf("add user2 as member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id = $1 AND workspace_id = $2`, user2ID, testWorkspaceID)
	})

	// Create a private runtime owned by testUser.
	var runtimeID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility
		)
		VALUES ($1, 'private-daemon', 'Private Runtime', 'local', 'claude', 'online',
			'test', '{}'::jsonb, $2, 'private')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create private runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// user2 should NOT see the private runtime.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/runtimes?workspace_id="+testWorkspaceID, nil)
	req.Header.Set("X-User-ID", user2ID)
	testHandler.ListAgentRuntimes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgentRuntimes (user2): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var user2Runtimes []AgentRuntimeResponse
	json.NewDecoder(w.Body).Decode(&user2Runtimes)
	for _, rt := range user2Runtimes {
		if rt.ID == runtimeID {
			t.Fatal("user2 should NOT see the private runtime owned by testUser")
		}
	}

	// testUser (owner) SHOULD see their private runtime.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/runtimes?workspace_id="+testWorkspaceID, nil)
	testHandler.ListAgentRuntimes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgentRuntimes (owner): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var ownerRuntimes []AgentRuntimeResponse
	json.NewDecoder(w.Body).Decode(&ownerRuntimes)
	found := false
	for _, rt := range ownerRuntimes {
		if rt.ID == runtimeID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("testUser (owner) should see their own private runtime")
	}
}

// TestCreateAgentOnOwnRuntimeSucceeds verifies that a user can create an agent
// on a runtime they own.
func TestCreateAgentOnOwnRuntimeSucceeds(t *testing.T) {
	ctx := context.Background()

	// Create a runtime owned by testUser.
	var runtimeID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, 'own-rt-daemon', 'Own Runtime', 'local', 'claude', 'online',
			'test', '{}'::jsonb, $2, now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "Own Runtime Agent",
		"runtime_id": runtimeID,
	})
	testHandler.CreateAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent on own runtime: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var agent AgentResponse
	json.NewDecoder(w.Body).Decode(&agent)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agent.ID)
	})

	if agent.RuntimeID != runtimeID {
		t.Fatalf("expected runtime_id=%s, got %s", runtimeID, agent.RuntimeID)
	}
}

// TestCreateAgentOnOtherUserRuntimeFails verifies that a non-admin user cannot
// create an agent on a runtime owned by someone else.
func TestCreateAgentOnOtherUserRuntimeFails(t *testing.T) {
	ctx := context.Background()

	// Create user3 as a regular member.
	var user3ID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('runtime-other-user3', 'runtime-other-user3@multica.ai')
		RETURNING id
	`).Scan(&user3ID)
	if err != nil {
		t.Fatalf("create user3: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, user3ID)
	})

	_, err = testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, user3ID)
	if err != nil {
		t.Fatalf("add user3 as member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id = $1 AND workspace_id = $2`, user3ID, testWorkspaceID)
	})

	// Create a runtime owned by testUser.
	var runtimeID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, 'other-rt-daemon', 'Other Runtime', 'local', 'claude', 'online',
			'test', '{}'::jsonb, $2, now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// user3 (member, not admin) should be forbidden.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "Should Fail Agent",
		"runtime_id": runtimeID,
	})
	req.Header.Set("X-User-ID", user3ID)
	testHandler.CreateAgent(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateAgent on other user's runtime: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Clean up in case it somehow succeeded.
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE name = 'Should Fail Agent' AND workspace_id = $1`, testWorkspaceID)
	})
}

// TestAdminCanCreateAgentOnAnyRuntime verifies that a workspace admin/owner
// can create an agent on any runtime, even one they don't own.
func TestAdminCanCreateAgentOnAnyRuntime(t *testing.T) {
	ctx := context.Background()

	// Create another user who owns a runtime.
	var otherUserID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('runtime-admin-other', 'runtime-admin-other@multica.ai')
		RETURNING id
	`).Scan(&otherUserID)
	if err != nil {
		t.Fatalf("create otherUser: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})

	_, err = testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID)
	if err != nil {
		t.Fatalf("add otherUser as member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id = $1 AND workspace_id = $2`, otherUserID, testWorkspaceID)
	})

	// Create a runtime owned by otherUser.
	var runtimeID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, 'admin-bypass-daemon', 'Other User Runtime', 'local', 'claude', 'online',
			'test', '{}'::jsonb, $2, now())
		RETURNING id
	`, testWorkspaceID, otherUserID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// testUser is workspace owner — should succeed.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":       "Admin Bypass Agent",
		"runtime_id": runtimeID,
	})
	testHandler.CreateAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Admin CreateAgent on other's runtime: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var agent AgentResponse
	json.NewDecoder(w.Body).Decode(&agent)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agent.ID)
	})
}
