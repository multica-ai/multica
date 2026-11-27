package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// createRequiredWorkType creates an active required select property, deleted
// when the test ends.
func createRequiredWorkType(t *testing.T) PropertyResponse {
	t.Helper()
	return createTestProperty(t, map[string]any{
		"name": "Work type " + uuid.NewString()[:8], "type": "select", "required": true,
		"config": map[string]any{"options": []map[string]any{
			{"name": "Oncall", "color": "#ef4444"},
			{"name": "Delivery", "color": "#3b82f6"},
		}},
	})
}

func updateTestPropertyRaw(t *testing.T, id string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PATCH", "/api/properties/"+id, body), "id", id)
	testHandler.UpdateProperty(w, req)
	return w
}

func TestRequiredPropertyIsSelectOnlyAndRoundTrips(t *testing.T) {
	w := httptest.NewRecorder()
	testHandler.CreateProperty(w, newRequest("POST", "/api/properties", map[string]any{
		"name": "Required text " + uuid.NewString()[:8], "type": "text", "required": true,
	}))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "only select properties can be required") {
		t.Fatalf("required text property: got %d %s, want 400", w.Code, w.Body.String())
	}

	text := createTestProperty(t, map[string]any{"name": "Pinned text " + uuid.NewString()[:8], "type": "text", "always_visible": true})
	if !text.AlwaysVisible || text.Required {
		t.Fatalf("created text property = %+v, want always_visible only", text)
	}
	if w := updateTestPropertyRaw(t, text.ID, map[string]any{"required": true}); w.Code != http.StatusBadRequest {
		t.Fatalf("requiring a text property: got %d %s, want 400", w.Code, w.Body.String())
	}

	sel := createRequiredWorkType(t)
	if !sel.Required || sel.AlwaysVisible {
		t.Fatalf("created select property = %+v, want required only", sel)
	}
	w = updateTestPropertyRaw(t, sel.ID, map[string]any{"required": false, "always_visible": true})
	if w.Code != http.StatusOK {
		t.Fatalf("update flags: %d %s", w.Code, w.Body.String())
	}
	var updated PropertyResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Required || !updated.AlwaysVisible {
		t.Fatalf("updated property = %+v, want always_visible only", updated)
	}
	// A rename leaves the flags alone.
	w = updateTestPropertyRaw(t, sel.ID, map[string]any{"name": "Renamed " + uuid.NewString()[:8]})
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Required || !updated.AlwaysVisible {
		t.Fatalf("renamed property = %+v, flags changed", updated)
	}
}

func TestCreateIssueRequiresRequiredPropertyValue(t *testing.T) {
	prop := createRequiredWorkType(t)
	title := "required property create " + uuid.NewString()[:8]

	response := testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": title}),
	).Want(http.StatusBadRequest)
	var body map[string]any
	json.NewDecoder(response.Body).Decode(&body)
	if body["code"] != "invalid_issue_property" || body["property_id"] != prop.ID {
		t.Fatalf("missing value body = %#v, want invalid_issue_property for %s", body, prop.ID)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, prop.Config.Options[0].ID) {
		t.Fatalf("error %q does not list the option ids", msg)
	}
	if n := countIssuesWithAtomicPropertyTitle(t, title); n != 0 {
		t.Fatalf("rejected create left %d issues", n)
	}

	issue := createIssueWithPropertyBody(t, map[string]any{
		"title":      title,
		"properties": map[string]any{prop.ID: prop.Config.Options[1].ID},
	})
	if issue.Properties[prop.ID] != prop.Config.Options[1].ID {
		t.Fatalf("created issue properties = %#v", issue.Properties)
	}

	// An archived required property no longer gates creates.
	if w := updateTestPropertyRaw(t, prop.ID, map[string]any{"archived": true}); w.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", w.Code, w.Body.String())
	}
	createIssueWithPropertyBody(t, map[string]any{"title": title + " archived"})
}

func TestRequiredPropertyValueCannotBeCleared(t *testing.T) {
	prop := createRequiredWorkType(t)
	issueID := createPropertyTestIssue(t, "required property clear "+uuid.NewString()[:8])
	if w := setIssuePropertyRaw(t, issueID, prop.ID, prop.Config.Options[0].ID); w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	req := withIssuePropertyParams(newRequest("DELETE", "/api/issues/"+issueID+"/properties/"+prop.ID, nil), issueID, prop.ID)
	testHandler.DeleteIssueProperty(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("delete required value: got %d %s, want 400", w.Code, w.Body.String())
	}
	code, _, raw := batchUpdate(t, map[string]any{
		"issue_ids":  []string{issueID},
		"properties": map[string]any{"unset": []string{prop.ID}},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("batch unset required value: got %d %s, want 400", code, raw)
	}
	if got, ok := issuePropertyValue(t, issueID, prop.ID); !ok || got != prop.Config.Options[0].ID {
		t.Fatalf("value after refused clears = %q, %v", got, ok)
	}
	// Changing to another option stays allowed.
	if w := setIssuePropertyRaw(t, issueID, prop.ID, prop.Config.Options[1].ID); w.Code != http.StatusOK {
		t.Fatalf("change option: %d %s", w.Code, w.Body.String())
	}
}
