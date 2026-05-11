package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mustSeedPRWithLinkage inserts a PR row and lets the caller specify
// linkage columns. Returns the PR's UUID. The Phase 4 columns are NOT
// NULL with defaults so an empty-options call produces a row identical
// to mustSeedPR's output.
func mustSeedPRWithLinkage(t *testing.T, projectID, repoURL string, number int, state string, originatingIssueID, originatingTaskID string, autoClose bool, source string) string {
	t.Helper()
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub not migrated")
	}
	url := fmt.Sprintf("https://example.com/%d", number)
	var prID string
	q := `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			author_login, base_ref, head_ref, head_sha, html_url,
			pr_created_at, pr_updated_at,
			originating_issue_id, originating_agent_task_id,
			auto_close_issue_on_merge, source
		) VALUES (
			$1, $2, $3, $4, $5, $6::pull_request_state,
			'alice', 'main', 'feat/x', 'sha', $11,
			now(), now(),
			NULLIF($7, '')::uuid, NULLIF($8, '')::uuid,
			$9, $10
		)
		RETURNING id
	`
	if err := testPool.QueryRow(context.Background(), q,
		testWorkspaceID, projectID, repoURL, number, "PR "+state, state,
		originatingIssueID, originatingTaskID, autoClose, source, url,
	).Scan(&prID); err != nil {
		t.Fatalf("seed PR %d: %v", number, err)
	}
	return prID
}

// TestShipPhase4_UpdatePullRequest covers the manual-override PATCH
// endpoint. Auto-detected linkage can be wrong (e.g. the user
// referenced two issues, the regex picked the wrong one); the user
// must be able to fix it without re-syncing.
func TestShipPhase4_UpdatePullRequest(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")

	// Create an issue we can link to.
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, description, status, priority,
			creator_type, creator_id, position, number)
		VALUES ($1, 'fix the thing', 'desc', 'todo', 'medium', 'member', $2, 1, 999)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	prID := mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 42, "open", "", "", false, "external_contributor")

	body := map[string]any{"originating_issue_id": issueID, "auto_close_issue_on_merge": true}
	req := newRequest("PATCH", "/api/pull_requests/"+prID, body)
	req = withURLParam(req, "id", prID)
	w := httptest.NewRecorder()
	testHandler.UpdatePullRequest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdatePullRequest: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := resp["originating_issue_id"]; got != issueID {
		t.Errorf("originating_issue_id = %v, want %s", got, issueID)
	}
	if resp["auto_close_issue_on_merge"] != true {
		t.Errorf("auto_close_issue_on_merge = %v, want true", resp["auto_close_issue_on_merge"])
	}
}

// TestShipPhase4_UpdatePullRequest_RejectsCrossWorkspaceIssue verifies
// the per-workspace scope check on originating_issue_id. Without it a
// user could link a PR to an issue from another workspace they have no
// access to.
func TestShipPhase4_UpdatePullRequest_RejectsCrossWorkspaceIssue(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")
	prID := mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 43, "open", "", "", false, "external_contributor")

	// Random UUID that isn't an issue in this workspace.
	body := map[string]any{"originating_issue_id": "00000000-0000-0000-0000-000000000099"}
	req := newRequest("PATCH", "/api/pull_requests/"+prID, body)
	req = withURLParam(req, "id", prID)
	w := httptest.NewRecorder()
	testHandler.UpdatePullRequest(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdatePullRequest cross-workspace: want 400, got %d %s", w.Code, w.Body.String())
	}
}

