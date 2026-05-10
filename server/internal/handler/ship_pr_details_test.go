package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestShipPRDetails_HappyPath covers a PR with linkage, reviews, checks,
// and a recent action all populated. Verifies the bundled response
// renders every section AND keeps newest-first ordering on the
// arrays.
func TestShipPRDetails_HappyPath(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")

	// Linkable issue.
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, description, status, priority,
			creator_type, creator_id, position, number)
		VALUES ($1, 'fix the thing', 'desc', 'todo', 'medium', 'member', $2, 1, 700)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	prID := mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 200, "open", issueID, "", true, "multica_human")

	// Two reviews — older first then newer. The endpoint must order
	// newest-first.
	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO pull_request_review (workspace_id, pull_request_id, reviewer_login, reviewer_avatar_url, state, body, submitted_at)
		VALUES ($1, $2, 'alice', '', 'COMMENTED', 'looks ok', $3),
		       ($1, $2, 'bob',   '', 'APPROVED',   'lgtm',     $4)
	`, testWorkspaceID, prID, t1, t2); err != nil {
		t.Fatalf("seed reviews: %v", err)
	}

	// Two checks — different started_at so we can check ordering.
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO pull_request_check (workspace_id, pull_request_id, head_sha, name, conclusion, status, details_url, started_at, completed_at)
		VALUES ($1, $2, 'sha', 'unit', 'success', 'completed', 'https://x/u', $3, $4),
		       ($1, $2, 'sha', 'lint', 'failure', 'completed', 'https://x/l', $5, $6)
	`, testWorkspaceID, prID, t1, t1, t2, t2); err != nil {
		t.Fatalf("seed checks: %v", err)
	}

	// One ship_card_action.
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO ship_card_action (workspace_id, pull_request_id, action, result_status, completed_at)
		VALUES ($1, $2, 'comment', 'succeeded', now())
	`, testWorkspaceID, prID); err != nil {
		t.Fatalf("seed ship_card_action: %v", err)
	}

	req := newRequest("GET", "/api/pull_requests/"+prID+"/details", nil)
	req = withURLParam(req, "id", prID)
	w := httptest.NewRecorder()
	testHandler.GetPullRequestDetails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetPullRequestDetails: %d %s", w.Code, w.Body.String())
	}

	var resp struct {
		PullRequest         map[string]any `json:"pull_request"`
		LinkedIssue         map[string]any `json:"linked_issue"`
		OriginatingAgentTask map[string]any `json:"originating_agent_task"`
		ConversationChannel map[string]any `json:"conversation_channel"`
		Reviews             []map[string]any `json:"reviews"`
		Checks              []map[string]any `json:"checks"`
		RecentActions       []map[string]any `json:"recent_actions"`
		StackChildren       []map[string]any `json:"stack_children"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := resp.PullRequest["id"]; got != prID {
		t.Errorf("pull_request.id = %v, want %s", got, prID)
	}
	if resp.LinkedIssue == nil {
		t.Errorf("expected linked_issue to be populated")
	} else if resp.LinkedIssue["id"] != issueID {
		t.Errorf("linked_issue.id = %v, want %s", resp.LinkedIssue["id"], issueID)
	}
	// Reviews: newest first.
	if len(resp.Reviews) != 2 {
		t.Fatalf("reviews count = %d, want 2", len(resp.Reviews))
	}
	if resp.Reviews[0]["reviewer_login"] != "bob" {
		t.Errorf("first review reviewer = %v, want bob (newest first)", resp.Reviews[0]["reviewer_login"])
	}
	// Checks: newest first by started_at.
	if len(resp.Checks) != 2 {
		t.Fatalf("checks count = %d, want 2", len(resp.Checks))
	}
	if resp.Checks[0]["name"] != "lint" {
		t.Errorf("first check name = %v, want lint (newest started_at first)", resp.Checks[0]["name"])
	}
	if len(resp.RecentActions) != 1 {
		t.Errorf("recent_actions count = %d, want 1", len(resp.RecentActions))
	}
	// Stack section is empty (no parent, no children seeded).
	if len(resp.StackChildren) != 0 {
		t.Errorf("stack_children count = %d, want 0", len(resp.StackChildren))
	}
}

