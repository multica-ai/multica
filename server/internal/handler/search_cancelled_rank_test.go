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

// MUL-5824: cancelled work is abandoned work. It stays reachable, but it must
// never sit above a live issue on relevance alone — and it must be what the
// LIMIT window drops first, not what pushes live work off the page.
//
// These run the real handler against the real SQL. The string assertions in
// search_test.go only prove the ORDER BY was assembled in the intended shape;
// they cannot show Postgres actually sorts this way.

// seedRankIssue inserts one issue in the handler test workspace and returns its
// title. Titles carry a per-run token so each case searches only its own rows.
func seedRankIssue(t *testing.T, title, status string) string {
	t.Helper()
	ctx := context.Background()

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}

	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, created_at, updated_at
		)
		VALUES ($1, $2, $3, 'none', 'member', $4, 0, $5, now(), now())
		RETURNING id
	`, testWorkspaceID, title, status, testUserID, number).Scan(&id); err != nil {
		t.Fatalf("create issue %q (%s): %v", title, status, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
	})

	return title
}

// searchTitles runs SearchIssues with include_closed=true, which is what every
// caller that can surface cancelled work sends: the web command palette, the web
// mention picker, the issue picker, and the mobile search screen. (The mobile
// mention picker at apps/mobile/components/issue/pickers/mention-picker-body.tsx
// sends include_closed=false, so it never sees cancelled issues at all and the
// ranking below cannot apply to it.) The closed-exclusion branch is therefore
// not what keeps cancelled work down on the surfaces that do include it.
func searchTitles(t *testing.T, q string) []string {
	t.Helper()

	path := fmt.Sprintf(
		"/api/issues/search?workspace_id=%s&q=%s&limit=50&include_closed=true",
		testWorkspaceID, url.QueryEscape(q),
	)
	w := httptest.NewRecorder()
	testHandler.SearchIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("SearchIssues(%q): expected 200, got %d: %s", q, w.Code, w.Body.String())
	}

	var response struct {
		Issues []SearchIssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response: %v", err)
	}

	titles := make([]string, 0, len(response.Issues))
	for _, issue := range response.Issues {
		titles = append(titles, issue.Title)
	}
	return titles
}

func assertOrder(t *testing.T, q string, got, want []string) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("search(%q) order = %v, want %v", q, got, want)
	}
}

// A cancelled issue that matches BETTER than a live one still loses. This is
// the case statusRank could never fix: it only orders issues that already tied
// on relevance, so before this change the cancelled starts-with match (tier 2)
// beat the in_progress contains match (tier 3).
func TestSearchIssues_CancelledLosesToLiveWorkDespiteBetterRelevance(t *testing.T) {
	token := fmt.Sprintf("mulrank%d", time.Now().UnixNano())

	cancelled := seedRankIssue(t, token+" leading match", "cancelled")
	live := seedRankIssue(t, "trailing "+token+" match", "in_progress")

	assertOrder(t, token, searchTitles(t, token), []string{live, cancelled})
}

// Every cancelled issue sinks below every live one, regardless of how the
// relevance tiers fall out among them.
func TestSearchIssues_AllCancelledSortBelowAllLive(t *testing.T) {
	token := fmt.Sprintf("mulrank%d", time.Now().UnixNano())

	cancelledStrong := seedRankIssue(t, token+" alpha", "cancelled")
	cancelledWeak := seedRankIssue(t, "zeta "+token+" alpha", "cancelled")
	liveWeak := seedRankIssue(t, "omega "+token+" beta", "todo")
	liveStrong := seedRankIssue(t, token+" beta", "in_progress")

	// Within each half the existing relevance tiers still apply: starts-with
	// (tier 2) ahead of contains (tier 3).
	assertOrder(t, token, searchTitles(t, token), []string{
		liveStrong, liveWeak, cancelledStrong, cancelledWeak,
	})
}

// Searching a title verbatim is unambiguous targeting — the searcher already
// knows which issue they want, so demoting it would hide the row they asked for.
func TestSearchIssues_CancelledExactTitleStaysOnTop(t *testing.T) {
	token := fmt.Sprintf("mulrank%d", time.Now().UnixNano())

	exactTitle := token + " alpha"
	cancelled := seedRankIssue(t, exactTitle, "cancelled")
	live := seedRankIssue(t, "alpha "+token+" delta", "todo")

	// Both match every word of the query; only the cancelled one matches it
	// exactly, and that direct hit outranks the demotion.
	assertOrder(t, exactTitle, searchTitles(t, exactTitle), []string{cancelled, live})
}

// Looking an issue up by its identifier must return it, cancelled or not.
func TestSearchIssues_CancelledIdentifierLookupStaysOnTop(t *testing.T) {
	token := fmt.Sprintf("mulrank%d", time.Now().UnixNano())

	cancelled := seedRankIssue(t, token+" cancelled target", "cancelled")

	var number int
	var prefix string
	if err := testPool.QueryRow(context.Background(), `
		SELECT i.number, w.issue_prefix
		FROM issue i JOIN workspace w ON w.id = i.workspace_id
		WHERE i.workspace_id = $1 AND i.title = $2
	`, testWorkspaceID, cancelled).Scan(&number, &prefix); err != nil {
		t.Fatalf("load seeded identifier: %v", err)
	}

	// A live decoy that also matches the identifier as free text, so the
	// assertion is about ordering rather than about being the only result.
	identifier := fmt.Sprintf("%s-%d", prefix, number)
	live := seedRankIssue(t, "notes about "+identifier+" "+token, "in_progress")

	assertOrder(t, identifier, searchTitles(t, identifier), []string{cancelled, live})
}

// Scope guard: 'done' is finished work worth referencing, and this change must
// not sweep it up. A done issue keeps ranking on relevance like any live issue.
func TestSearchIssues_DoneStillRanksOnRelevance(t *testing.T) {
	token := fmt.Sprintf("mulrank%d", time.Now().UnixNano())

	done := seedRankIssue(t, token+" leading match", "done")
	live := seedRankIssue(t, "trailing "+token+" match", "in_progress")

	assertOrder(t, token, searchTitles(t, token), []string{done, live})
}
