package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Triage entries are absent from every work surface (MUL-7189 §2.4).
//
// Each case below reads one surface twice over the SAME two issues: one in
// Triage and one not, both `status = todo`, differing in that single column.
// The ordinary issue is not scenery — it is the positive control. A filter
// typo, a missing workspace fixture or an empty page would also produce "the
// Triage entry is absent", and only the visible twin tells those apart from
// the rule actually working.

type triageReadFixture struct {
	projectID string
	ordinary  string
	inTriage  string
	token     string
}

// newTriageReadFixture builds the pair inside its own project, so a surface
// scoped to that project sees these two issues and nothing else the suite left
// behind. The metadata token does the same for the flat list.
func newTriageReadFixture(t *testing.T) triageReadFixture {
	t.Helper()
	token := fmt.Sprintf("triage-read-%d", time.Now().UnixNano())
	projectID := dbfx.Project(t, "Triage read "+token)

	issue := func(title, triageState string) string {
		cols := testutil.Cols{
			"status":     "todo",
			"project_id": projectID,
			"metadata":   fmt.Sprintf(`{"triage_read_test":%q}`, token),
			"number":     nextWorkspaceIssueNumber(t),
		}
		if triageState != "" {
			cols["triage_state"] = triageState
		}
		return dbfx.Issue(t, title, cols)
	}

	return triageReadFixture{
		projectID: projectID,
		ordinary:  issue(token+" ordinary", ""),
		inTriage:  issue(token+" in triage", triagePending),
		token:     token,
	}
}

func (f triageReadFixture) listPath(extra string) string {
	path := "/api/issues?workspace_id=" + testWorkspaceID + "&project_id=" + f.projectID
	if extra != "" {
		path += "&" + extra
	}
	return path
}

// listedIDs returns the ids GET /api/issues reported, and the total it claims
// beside them. The total is asserted too: it comes from a second query built
// from the same WHERE clause, and a predicate added to only one of them shows
// up as a page and a count that disagree.
func listedIDs(t *testing.T, path string) ([]string, int64) {
	t.Helper()
	var body struct {
		Issues []struct {
			ID string `json:"id"`
		} `json:"issues"`
		Total int64 `json:"total"`
	}
	testutil.Call(t, testHandler.ListIssues, newRequest(http.MethodGet, path, nil)).
		Want(http.StatusOK).JSON(&body)
	ids := make([]string, 0, len(body.Issues))
	for _, issue := range body.Issues {
		ids = append(ids, issue.ID)
	}
	return ids, body.Total
}

func assertExactly(t *testing.T, surface string, got []string, want ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	if len(got) != len(want) {
		t.Fatalf("%s returned %d issues, want %d: %v", surface, len(got), len(want), got)
	}
	for _, id := range want {
		if !seen[id] {
			t.Fatalf("%s did not return %s; got %v", surface, id, got)
		}
	}
}

func TestTriageEntriesAreAbsentFromTheIssueList(t *testing.T) {
	f := newTriageReadFixture(t)

	ids, total := listedIDs(t, f.listPath(""))
	assertExactly(t, "GET /api/issues", ids, f.ordinary)
	if total != 1 {
		t.Fatalf("list total counted %d issues, want 1 — the page and the count disagree", total)
	}

	ids, total = listedIDs(t, f.listPath("triage=true"))
	assertExactly(t, "GET /api/issues?triage=true", ids, f.inTriage)
	if total != 1 {
		t.Fatalf("triage list total counted %d issues, want 1", total)
	}
}

// The `triage` filter is the only way in, and only its exact spelling counts: a
// junk value must not widen a list.
func TestTriageFilterOnlyOptsInOnTrue(t *testing.T) {
	f := newTriageReadFixture(t)

	for _, value := range []string{"", "false", "1", "yes", "TRUE"} {
		ids, _ := listedIDs(t, f.listPath("triage="+value))
		assertExactly(t, "GET /api/issues?triage="+value, ids, f.ordinary)
	}
}

func TestTriageEntriesAreAbsentFromOpenOnlyAndTheTriageFilterIsRefused(t *testing.T) {
	f := newTriageReadFixture(t)

	ids, _ := listedIDs(t, f.listPath("open_only=true"))
	assertExactly(t, "GET /api/issues?open_only=true", ids, f.ordinary)

	// open_only answers from a static query that always excludes Triage, so the
	// combination is refused rather than silently answered with the opposite of
	// what was asked for.
	testutil.Call(t, testHandler.ListIssues,
		newRequest(http.MethodGet, f.listPath("open_only=true&triage=true"), nil)).
		Want(http.StatusBadRequest)
}

func TestTriageEntriesAreAbsentFromGroupedIssues(t *testing.T) {
	f := newTriageReadFixture(t)

	var body struct {
		Groups []struct {
			Issues []struct {
				ID string `json:"id"`
			} `json:"issues"`
		} `json:"groups"`
	}
	testutil.Call(t, testHandler.ListGroupedIssues, newRequest(http.MethodGet,
		"/api/issues/grouped?workspace_id="+testWorkspaceID+
			"&group_by=assignee&project_id="+f.projectID, nil)).
		Want(http.StatusOK).JSON(&body)

	var ids []string
	for _, group := range body.Groups {
		for _, issue := range group.Issues {
			ids = append(ids, issue.ID)
		}
	}
	assertExactly(t, "GET /api/issues/grouped", ids, f.ordinary)
}

func (f triageReadFixture) tableQuery(triage bool) issueTableQuerySpec {
	return issueTableQuerySpec{
		Scope:  issueTableScope{Kind: "project", ProjectID: f.projectID},
		Sort:   issueTableSortRequest{Field: "position", Direction: "asc"},
		Triage: triage,
	}
}