// TestShipPRDetails_NoOptionalSections verifies a "bare" PR (no linkage,
// no reviews, no checks, no actions) still returns 200 with empty
// arrays. The drawer must render even when nothing is enriched yet.
func TestShipPRDetails_NoOptionalSections(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")
	prID := mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 201, "open", "", "", false, "external_contributor")

	req := newRequest("GET", "/api/pull_requests/"+prID+"/details", nil)
	req = withURLParam(req, "id", prID)
	w := httptest.NewRecorder()
	testHandler.GetPullRequestDetails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}

	var resp struct {
		LinkedIssue          *map[string]any   `json:"linked_issue"`
		OriginatingAgentTask *map[string]any   `json:"originating_agent_task"`
		ConversationChannel  *map[string]any   `json:"conversation_channel"`
		Reviews              []map[string]any  `json:"reviews"`
		Checks               []map[string]any  `json:"checks"`
		RecentActions        []map[string]any  `json:"recent_actions"`
		StackChildren        []map[string]any  `json:"stack_children"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.LinkedIssue != nil {
		t.Errorf("linked_issue should be nil/omitted, got %v", resp.LinkedIssue)
	}
	if resp.OriginatingAgentTask != nil {
		t.Errorf("originating_agent_task should be nil/omitted")
	}
	// Arrays must be empty (not nil) so the frontend doesn't have to
	// branch on `null` vs `[]`.
	if resp.Reviews == nil {
		t.Errorf("reviews should be []")
	}
	if resp.Checks == nil {
		t.Errorf("checks should be []")
	}
	if resp.RecentActions == nil {
		t.Errorf("recent_actions should be []")
	}
	if resp.StackChildren == nil {
		t.Errorf("stack_children should be []")
	}
}

// TestShipPRDetails_CrossWorkspace verifies a PR from another workspace
// returns 404. The endpoint reuses loadPullRequestInWorkspace's gate so
// this exercises the same path every Phase 4 endpoint already trusts;
// the test guards against accidental leakage if the gate is removed.
func TestShipPRDetails_CrossWorkspace(t *testing.T) {
	enableShipHub(t, false)

	// Create a second workspace with its own PR.
	var otherWS string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, ship_hub_enabled) VALUES ('other', 'other-ws-prdrawer', TRUE)
		RETURNING id
	`).Scan(&otherWS); err != nil {
		t.Fatalf("seed other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM pull_request WHERE workspace_id = $1`, otherWS)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE workspace_id = $1`, otherWS)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWS)
	})

	var otherProject string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'other proj')
		RETURNING id
	`, otherWS).Scan(&otherProject); err != nil {
		t.Fatalf("seed other project: %v", err)
	}

	var otherPR string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			author_login, base_ref, head_ref, head_sha, html_url,
			pr_created_at, pr_updated_at, source
		) VALUES (
			$1, $2, 'https://github.com/x/y', 1, 'foreign', 'open',
			'alice', 'main', 'feat/x', 'sha', 'https://example.com/1',
			now(), now(), 'external_contributor'
		) RETURNING id
	`, otherWS, otherProject).Scan(&otherPR); err != nil {
		t.Fatalf("seed other pr: %v", err)
	}

	// Caller is testWorkspaceID; the PR lives in otherWS, so the
	// endpoint must 404.
	req := newRequest("GET", "/api/pull_requests/"+otherPR+"/details", nil)
	req = withURLParam(req, "id", otherPR)
	w := httptest.NewRecorder()
	testHandler.GetPullRequestDetails(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace request: want 404, got %d %s", w.Code, w.Body.String())
	}
}

// TestShipPRDetails_StackChildren covers the parent + children section.
// Parent renders its identity, children render in pr_number order.
func TestShipPRDetails_StackChildren(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/foo/bar")

	// Root PR.
	rootID := mustSeedPRWithLinkage(t, projectID, "https://github.com/foo/bar", 300, "open", "", "", false, "multica_human")

	// Two children, each with stack_parent_pr_id pointing at root.
	var childA, childB string
	q := `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			author_login, base_ref, head_ref, head_sha, html_url,
			pr_created_at, pr_updated_at, source, stack_parent_pr_id
		) VALUES (
			$1, $2, 'https://github.com/foo/bar', $3, 'child', 'open',
			'alice', 'feat/root', $4, 'sha', 'https://example.com/' || $3,
			now(), now(), 'multica_human', $5
		) RETURNING id
	`
	if err := testPool.QueryRow(context.Background(), q,
		testWorkspaceID, projectID, 302, "feat/c2", rootID).Scan(&childB); err != nil {
		t.Fatalf("seed childB: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), q,
		testWorkspaceID, projectID, 301, "feat/c1", rootID).Scan(&childA); err != nil {
		t.Fatalf("seed childA: %v", err)
	}

	// Hit /details for the ROOT — children must come back ordered by pr_number.
	req := newRequest("GET", "/api/pull_requests/"+rootID+"/details", nil)
	req = withURLParam(req, "id", rootID)
	w := httptest.NewRecorder()
	testHandler.GetPullRequestDetails(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		StackChildren []map[string]any `json:"stack_children"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.StackChildren) != 2 {
		t.Fatalf("stack_children count = %d, want 2; body=%s", len(resp.StackChildren), w.Body.String())
	}
	// Ascending pr_number — childA (#301) precedes childB (#302).
	if got, _ := resp.StackChildren[0]["number"].(float64); int(got) != 301 {
		t.Errorf("first child number = %v, want 301", resp.StackChildren[0]["number"])
	}
	if got, _ := resp.StackChildren[1]["number"].(float64); int(got) != 302 {
		t.Errorf("second child number = %v, want 302", resp.StackChildren[1]["number"])
	}

	// And the child's /details surfaces the parent ref.
	req2 := newRequest("GET", "/api/pull_requests/"+childA+"/details", nil)
	req2 = withURLParam(req2, "id", childA)
	w2 := httptest.NewRecorder()
	testHandler.GetPullRequestDetails(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("child status = %d %s", w2.Code, w2.Body.String())
	}
	var childResp struct {
		StackParent *map[string]any `json:"stack_parent"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&childResp); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	if childResp.StackParent == nil {
		t.Fatalf("expected stack_parent to be set")
	}
	if (*childResp.StackParent)["id"] != rootID {
		t.Errorf("stack_parent.id = %v, want %s", (*childResp.StackParent)["id"], rootID)
	}
}
