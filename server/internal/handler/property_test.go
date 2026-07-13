package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func makePropertyDef(propType string, options []PropertyOption) db.IssueProperty {
	cfg, _ := json.Marshal(PropertyConfig{Options: options})
	return db.IssueProperty{Type: propType, Config: cfg}
}

// withIssuePropertyParams sets both chi URL params in one route context —
// withURLParam builds a fresh context per call, so chaining it would drop
// the first param.
func withIssuePropertyParams(req *http.Request, issueID, propertyID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	rctx.URLParams.Add("propertyId", propertyID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func createTestProperty(t *testing.T, body map[string]any) PropertyResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/properties", body)
	testHandler.CreateProperty(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProperty: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created PropertyResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() { deleteTestProperty(t, created.ID) })
	return created
}

// deleteTestProperty removes the row directly — the API only archives, but
// tests must not leak definitions into the shared workspace fixture (the
// 20-active cap and list assertions would couple unrelated tests).
func deleteTestProperty(t *testing.T, id string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM issue_property WHERE id = $1`, id); err != nil {
		t.Fatalf("cleanup property %s: %v", id, err)
	}
}

func createPropertyTestIssue(t *testing.T, title string) string {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, $2, 'todo', 'none', 'member', $3,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, title, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID
}

func setIssuePropertyRaw(t *testing.T, issueID, propertyID string, value any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID+"/properties/"+propertyID, map[string]any{"value": value})
	req = withIssuePropertyParams(req, issueID, propertyID)
	testHandler.SetIssueProperty(w, req)
	return w
}

func TestPropertyDefinitionCRUD(t *testing.T) {
	created := createTestProperty(t, map[string]any{
		"name":        "Severity",
		"type":        "select",
		"description": "How bad it is",
		"config": map[string]any{"options": []map[string]any{
			{"name": "Critical", "color": "EF4444"},
			{"name": "Minor", "color": "#6b7280"},
		}},
	})
	if created.Type != "select" || len(created.Config.Options) != 2 {
		t.Fatalf("unexpected created property: %+v", created)
	}
	// Server assigns option ids and normalizes colors to lowercase #rrggbb.
	if created.Config.Options[0].ID == "" {
		t.Fatalf("option id not assigned: %+v", created.Config.Options[0])
	}
	if created.Config.Options[0].Color != "#ef4444" {
		t.Fatalf("color not normalized: %q", created.Config.Options[0].Color)
	}

	// Duplicate name (case-insensitive) → 409.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/properties", map[string]any{
		"name": "severity", "type": "text",
	})
	testHandler.CreateProperty(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate name: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Rename + replace options, keeping the first option's id: values that
	// reference it must survive option-list edits.
	keepID := created.Config.Options[0].ID
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/properties/"+created.ID, map[string]any{
		"name": "Sev",
		"config": map[string]any{"options": []map[string]any{
			{"id": keepID, "name": "Blocker", "color": "#ef4444"},
			{"name": "Trivial", "color": "#a1a1aa"},
		}},
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateProperty(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProperty: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated PropertyResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Name != "Sev" || updated.Config.Options[0].ID != keepID || updated.Config.Options[0].Name != "Blocker" {
		t.Fatalf("option id not preserved on update: %+v", updated.Config.Options)
	}

	// Archive → default list hides it, include_archived shows it.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/properties/"+created.ID, map[string]any{"archived": true})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateProperty(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	listProperties := func(query string) []PropertyResponse {
		w := httptest.NewRecorder()
		testHandler.ListProperties(w, newRequest("GET", "/api/properties"+query, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListProperties%s: expected 200, got %d: %s", query, w.Code, w.Body.String())
		}
		var resp struct {
			Properties []PropertyResponse `json:"properties"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Properties
	}
	contains := func(list []PropertyResponse, id string) bool {
		for _, p := range list {
			if p.ID == id {
				return true
			}
		}
		return false
	}
	if contains(listProperties(""), created.ID) {
		t.Fatalf("archived property still in default list")
	}
	if !contains(listProperties("?include_archived=true"), created.ID) {
		t.Fatalf("archived property missing from include_archived list")
	}
}

