package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regression coverage for the MUL-4809 status-management review (comment
// e5db7038): field-presence immutability, built-in reserved-name rename, the
// admin-only archived view, and cross-workspace tenant isolation.

// makeNonAdminMember adds a fresh user to the test workspace with the plain
// "member" role and returns its user id.
func makeNonAdminMember(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Status Nonadmin', 'status-nonadmin-' || gen_random_uuid()::text || '@multica.ai')
		RETURNING id::text
	`).Scan(&userID); err != nil {
		t.Fatalf("create non-admin user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("add non-admin member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

// makeSecondWorkspaceWithStatuses creates a second workspace that testUserID
// owns (so the admin gate passes for it) and seeds its status catalog.
func makeSecondWorkspaceWithStatuses(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Status Second WS', 'status-second-' || substr(md5(gen_random_uuid()::text), 1, 12), '', 'SEC')
		RETURNING id::text
	`).Scan(&wsID); err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsID, testUserID); err != nil {
		t.Fatalf("add owner to second workspace: %v", err)
	}
	if err := issuestatus.Ensure(ctx, db.New(testPool), parseUUID(wsID)); err != nil {
		t.Fatalf("seed second workspace statuses: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE workspace_id = $1`, wsID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	return wsID
}

// TestUpdateStatusRejectsImmutableNull covers the field-presence rule (plan
// §5.3): an immutable field is rejected whenever its key is present, including
// as an explicit JSON null, which a pointer decode cannot distinguish from an
// omitted key.
func TestUpdateStatusRejectsImmutableNull(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	created, _, _ := createStatus(t, map[string]any{"name": "Presence", "category": "in_progress", "icon": "in_progress", "color": "warning"})
	for _, body := range []map[string]any{
		{"category": nil},
		{"system_key": nil},
		{"workspace_id": nil},
		{"name": "Renamed", "category": nil}, // present even alongside a valid mutable field
	} {
		_, code, msg := patchStatus(t, created.ID, body)
		if code != http.StatusBadRequest {
			t.Errorf("immutable null %v: expected 400, got %d: %s", body, code, msg)
		}
	}
	// The rejected requests must not have applied the mutable field either.
	if cat := getStatusCatalog(t, false); true {
		for _, s := range cat.Statuses {
			if s.ID == created.ID && s.Name != "Presence" {
				t.Errorf("status was renamed by a rejected request: name = %q", s.Name)
			}
		}
	}
}

// TestRenameBuiltinToReservedNameAllowed covers the reserved-token scope (plan
// §3.1): only custom statuses may not bear a reserved alias token; a built-in
// keeps its reserved default name, so renaming it away and back must succeed.
func TestRenameBuiltinToReservedNameAllowed(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	todoID := getStatusCatalog(t, false).CategoryDefaults["todo"] // built-in Todo

	if _, code, msg := patchStatus(t, todoID, map[string]any{"name": "待排期"}); code != http.StatusOK {
		t.Fatalf("rename built-in away: expected 200, got %d: %s", code, msg)
	}
	if updated, code, msg := patchStatus(t, todoID, map[string]any{"name": "Todo"}); code != http.StatusOK {
		t.Fatalf("rename built-in back to reserved name: expected 200, got %d: %s", code, msg)
	} else if updated.Name != "Todo" {
		t.Errorf("built-in name = %q, want Todo", updated.Name)
	}

	// A custom status still may not take a reserved token, on create or rename.
	custom, _, _ := createStatus(t, map[string]any{"name": "Custom Stage", "category": "in_progress", "icon": "in_progress", "color": "warning"})
	if _, code, _ := patchStatus(t, custom.ID, map[string]any{"name": "in_progress"}); code != http.StatusBadRequest {
		t.Errorf("custom rename to reserved token: expected 400, got %d", code)
	}
}

// TestIncludeArchivedRequiresAdmin covers the archived-view gate (plan §5.1):
// the active catalog is readable by any member/agent, but include_archived=true
// is an admin-only management view.
func TestIncludeArchivedRequiresAdmin(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	created, _, _ := createStatus(t, map[string]any{"name": "Retired", "category": "backlog", "icon": "backlog", "color": "muted-foreground"})
	if code, msg := deleteStatus(t, created.ID, ""); code != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d: %s", code, msg)
	}

	listAs := func(req *http.Request) int {
		w := httptest.NewRecorder()
		testHandler.ListIssueStatuses(w, req)
		return w.Code
	}

	// Admin (default owner) may see archived rows.
	if code := listAs(newRequest("GET", "/api/issue-statuses?include_archived=true", nil)); code != http.StatusOK {
		t.Fatalf("admin include_archived: expected 200, got %d", code)
	}

	// Non-admin member: active catalog OK, archived view forbidden.
	memberID := makeNonAdminMember(t)
	if code := listAs(newRequestAs(memberID, "GET", "/api/issue-statuses", nil)); code != http.StatusOK {
		t.Errorf("non-admin active catalog: expected 200, got %d", code)
	}
	if code := listAs(newRequestAs(memberID, "GET", "/api/issue-statuses?include_archived=true", nil)); code != http.StatusForbidden {
		t.Errorf("non-admin include_archived: expected 403, got %d", code)
	}

	// Agent: archived view forbidden (agents never manage the catalog).
	agentReq := newRequest("GET", "/api/issue-statuses?include_archived=true", nil)
	agentReq.Header.Set("X-Actor-Source", "task_token")
	agentReq.Header.Set("X-Agent-ID", testUserID)
	if code := listAs(agentReq); code != http.StatusForbidden {
		t.Errorf("agent include_archived: expected 403, got %d", code)
	}
	// The agent can still read the active catalog (the alias table it needs).
	agentActive := newRequest("GET", "/api/issue-statuses", nil)
	agentActive.Header.Set("X-Actor-Source", "task_token")
	agentActive.Header.Set("X-Agent-ID", testUserID)
	if code := listAs(agentActive); code != http.StatusOK {
		t.Errorf("agent active catalog: expected 200, got %d", code)
	}
}

