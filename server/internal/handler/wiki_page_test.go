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

// TestWikiPageMemberEditPermissions locks in OPE-3198: workspace members
// (not just owner/admin) can create / update content / reorder wiki pages,
// while deletion is ownership-gated — a member may delete only pages they
// created, owner/admin may delete any.
func TestWikiPageMemberEditPermissions(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Seed a second workspace member (role=member) to act as the non-admin caller.
	var memberUserID string
	memberEmail := "wiki-member-" + suffix + "@multica.test"
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Wiki Member', $1)
		RETURNING id
	`, memberEmail).Scan(&memberUserID); err != nil {
		t.Fatalf("create member user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUserID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, memberUserID); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Helper: build a request acting as a specific user.
	newRequestAs := func(method, path string, userID string, body any) *http.Request {
		req := newRequest(method, path, body)
		req.Header.Set("X-User-ID", userID)
		return req
	}

	createPage := func(t *testing.T, userID, title string) string {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequestAs("POST", "/api/wiki/pages", userID, map[string]any{"title": title})
		testHandler.CreateWikiPage(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateWikiPage as %s: expected 201, got %d: %s", userID, w.Code, w.Body.String())
		}
		var page WikiPageResponse
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode created page: %v", err)
		}
		return page.ID
	}

	// Page created by the workspace owner.
	ownerPage := createPage(t, testUserID, "Owner page "+suffix)
	// Page created by the member.
	memberPage := createPage(t, memberUserID, "Member page "+suffix)
	t.Cleanup(func() {
		// Owner can delete any remaining page.
		for _, id := range []string{ownerPage, memberPage} {
			req := newRequestAs("DELETE", "/api/wiki/pages/"+id, testUserID, nil)
			req = withURLParam(req, "id", id)
			testHandler.DeleteWikiPage(httptest.NewRecorder(), req)
		}
	})

	// Member can UPDATE any page's content (edit is open to all members).
	w := httptest.NewRecorder()
	req := newRequestAs("PUT", "/api/wiki/pages/"+ownerPage, memberUserID, map[string]any{"content": "edited by member"})
	req = withURLParam(req, "id", ownerPage)
	testHandler.UpdateWikiPage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateWikiPage as member on owner's page: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Member can REORDER pages.
	w = httptest.NewRecorder()
	req = newRequestAs("POST", "/api/wiki/pages/reorder", memberUserID, map[string]any{
		"pages": []map[string]any{{"id": ownerPage, "position": 0.5}},
	})
	testHandler.ReorderWikiPages(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ReorderWikiPages as member: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Member CANNOT delete a page they did not create.
	w = httptest.NewRecorder()
	req = newRequestAs("DELETE", "/api/wiki/pages/"+ownerPage, memberUserID, nil)
	req = withURLParam(req, "id", ownerPage)
	testHandler.DeleteWikiPage(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DeleteWikiPage as member on owner's page: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Member CAN delete a page they created.
	w = httptest.NewRecorder()
	req = newRequestAs("DELETE", "/api/wiki/pages/"+memberPage, memberUserID, nil)
	req = withURLParam(req, "id", memberPage)
	testHandler.DeleteWikiPage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteWikiPage as member on own page: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Owner CAN delete any page.
	w = httptest.NewRecorder()
	req = newRequestAs("DELETE", "/api/wiki/pages/"+ownerPage, testUserID, nil)
	req = withURLParam(req, "id", ownerPage)
	testHandler.DeleteWikiPage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteWikiPage as owner: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
