package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIssueViewPreferenceRoundTrip(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM issue_view_preference WHERE workspace_id = $1`, testWorkspaceID)
	})

	// Empty document before any write.
	w := httptest.NewRecorder()
	testHandler.GetIssueViewPreference(w, newRequest("GET", "/api/issue-view-preferences?scope_type=workspace", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get(empty): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got IssueViewPreferenceResponse
	json.NewDecoder(w.Body).Decode(&got)
	if string(got.Prefs) != "{}" {
		t.Fatalf("expected empty prefs doc, got %s", got.Prefs)
	}

	// Upsert then read back.
	prefs := map[string]any{
		"hidden": []string{"builtin:members"},
		"order":  []string{"builtin:all", "view:abc"},
	}
	w = httptest.NewRecorder()
	testHandler.PutIssueViewPreference(w, newRequest("PUT", "/api/issue-view-preferences", map[string]any{
		"scope_type": "workspace",
		"prefs":      prefs,
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("put: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.GetIssueViewPreference(w, newRequest("GET", "/api/issue-view-preferences?scope_type=workspace", nil))
	json.NewDecoder(w.Body).Decode(&got)
	var round map[string]any
	json.Unmarshal(got.Prefs, &round)
	if hidden, _ := round["hidden"].([]any); len(hidden) != 1 || hidden[0] != "builtin:members" {
		t.Fatalf("round-trip lost hidden: %s", got.Prefs)
	}

	// Second write replaces wholesale (last-write-wins).
	w = httptest.NewRecorder()
	testHandler.PutIssueViewPreference(w, newRequest("PUT", "/api/issue-view-preferences", map[string]any{
		"scope_type": "workspace",
		"prefs":      map[string]any{"hidden": []string{}},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("put2: expected 200, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	testHandler.GetIssueViewPreference(w, newRequest("GET", "/api/issue-view-preferences?scope_type=workspace", nil))
	json.NewDecoder(w.Body).Decode(&got)
	// Fresh map: Unmarshal into a populated map merges keys instead of
	// replacing them, which would fake a stale `order` here.
	round = nil
	json.Unmarshal(got.Prefs, &round)
	if _, hasOrder := round["order"]; hasOrder {
		t.Fatalf("expected wholesale replace to drop order, got %s", got.Prefs)
	}
}

func TestIssueViewPreferenceIsPerUser(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM issue_view_preference WHERE workspace_id = $1`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	testHandler.PutIssueViewPreference(w, newRequest("PUT", "/api/issue-view-preferences", map[string]any{
		"scope_type": "workspace",
		"prefs":      map[string]any{"hidden": []string{"view:x"}},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("put: expected 200, got %d", w.Code)
	}

	otherID := createSecondWorkspaceMember(t)
	w = httptest.NewRecorder()
	req := newRequest("GET", "/api/issue-view-preferences?scope_type=workspace", nil)
	req.Header.Set("X-User-ID", otherID)
	testHandler.GetIssueViewPreference(w, req)
	var got IssueViewPreferenceResponse
	json.NewDecoder(w.Body).Decode(&got)
	if string(got.Prefs) != "{}" {
		t.Fatalf("another member must not inherit my prefs, got %s", got.Prefs)
	}
}

func TestSidebarIssueViewPreferenceIsSeparateFromWorkspace(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM issue_view_preference WHERE workspace_id = $1`, testWorkspaceID)
	})

	put := func(scopeType string, prefs map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.PutIssueViewPreference(w, newRequest("PUT", "/api/issue-view-preferences", map[string]any{
			"scope_type": scopeType,
			"prefs":      prefs,
		}))
		if w.Code != http.StatusOK {
			t.Fatalf("put %s: expected 200, got %d: %s", scopeType, w.Code, w.Body.String())
		}
	}
	put("sidebar", map[string]any{"order": []string{"builtin:my_issues", "view:abc"}})
	// An older client writing the workspace doc wholesale must not touch it.
	put("workspace", map[string]any{"hidden": []string{}})

	w := httptest.NewRecorder()
	testHandler.GetIssueViewPreference(w, newRequest("GET", "/api/issue-view-preferences?scope_type=sidebar", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get sidebar: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got IssueViewPreferenceResponse
	json.NewDecoder(w.Body).Decode(&got)
	var round map[string]any
	json.Unmarshal(got.Prefs, &round)
	if order, _ := round["order"].([]any); len(order) != 2 || order[1] != "view:abc" {
		t.Fatalf("sidebar order lost: %s", got.Prefs)
	}
	if got.ScopeID == nil || *got.ScopeID != testUserID {
		t.Fatalf("sidebar scope must be keyed by the user, got %v", got.ScopeID)
	}
}

func TestSidebarProjectsPreferenceIsItsOwnDocument(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM issue_view_preference WHERE workspace_id = $1`, testWorkspaceID)
	})

	put := func(scopeType string, prefs map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.PutIssueViewPreference(w, newRequest("PUT", "/api/issue-view-preferences", map[string]any{
			"scope_type": scopeType,
			"prefs":      prefs,
		}))
		if w.Code != http.StatusOK {
			t.Fatalf("put %s: expected 200, got %d: %s", scopeType, w.Code, w.Body.String())
		}
	}
	get := func(scopeType, userID string) (IssueViewPreferenceResponse, map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/issue-view-preferences?scope_type="+scopeType, nil)
		req.Header.Set("X-User-ID", userID)
		testHandler.GetIssueViewPreference(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get %s: expected 200, got %d: %s", scopeType, w.Code, w.Body.String())
		}
		var got IssueViewPreferenceResponse
		json.NewDecoder(w.Body).Decode(&got)
		var doc map[string]any
		json.Unmarshal(got.Prefs, &doc)
		return got, doc
	}

	put("sidebar_projects", map[string]any{
		"order":    []string{"p2", "p1"},
		"projects": map[string]any{"ordering": "manual", "showCount": false},
	})
	// The starred-views list writes its own document wholesale.
	put("sidebar", map[string]any{"hidden": []string{}, "order": []string{"builtin:my_issues"}})

	got, doc := get("sidebar_projects", testUserID)
	if order, _ := doc["order"].([]any); len(order) != 2 || order[0] != "p2" {
		t.Fatalf("project order lost: %s", got.Prefs)
	}
	if projects, _ := doc["projects"].(map[string]any); projects["showCount"] != false {
		t.Fatalf("project settings lost: %s", got.Prefs)
	}
	if got.ScopeID == nil || *got.ScopeID != testUserID {
		t.Fatalf("sidebar_projects scope must be keyed by the user, got %v", got.ScopeID)
	}

	otherID := createSecondWorkspaceMember(t)
	if got, _ := get("sidebar_projects", otherID); string(got.Prefs) != "{}" {
		t.Fatalf("another member must not inherit my project settings, got %s", got.Prefs)
	}
}
