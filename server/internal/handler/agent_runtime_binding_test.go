package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// Tests for runtime-owner enforcement on agent create + update.
// Helpers `createTestRuntime` / `createTestMember` live in
// runtime_visibility_test.go (same package).

func newRequestAs(userID, method, path string, body any) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
	return req
}

func TestCreateAgent_DeniedWhenNotRuntimeOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	owner := createTestMember(t, "member")
	other := createTestMember(t, "member")
	rtID := createTestRuntime(t, owner, "workspace")

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "binding-deny-agent",
		)
	})

	body := map[string]any{
		"name":                 "binding-deny-agent",
		"description":          "",
		"runtime_id":           rtID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(other, http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_AllowedForRuntimeOwner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	owner := createTestMember(t, "member")
	rtID := createTestRuntime(t, owner, "workspace")

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "binding-self-agent",
		)
	})

	body := map[string]any{
		"name":                 "binding-self-agent",
		"description":          "",
		"runtime_id":           rtID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(owner, http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_AllowedForWorkspaceAdmin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	owner := createTestMember(t, "member")
	admin := createTestMember(t, "admin")
	rtID := createTestRuntime(t, owner, "workspace")

	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "binding-admin-bypass-agent",
		)
	})

	body := map[string]any{
		"name":                 "binding-admin-bypass-agent",
		"description":          "",
		"runtime_id":           rtID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(admin, http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 (admin bypass), got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_DeniedOnUnownedRuntimeForRegularMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// testRuntimeID is the workspace-fixture cloud runtime with NULL owner_id.
	// A plain member must be denied — there's no claim of ownership to delegate.
	member := createTestMember(t, "member")

	body := map[string]any{
		"name":                 fmt.Sprintf("unowned-binding-%d", time.Now().UnixNano()),
		"description":          "",
		"runtime_id":           testRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(member, http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgent_DeniedRebindToOtherRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	owner := createTestMember(t, "member")
	other := createTestMember(t, "member")
	ownerRT := createTestRuntime(t, owner, "workspace")
	otherRT := createTestRuntime(t, other, "workspace")

	// agent owned by `owner`, bound to ownerRT.
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("rebind-test-%d", time.Now().UnixNano()), ownerRT, owner).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	// owner tries to rebind to other's runtime — denied.
	body := map[string]any{"runtime_id": otherRT}
	req := withURLParam(newRequestAs(owner, http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
	rctx := chi.RouteContext(req.Context())
	rctx.URLParams.Add("id", agentID)

	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm DB row not changed.
	var dbRT string
	testPool.QueryRow(context.Background(), `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&dbRT)
	if dbRT != ownerRT {
		t.Fatalf("rebind leaked through: agent.runtime_id changed to %s", dbRT)
	}
}

func TestUpdateAgent_AdminCanRebindToOtherRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	owner := createTestMember(t, "member")
	other := createTestMember(t, "member")
	admin := createTestMember(t, "admin")
	ownerRT := createTestRuntime(t, owner, "workspace")
	otherRT := createTestRuntime(t, other, "workspace")

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("admin-rebind-%d", time.Now().UnixNano()), ownerRT, owner).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	body := map[string]any{"runtime_id": otherRT}
	req := withURLParam(newRequestAs(admin, http.MethodPatch, "/api/agents/"+agentID, body), "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["runtime_id"] != otherRT {
		t.Fatalf("rebind not persisted: response runtime_id=%v want %s", resp["runtime_id"], otherRT)
	}
}