func TestTriageEntriesAreAbsentFromTheIssueTable(t *testing.T) {
	f := newTriageReadFixture(t)

	tableIDs := func(triage bool) ([]string, int64) {
		t.Helper()
		var rows issueTableRowsResponse
		testutil.Call(t, testHandler.ListIssueTableRows,
			newRequest(http.MethodPost, "/api/issues/table/rows", issueTableRowsRequest{
				Query: f.tableQuery(triage),
				Group: issueTableGroupSpec{Kind: "none"},
				Page:  issueTablePageRequest{Limit: 50},
			})).Want(http.StatusOK).JSON(&rows)
		ids := make([]string, 0, len(rows.Rows))
		for _, row := range rows.Rows {
			ids = append(ids, row.Issue.ID)
		}
		return ids, rows.Total
	}

	ids, total := tableIDs(false)
	assertExactly(t, "table rows", ids, f.ordinary)
	if total != 1 {
		t.Fatalf("table total counted %d issues, want 1", total)
	}

	ids, total = tableIDs(true)
	assertExactly(t, "table rows (triage)", ids, f.inTriage)
	if total != 1 {
		t.Fatalf("triage table total counted %d issues, want 1", total)
	}
}

// Facet counts are built from the same compiled WHERE clause as the rows, and
// they are what the filter sidebar shows. A count that still includes Triage
// would advertise a row the table cannot open.
func TestTriageEntriesAreAbsentFromTableFacetCounts(t *testing.T) {
	f := newTriageReadFixture(t)

	includeTotal := true
	var facets issueTableFacetsResponse
	testutil.Call(t, testHandler.ListIssueTableFacets,
		newRequest(http.MethodPost, "/api/issues/table/facets", issueTableFacetsRequest{
			Query:        f.tableQuery(false),
			Facets:       []issueTableFacetSpec{{Kind: "status"}},
			IncludeTotal: &includeTotal,
		})).Want(http.StatusOK).JSON(&facets)

	if facets.Total != 1 {
		t.Fatalf("facet total counted %d issues, want 1 (the ordinary issue only)", facets.Total)
	}
	var todo int64
	for _, facet := range facets.Facets {
		for _, value := range facet.Values {
			if value.Key == "todo" {
				todo = value.Count
			}
		}
	}
	if todo != 1 {
		t.Fatalf("status facet counted %d todo issues, want 1 — both fixtures are todo, so this is the Triage one leaking", todo)
	}
}

// A project's progress bar is a count, and the Triage entry carries a PROPOSED
// project. Counting it would move the bar for work nobody accepted.
func TestTriageEntriesAreAbsentFromProjectStats(t *testing.T) {
	f := newTriageReadFixture(t)

	total, done := testHandler.loadProjectIssueStats(t.Context(), parseUUID(testWorkspaceID), parseUUID(f.projectID))
	if total != 1 {
		t.Fatalf("project stats counted %d issues, want 1 (the ordinary issue only)", total)
	}
	if done != 0 {
		t.Fatalf("project stats counted %d done issues, want 0", done)
	}
}

// Search is the one read that can widen to Triage, because an entry has to stay
// findable in order to be merged into (MUL-7189 §2.4).
func TestSearchExcludesTriageUnlessAskedAndLabelsWhatItReturns(t *testing.T) {
	f := newTriageReadFixture(t)

	search := func(query string) map[string]*string {
		t.Helper()
		var body struct {
			Issues []struct {
				ID          string  `json:"id"`
				TriageState *string `json:"triage_state"`
			} `json:"issues"`
		}
		testutil.Call(t, testHandler.SearchIssues, newRequest(http.MethodGet,
			"/api/issues/search?workspace_id="+testWorkspaceID+"&q="+f.token+query, nil)).
			Want(http.StatusOK).JSON(&body)
		found := map[string]*string{}
		for _, issue := range body.Issues {
			found[issue.ID] = issue.TriageState
		}
		return found
	}

	found := search("")
	if _, ok := found[f.ordinary]; !ok {
		t.Fatalf("search did not find the ordinary issue; found %v", found)
	}
	if _, ok := found[f.inTriage]; ok {
		t.Fatal("search returned the Triage entry without include_triage")
	}

	found = search("&include_triage=true")
	if _, ok := found[f.ordinary]; !ok {
		t.Fatalf("include_triage narrowed the search instead of widening it; found %v", found)
	}
	state, ok := found[f.inTriage]
	if !ok {
		t.Fatalf("include_triage did not return the Triage entry; found %v", found)
	}
	if state == nil || *state != triagePending {
		t.Fatalf("Triage hit carries triage_state %v, want %q — the badge has nothing to render from",
			state, triagePending)
	}
	if found[f.ordinary] != nil {
		t.Fatalf("an ordinary issue carries triage_state %q; only Triage hits may", *found[f.ordinary])
	}
}

// The three hand-built WHERE clauses are the ones a text lint cannot see into,
// so they are pinned here instead: the default is the work surface, and only an
// explicit ask flips it.
func TestIssueWhereBuildersCarryTheTriagePredicate(t *testing.T) {
	query, _ := buildSearchQuery("hello", []string{"hello"}, 0, false, false, false, nil)
	if !strings.Contains(query, "i.triage_state IS NULL") {
		t.Fatalf("search does not exclude Triage by default:\n%s", query)
	}
	query, _ = buildSearchQuery("hello", []string{"hello"}, 0, false, false, true, nil)
	if strings.Contains(query, "i.triage_state IS NULL") {
		t.Fatalf("include_triage did not lift the exclusion:\n%s", query)
	}
	if !strings.Contains(query, "i.triage_state") {
		t.Fatalf("an include_triage search does not project triage_state, so no hit can be labelled:\n%s", query)
	}
}
