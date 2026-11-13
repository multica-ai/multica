package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func issueViewForQueryTest(t *testing.T, scopeType, scopeVariant string, scopeID string, query, display map[string]any) db.IssueView {
	t.Helper()
	view := db.IssueView{ScopeType: scopeType}
	if scopeVariant != "" {
		view.ScopeVariant = pgtype.Text{String: scopeVariant, Valid: true}
	}
	if scopeID != "" {
		view.ScopeID = parseUUID(scopeID)
	}
	var err error
	if view.Query, err = json.Marshal(query); err != nil {
		t.Fatal(err)
	}
	if view.Display, err = json.Marshal(display); err != nil {
		t.Fatal(err)
	}
	return view
}

// assertIssueViewJSON compares a value with the expected JSON object by
// meaning, so field order and omitted empties do not matter.
func assertIssueViewJSON(t *testing.T, name string, got any, want string) {
	t.Helper()
	gotRaw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(gotRaw, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("%s: bad expectation: %v", name, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("%s:\n got  %s\n want %s", name, gotRaw, want)
	}
}

const issueViewTestToday = "2026-03-11"

var issueViewTestActiveProperties = map[string]bool{"p1": true, "p2": true}

func TestIssueViewTableQueryCarriesFiltersVariantAndOrdering(t *testing.T) {
	spec, err := issueViewTableQuery(issueViewForQueryTest(t, "workspace", "agents", "",
		map[string]any{"statusFilters": []string{"todo"}, "labelFilters": []string{"l1"}, "priorityFilters": []string{"bogus"}},
		map[string]any{"sortBy": "due_date", "sortDirection": "desc", "showSubIssues": false},
	), issueViewTestToday, issueViewTestActiveProperties)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueViewJSON(t, "spec", spec, `{
		"scope": {"kind": "workspace", "assignee_types": ["agent", "squad"]},
		"filters": {"statuses": ["todo"], "label_ids": ["l1"], "include_sub_issues": false},
		"sort": {"field": "due_date", "direction": "desc"}
	}`)
}

func TestIssueViewTableQueryKeepsEveryViewCondition(t *testing.T) {
	filters := func(query map[string]any) issueTableFiltersRequest {
		t.Helper()
		spec, err := issueViewTableQuery(issueViewForQueryTest(t, "workspace", "", "", query, map[string]any{}), issueViewTestToday, issueViewTestActiveProperties)
		if err != nil {
			t.Fatal(err)
		}
		return spec.Filters
	}
	cases := []struct {
		name  string
		query map[string]any
		want  string
	}{
		{"relations", map[string]any{"relationFilters": []string{"assigned", "bogus"}},
			`{"relations": ["assigned"], "include_sub_issues": true}`},
		{"negation", map[string]any{"statusFilters": []string{"done"}, "negatedFilters": []string{"status", "bogus"}},
			`{"statuses": ["done"], "negate": ["statuses"], "include_sub_issues": true}`},
		{"due dates need the viewer's day", map[string]any{"dueDateFilters": []string{"overdue", "overdue"}},
			`{"due_dates": ["overdue"], "today": "2026-03-11", "include_sub_issues": true}`},
		{"project statuses", map[string]any{"projectStatusFilters": []string{"completed", "bogus"}},
			`{"project_statuses": ["completed"], "include_sub_issues": true}`},
		{"actors and no-assignee", map[string]any{
			"assigneeFilters":   []any{map[string]any{"type": "agent", "id": "a1"}, "junk"},
			"includeNoAssignee": true,
			"creatorFilters":    []any{map[string]any{"type": "member", "id": "m1"}},
		}, `{"assignees": [{"type": "agent", "id": "a1"}], "include_no_assignee": true,
			"creators": [{"type": "member", "id": "m1"}], "include_sub_issues": true}`},
		{"properties keep strings and known operators", map[string]any{"propertyFilters": map[string]any{
			"p1": []any{"opt", map[string]any{"op": "gte", "value": "3"}, map[string]any{"op": "bogus", "value": "x"}},
			"p2": []any{},
		}}, `{"properties": {"p1": ["opt", {"op": "gte", "value": "3"}]}, "include_sub_issues": true}`},
		{"agent running", map[string]any{"agentRunningFilter": true},
			`{"working_only": true, "include_sub_issues": true}`},
		{"advanced tree", map[string]any{"advancedFilter": map[string]any{
			"type": "group", "id": "root", "match": "any",
			"children": []any{
				map[string]any{"type": "condition", "id": "c1", "field": "priority", "negated": false, "values": []string{"urgent", "bogus"}},
				map[string]any{"type": "group", "id": "g1", "match": "all", "children": []any{
					map[string]any{"type": "condition", "id": "c2", "field": "assignee", "negated": true, "values": []string{"none", "agent:a1", "robot:x"}},
					map[string]any{"type": "condition", "id": "c3", "field": "label", "values": []string{}},
				}},
				map[string]any{"type": "group", "id": "empty", "children": []any{}},
			},
		}}, `{"advanced": {"match": "any", "children": [
				{"field": "priorities", "values": ["urgent"]},
				{"match": "all", "children": [{"field": "assignees", "negate": true, "values": ["none", "agent:a1"]}]}
			]}, "include_sub_issues": true}`},
		{"advanced due date needs the viewer's day", map[string]any{"advancedFilter": map[string]any{
			"type": "group", "match": "all",
			"children": []any{map[string]any{"type": "condition", "field": "dueDate", "values": []string{"today"}}},
		}}, `{"advanced": {"match": "all", "children": [{"field": "due_dates", "values": ["today"]}]},
			"today": "2026-03-11", "include_sub_issues": true}`},
		{"advanced property condition", map[string]any{"advancedFilter": map[string]any{
			"type": "group", "match": "all",
			"children": []any{map[string]any{"type": "condition", "field": "property:p1", "values": []any{"", "a", "a", map[string]any{"op": "contains", "value": "x"}}}},
		}}, `{"advanced": {"match": "all", "children": [{"field": "property:p1", "values": ["a", {"op": "contains", "value": "x"}]}]},
			"include_sub_issues": true}`},
		{"an empty tree constrains nothing", map[string]any{"advancedFilter": map[string]any{
			"type": "group", "match": "all", "children": []any{map[string]any{"type": "condition", "field": "status", "values": []string{}}},
		}}, `{"include_sub_issues": true}`},
		{"is-not without a selection is no condition", map[string]any{"negatedFilters": []string{"status"}},
			`{"include_sub_issues": true}`},
	}
	for _, tc := range cases {
		assertIssueViewJSON(t, tc.name, filters(tc.query), tc.want)
	}
}

func TestIssueViewTableQueryScopesLikeTheViewPage(t *testing.T) {
	scope := func(scopeType, variant, scopeID string) (issueTableScope, error) {
		spec, err := issueViewTableQuery(issueViewForQueryTest(t, scopeType, variant, scopeID, map[string]any{}, map[string]any{}), issueViewTestToday, issueViewTestActiveProperties)
		return spec.Scope, err
	}
	projectID := "11111111-1111-1111-1111-111111111111"
	for _, tc := range []struct {
		name, scopeType, variant, scopeID, want string
	}{
		{"project", "project", "", projectID, `{"kind": "project", "project_id": "` + projectID + `"}`},
		{"project members", "project", "members", projectID, `{"kind": "project", "project_id": "` + projectID + `", "assignee_types": ["member"]}`},
		{"my created", "my", "created", "", `{"kind": "my", "relation": "created"}`},
		{"my without variant", "my", "", "", `{"kind": "my", "relation": "any"}`},
	} {
		got, err := scope(tc.scopeType, tc.variant, tc.scopeID)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		assertIssueViewJSON(t, tc.name, got, tc.want)
	}
	if _, err := scope("project", "", ""); err == nil {
		t.Fatal("project view without a project: want an error")
	}
	if _, err := scope("team", "", ""); err == nil {
		t.Fatal("unknown scope: want an error")
	}
}

func TestIssueViewTableQueryDropsTheDimensionTheScopeFixes(t *testing.T) {
	project, err := issueViewTableQuery(issueViewForQueryTest(t, "project", "", "11111111-1111-1111-1111-111111111111",
		map[string]any{"projectFilters": []string{"p2"}, "includeNoProject": true, "negatedFilters": []string{"project"}}, map[string]any{},
	), issueViewTestToday, issueViewTestActiveProperties)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueViewJSON(t, "project view", project.Filters, `{"include_sub_issues": true}`)
	mine, err := issueViewTableQuery(issueViewForQueryTest(t, "my", "assigned", "",
		map[string]any{"relationFilters": []string{"created"}, "negatedFilters": []string{"relation"}}, map[string]any{},
	), issueViewTestToday, issueViewTestActiveProperties)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueViewJSON(t, "my view", mine.Filters, `{"include_sub_issues": true}`)

	advanced := func(field string) map[string]any {
		return map[string]any{"advancedFilter": map[string]any{"type": "group", "match": "all", "children": []any{
			map[string]any{"type": "condition", "field": field, "values": []string{"x"}},
			map[string]any{"type": "condition", "field": "label", "values": []string{"l1"}},
		}}}
	}
	project, err = issueViewTableQuery(issueViewForQueryTest(t, "project", "", "11111111-1111-1111-1111-111111111111",
		advanced("project"), map[string]any{}), issueViewTestToday, issueViewTestActiveProperties)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueViewJSON(t, "project view tree", project.Filters,
		`{"advanced": {"match": "all", "children": [{"field": "label_ids", "values": ["l1"]}]}, "include_sub_issues": true}`)
	mine, err = issueViewTableQuery(issueViewForQueryTest(t, "my", "assigned", "",
		advanced("relation"), map[string]any{}), issueViewTestToday, issueViewTestActiveProperties)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueViewJSON(t, "my view tree", mine.Filters,
		`{"advanced": {"match": "all", "children": [{"field": "label_ids", "values": ["l1"]}]}, "include_sub_issues": true}`)
}

func TestIssueViewTableQueryDropsInactiveProperties(t *testing.T) {
	view := issueViewForQueryTest(t, "workspace", "", "", map[string]any{
		"propertyFilters": map[string]any{"p1": []string{"a"}, "gone": []string{"b"}},
		"advancedFilter": map[string]any{"type": "group", "match": "any", "children": []any{
			map[string]any{"type": "condition", "field": "property:gone", "values": []string{"c"}},
			map[string]any{"type": "group", "match": "all", "children": []any{
				map[string]any{"type": "condition", "field": "property:gone", "values": []string{"d"}},
			}},
			map[string]any{"type": "condition", "field": "property:p1", "values": []string{"e"}},
		}},
	}, map[string]any{"sortBy": "property:gone", "sortDirection": "desc"})
	if got := issueViewReferencedPropertyIDs(view); len(got) != 2 {
		t.Fatalf("referenced properties = %v, want p1 and gone", got)
	}
	spec, err := issueViewTableQuery(view, issueViewTestToday, issueViewTestActiveProperties)
	if err != nil {
		t.Fatal(err)
	}
	assertIssueViewJSON(t, "filters", spec.Filters, `{
		"properties": {"p1": ["a"]},
		"advanced": {"match": "any", "children": [{"field": "property:p1", "values": ["e"]}]},
		"include_sub_issues": true
	}`)
	assertIssueViewJSON(t, "sort", spec.Sort, `{"field": "position", "direction": "desc"}`)
}

func TestIssueViewTableQuerySort(t *testing.T) {
	sort := func(display map[string]any) issueTableSortRequest {
		spec, err := issueViewTableQuery(issueViewForQueryTest(t, "workspace", "", "", map[string]any{}, display), issueViewTestToday, issueViewTestActiveProperties)
		if err != nil {
			t.Fatal(err)
		}
		return spec.Sort
	}
	assertIssueViewJSON(t, "default", sort(map[string]any{}), `{"field": "position", "direction": "asc"}`)
	assertIssueViewJSON(t, "unknown field", sort(map[string]any{"sortBy": "bogus", "sortDirection": "sideways"}), `{"field": "position", "direction": "asc"}`)
	assertIssueViewJSON(t, "property", sort(map[string]any{"sortBy": "property:p1", "sortDirection": "desc"}), `{"field": "property:p1", "direction": "desc"}`)
}

func listIssueViewIssuesForTest(t *testing.T, userID, viewID, query string) *testutil.Response {
	t.Helper()
	path := "/api/issue-views/" + viewID + "/issues"
	if query != "" {
		path += "?" + query
	}
	req := newRequest(http.MethodGet, path, nil)
	req.Header.Set("X-User-ID", userID)
	return testutil.Call(t, testHandler.ListIssueViewIssues, withURLParam(req, "id", viewID))
}

func issueViewIssueTitles(response issueViewIssuesResponse) []string {
	titles := make([]string, len(response.Issues))
	for i, issue := range response.Issues {
		titles[i] = issue.Title
	}
	return titles
}

func TestListIssueViewIssues(t *testing.T) {
	suffix := time.Now().UnixNano()
	project := dbfx.Project(t, fmt.Sprintf("view issues %d", suffix))
	title := func(name string) string { return fmt.Sprintf("%s view issues %d", name, suffix) }
	dbfx.Issue(t, title("a urgent todo"), testutil.Cols{"project_id": project, "priority": "urgent", "due_date": "2026-01-01"})
	dbfx.Issue(t, title("b urgent done"), testutil.Cols{"project_id": project, "priority": "urgent", "status": "done"})
	dbfx.Issue(t, title("c low todo"), testutil.Cols{"project_id": project, "priority": "low"})

	newView := func(visibility string, query map[string]any) string {
		t.Helper()
		view, code, body := createIssueViewForTest(t, map[string]any{
			"name":       fmt.Sprintf("view issues %d", suffix),
			"scope_type": "project",
			"scope_id":   project,
			"visibility": visibility,
			"query":      query,
			"display":    map[string]any{"sortBy": "title", "sortDirection": "asc"},
		})
		if code != http.StatusCreated {
			t.Fatalf("create view: %d %s", code, body)
		}
		return view.ID
	}

	urgent := newView("workspace", map[string]any{"priorityFilters": []string{"urgent"}})
	var first issueViewIssuesResponse
	listIssueViewIssuesForTest(t, testUserID, urgent, "limit=1").Want(http.StatusOK).JSON(&first)
	if got := issueViewIssueTitles(first); !reflect.DeepEqual(got, []string{title("a urgent todo")}) {
		t.Fatalf("first page = %v", got)
	}
	if first.Total == nil || *first.Total != 2 || !first.HasMore || first.NextCursor == nil {
		t.Fatalf("first page meta: total=%v has_more=%v cursor=%v", first.Total, first.HasMore, first.NextCursor)
	}
	var second issueViewIssuesResponse
	listIssueViewIssuesForTest(t, testUserID, urgent, "limit=1&cursor="+*first.NextCursor).Want(http.StatusOK).JSON(&second)
	if got := issueViewIssueTitles(second); !reflect.DeepEqual(got, []string{title("b urgent done")}) {
		t.Fatalf("second page = %v", got)
	}
	if second.Total != nil || second.HasMore || second.NextCursor != nil {
		t.Fatalf("second page meta: total=%v has_more=%v cursor=%v", second.Total, second.HasMore, second.NextCursor)
	}

	notDone := newView("workspace", map[string]any{
		"priorityFilters": []string{"urgent"}, "statusFilters": []string{"done"}, "negatedFilters": []string{"status"},
	})
	var negated issueViewIssuesResponse
	listIssueViewIssuesForTest(t, testUserID, notDone, "").Want(http.StatusOK).JSON(&negated)
	if got := issueViewIssueTitles(negated); !reflect.DeepEqual(got, []string{title("a urgent todo")}) {
		t.Fatalf("urgent and not done = %v", got)
	}

	overdue := newView("workspace", map[string]any{"dueDateFilters": []string{"overdue"}})
	var late, early issueViewIssuesResponse
	listIssueViewIssuesForTest(t, testUserID, overdue, "today=2026-03-11").Want(http.StatusOK).JSON(&late)
	if got := issueViewIssueTitles(late); !reflect.DeepEqual(got, []string{title("a urgent todo")}) {
		t.Fatalf("overdue on 2026-03-11 = %v", got)
	}
	listIssueViewIssuesForTest(t, testUserID, overdue, "today=2025-12-01").Want(http.StatusOK).JSON(&early)
	if len(early.Issues) != 0 {
		t.Fatalf("overdue on 2025-12-01 = %v", issueViewIssueTitles(early))
	}

	listIssueViewIssuesForTest(t, testUserID, urgent, "today=03/11/2026").Want(http.StatusBadRequest)
	listIssueViewIssuesForTest(t, testUserID, urgent, "limit=0").Want(http.StatusBadRequest)
	listIssueViewIssuesForTest(t, testUserID, urgent, "limit=101").Want(http.StatusBadRequest)
}

func TestListIssueViewIssuesDropsArchivedPropertyConditions(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		name := "flat"
		if advanced {
			name = "advanced"
		}
		t.Run(name, func(t *testing.T) {
			suffix := time.Now().UnixNano()
			project := dbfx.Project(t, fmt.Sprintf("view archived property %s %d", name, suffix))
			property := createTestProperty(t, map[string]any{"name": fmt.Sprintf("Sev %s %d", name, suffix%1e9), "type": "text"})
			critical := dbfx.Issue(t, "view property critical", testutil.Cols{"project_id": project})
			dbfx.Issue(t, "view property unset", testutil.Cols{"project_id": project})
			if w := setIssuePropertyRaw(t, critical, property.ID, "critical"); w.Code != http.StatusOK {
				t.Fatalf("set property: %d %s", w.Code, w.Body.String())
			}
			query := map[string]any{"propertyFilters": map[string]any{property.ID: []string{"critical"}}}
			if advanced {
				query = map[string]any{"advancedFilter": map[string]any{"type": "group", "match": "all", "children": []any{
					map[string]any{"type": "condition", "field": "property:" + property.ID, "values": []string{"critical"}},
				}}}
			}
			view, code, body := createIssueViewForTest(t, map[string]any{
				"name": fmt.Sprintf("view archived property %s %d", name, suffix), "scope_type": "project", "scope_id": project,
				"query": query, "display": map[string]any{"sortBy": "property:" + property.ID},
			})
			if code != http.StatusCreated {
				t.Fatalf("create view: %d %s", code, body)
			}

			var before, after issueViewIssuesResponse
			listIssueViewIssuesForTest(t, testUserID, view.ID, "").Want(http.StatusOK).JSON(&before)
			if len(before.Issues) != 1 {
				t.Fatalf("active property: got %v, want only the critical issue", issueViewIssueTitles(before))
			}
			req := withURLParam(newRequest(http.MethodPatch, "/api/properties/"+property.ID, map[string]any{"archived": true}), "id", property.ID)
			testutil.Call(t, testHandler.UpdateProperty, req).Want(http.StatusOK)
			listIssueViewIssuesForTest(t, testUserID, view.ID, "").Want(http.StatusOK).JSON(&after)
			if len(after.Issues) != 2 {
				t.Fatalf("archived property: got %v, want both issues", issueViewIssueTitles(after))
			}
		})
	}
}

func TestListIssueViewIssuesHidesOtherMembersPrivateViews(t *testing.T) {
	view, code, body := createIssueViewForTest(t, map[string]any{
		"name":       "Private issue list",
		"scope_type": "workspace",
		"visibility": "private",
		"query":      map[string]any{},
	})
	if code != http.StatusCreated {
		t.Fatalf("create view: %d %s", code, body)
	}
	other := createSecondWorkspaceMember(t)
	listIssueViewIssuesForTest(t, other, view.ID, "").Want(http.StatusNotFound)
	listIssueViewIssuesForTest(t, testUserID, view.ID, "limit=1").Want(http.StatusOK)
}
