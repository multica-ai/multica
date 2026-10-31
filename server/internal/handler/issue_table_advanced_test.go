package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// rawValues encodes condition values the way a client sends them.
func rawValues(values ...any) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		buf, err := json.Marshal(value)
		if err != nil {
			panic(err)
		}
		out = append(out, buf)
	}
	return out
}

// issueTableFixtureRows lists, sorted, the ids from `fixture` that a workspace
// table query with these filters returns.
func issueTableFixtureRows(t *testing.T, fixture map[string]struct{}, filters issueTableFiltersRequest) []string {
	t.Helper()
	var response issueTableRowsResponse
	testutil.Call(t, testHandler.ListIssueTableRows,
		newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
			Query: issueTableQuerySpec{
				Scope:   issueTableScope{Kind: "workspace"},
				Filters: filters,
				Sort:    issueTableSortRequest{Field: "title", Direction: "asc"},
			},
			Group: issueTableGroupSpec{Kind: "none"},
			Page:  issueTablePageRequest{Limit: 100},
		}),
	).Want(http.StatusOK).JSON(&response)
	ids := make([]string, 0, len(response.Rows))
	for _, row := range response.Rows {
		if _, ok := fixture[row.Issue.ID]; ok {
			ids = append(ids, row.Issue.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// Buckets resolve against the viewer's day: 2026-03-11 is a Wednesday, so
// "this week" is Mar 9–15 and "next 7 days" is Mar 11–17.
func TestIssueTableRowsDueDateBuckets(t *testing.T) {
	suffix := time.Now().UnixNano()
	issue := func(title string, due any) string {
		return dbfx.Issue(t, fmt.Sprintf("due %s %d", title, suffix), testutil.Cols{"due_date": due})
	}
	yesterday := issue("yesterday", "2026-03-10")
	today := issue("today", "2026-03-11")
	sunday := issue("sunday", "2026-03-15")
	nextMonday := issue("next monday", "2026-03-16")
	later := issue("later", "2026-03-30")
	undated := issue("undated", nil)
	fixture := map[string]struct{}{
		yesterday: {}, today: {}, sunday: {}, nextMonday: {}, later: {}, undated: {},
	}
	assertRows := func(name string, filters issueTableFiltersRequest, want ...string) {
		t.Helper()
		filters.Today = "2026-03-11"
		got := issueTableFixtureRows(t, fixture, filters)
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: ids = %v, want %v", name, got, want)
		}
	}

	assertRows("overdue", issueTableFiltersRequest{DueDates: []string{"overdue"}}, yesterday)
	assertRows("today", issueTableFiltersRequest{DueDates: []string{"today"}}, today)
	assertRows("this week runs Monday to Sunday",
		issueTableFiltersRequest{DueDates: []string{"this_week"}}, yesterday, today, sunday)
	assertRows("next 7 days starts today",
		issueTableFiltersRequest{DueDates: []string{"next_7_days"}}, today, sunday, nextMonday)
	assertRows("no due date", issueTableFiltersRequest{DueDates: []string{"none"}}, undated)
	assertRows("buckets OR together",
		issueTableFiltersRequest{DueDates: []string{"overdue", "none"}}, yesterday, undated)
	assertRows("is not overdue keeps undated rows",
		issueTableFiltersRequest{DueDates: []string{"overdue"}, Negate: []string{"due_dates"}},
		today, sunday, nextMonday, later, undated)
}

// The tree ORs across dimensions, which no combination of flat filters can.
func TestIssueTableRowsAdvancedFilter(t *testing.T) {
	suffix := time.Now().UnixNano()
	project := dbfx.Project(t, fmt.Sprintf("advanced project %d", suffix))
	label := dbfx.Insert(t, "issue_label", testutil.Cols{
		"workspace_id": testWorkspaceID, "name": fmt.Sprintf("bug %d", suffix), "color": "#ff0000",
	})
	issue := func(title string, cols testutil.Cols) string {
		return dbfx.Issue(t, fmt.Sprintf("advanced %s %d", title, suffix), cols)
	}
	estimate := uuid.NewString()
	estimateOf := func(n int) any { return testutil.Raw(fmt.Sprintf(`'{"%s": %d}'::jsonb`, estimate, n)) }
	urgentInProject := issue("urgent in project", testutil.Cols{"priority": "urgent", "project_id": project, "properties": estimateOf(5)})
	bugInProject := issue("bug in project", testutil.Cols{"priority": "low", "project_id": project, "due_date": "2026-03-10", "properties": estimateOf(2)})
	quietInProject := issue("quiet in project", testutil.Cols{"priority": "low", "project_id": project, "status": "done"})
	urgentElsewhere := issue("urgent elsewhere", testutil.Cols{"priority": "urgent", "assignee_type": "member", "assignee_id": testUserID})
	dbfx.InsertNoID(t, "issue_to_label", testutil.Cols{"issue_id": bugInProject, "label_id": label},
		"issue_id = $1", bugInProject)
	fixture := map[string]struct{}{
		urgentInProject: {}, bugInProject: {}, quietInProject: {}, urgentElsewhere: {},
	}
	cond := func(field string, negate bool, values ...any) issueTableFilterNode {
		return issueTableFilterNode{Field: field, Negate: negate, Values: rawValues(values...)}
	}
	atLeast3 := map[string]string{"op": "gte", "value": "3"}
	assertTree := func(name string, root issueTableFilterNode, want ...string) {
		t.Helper()
		got := issueTableFixtureRows(t, fixture, issueTableFiltersRequest{Advanced: &root, Today: "2026-03-11"})
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s: ids = %v, want %v", name, got, want)
		}
	}

	assertTree("project AND (urgent OR bug)",
		issueTableFilterNode{Match: "all", Children: []issueTableFilterNode{
			cond("project_ids", false, project),
			{Match: "any", Children: []issueTableFilterNode{
				cond("priorities", false, "urgent"),
				cond("label_ids", false, label),
			}},
		}},
		urgentInProject, bugInProject)
	assertTree("any at the root ORs across dimensions",
		issueTableFilterNode{Match: "any", Children: []issueTableFilterNode{
			cond("label_ids", false, label),
			cond("relations", false, "assigned"),
		}},
		bugInProject, urgentElsewhere)
	assertTree("negated condition keeps rows without a value",
		issueTableFilterNode{Children: []issueTableFilterNode{
			cond("project_ids", true, project),
		}},
		urgentElsewhere)
	assertTree("assignee none and due buckets",
		issueTableFilterNode{Children: []issueTableFilterNode{
			cond("assignees", false, "none"),
			cond("due_dates", true, "overdue"),
			cond("statuses", true, "done"),
		}},
		urgentInProject)
	// An empty condition or group must not read as TRUE inside "any".
	assertTree("empty members drop out",
		issueTableFilterNode{Match: "any", Children: []issueTableFilterNode{
			cond("priorities", false),
			{Match: "all"},
			cond("label_ids", false, label),
		}},
		bugInProject)
	assertTree("an empty tree constrains nothing",
		issueTableFilterNode{Match: "any"},
		urgentInProject, bugInProject, quietInProject, urgentElsewhere)
	assertTree("a one-condition group is that condition",
		issueTableFilterNode{Match: "any", Children: []issueTableFilterNode{
			cond("priorities", false, "urgent"),
		}},
		urgentInProject, urgentElsewhere)
	assertTree("a property condition ORs with a dimension",
		issueTableFilterNode{Match: "any", Children: []issueTableFilterNode{
			cond("property:"+estimate, false, atLeast3),
			cond("label_ids", false, label),
		}},
		urgentInProject, bugInProject)
	assertTree("a negated property condition keeps rows without a value",
		issueTableFilterNode{Children: []issueTableFilterNode{
			cond("property:"+estimate, true, atLeast3),
		}},
		bugInProject, quietInProject, urgentElsewhere)
	assertTree("a property condition takes no value",
		issueTableFilterNode{Children: []issueTableFilterNode{
			cond("property:"+estimate, false, noPropertyValue),
			cond("priorities", false, "urgent"),
		}},
		urgentElsewhere)
	got := issueTableFixtureRows(t, fixture, issueTableFiltersRequest{
		ProjectIDs: []string{project},
		Advanced: &issueTableFilterNode{Children: []issueTableFilterNode{
			cond("priorities", false, "urgent"),
		}},
	})
	if fmt.Sprint(got) != fmt.Sprint([]string{urgentInProject}) {
		t.Fatalf("flat AND tree: ids = %v, want [%s]", got, urgentInProject)
	}
}

func TestIssueTableRowsRejectsInvalidAdvancedAndDueDates(t *testing.T) {
	nested := issueTableFilterNode{Children: []issueTableFilterNode{
		{Match: "any", Children: []issueTableFilterNode{
			{Match: "all", Children: []issueTableFilterNode{{Field: "priorities", Values: rawValues("high")}}},
		}},
	}}
	tooMany := issueTableFilterNode{}
	for i := 0; i <= issueTableFilterMaxConditions; i++ {
		tooMany.Children = append(tooMany.Children, issueTableFilterNode{Field: "priorities", Values: rawValues("high")})
	}
	for name, filters := range map[string]issueTableFiltersRequest{
		"unknown bucket":         {DueDates: []string{"tomorrow"}, Today: "2026-03-11"},
		"bucket without today":   {DueDates: []string{"today"}},
		"malformed today":        {DueDates: []string{"today"}, Today: "11/03/2026"},
		"tree nested too deep":   {Advanced: &nested},
		"unknown field":          {Advanced: &issueTableFilterNode{Children: []issueTableFilterNode{{Field: "title", Values: rawValues("x")}}}},
		"bad actor value":        {Advanced: &issueTableFilterNode{Children: []issueTableFilterNode{{Field: "assignees", Values: rawValues("robot:1")}}}},
		"creator cannot be none": {Advanced: &issueTableFilterNode{Children: []issueTableFilterNode{{Field: "creators", Values: rawValues("none")}}}},
		"unknown match":          {Advanced: &issueTableFilterNode{Match: "xor"}},
		"root is a condition":    {Advanced: &issueTableFilterNode{Field: "priorities", Values: rawValues("high")}},
		"tree due without today": {Advanced: &issueTableFilterNode{Children: []issueTableFilterNode{{Field: "due_dates", Values: rawValues("overdue")}}}},
		"too many conditions":    {Advanced: &tooMany},
		"property id not a uuid": {Advanced: &issueTableFilterNode{Children: []issueTableFilterNode{{Field: "property:estimate", Values: rawValues("3")}}}},
		"bad property operator": {Advanced: &issueTableFilterNode{Children: []issueTableFilterNode{
			{Field: "property:" + uuid.NewString(), Values: rawValues(map[string]string{"op": "gte", "value": "many"})},
		}}},
		"non-string dimension value": {Advanced: &issueTableFilterNode{Children: []issueTableFilterNode{{Field: "priorities", Values: rawValues(1)}}}},
	} {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.ListIssueTableRows,
				newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
					Query: issueTableQuerySpec{
						Scope:   issueTableScope{Kind: "workspace"},
						Filters: filters,
						Sort:    issueTableSortRequest{Field: "position", Direction: "asc"},
					},
					Group: issueTableGroupSpec{Kind: "none"},
					Page:  issueTablePageRequest{Limit: 50},
				}),
			).Want(http.StatusBadRequest)
		})
	}
}

func TestIssueTableFingerprintCanonicalizesAdvancedValues(t *testing.T) {
	spec := func(values ...any) issueTableQuerySpec {
		return issueTableQuerySpec{
			Scope: issueTableScope{Kind: "workspace"},
			Filters: issueTableFiltersRequest{Advanced: &issueTableFilterNode{
				Match:    "any",
				Children: []issueTableFilterNode{{Field: "priorities", Values: rawValues(values...)}},
			}},
			Sort: issueTableSortRequest{Field: "position", Direction: "asc"},
		}
	}
	original := spec("urgent", "high", "urgent")
	a, err := canonicalIssueTableFingerprint(testWorkspaceID, original)
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalIssueTableFingerprint(testWorkspaceID, spec("high", "urgent"))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("value order changed the fingerprint: %s != %s", a, b)
	}
	values := make([]string, 0, 3)
	for _, raw := range original.Filters.Advanced.Children[0].Values {
		values = append(values, string(raw))
	}
	if fmt.Sprint(values) != `["urgent" "high" "urgent"]` {
		t.Fatalf("fingerprint mutated the request: %v", values)
	}
}
