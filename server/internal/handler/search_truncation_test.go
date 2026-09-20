package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// MUL-8365: search truncates at a fixed window (20 by default), and until now
// the window was invisible — the endpoint returned a flat `issues` list with no
// total and no has_more, so a query matching more rows than the window silently
// dropped the overflow. Done/cancelled ranks sort below live work, so the rows
// dropped first under the old no-signal contract were exactly the completed
// matches an "earliest occurrence" query is looking for. The fix surfaces the
// truncation (has_more) so the drop is no longer silent, and keeps overflow
// reachable through offset pagination instead of destroying it.
//
// These run the real handler against the real SQL.

// searchResponse is the decoded SearchIssues / SearchProjects payload plus the
// truncation flag this work introduces.
type searchResponse struct {
	Items   []SearchIssueResponse `json:"issues"`
	HasMore bool                  `json:"has_more"`
}

type projectSearchResponse struct {
	Items   []SearchProjectResponse `json:"projects"`
	HasMore bool                    `json:"has_more"`
}

func searchIssuesResponse(t *testing.T, q string, limit, offset int) searchResponse {
	t.Helper()
	path := fmt.Sprintf(
		"/api/issues/search?workspace_id=%s&q=%s&limit=%d&offset=%d&include_closed=true",
		testWorkspaceID, url.QueryEscape(q), limit, offset,
	)
	w := httptest.NewRecorder()
	testHandler.SearchIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("SearchIssues(%q, limit=%d, offset=%d): expected 200, got %d: %s", q, limit, offset, w.Code, w.Body.String())
	}
	var res searchResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	return res
}

// seedRankProject inserts one project in the test workspace and returns its
// title, mirroring seedRankIssue.
func seedRankProject(t *testing.T, title string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, title).Scan(&id); err != nil {
		t.Fatalf("create project %q: %v", title, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, id)
	})
	return title
}

func searchProjectsResponse(t *testing.T, q string, limit, offset int) projectSearchResponse {
	t.Helper()
	path := fmt.Sprintf(
		"/api/projects/search?workspace_id=%s&q=%s&limit=%d&offset=%d&include_closed=true",
		testWorkspaceID, url.QueryEscape(q), limit, offset,
	)
	w := httptest.NewRecorder()
	testHandler.SearchProjects(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("SearchProjects(%q, limit=%d, offset=%d): expected 200, got %d: %s", q, limit, offset, w.Code, w.Body.String())
	}
	var res projectSearchResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode project search response: %v", err)
	}
	return res
}

func titlesOf(res searchResponse) []string {
	out := make([]string, 0, len(res.Items))
	for _, item := range res.Items {
		out = append(out, item.Title)
	}
	return out
}

func issueIDsOf(res searchResponse) []string {
	out := make([]string, 0, len(res.Items))
	for _, item := range res.Items {
		out = append(out, item.ID)
	}
	return out
}

func projectIDsOf(res projectSearchResponse) []string {
	out := make([]string, 0, len(res.Items))
	for _, item := range res.Items {
		out = append(out, item.ID)
	}
	return out
}

func assertNoDuplicateIDs(t *testing.T, ids []string) {
	t.Helper()
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id %q in paged search results", id)
		}
		seen[id] = true
	}
}

// Regression: a term matching more issues than the window must not silently
// drop the overflow. We seed a mix of live and done matches and confirm (a) the
// 20-row page flags has_more instead of pretending it is exhaustive, and (b)
// offset pagination reaches every seeded match without duplicates.
func TestSearchIssues_TruncationIsVisibleAndDoneMatchesArePaged(t *testing.T) {
	token := fmt.Sprintf("mulmore%d", time.Now().UnixNano())

	const liveN, doneN = 15, 15
	seeded := make([]string, 0, liveN+doneN)
	for i := 0; i < liveN; i++ {
		seeded = append(seeded, seedRankIssue(t, fmt.Sprintf("variant %s live %02d", token, i), "in_progress"))
	}
	for i := 0; i < doneN; i++ {
		seeded = append(seeded, seedRankIssue(t, fmt.Sprintf("variant %s done %02d", token, i), "done"))
	}

	const limit = 20
	page1 := searchIssuesResponse(t, token, limit, 0)
	if len(page1.Items) != limit {
		t.Fatalf("search(%q) page1 returned %d rows, want %d", token, len(page1.Items), limit)
	}
	if !page1.HasMore {
		t.Fatalf("search(%q) page1 with %d matches returned has_more=false; truncation must be visible", token, liveN+doneN)
	}

	page2 := searchIssuesResponse(t, token, limit, limit)
	if page2.HasMore {
		t.Fatalf("search(%q) page2 reported has_more=true after exhausting the set", token)
	}
	combined := append(issueIDsOf(page1), issueIDsOf(page2)...)
	assertNoDuplicateIDs(t, combined)
	if len(combined) != liveN+doneN {
		t.Fatalf("search(%q) page1+page2 returned %d rows, want %d", token, len(combined), liveN+doneN)
	}

	got := make(map[string]bool, len(page1.Items)+len(page2.Items))
	for _, title := range append(titlesOf(page1), titlesOf(page2)...) {
		got[title] = true
	}
	for _, title := range seeded {
		if !got[title] {
			t.Fatalf("search(%q) dropped seeded match %q across offset pages", token, title)
		}
	}
}

