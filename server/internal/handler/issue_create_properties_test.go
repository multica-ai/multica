package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func createIssueWithPropertyBody(t *testing.T, body map[string]any) IssueResponse {
	t.Helper()
	response := testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body),
	).Want(http.StatusCreated)
	var issue IssueResponse
	if err := json.NewDecoder(response.Body).Decode(&issue); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue
}

func countIssuesWithAtomicPropertyTitle(t *testing.T, title string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title).Scan(&count); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	return count
}

func TestCreateIssueWithPropertiesCanonicalizesEveryType(t *testing.T) {
	suffix := uuid.NewString()[:8]
	text := createTestProperty(t, map[string]any{"name": "CreateText" + suffix, "type": "text"})
	number := createTestProperty(t, map[string]any{"name": "CreateNumber" + suffix, "type": "number"})
	selectOne := createTestProperty(t, map[string]any{
		"name": "CreateSelect" + suffix, "type": "select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "First", "color": "#3b82f6"},
			{"name": "Second", "color": "#22c55e"},
		}},
	})
	multiSelect := createTestProperty(t, map[string]any{
		"name": "CreateMulti" + suffix, "type": "multi_select",
		"config": map[string]any{"options": []map[string]any{
			{"name": "One", "color": "#3b82f6"},
			{"name": "Two", "color": "#22c55e"},
		}},
	})
	date := createTestProperty(t, map[string]any{"name": "CreateDate" + suffix, "type": "date"})
	checkbox := createTestProperty(t, map[string]any{"name": "CreateCheck" + suffix, "type": "checkbox"})
	urlProperty := createTestProperty(t, map[string]any{"name": "CreateURL" + suffix, "type": "url"})
	actor := createTestProperty(t, map[string]any{"name": "CreateActor" + suffix, "type": "actor"})
	multiActor := createTestProperty(t, map[string]any{"name": "CreateActors" + suffix, "type": "multi_actor"})

	memberRef := "member:" + testUserID
	issue := createIssueWithPropertyBody(t, map[string]any{
		"title": "atomic all property types " + suffix,
		"properties": map[string]any{
			text.ID:        "value",
			number.ID:      3.5,
			selectOne.ID:   selectOne.Config.Options[1].ID,
			multiSelect.ID: []string{multiSelect.Config.Options[1].ID, multiSelect.Config.Options[0].ID, multiSelect.Config.Options[1].ID},
			date.ID:        "2026-09-22",
			checkbox.ID:    true,
			urlProperty.ID: "  https://example.com/spec  ",
			actor.ID:       memberRef,
			multiActor.ID:  []string{memberRef, memberRef},
		},
	})

	wantMulti := []any{multiSelect.Config.Options[0].ID, multiSelect.Config.Options[1].ID}
	if got := issue.Properties[multiSelect.ID]; !reflect.DeepEqual(got, wantMulti) {
		t.Fatalf("multi_select = %#v, want canonical config order %#v", got, wantMulti)
	}
	if got := issue.Properties[multiActor.ID]; !reflect.DeepEqual(got, []any{memberRef}) {
		t.Fatalf("multi_actor = %#v, want first-occurrence dedupe", got)
	}
	if got := issue.Properties[urlProperty.ID]; got != "https://example.com/spec" {
		t.Fatalf("url = %#v, want trimmed canonical value", got)
	}

	var stored []byte
	if err := testPool.QueryRow(context.Background(), `SELECT properties FROM issue WHERE id = $1`, issue.ID).Scan(&stored); err != nil {
		t.Fatalf("load stored properties: %v", err)
	}
	var bag map[string]any
	if err := json.Unmarshal(stored, &bag); err != nil {
		t.Fatalf("decode stored properties: %v", err)
	}
	if !reflect.DeepEqual(bag, issue.Properties) {
		t.Fatalf("response properties differ from stored snapshot: response=%#v stored=%#v", issue.Properties, bag)
	}
	if len(issue.Properties) != 9 {
		t.Fatalf("response property count = %d, want all 9 supported types", len(issue.Properties))
	}
}

