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
// reachable through a larger limit / offset instead of destroying it.
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

func searchIssuesResponse(t *testing.T, q string, limit int) searchResponse {
	t.Helper()
	path := fmt.Sprintf(
		"/api/issues/search?workspace_id=%s&q=%s&limit=%d&include_closed=true",
		testWorkspaceID, url.QueryEscape(q), limit,
	)
	w := httptest.NewRecorder()
	testHandler.SearchIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("SearchIssues(%q, limit=%d): expected 200, got %d: %s", q, limit, w.Code, w.Body.String())
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

func searchProjectsResponse(t *testing.T, q string, limit int) projectSearchResponse {
	t.Helper()
	path := fmt.Sprintf(
		"/api/projects/search?workspace_id=%s&q=%s&limit=%d&include_closed=true",
		testWorkspaceID, url.QueryEscape(q), limit,
	)
	w := httptest.NewRecorder()
	testHandler.SearchProjects(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("SearchProjects(%q, limit=%d): expected 200, got %d: %s", q, limit, w.Code, w.Body.String())
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

// Regression: a term matching more issues than the window must not silently
// drop the overflow. We seed a mix of live and done matches and confirm (a) the
// 20-row page flags has_more instead of pretending it is exhaustive, and (b) the
// done matches that fell past the window under the old status_rank are still
// reachable at a larger limit — paged, not destroyed.
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

	// The default 20-row window cannot hold 30 matches. It must say so.
	page := searchIssuesResponse(t, token, 20)
	if len(page.Items) != 20 {
		t.Fatalf("search(%q) at limit=20 returned %d rows, want 20", token, len(page.Items))
	}
	if !page.HasMore {
		t.Fatalf("search(%q) at limit=20 with 30 matches returned has_more=false; truncation must be visible", token)
	}

	// A larger limit must surface every match — including the done ones that
	// would otherwise fall past the window — so nothing is silently destroyed.
	full := searchIssuesResponse(t, token, 50)
	if len(full.Items) != liveN+doneN {
		t.Fatalf("search(%q) at limit=50 returned %d rows, want %d", token, len(full.Items), liveN+doneN)
	}
	if full.HasMore {
		t.Fatalf("search(%q) at limit=50 reported has_more=true but returned all %d matches", token, liveN+doneN)
	}
	got := make(map[string]bool, len(full.Items))
	for _, title := range titlesOf(full) {
		got[title] = true
	}
	for _, title := range seeded {
		if !got[title] {
			t.Fatalf("search(%q) dropped seeded match %q at a non-truncating window", token, title)
		}
	}
}

// The flag must not be sticky when the window genuinely holds every match.
func TestSearchIssues_HasMoreIsFalseWhenWindowHoldsAllMatches(t *testing.T) {
	token := fmt.Sprintf("mulmore%d", time.Now().UnixNano())

	seedRankIssue(t, fmt.Sprintf("variant %s alpha", token), "done")
	seedRankIssue(t, fmt.Sprintf("variant %s beta", token), "in_progress")

	res := searchIssuesResponse(t, token, 50)
	if len(res.Items) != 2 {
		t.Fatalf("search(%q) returned %d rows, want 2", token, len(res.Items))
	}
	if res.HasMore {
		t.Fatalf("search(%q) returned every match but reported has_more=true", token)
	}
}

// Project search truncates at the same invisible window in the command palette;
// give it the same has_more signal so the "more results" story is consistent.
func TestSearchProjects_TruncationIsVisible(t *testing.T) {
	token := fmt.Sprintf("mulproj%d", time.Now().UnixNano())

	for i := 0; i < 25; i++ {
		seedRankProject(t, fmt.Sprintf("project %s unit %02d", token, i))
	}

	page := searchProjectsResponse(t, token, 20)
	if len(page.Items) != 20 {
		t.Fatalf("project search(%q) at limit=20 returned %d rows, want 20", token, len(page.Items))
	}
	if !page.HasMore {
		t.Fatalf("project search(%q) at limit=20 with 25 matches returned has_more=false; truncation must be visible", token)
	}
}