func TestPropertyDefinitionValidation(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"reserved name", map[string]any{"name": "Due Date", "type": "text"}, "reserved"},
		{"invalid type", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "formula"}, "invalid type"},
		{"options on text", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "text",
			"config": map[string]any{"options": []map[string]any{{"name": "a", "color": "#000000"}}}}, "does not accept options"},
		{"select without options", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "select"}, "at least one option"},
		{"duplicate option names", map[string]any{"name": "X" + uuid.NewString()[:8], "type": "select",
			"config": map[string]any{"options": []map[string]any{
				{"name": "One", "color": "#000000"}, {"name": "one", "color": "#111111"},
			}}}, "duplicate option name"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		testHandler.CreateProperty(w, newRequest("POST", "/api/properties", tc.body))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("%s: expected 400 containing %q, got %d: %s", tc.name, tc.want, w.Code, w.Body.String())
		}
	}
}

// TestPropertyAdminGate verifies the two definition-management gates: agent
// actors are rejected outright (even though the fixture user is the workspace
// owner), while value writes from the same agent context succeed.
func TestPropertyAdminGate(t *testing.T) {
	// Agent actor (task_token path is trusted directly by resolveActor).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/properties", map[string]any{"name": "AgentMade", "type": "text"})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", uuid.NewString())
	testHandler.CreateProperty(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent CreateProperty: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	property := createTestProperty(t, map[string]any{"name": "AgentWritable" + uuid.NewString()[:8], "type": "text"})
	issueID := createPropertyTestIssue(t, "agent value write")

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/issues/"+issueID+"/properties/"+property.ID, map[string]any{"value": "set by agent"})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", uuid.NewString())
	req = withIssuePropertyParams(req, issueID, property.ID)
	testHandler.SetIssueProperty(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("agent SetIssueProperty: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIssuePropertyValues(t *testing.T) {
	sel := createTestProperty(t, map[string]any{
		"name": "Env" + uuid.NewString()[:8], "type": "select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "Staging", "color": "#22c55e"},
			{"name": "Production", "color": "#ef4444"},
		}},
	})
	multi := createTestProperty(t, map[string]any{
		"name": "Platforms" + uuid.NewString()[:8], "type": "multi_select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "iOS", "color": "#3b82f6"},
			{"name": "Android", "color": "#22c55e"},
			{"name": "Web", "color": "#f59e0b"},
		}},
	})
	date := createTestProperty(t, map[string]any{"name": "Reviewed" + uuid.NewString()[:8], "type": "date"})
	link := createTestProperty(t, map[string]any{"name": "Spec" + uuid.NewString()[:8], "type": "url"})
	num := createTestProperty(t, map[string]any{"name": "Effort" + uuid.NewString()[:8], "type": "number"})

	issueID := createPropertyTestIssue(t, "property value matrix")

	// select: valid option id.
	if w := setIssuePropertyRaw(t, issueID, sel.ID, sel.Config.Options[0].ID); w.Code != http.StatusOK {
		t.Fatalf("select set: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// select: unknown option → 400 listing legal ids (agents self-correct on this).
	if w := setIssuePropertyRaw(t, issueID, sel.ID, "nope"); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), sel.Config.Options[0].ID) {
		t.Fatalf("select invalid: expected 400 listing option ids, got %d: %s", w.Code, w.Body.String())
	}

	// multi_select: duplicates dropped, order canonicalized to config order.
	webID, iosID := multi.Config.Options[2].ID, multi.Config.Options[0].ID
	w := setIssuePropertyRaw(t, issueID, multi.ID, []string{webID, iosID, webID})
	if w.Code != http.StatusOK {
		t.Fatalf("multi_select set: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Properties map[string]any `json:"properties"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	stored, _ := resp.Properties[multi.ID].([]any)
	if len(stored) != 2 || stored[0] != iosID || stored[1] != webID {
		t.Fatalf("multi_select not canonicalized to config order: %v", stored)
	}

	// date / url / number validation.
	if w := setIssuePropertyRaw(t, issueID, date.ID, "13/07/2026"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad date: expected 400, got %d", w.Code)
	}
	if w := setIssuePropertyRaw(t, issueID, date.ID, "2026-07-13"); w.Code != http.StatusOK {
		t.Fatalf("good date: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := setIssuePropertyRaw(t, issueID, link.ID, "javascript:alert(1)"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad url: expected 400, got %d", w.Code)
	}
	if w := setIssuePropertyRaw(t, issueID, link.ID, "https://example.com/spec"); w.Code != http.StatusOK {
		t.Fatalf("good url: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w := setIssuePropertyRaw(t, issueID, num.ID, "3"); w.Code != http.StatusBadRequest {
		t.Fatalf("string into number: expected 400, got %d", w.Code)
	}
	if w := setIssuePropertyRaw(t, issueID, num.ID, 3.5); w.Code != http.StatusOK {
		t.Fatalf("good number: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Archived definitions reject new values but allow unset.
	warch := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/properties/"+sel.ID, map[string]any{"archived": true})
	req = withURLParam(req, "id", sel.ID)
	testHandler.UpdateProperty(warch, req)
	if warch.Code != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d: %s", warch.Code, warch.Body.String())
	}
	if w := setIssuePropertyRaw(t, issueID, sel.ID, sel.Config.Options[1].ID); w.Code != http.StatusBadRequest {
		t.Fatalf("set on archived: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	wdel := httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+issueID+"/properties/"+sel.ID, nil)
	req = withIssuePropertyParams(req, issueID, sel.ID)
	testHandler.DeleteIssueProperty(wdel, req)
	if wdel.Code != http.StatusOK {
		t.Fatalf("unset on archived: expected 200, got %d: %s", wdel.Code, wdel.Body.String())
	}
	// Fresh struct: json.Decode merges into a pre-populated map, which would
	// leave the earlier bag contents (including sel.ID) in place.
	var afterDelete struct {
		Properties map[string]any `json:"properties"`
	}
	json.NewDecoder(wdel.Body).Decode(&afterDelete)
	if _, present := afterDelete.Properties[sel.ID]; present {
		t.Fatalf("value not removed: %v", afterDelete.Properties)
	}
}

func TestValidatePropertyValueUnit(t *testing.T) {
	textDef := makePropertyDef("text", nil)
	if _, err := validatePropertyValue(textDef, json.RawMessage(`"  "`)); err == nil {
		t.Fatalf("blank text accepted")
	}
	if _, err := validatePropertyValue(textDef, json.RawMessage(`"`+strings.Repeat("x", 2001)+`"`)); err == nil {
		t.Fatalf("overlong text accepted")
	}
	if _, err := validatePropertyValue(textDef, json.RawMessage(`null`)); err == nil {
		t.Fatalf("null accepted")
	}
	boolDef := makePropertyDef("checkbox", nil)
	if _, err := validatePropertyValue(boolDef, json.RawMessage(`"true"`)); err == nil {
		t.Fatalf("string into checkbox accepted")
	}
	if _, err := validatePropertyValue(boolDef, json.RawMessage(`false`)); err != nil {
		t.Fatalf("false rejected: %v", err)
	}
}

func TestValidatePropertyNameReserved(t *testing.T) {
	for _, name := range []string{"status", "Priority", "due date", "Due_Date", "START DATE", "labels"} {
		if _, err := validatePropertyName(name); err == nil {
			t.Fatalf("reserved name %q accepted", name)
		}
	}
	if _, err := validatePropertyName("Severity"); err != nil {
		t.Fatalf("legit name rejected: %v", err)
	}
}