func TestCreateIssuePropertiesInvalidSecondValueRollsBack(t *testing.T) {
	suffix := uuid.NewString()[:8]
	first := createTestProperty(t, map[string]any{
		"name": "RollbackFirst" + suffix, "type": "select",
		"config": map[string]any{"options": []map[string]any{{"name": "Valid", "color": "#3b82f6"}}},
	})
	second := createTestProperty(t, map[string]any{
		"name": "RollbackSecond" + suffix, "type": "select",
		"config": map[string]any{"options": []map[string]any{{"name": "Valid", "color": "#3b82f6"}}},
	})
	if first.ID > second.ID {
		first, second = second, first
	}
	title := "atomic property rollback " + suffix

	response := testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":      title,
			"properties": map[string]any{first.ID: first.Config.Options[0].ID, second.ID: uuid.NewString()},
		}),
	).Want(http.StatusBadRequest)
	var body map[string]any
	_ = json.NewDecoder(response.Body).Decode(&body)
	if body["code"] != "invalid_issue_property" || body["property_id"] != second.ID {
		t.Fatalf("unexpected error body: %#v", body)
	}
	if got := countIssuesWithAtomicPropertyTitle(t, title); got != 0 {
		t.Fatalf("invalid atomic create persisted %d issue(s)", got)
	}
}

