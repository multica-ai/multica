package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// createTestRuntime inserts an agent_runtime row with the given owner and
// visibility, scoped to the global testWorkspaceID. Returns the runtime UUID.
// Cleanup is registered with t.
func createTestRuntime(t *testing.T, ownerID, visibility string) string {
	t.Helper()

	daemonID := fmt.Sprintf("vis-test-daemon-%d", time.Now().UnixNano())
	name := fmt.Sprintf("vis-test-runtime-%d", time.Now().UnixNano())

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, $2, $3, 'local', 'claude', 'online',
			'visibility test', '{}'::jsonb, $4, $5, now())
		RETURNING id
	`, testWorkspaceID, daemonID, name, ownerID, visibility).Scan(&runtimeID); err != nil {
		t.Fatalf("create test runtime: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	return runtimeID
}

// createTestMember inserts a user + member row in the global test workspace
// with the given role. Returns the user UUID.
func createTestMember(t *testing.T, role string) string {
	t.Helper()

	email := fmt.Sprintf("vis-%d@multica.ai", time.Now().UnixNano())

	var userID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Visibility Test "+role, email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
	`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("create member: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return userID
}

// ---------------------------------------------------------------------------
// UpdateAgentRuntime
// ---------------------------------------------------------------------------

func TestUpdateAgentRuntime_OwnerCanFlipVisibility(t *testing.T) {
	rtID := createTestRuntime(t, testUserID, "workspace")

	body := map[string]any{"visibility": "private"}
	req := withURLParam(newRequest("PATCH", "/api/runtimes/"+rtID, body), "runtimeId", rtID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentRuntime(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.Visibility != "private" {
		t.Fatalf("expected visibility=private in response, got %q", resp.Visibility)
	}

	var dbVis string
	testPool.QueryRow(context.Background(), `SELECT visibility FROM agent_runtime WHERE id = $1`, rtID).Scan(&dbVis)
	if dbVis != "private" {
		t.Fatalf("expected DB visibility=private, got %q", dbVis)
	}
}

func TestUpdateAgentRuntime_NonOwnerForbidden(t *testing.T) {
	rtID := createTestRuntime(t, testUserID, "workspace")
	otherUserID := createTestMember(t, "member")

	body := map[string]any{"visibility": "private"}
	req := withURLParam(newRequest("PATCH", "/api/runtimes/"+rtID, body), "runtimeId", rtID)
	req.Header.Set("X-User-ID", otherUserID) // override
	w := httptest.NewRecorder()
	testHandler.UpdateAgentRuntime(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRuntime_AdminAlsoForbidden(t *testing.T) {
	// "Private" must mean private from admins too — only the owner can change it.
	rtID := createTestRuntime(t, testUserID, "workspace")
	adminID := createTestMember(t, "admin")

	body := map[string]any{"visibility": "private"}
	req := withURLParam(newRequest("PATCH", "/api/runtimes/"+rtID, body), "runtimeId", rtID)
	req.Header.Set("X-User-ID", adminID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentRuntime(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for admin, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRuntime_InvalidVisibility(t *testing.T) {
	rtID := createTestRuntime(t, testUserID, "workspace")

	body := map[string]any{"visibility": "secret"}
	req := withURLParam(newRequest("PATCH", "/api/runtimes/"+rtID, body), "runtimeId", rtID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentRuntime(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRuntime_InvalidUUID(t *testing.T) {
	body := map[string]any{"visibility": "workspace"}
	req := withURLParam(newRequest("PATCH", "/api/runtimes/not-a-uuid", body), "runtimeId", "not-a-uuid")
	w := httptest.NewRecorder()
	testHandler.UpdateAgentRuntime(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRuntime_RuntimeWithoutOwnerForbidden(t *testing.T) {
	// Legacy rows with NULL owner_id are unowned — nobody should be able to
	// flip visibility on them, since there's no claim of ownership.
	daemonID := fmt.Sprintf("orphan-daemon-%d", time.Now().UnixNano())
	name := fmt.Sprintf("orphan-runtime-%d", time.Now().UnixNano())
	var rtID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, $2, $3, 'local', 'claude', 'online',
			'orphan', '{}'::jsonb, NULL, 'workspace', now())
		RETURNING id
	`, testWorkspaceID, daemonID, name).Scan(&rtID); err != nil {
		t.Fatalf("create orphan runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, rtID)
	})

	body := map[string]any{"visibility": "private"}
	req := withURLParam(newRequest("PATCH", "/api/runtimes/"+rtID, body), "runtimeId", rtID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentRuntime(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for orphan runtime, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ListAgentRuntimes — visibility filter
// ---------------------------------------------------------------------------

func TestListAgentRuntimes_HidesPrivateFromNonOwners(t *testing.T) {
	owner := testUserID
	otherID := createTestMember(t, "member")
	privateRT := createTestRuntime(t, owner, "private")

	// Other user lists runtimes — should NOT see the private one.
	req := newRequest("GET", "/api/runtimes", nil)
	req.Header.Set("X-User-ID", otherID)
	w := httptest.NewRecorder()
	testHandler.ListAgentRuntimes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []AgentRuntimeResponse
	json.NewDecoder(w.Body).Decode(&list)
	for _, rt := range list {
		if rt.ID == privateRT {
			t.Fatalf("non-owner saw private runtime %s", privateRT)
		}
	}
}

func TestListAgentRuntimes_AdminAlsoCannotSeePrivate(t *testing.T) {
	// "Private" hides from admins too.
	owner := testUserID
	adminID := createTestMember(t, "admin")
	privateRT := createTestRuntime(t, owner, "private")

	req := newRequest("GET", "/api/runtimes", nil)
	req.Header.Set("X-User-ID", adminID)
	w := httptest.NewRecorder()
	testHandler.ListAgentRuntimes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []AgentRuntimeResponse
	json.NewDecoder(w.Body).Decode(&list)
	for _, rt := range list {
		if rt.ID == privateRT {
			t.Fatalf("admin saw private runtime %s", privateRT)
		}
	}
}

func TestListAgentRuntimes_OwnerSeesOwnPrivate(t *testing.T) {
	privateRT := createTestRuntime(t, testUserID, "private")

	req := newRequest("GET", "/api/runtimes", nil)
	w := httptest.NewRecorder()
	testHandler.ListAgentRuntimes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []AgentRuntimeResponse
	json.NewDecoder(w.Body).Decode(&list)
	found := false
	for _, rt := range list {
		if rt.ID == privateRT {
			found = true
			if rt.Visibility != "private" {
				t.Fatalf("owner sees runtime but visibility wrong: %q", rt.Visibility)
			}
		}
	}
	if !found {
		t.Fatalf("owner did not see own private runtime %s", privateRT)
	}
}

func TestListAgentRuntimes_WorkspaceVisibleSeenByEveryone(t *testing.T) {
	owner := testUserID
	otherID := createTestMember(t, "member")
	wsRT := createTestRuntime(t, owner, "workspace")

	req := newRequest("GET", "/api/runtimes", nil)
	req.Header.Set("X-User-ID", otherID)
	w := httptest.NewRecorder()
	testHandler.ListAgentRuntimes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []AgentRuntimeResponse
	json.NewDecoder(w.Body).Decode(&list)
	for _, rt := range list {
		if rt.ID == wsRT {
			return
		}
	}
	t.Fatalf("non-owner did not see workspace-visible runtime %s", wsRT)
}