// TestCrossWorkspaceStatusIsolation covers tenant isolation: a status in another
// workspace cannot be mutated by pointing X-Workspace-ID at ours — the
// workspace_id WHERE guard makes it a 404, while the owner can still manage it in
// its own workspace.
func TestCrossWorkspaceStatusIsolation(t *testing.T) {
	seedTestWorkspaceStatuses(t)
	otherWS := makeSecondWorkspaceWithStatuses(t)

	// Create a custom status in the OTHER workspace.
	createReq := newRequest("POST", "/api/issue-statuses", map[string]any{"name": "Foreign Stage", "category": "in_progress", "icon": "in_progress", "color": "warning"})
	createReq.Header.Set("X-Workspace-ID", otherWS)
	wc := httptest.NewRecorder()
	testHandler.CreateIssueStatus(wc, createReq)
	if wc.Code != http.StatusCreated {
		t.Fatalf("create in other workspace: expected 201, got %d: %s", wc.Code, wc.Body.String())
	}
	var foreign IssueStatusResponse
	if err := json.NewDecoder(wc.Body).Decode(&foreign); err != nil {
		t.Fatalf("decode foreign status: %v", err)
	}

	// PATCH/DELETE it while X-Workspace-ID points at OUR workspace -> 404.
	patchReq := withURLParam(newRequest("PATCH", "/api/issue-statuses/"+foreign.ID, map[string]any{"name": "Hijacked"}), "id", foreign.ID)
	wp := httptest.NewRecorder()
	testHandler.UpdateIssueStatus(wp, patchReq)
	if wp.Code != http.StatusNotFound {
		t.Errorf("cross-workspace PATCH: expected 404, got %d: %s", wp.Code, wp.Body.String())
	}
	delReq := withURLParam(newRequest("DELETE", "/api/issue-statuses/"+foreign.ID, nil), "id", foreign.ID)
	wd := httptest.NewRecorder()
	testHandler.DeleteIssueStatus(wd, delReq)
	if wd.Code != http.StatusNotFound {
		t.Errorf("cross-workspace DELETE: expected 404, got %d: %s", wd.Code, wd.Body.String())
	}

	// The owner can still manage it in its own workspace.
	okReq := withURLParam(newRequest("PATCH", "/api/issue-statuses/"+foreign.ID, map[string]any{"name": "Renamed OK"}), "id", foreign.ID)
	okReq.Header.Set("X-Workspace-ID", otherWS)
	wok := httptest.NewRecorder()
	testHandler.UpdateIssueStatus(wok, okReq)
	if wok.Code != http.StatusOK {
		t.Errorf("same-workspace PATCH: expected 200, got %d: %s", wok.Code, wok.Body.String())
	}
}