func TestCreateIssuePropertiesRejectDefinitionAndActorBoundaries(t *testing.T) {
	suffix := uuid.NewString()[:8]
	archived := createTestProperty(t, map[string]any{"name": "ArchivedCreate" + suffix, "type": "text"})
	archive := testutil.Call(t, testHandler.UpdateProperty,
		withURLParam(newRequest(http.MethodPatch, "/api/properties/"+archived.ID, map[string]any{"archived": true}), "id", archived.ID),
	).Want(http.StatusOK)
	_ = archive
	actor := createTestProperty(t, map[string]any{"name": "ForeignActor" + suffix, "type": "actor"})

	foreignWorkspaceID := uuid.NewString()
	foreignPropertyID := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `INSERT INTO workspace (id, name, slug, issue_prefix) VALUES ($1, $2, $3, 'F')`, foreignWorkspaceID, "Foreign create test", "foreign-create-"+suffix); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_property WHERE id = $1`, foreignPropertyID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, foreignWorkspaceID)
	})
	if _, err := testPool.Exec(context.Background(), `INSERT INTO issue_property (id, workspace_id, name, type, config) VALUES ($1, $2, 'Foreign', 'text', '{}')`, foreignPropertyID, foreignWorkspaceID); err != nil {
		t.Fatalf("create foreign property: %v", err)
	}

	tests := []struct {
		name       string
		propertyID string
		value      any
	}{
		{name: "unknown", propertyID: uuid.NewString(), value: "x"},
		{name: "foreign", propertyID: foreignPropertyID, value: "x"},
		{name: "archived", propertyID: archived.ID, value: "x"},
		{name: "foreign actor", propertyID: actor.ID, value: "member:" + uuid.NewString()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			title := "reject " + test.name + " " + uuid.NewString()
			response := testutil.Call(t, testHandler.CreateIssue,
				newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
					"title": title, "properties": map[string]any{test.propertyID: test.value},
				}),
			).Want(http.StatusBadRequest)
			var body map[string]any
			_ = json.NewDecoder(response.Body).Decode(&body)
			if body["code"] != "invalid_issue_property" || body["property_id"] != test.propertyID {
				t.Fatalf("unexpected error body: %#v", body)
			}
			if countIssuesWithAtomicPropertyTitle(t, title) != 0 {
				t.Fatal("rejected create persisted an issue")
			}
		})
	}
}

func TestCreateIssuePropertiesRejectsDuplicateJSONKey(t *testing.T) {
	property := createTestProperty(t, map[string]any{"name": "DuplicateCreate" + uuid.NewString()[:8], "type": "text"})
	title := "duplicate property JSON " + uuid.NewString()
	body := fmt.Sprintf(`{"title":%q,"properties":{"%s":"first","%s":"second"}}`, title, property.ID, property.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusBadRequest)
	if countIssuesWithAtomicPropertyTitle(t, title) != 0 {
		t.Fatal("duplicate-key create persisted an issue")
	}
}

func TestCreateIssuePropertiesRejectsDuplicateCanonicalPropertyID(t *testing.T) {
	property := createTestProperty(t, map[string]any{"name": "DuplicateCanonical" + uuid.NewString()[:8], "type": "text"})
	title := "duplicate canonical property " + uuid.NewString()
	body := fmt.Sprintf(
		`{"title":%q,"properties":{"%s":"first","%s":"second"}}`,
		title,
		property.ID,
		strings.ReplaceAll(property.ID, "-", ""),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	response := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusBadRequest)
	var errorBody map[string]any
	_ = json.NewDecoder(response.Body).Decode(&errorBody)
	if errorBody["code"] != "invalid_issue_property" {
		t.Fatalf("unexpected error body: %#v", errorBody)
	}
	if countIssuesWithAtomicPropertyTitle(t, title) != 0 {
		t.Fatal("duplicate canonical property create persisted an issue")
	}
}

func TestCreateIssuePropertiesRejectsEmptyMultiValue(t *testing.T) {
	property := createTestProperty(t, map[string]any{
		"name": "EmptyMultiCreate" + uuid.NewString()[:8],
		"type": "multi_select",
		"config": map[string]any{"options": []map[string]any{{
			"name": "Only", "color": "#3b82f6",
		}}},
	})
	title := "empty multi property " + uuid.NewString()
	response := testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title": title, "properties": map[string]any{property.ID: []string{}},
		}),
	).Want(http.StatusBadRequest)
	var body map[string]any
	_ = json.NewDecoder(response.Body).Decode(&body)
	if body["code"] != "invalid_issue_property" || body["property_id"] != property.ID {
		t.Fatalf("unexpected error body: %#v", body)
	}
	if countIssuesWithAtomicPropertyTitle(t, title) != 0 {
		t.Fatal("empty multi-value create persisted an issue")
	}
}

func TestCreateIssuePropertiesAllowsPlainWorkspaceMember(t *testing.T) {
	property := createTestProperty(t, map[string]any{
		"name": "MemberCreate" + uuid.NewString()[:8], "type": "text",
	})
	memberID := createPropertyTestMember(t)
	response := testutil.Call(t, testHandler.CreateIssue,
		newRequestAs(memberID, http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":      "plain member property create " + uuid.NewString(),
			"properties": map[string]any{property.ID: "allowed"},
		}),
	).Want(http.StatusCreated)
	var issue IssueResponse
	if err := json.NewDecoder(response.Body).Decode(&issue); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID) })
	if issue.Properties[property.ID] != "allowed" {
		t.Fatalf("plain member property snapshot = %#v", issue.Properties)
	}
}

func TestCreateIssuePropertiesPreservesAssignmentPermissionBoundary(t *testing.T) {
	property := createTestProperty(t, map[string]any{
		"name": "PermissionCreate" + uuid.NewString()[:8], "type": "text",
	})
	privateAgentID, _, plainMemberID := privateAgentTestFixture(t)
	title := "property create permission " + uuid.NewString()
	testutil.Call(t, testHandler.CreateIssue,
		newRequestAs(plainMemberID, http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title": title, "assignee_type": "agent", "assignee_id": privateAgentID,
			"properties": map[string]any{property.ID: "must roll back"},
		}),
	).Want(http.StatusForbidden)
	if countIssuesWithAtomicPropertyTitle(t, title) != 0 {
		t.Fatal("permission-denied property create persisted an issue")
	}
}

func TestCreateIssuePropertiesEnforcesSharedBagSizeLimit(t *testing.T) {
	properties := make(map[string]any)
	for index := 0; index < 9; index++ {
		property := createTestProperty(t, map[string]any{
			"name": fmt.Sprintf("LargeCreate%d%s", index, uuid.NewString()[:8]), "type": "text",
		})
		properties[property.ID] = strings.Repeat("x", 2000)
	}
	title := "large property bag " + uuid.NewString()
	response := testutil.Call(t, testHandler.CreateIssue,
		newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title": title, "properties": properties,
		}),
	).Want(http.StatusBadRequest)
	var body map[string]any
	_ = json.NewDecoder(response.Body).Decode(&body)
	if body["code"] != "issue_properties_too_large" {
		t.Fatalf("unexpected error body: %#v", body)
	}
	if countIssuesWithAtomicPropertyTitle(t, title) != 0 {
		t.Fatal("oversized property-bag create persisted an issue")
	}
}
