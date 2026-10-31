package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// handlerRecordingIssueViewEvents returns a handler whose own bus records the
// saved-view and preference events it publishes.
func handlerRecordingIssueViewEvents(t *testing.T) (*Handler, *[]events.Event) {
	t.Helper()
	h := *testHandler
	h.Bus = events.New()
	got := []events.Event{}
	for _, eventType := range []string{protocol.EventIssueViewChanged, protocol.EventIssueViewPreferenceChanged} {
		h.Bus.Subscribe(eventType, func(e events.Event) { got = append(got, e) })
	}
	return &h, &got
}

func TestIssueViewChangedReachesOnlyMembersWhoCanSeeTheView(t *testing.T) {
	h, got := handlerRecordingIssueViewEvents(t)

	create := func(visibility string) IssueViewResponse {
		t.Helper()
		w := httptest.NewRecorder()
		h.CreateIssueView(w, newRequest("POST", "/api/issue-views", map[string]any{
			"name":       "Realtime " + visibility,
			"scope_type": "workspace",
			"visibility": visibility,
			"query":      map[string]any{},
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: expected 201, got %d: %s", visibility, w.Code, w.Body.String())
		}
		var view IssueViewResponse
		json.NewDecoder(w.Body).Decode(&view)
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM issue_view WHERE id = $1`, view.ID)
		})
		return view
	}
	update := func(view IssueViewResponse, revision int32, patch map[string]any) {
		t.Helper()
		patch["expected_revision"] = revision
		w := httptest.NewRecorder()
		h.UpdateIssueView(w, withURLParam(newRequest("PATCH", "/api/issue-views/"+view.ID, patch), "id", view.ID))
		if w.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}
	remove := func(view IssueViewResponse) {
		t.Helper()
		w := httptest.NewRecorder()
		h.DeleteIssueView(w, withURLParam(newRequest("DELETE", "/api/issue-views/"+view.ID, nil), "id", view.ID))
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete: expected 204, got %d: %s", w.Code, w.Body.String())
		}
	}

	private := create("private")
	shared := create("workspace")
	// Members who could see it must hear that it left.
	update(shared, 1, map[string]any{"visibility": "private"})
	update(shared, 2, map[string]any{"name": "Now mine"})
	remove(private)

	want := []struct {
		viewID, action, privateTo string
	}{
		{private.ID, "created", testUserID},
		{shared.ID, "created", ""},
		{shared.ID, "updated", ""},
		{shared.ID, "updated", testUserID},
		{private.ID, "deleted", testUserID},
	}
	if len(*got) != len(want) {
		t.Fatalf("published %d events, want %d: %+v", len(*got), len(want), *got)
	}
	for i, w := range want {
		e := (*got)[i]
		payload, _ := e.Payload.(map[string]any)
		if e.Type != protocol.EventIssueViewChanged || e.WorkspaceID != testWorkspaceID ||
			payload["view_id"] != w.viewID || payload["action"] != w.action || e.MemberPrivateUserID != w.privateTo {
			t.Fatalf("event %d = type %q workspace %q payload %v private to %q, want view %s %s private to %q",
				i, e.Type, e.WorkspaceID, payload, e.MemberPrivateUserID, w.viewID, w.action, w.privateTo)
		}
	}
}

func TestIssueViewPreferenceChangedIsPrivateToItsOwner(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM issue_view_preference WHERE workspace_id = $1`, testWorkspaceID)
	})
	h, got := handlerRecordingIssueViewEvents(t)

	w := httptest.NewRecorder()
	h.PutIssueViewPreference(w, newRequest("PUT", "/api/issue-view-preferences", map[string]any{
		"scope_type": "sidebar_projects",
		"prefs":      map[string]any{"order": []string{"p1"}},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("put: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if len(*got) != 1 {
		t.Fatalf("published %d events, want 1: %+v", len(*got), *got)
	}
	e := (*got)[0]
	payload, _ := e.Payload.(map[string]any)
	if e.Type != protocol.EventIssueViewPreferenceChanged || e.WorkspaceID != testWorkspaceID ||
		e.MemberPrivateUserID != testUserID || payload["scope_type"] != "sidebar_projects" {
		t.Fatalf("event = type %q workspace %q private to %q payload %v", e.Type, e.WorkspaceID, e.MemberPrivateUserID, payload)
	}
}