// TestShipPhase4_GetLinkedIssues_NullCase returns empty fields when no
// linkage is set rather than 404. The frontend renders "no linked issue"
// gracefully when the response is `{"issue":null,"agent_task":null}`.
func TestShipPhase4_GetLinkedIssues_NullCase(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")
	prID := mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 44, "open", "", "", false, "external_contributor")

	req := newRequest("GET", "/api/pull_requests/"+prID+"/linked_issues", nil)
	req = withURLParam(req, "id", prID)
	w := httptest.NewRecorder()
	testHandler.GetLinkedIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetLinkedIssues: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"issue":null`) {
		t.Errorf("expected issue:null in response, got: %s", body)
	}
	if !strings.Contains(body, `"agent_task":null`) {
		t.Errorf("expected agent_task:null in response, got: %s", body)
	}
}

// TestShipPhase4_ListIssuePullRequests returns PRs whose
// originating_issue_id matches.
func TestShipPhase4_ListIssuePullRequests(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, description, status, priority,
			creator_type, creator_id, position, number)
		VALUES ($1, 'lk', '', 'todo', 'medium', 'member', $2, 1, 998)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 45, "open", issueID, "", false, "multica_human")
	mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 46, "open", issueID, "", false, "multica_human")
	// PR not linked to this issue — must NOT appear.
	mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 47, "open", "", "", false, "external_contributor")

	req := newRequest("GET", "/api/issues/"+issueID+"/pull_requests", nil)
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.ListIssuePullRequests(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssuePullRequests: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		PullRequests []map[string]any `json:"pull_requests"`
		Total        int              `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("total = %d, want 2", resp.Total)
	}
}

// TestShipPhase4_TalkToAgent_RequiresAgentTask rejects PRs without an
// originating_agent_task_id. The chip in the UI hides itself for those,
// but a hand-rolled API call must still 400.
func TestShipPhase4_TalkToAgent_RequiresAgentTask(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")
	prID := mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 48, "open", "", "", false, "external_contributor")

	req := newRequest("POST", "/api/pull_requests/"+prID+"/talk_to_agent", map[string]any{})
	req = withURLParam(req, "id", prID)
	w := httptest.NewRecorder()
	testHandler.TalkToAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("TalkToAgent without agent task: want 400, got %d %s", w.Code, w.Body.String())
	}
}

// TestShipPhase4_PullRequestStacks_Empty verifies the empty-state
// shape: stacks=[] when no PRs exist for the project.
func TestShipPhase4_PullRequestStacks_Empty(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")

	req := newRequest("GET", "/api/projects/"+projectID+"/pull_request_stacks", nil)
	req = withURLParam(req, "id", projectID)
	w := httptest.NewRecorder()
	testHandler.ListProjectPRStacks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectPRStacks: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"stacks":[]`) {
		t.Errorf("expected stacks:[], got %s", body)
	}
}

// TestShipPhase4_PullRequestStacks_TwoLevel covers a 2-PR stack: PR-B
// rebased onto PR-A's branch. Recompute happens via direct SQL since
// the webhook driver isn't in this test path.
func TestShipPhase4_PullRequestStacks_TwoLevel(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")

	// Insert root PR with head_ref="feat/a"; base_ref="main".
	var rootID, childID string
	q := `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			author_login, base_ref, head_ref, head_sha, html_url,
			pr_created_at, pr_updated_at, source
		) VALUES (
			$1, $2, 'https://github.com/foo/bar', $3, 'PR open', 'open',
			'alice', 'main', $4, 'sha', $5,
			now(), now(), 'multica_human'
		) RETURNING id
	`
	if err := testPool.QueryRow(context.Background(), q,
		testWorkspaceID, projectID, 50, "feat/a", "https://example.com/50").Scan(&rootID); err != nil {
		t.Fatalf("seed root: %v", err)
	}
	// Child PR: base=feat/a, head=feat/b. stack_parent_pr_id points at root.
	q2 := `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			author_login, base_ref, head_ref, head_sha, html_url,
			pr_created_at, pr_updated_at, source, stack_parent_pr_id
		) VALUES (
			$1, $2, 'https://github.com/foo/bar', $3, 'PR open child', 'open',
			'alice', $4, $5, 'sha', $7,
			now(), now(), 'multica_human', $6
		) RETURNING id
	`
	if err := testPool.QueryRow(context.Background(), q2,
		testWorkspaceID, projectID, 51, "feat/a", "feat/b", rootID, "https://example.com/51").Scan(&childID); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	req := newRequest("GET", "/api/projects/"+projectID+"/pull_request_stacks", nil)
	req = withURLParam(req, "id", projectID)
	w := httptest.NewRecorder()
	testHandler.ListProjectPRStacks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectPRStacks: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Stacks []struct {
			PR       map[string]any `json:"pr"`
			Children []struct {
				PR map[string]any `json:"pr"`
			} `json:"children"`
		} `json:"stacks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Stacks) != 1 {
		t.Fatalf("stacks length = %d, want 1; body=%s", len(resp.Stacks), w.Body.String())
	}
	if len(resp.Stacks[0].Children) != 1 {
		t.Fatalf("root has %d children, want 1", len(resp.Stacks[0].Children))
	}
	if got := resp.Stacks[0].PR["id"]; got != rootID {
		t.Errorf("root id = %v, want %s", got, rootID)
	}
}