// Exact boundary: N == limit must not claim has_more, and the next offset page
// must be empty. N == limit+1 must set has_more and page2 must hold the leftover.
func TestSearchIssues_OffsetPaginationBoundaries(t *testing.T) {
	tokenExact := fmt.Sprintf("mulexact%d", time.Now().UnixNano())
	const limit = 5
	for i := 0; i < limit; i++ {
		seedRankIssue(t, fmt.Sprintf("exact %s %02d", tokenExact, i), "todo")
	}
	exact := searchIssuesResponse(t, tokenExact, limit, 0)
	if len(exact.Items) != limit {
		t.Fatalf("N=limit: got %d rows, want %d", len(exact.Items), limit)
	}
	if exact.HasMore {
		t.Fatalf("N=limit must report has_more=false")
	}
	exactNext := searchIssuesResponse(t, tokenExact, limit, limit)
	if len(exactNext.Items) != 0 {
		t.Fatalf("N=limit next page returned %d rows, want 0", len(exactNext.Items))
	}

	tokenOver := fmt.Sprintf("mulover%d", time.Now().UnixNano())
	for i := 0; i < limit+1; i++ {
		seedRankIssue(t, fmt.Sprintf("over %s %02d", tokenOver, i), "todo")
	}
	over := searchIssuesResponse(t, tokenOver, limit, 0)
	if len(over.Items) != limit {
		t.Fatalf("N=limit+1 page1: got %d rows, want %d", len(over.Items), limit)
	}
	if !over.HasMore {
		t.Fatalf("N=limit+1 must report has_more=true")
	}
	overNext := searchIssuesResponse(t, tokenOver, limit, limit)
	if len(overNext.Items) != 1 {
		t.Fatalf("N=limit+1 page2: got %d rows, want 1", len(overNext.Items))
	}
	if overNext.HasMore {
		t.Fatalf("N=limit+1 page2 must report has_more=false")
	}
	combined := append(issueIDsOf(over), issueIDsOf(overNext)...)
	assertNoDuplicateIDs(t, combined)
	if len(combined) != limit+1 {
		t.Fatalf("N=limit+1 page1+page2: got %d ids, want %d", len(combined), limit+1)
	}
}

// The flag must not be sticky when the window genuinely holds every match.
func TestSearchIssues_HasMoreIsFalseWhenWindowHoldsAllMatches(t *testing.T) {
	token := fmt.Sprintf("mulmore%d", time.Now().UnixNano())

	seedRankIssue(t, fmt.Sprintf("variant %s alpha", token), "done")
	seedRankIssue(t, fmt.Sprintf("variant %s beta", token), "in_progress")

	res := searchIssuesResponse(t, token, 50, 0)
	if len(res.Items) != 2 {
		t.Fatalf("search(%q) returned %d rows, want 2", token, len(res.Items))
	}
	if res.HasMore {
		t.Fatalf("search(%q) returned every match but reported has_more=true", token)
	}
}

// Project search truncates at the same invisible window in the command palette;
// give it the same has_more signal and deterministic offset pages (p.id tie-break).
func TestSearchProjects_TruncationIsVisible(t *testing.T) {
	token := fmt.Sprintf("mulproj%d", time.Now().UnixNano())

	for i := 0; i < 25; i++ {
		seedRankProject(t, fmt.Sprintf("project %s unit %02d", token, i))
	}

	const limit = 20
	page1 := searchProjectsResponse(t, token, limit, 0)
	if len(page1.Items) != limit {
		t.Fatalf("project search(%q) page1 returned %d rows, want %d", token, len(page1.Items), limit)
	}
	if !page1.HasMore {
		t.Fatalf("project search(%q) page1 with 25 matches returned has_more=false; truncation must be visible", token)
	}

	page2 := searchProjectsResponse(t, token, limit, limit)
	combined := append(projectIDsOf(page1), projectIDsOf(page2)...)
	assertNoDuplicateIDs(t, combined)
	if len(combined) != 25 {
		t.Fatalf("project search(%q) page1+page2 returned %d rows, want 25", token, len(combined))
	}
	if page2.HasMore {
		t.Fatalf("project search(%q) page2 reported has_more=true after exhausting the set", token)
	}
}

func TestSearchProjects_OffsetPaginationBoundaries(t *testing.T) {
	tokenExact := fmt.Sprintf("projexact%d", time.Now().UnixNano())
	const limit = 5
	for i := 0; i < limit; i++ {
		seedRankProject(t, fmt.Sprintf("exact %s %02d", tokenExact, i))
	}
	exact := searchProjectsResponse(t, tokenExact, limit, 0)
	if len(exact.Items) != limit {
		t.Fatalf("project N=limit: got %d rows, want %d", len(exact.Items), limit)
	}
	if exact.HasMore {
		t.Fatalf("project N=limit must report has_more=false")
	}
	exactNext := searchProjectsResponse(t, tokenExact, limit, limit)
	if len(exactNext.Items) != 0 {
		t.Fatalf("project N=limit next page returned %d rows, want 0", len(exactNext.Items))
	}

	tokenOver := fmt.Sprintf("projover%d", time.Now().UnixNano())
	for i := 0; i < limit+1; i++ {
		seedRankProject(t, fmt.Sprintf("over %s %02d", tokenOver, i))
	}
	over := searchProjectsResponse(t, tokenOver, limit, 0)
	if len(over.Items) != limit {
		t.Fatalf("project N=limit+1 page1: got %d rows, want %d", len(over.Items), limit)
	}
	if !over.HasMore {
		t.Fatalf("project N=limit+1 must report has_more=true")
	}
	overNext := searchProjectsResponse(t, tokenOver, limit, limit)
	if len(overNext.Items) != 1 {
		t.Fatalf("project N=limit+1 page2: got %d rows, want 1", len(overNext.Items))
	}
	combined := append(projectIDsOf(over), projectIDsOf(overNext)...)
	assertNoDuplicateIDs(t, combined)
	if len(combined) != limit+1 {
		t.Fatalf("project N=limit+1 page1+page2: got %d ids, want %d", len(combined), limit+1)
	}
}
