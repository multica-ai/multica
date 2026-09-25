package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueStatusIconPresentation(t *testing.T) {
	entry := createTestCustomStatus(t, "icon_presentation_test", "started")
	id := uuidToString(entry.ID)
	update := func(body map[string]any, code int) IssueStatusResponse {
		t.Helper()
		var result IssueStatusResponse
		testutil.Call(t, testHandler.UpdateIssueStatus, withURLParam(
			newRequest(http.MethodPatch, "/api/issue-statuses/"+id, body), "id", id,
		)).Want(code).JSON(&result)
		return result
	}
	for _, icon := range []string{"dotted", "circle", "half", "three_quarters", "check", "slash", "cross"} {
		got := update(map[string]any{"icon": icon}, http.StatusOK)
		if got.Icon != icon || got.Category != "in_progress" || got.Key != entry.Key {
			t.Fatalf("icon edit changed status semantics: %+v", got)
		}
		if effective := issuestatus.Effective(context.Background(), testHandler.Queries, entry.WorkspaceID, entry.Key); effective != entry.Key {
			t.Fatalf("icon changed behavior: %s", effective)
		}
	}
	got := update(map[string]any{"name": "Icon renamed by old client"}, http.StatusOK)
	if got.Icon != "cross" {
		t.Fatalf("old client erased icon: %+v", got)
	}
	for _, icon := range []string{"in_review", "<svg/>", "unknown"} {
		update(map[string]any{"icon": icon}, http.StatusBadRequest)
	}
	stored, err := testHandler.Queries.GetIssueStatusEntryByID(context.Background(), db.GetIssueStatusEntryByIDParams{ID: entry.ID, WorkspaceID: entry.WorkspaceID})
	if err != nil || stored.Icon != "cross" {
		t.Fatalf("invalid write altered stored icon: %+v, %v", stored, err)
	}
	if got := update(map[string]any{"icon": ""}, http.StatusOK); got.Icon != "" {
		t.Fatal("cannot reset icon to default")
	}
	// Built-in definitions remain protected even when the edit is only visual.
	builtin, err := testHandler.Queries.GetIssueStatusEntryByKey(context.Background(), db.GetIssueStatusEntryByKeyParams{WorkspaceID: entry.WorkspaceID, Key: "todo"})
	if err != nil {
		t.Fatal(err)
	}
	id = uuidToString(builtin.ID)
	update(map[string]any{"icon": "check"}, http.StatusForbidden)
}

func TestCreateIssueStatusIcon(t *testing.T) {
	for _, icon := range []string{"", "three_quarters", "unknown"} {
		body := map[string]any{"name": "Create icon " + icon, "category": "started", "color": "#123456"}
		if icon != "" {
			body["icon"] = icon
		}
		if icon == "unknown" {
			testutil.Call(t, testHandler.CreateIssueStatus, newRequest(http.MethodPost, "/api/issue-statuses", body)).Want(http.StatusBadRequest)
			continue
		}
		var created IssueStatusResponse
		testutil.Call(t, testHandler.CreateIssueStatus, newRequest(http.MethodPost, "/api/issue-statuses", body)).Want(http.StatusCreated).JSON(&created)
		t.Cleanup(func() {
			testPool.Exec(context.Background(), "DELETE FROM issue_status WHERE id = $1", parseUUID(created.ID))
		})
		if created.Icon != icon {
			t.Fatalf("create icon = %q, want %q", created.Icon, icon)
		}
		// Response round-trips through the catalog, including archived entries.
		testutil.Call(t, testHandler.ArchiveIssueStatus, withURLParam(newRequest(http.MethodDelete, "/api/issue-statuses/"+created.ID, nil), "id", created.ID)).Want(http.StatusOK)
		var listed struct {
			Statuses []IssueStatusResponse `json:"statuses"`
		}
		testutil.Call(t, testHandler.ListIssueStatuses, newRequest(http.MethodGet, "/api/issue-statuses?include_archived=true", nil)).Want(http.StatusOK).JSON(&listed)
		found := false
		for _, status := range listed.Statuses {
			if status.ID == created.ID {
				found = true
				if status.Icon != icon {
					b, _ := json.Marshal(status)
					t.Fatalf("lost archived icon: %s", b)
				}
			}
		}
		if !found {
			t.Fatal("archived status missing from catalog")
		}
	}
}
