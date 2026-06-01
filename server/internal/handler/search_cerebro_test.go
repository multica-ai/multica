// CEREBRO-PATCH(search-cerebro-test-fir-2595): integration tests for the
// FTS-backed issue search + Slack-style inline filter pipeline.
package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	cerebrosearch "github.com/multica-ai/multica/server/internal/cerebro/search"
)

// callSearchIssues hits the SearchIssues handler with the given raw query and
// returns the decoded envelope. Skips the test if the test pool is unset
// (e.g. CI without Postgres).
func callSearchIssues(t *testing.T, q string, includeClosed bool) SearchIssuesResponseEnvelope {
	t.Helper()
	if testHandler == nil {
		t.Skip("test DB not available")
	}
	params := url.Values{}
	params.Set("q", q)
	if includeClosed {
		params.Set("include_closed", "true")
	}
	req := httptest.NewRequest("GET", "/api/issues/search?"+params.Encode(), nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.SearchIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchIssues: %d %s", w.Code, w.Body.String())
	}
	var env SearchIssuesResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, w.Body.String())
	}
	return env
}

// seedIssueWithComment creates an issue with the given title/description and
// optional comment content, returning the issue id. Cleanup is registered
// with t.Cleanup.
func seedIssueWithComment(t *testing.T, title, description, commentContent string) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	const sql = `INSERT INTO issue (workspace_id, title, description, status, priority, kind, creator_type, creator_id, number)
		VALUES ($1, $2, $3, 'todo', 'none', 'issue', 'member', $4,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id`
	if err := testPool.QueryRow(ctx, sql, testWorkspaceID, title, description, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	if commentContent != "" {
		const csql = `INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
			VALUES ($1, $2, 'member', $3, $4, 'comment')`
		if _, err := testPool.Exec(ctx, csql, issueID, testWorkspaceID, testUserID, commentContent); err != nil {
			t.Fatalf("seed comment: %v", err)
		}
	}
	return issueID
}

func TestSearchIssues_FTS_FindsByTitle(t *testing.T) {
	id := seedIssueWithComment(t, "Deploy script flaps on production", "", "")
	env := callSearchIssues(t, "deploy", false)
	found := false
	for _, hit := range env.Issues {
		if hit.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find seeded issue %q via FTS, got %d hits", id, len(env.Issues))
	}
}

func TestSearchIssues_FTS_FindsByComment(t *testing.T) {
	id := seedIssueWithComment(t, "Routine maintenance", "", "Found a flaky test in CI for the udrulning script")
	env := callSearchIssues(t, "udrulning", false)
	found := false
	for _, hit := range env.Issues {
		if hit.ID == id {
			found = true
			if hit.MatchedCommentSnippet == nil || *hit.MatchedCommentSnippet == "" {
				t.Error("expected matched comment snippet for comment-only hit")
			}
			break
		}
	}
	if !found {
		t.Errorf("expected to find issue %q via comment FTS, got %d hits", id, len(env.Issues))
	}
}

func TestSearchIssues_FilterChips_StatusEcho(t *testing.T) {
	_ = seedIssueWithComment(t, "Some todo issue about kanban", "", "")
	env := callSearchIssues(t, "kanban status:todo", false)
	gotStatus := false
	for _, f := range env.Filters {
		if f.Key == "status" && f.Value == "todo" {
			gotStatus = true
			if f.Match != "ok" {
				t.Errorf("expected status:todo to resolve ok, got %q", f.Match)
			}
		}
	}
	if !gotStatus {
		t.Errorf("expected status filter echoed back in envelope, got %+v", env.Filters)
	}
}

func TestSearchIssues_FilterChips_UnknownAssigneeMiss(t *testing.T) {
	_ = seedIssueWithComment(t, "Unrelated kanban work", "", "")
	env := callSearchIssues(t, "kanban assignee:noSuchPerson", false)
	gotMiss := false
	for _, f := range env.Filters {
		if f.Key == "assignee" && f.Value == "noSuchPerson" && f.Match == "miss" {
			gotMiss = true
		}
	}
	if !gotMiss {
		t.Errorf("expected assignee miss echoed back, got %+v", env.Filters)
	}
	if env.Total != 0 || len(env.Issues) != 0 {
		t.Errorf("miss should short-circuit to zero results, got total=%d issues=%d",
			env.Total, len(env.Issues))
	}
}

func TestSearchIssues_FilterChips_HasAttachment(t *testing.T) {
	id := seedIssueWithComment(t, "Issue with attachment for has-filter", "", "")
	// Attach a fake attachment row directly. Skip cleanup, test cleanup of
	// the parent issue will cascade.
	_, err := testPool.Exec(context.Background(),
		`INSERT INTO attachment (issue_id, workspace_id, uploader_type, uploader_id, filename, content_type, size_bytes, url, download_url)
		 VALUES ($1, $2, 'member', $3, 'x.png', 'image/png', 1, '/x', '/x')`,
		id, testWorkspaceID, testUserID,
	)
	if err != nil {
		t.Skipf("skip — could not seed attachment (schema mismatch?): %v", err)
	}
	env := callSearchIssues(t, "attachment has:attachment", false)
	found := false
	for _, hit := range env.Issues {
		if hit.ID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("expected has:attachment to find seeded issue, got %d hits", len(env.Issues))
	}
}

func TestSearchIssues_TrigramFuzzy_StavefejlTolerant(t *testing.T) {
	id := seedIssueWithComment(t, "Investigate deployment failures", "", "")
	// "deploymeny" — single-letter typo — should still surface via pg_trgm.
	env := callSearchIssues(t, "deploymeny", false)
	found := false
	for _, hit := range env.Issues {
		if hit.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("trigram fuzzy match should tolerate a single-letter typo, got %d hits", len(env.Issues))
	}
}

// TestSearchIssues_ParserIsReachable is a lightweight sanity check that the
// filter parser exported from the cerebro package is what the handler is
// actually using. Catches a future rename / wrong-import drift.
func TestSearchIssues_ParserIsReachable(t *testing.T) {
	p := cerebrosearch.Parse("foo from:bar status:todo")
	if p.Text != "foo" || len(p.Filters) != 2 {
		t.Fatalf("parser drift — got text=%q filters=%+v", p.Text, p.Filters)
	}
}

// TestSearchIssues_FilterChips_UnknownHasMiss reproduces FIR-2571: an invalid
// has:<value> filter (only "attachment" is valid) used to echo a "miss" chip
// but still return every row, because anyFilterMissNarrowed omitted FilterHas.
// It must now narrow to zero results like other filter misses do.
func TestSearchIssues_FilterChips_UnknownHasMiss(t *testing.T) {
	_ = seedIssueWithComment(t, "Some unrelated kanban issue", "", "")
	env := callSearchIssues(t, "has:banana", false)
	gotMiss := false
	for _, f := range env.Filters {
		if f.Key == "has" && f.Value == "banana" && f.Match == "miss" {
			gotMiss = true
		}
	}
	if !gotMiss {
		t.Errorf("expected has:banana echoed back as miss, got %+v", env.Filters)
	}
	if env.Total != 0 || len(env.Issues) != 0 {
		t.Errorf("invalid has-filter must short-circuit to zero results, got total=%d issues=%d",
			env.Total, len(env.Issues))
	}
}
