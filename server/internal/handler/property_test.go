package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestPropertyOptionRemovalGuard: removing a select option still referenced
// by issues is rejected with a usage census; renames (same id) and removal
// of unused options pass.
func TestPropertyOptionRemovalGuard(t *testing.T) {
	property := createTestProperty(t, map[string]any{
		"name": "Guard" + uuid.NewString()[:8], "type": "select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "Used", "color": "#ef4444"},
			{"name": "Unused", "color": "#6b7280"},
		}},
	})
	usedID := property.Config.Options[0].ID
	unusedID := property.Config.Options[1].ID
	issueID := createPropertyTestIssue(t, "option removal guard")
	if w := setIssuePropertyRaw(t, issueID, property.ID, usedID); w.Code != http.StatusOK {
		t.Fatalf("seed value: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	patchConfig := func(options []map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := newRequest("PATCH", "/api/properties/"+property.ID, map[string]any{
			"config": map[string]any{"options": options},
		})
		req = withURLParam(req, "id", property.ID)
		testHandler.UpdateProperty(w, req)
		return w
	}

	// Dropping the in-use option → 409 naming it with the census.
	w := patchConfig([]map[string]any{{"id": unusedID, "name": "Unused", "color": "#6b7280"}})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "Used") || !strings.Contains(w.Body.String(), "1 issues") {
		t.Fatalf("in-use removal: expected 409 with census, got %d: %s", w.Code, w.Body.String())
	}

	// Renaming the in-use option (id preserved) → 200.
	w = patchConfig([]map[string]any{
		{"id": usedID, "name": "Used (renamed)", "color": "#ef4444"},
		{"id": unusedID, "name": "Unused", "color": "#6b7280"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Dropping only the unused option → 200.
	w = patchConfig([]map[string]any{{"id": usedID, "name": "Used (renamed)", "color": "#ef4444"}})
	if w.Code != http.StatusOK {
		t.Fatalf("unused removal: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListIssuesPropertyFilterAndSort covers the server-side list support:
// containment filtering (select / multi_select / checkbox) beyond the first
// page window, and typed property sort expressions with missing-last
// semantics. The shared workspace fixture may hold foreign issues, so
// assertions check relative order / membership rather than exact lists.
func TestListIssuesPropertyFilterAndSort(t *testing.T) {
	sel := createTestProperty(t, map[string]any{
		"name": "FS" + uuid.NewString()[:8], "type": "select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "Hit", "color": "#ef4444"},
			{"name": "Miss", "color": "#6b7280"},
		}},
	})
	hitID := sel.Config.Options[0].ID
	multi := createTestProperty(t, map[string]any{
		"name": "FM" + uuid.NewString()[:8], "type": "multi_select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "A", "color": "#3b82f6"},
			{"name": "B", "color": "#22c55e"},
		}},
	})
	multiB := multi.Config.Options[1].ID
	box := createTestProperty(t, map[string]any{"name": "FB" + uuid.NewString()[:8], "type": "checkbox"})
	num := createTestProperty(t, map[string]any{"name": "FN" + uuid.NewString()[:8], "type": "number"})

	// 54 padding issues at explicit ascending positions, then the matching
	// issue at the highest position — genuinely beyond the 50-row first page
	// (review round 3: without explicit positions everything ties at 0 and
	// the created_at DESC tie-breaker put the target on page one).
	setPosition := func(issueID string, position float64) {
		t.Helper()
		if _, err := testPool.Exec(context.Background(),
			`UPDATE issue SET position = $1 WHERE id = $2`, position, issueID); err != nil {
			t.Fatalf("set position: %v", err)
		}
	}
	for i := 0; i < 54; i++ {
		setPosition(createPropertyTestIssue(t, fmt.Sprintf("filter pad %02d", i)), float64(i))
	}
	target := createPropertyTestIssue(t, "filter target beyond page one")
	setPosition(target, 1000)
	if w := setIssuePropertyRaw(t, target, sel.ID, hitID); w.Code != http.StatusOK {
		t.Fatalf("seed select: %d %s", w.Code, w.Body.String())
	}
	if w := setIssuePropertyRaw(t, target, multi.ID, []string{multiB}); w.Code != http.StatusOK {
		t.Fatalf("seed multi: %d %s", w.Code, w.Body.String())
	}
	if w := setIssuePropertyRaw(t, target, box.ID, true); w.Code != http.StatusOK {
		t.Fatalf("seed checkbox: %d %s", w.Code, w.Body.String())
	}

	numLow := createPropertyTestIssue(t, "sort low")
	numHigh := createPropertyTestIssue(t, "sort high")
	if w := setIssuePropertyRaw(t, numLow, num.ID, 1); w.Code != http.StatusOK {
		t.Fatalf("seed low: %d %s", w.Code, w.Body.String())
	}
	if w := setIssuePropertyRaw(t, numHigh, num.ID, 9.5); w.Code != http.StatusOK {
		t.Fatalf("seed high: %d %s", w.Code, w.Body.String())
	}

	listIssues := func(query string) []IssueResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", "/api/issues"+query, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssues%s: expected 200, got %d: %s", query, w.Code, w.Body.String())
		}
		var resp struct {
			Issues []IssueResponse `json:"issues"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Issues
	}
	ids := func(list []IssueResponse) map[string]int {
		out := make(map[string]int, len(list))
		for i, issue := range list {
			out[issue.ID] = i
		}
		return out
	}
	filterQuery := func(defID string, values ...string) string {
		buf, _ := json.Marshal(map[string][]string{defID: values})
		return "?limit=50&properties=" + url.QueryEscape(string(buf))
	}

	// Preconditions: the UNFILTERED first page must not contain the target
	// (otherwise the assertions below prove nothing about windowing).
	if _, present := ids(listIssues("?limit=50&sort=position&status=todo"))[target]; present {
		t.Fatalf("windowing precondition broken: target already on the unfiltered first page")
	}

	// Select filter finds the issue past the 50-row window.
	got := listIssues(filterQuery(sel.ID, hitID))
	if _, present := ids(got)[target]; !present {
		t.Fatalf("select filter missed the issue beyond page one")
	}
	for _, issue := range got {
		if issue.Properties[sel.ID] != hitID {
			t.Fatalf("select filter returned non-matching issue %s", issue.ID)
		}
	}

	// Multi and checkbox containment forms.
	if _, present := ids(listIssues(filterQuery(multi.ID, multiB)))[target]; !present {
		t.Fatalf("multi_select filter missed the issue")
	}
	if _, present := ids(listIssues(filterQuery(box.ID, "true")))[target]; !present {
		t.Fatalf("checkbox filter missed the issue")
	}
	// AND across definitions: matching select + non-matching checkbox → empty of target.
	buf, _ := json.Marshal(map[string][]string{sel.ID: {hitID}, box.ID: {"false"}})
	if _, present := ids(listIssues("?limit=50&properties=" + url.QueryEscape(string(buf))))[target]; present {
		t.Fatalf("AND semantics failed: target matched with contradictory checkbox filter")
	}

	// The open_only branch honors the same filter via the static
	// properties_filter param in ListOpenIssues (clean-room review: it used
	// to be parsed and then silently dropped on this path).
	openGot := listIssues(filterQuery(sel.ID, hitID) + "&open_only=true")
	if _, present := ids(openGot)[target]; !present {
		t.Fatalf("open_only ignored the properties filter: target missing")
	}
	for _, issue := range openGot {
		if issue.Properties[sel.ID] != hitID {
			t.Fatalf("open_only properties filter returned non-matching issue %s", issue.ID)
		}
	}
	openBuf, _ := json.Marshal(map[string][]string{sel.ID: {hitID}, box.ID: {"false"}})
	if _, present := ids(listIssues("?open_only=true&properties=" + url.QueryEscape(string(openBuf))))[target]; present {
		t.Fatalf("open_only AND semantics failed: target matched contradictory filter")
	}

	// Property sort: asc = low before high, valueless after both (missing last).
	sorted := listIssues("?limit=200&sort=property:" + num.ID + "&direction=asc")
	pos := ids(sorted)
	lowIdx, lowOK := pos[numLow]
	highIdx, highOK := pos[numHigh]
	padIdx, padOK := pos[target]
	if !lowOK || !highOK || !padOK {
		t.Fatalf("sorted list missing seeded issues (low=%v high=%v pad=%v)", lowOK, highOK, padOK)
	}
	if !(lowIdx < highIdx && highIdx < padIdx) {
		t.Fatalf("asc property sort order wrong: low=%d high=%d valueless=%d", lowIdx, highIdx, padIdx)
	}
	sorted = listIssues("?limit=200&sort=property:" + num.ID + "&direction=desc")
	pos = ids(sorted)
	if !(pos[numHigh] < pos[numLow] && pos[numLow] < pos[target]) {
		t.Fatalf("desc property sort order wrong: high=%d low=%d valueless=%d", pos[numHigh], pos[numLow], pos[target])
	}

	// Malformed property sort id → 400; unknown definition → 200 position order.
	w := httptest.NewRecorder()
	testHandler.ListIssues(w, newRequest("GET", "/api/issues?sort=property:nope", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed property sort: expected 400, got %d", w.Code)
	}
	if got := listIssues("?limit=5&sort=property:" + uuid.NewString()); len(got) == 0 {
		t.Fatalf("unknown-definition sort should fall back to position order, got empty")
	}
}