// TestShipPhase4_GetOrCreatePRConversationChannel — first call creates,
// second call returns the same channel.
func TestShipPhase4_GetOrCreatePRConversationChannel(t *testing.T) {
	enableShipHub(t, false)
	// Channels feature must also be enabled for the channel service.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace SET channels_enabled = TRUE WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("enable channels: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`UPDATE workspace SET channels_enabled = FALSE WHERE id = $1`, testWorkspaceID)
	})

	projectID := createShipProject(t, "https://github.com/foo/bar")
	prID := mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 60, "open", "", "", false, "external_contributor")

	// First call creates.
	req := newRequest("POST", "/api/pull_requests/"+prID+"/conversation_channel", nil)
	req = withURLParam(req, "id", prID)
	w := httptest.NewRecorder()
	testHandler.GetOrCreatePRConversationChannel(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create conversation channel: %d %s", w.Code, w.Body.String())
	}
	var first map[string]any
	if err := json.NewDecoder(w.Body).Decode(&first); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	firstID, _ := first["id"].(string)
	if firstID == "" {
		t.Fatalf("expected channel id in response, got %v", first)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM channel WHERE id = $1`, firstID)
	})

	// Second call returns the same channel (idempotent).
	req2 := newRequest("POST", "/api/pull_requests/"+prID+"/conversation_channel", nil)
	req2 = withURLParam(req2, "id", prID)
	w2 := httptest.NewRecorder()
	testHandler.GetOrCreatePRConversationChannel(w2, req2)
	if w2.Code != http.StatusCreated && w2.Code != http.StatusOK {
		t.Fatalf("idempotent get: %d %s", w2.Code, w2.Body.String())
	}
	var second map[string]any
	if err := json.NewDecoder(w2.Body).Decode(&second); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if second["id"] != firstID {
		t.Errorf("expected same channel id; first=%s second=%v", firstID, second["id"])
	}
}

// TestShipPhase4_RepoSlugFromURL covers the URL → slug normalization.
// Tests live in the same package as the helper to assert without
// exporting it.
func TestShipPhase4_RepoSlugFromURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/foo/bar":     "foo-bar",
		"https://github.com/foo/bar/":    "foo-bar",
		"https://github.com/foo/bar.git": "foo-bar",
		"git@github.com:foo/bar.git":     "foo-bar",
		"https://github.com/Foo/Bar":     "foo-bar",
	}
	for in, want := range cases {
		got := repoSlugFromURL(in)
		if got != want {
			t.Errorf("repoSlugFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestShipPhase4_TruncateForDisplay sanity-checks the helper used to crop
// a PR title into a channel display name.
func TestShipPhase4_TruncateForDisplay(t *testing.T) {
	if got := truncateForDisplay("hello", 10); got != "hello" {
		t.Errorf("short string: got %q", got)
	}
	if got := truncateForDisplay("hello world", 5); got != "hello" {
		t.Errorf("truncate: got %q", got)
	}
	if got := truncateForDisplay("", 5); got != "" {
		t.Errorf("empty: got %q", got)
	}
}
