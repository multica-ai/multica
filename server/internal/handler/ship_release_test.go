// Phase 7a — Release handler tests. These exercise the create / get /
// add-pr / remove-pr / cancel paths against the real Postgres test
// pool. Each test calls enableShipHub() to flip the workspace flag
// AND calls a local cleanup that drops every release row the test
// inserted, so tests can run in any order.

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// shipReleaseMigrationApplied probes for the 085 migration. Mirrors
// shipHubMigrationApplied from ship_test.go so a CI checkout running
// pre-085 just skips the new tests instead of hard-failing.
func shipReleaseMigrationApplied(t *testing.T) bool {
	t.Helper()
	var exists bool
	err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'ship_release'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("probe ship_release migration: %v", err)
	}
	return exists
}

// enableShipReleaseTest enables Ship Hub on the test workspace and
// registers a cleanup that wipes every release-side row. Called at
// the top of every release test.
func enableShipReleaseTest(t *testing.T) {
	t.Helper()
	if !shipReleaseMigrationApplied(t) {
		t.Skip("ship_release migration not yet applied; skipping")
	}
	// channels also need to be enabled so the auto-create channel
	// step in release creation succeeds. Without the flag the
	// channel service refuses; the release still creates but
	// without a channel, which is acceptable for some tests but
	// noisy for others — flipping the flag here keeps the happy
	// path clean.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace SET channels_enabled = TRUE WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("enable channels: %v", err)
	}
	enableShipHub(t, false)
	t.Cleanup(func() {
		ctx := context.Background()
		// Drop in dependency order so FK casts don't cascade
		// surprises.
		testPool.Exec(ctx, `DELETE FROM ship_release_event WHERE release_id IN (SELECT id FROM ship_release WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM ship_release_pull_request WHERE release_id IN (SELECT id FROM ship_release WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM ship_release WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM channel_membership WHERE channel_id IN (SELECT id FROM channel WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `UPDATE workspace SET channels_enabled = FALSE WHERE id = $1`, testWorkspaceID)
	})
}

// seedReleasePR inserts an open PR that's eligible for release
// inclusion (CI passing, approved, mergeable, non-draft). Returns
// the PR id as a UUID string.
func seedReleasePR(t *testing.T, projectID, repoURL string, number int) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			is_draft, author_login, base_ref, head_ref, head_sha, html_url,
			ci_status, review_decision, mergeable,
			pr_created_at, pr_updated_at, risk_level
		) VALUES (
			$1, $2, $3, $4, $5, 'open',
			FALSE, 'alice', 'main', 'feat/x', 'sha-' || $4, 'https://example.com/' || $4,
			'success', 'APPROVED', 'MERGEABLE',
			NOW(), NOW() + ($4 || ' seconds')::interval, 'medium'
		)
		RETURNING id
	`, testWorkspaceID, projectID, repoURL, number, "Release PR "+repoURL).Scan(&id); err != nil {
		t.Fatalf("seed release PR %d: %v", number, err)
	}
	return id
}

// TestRelease_Create_HappyPath — POST /api/projects/{id}/releases with
// two eligible PRs creates a release row + auto-creates the channel +
// auto-creates the tracking issue. The response carries all three.
func TestRelease_Create_HappyPath(t *testing.T) {
	enableShipReleaseTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica-rel")
	pr1 := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-rel", 101)
	pr2 := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-rel", 102)

	body, _ := json.Marshal(map[string]any{
		"title":            "Memory KB rollout",
		"description":      "Ships memory artifacts to web + desktop",
		"pull_request_ids": []string{pr1, pr2},
	})
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/releases", body)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateRelease(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateRelease: want 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Release struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Stage   string `json:"stage"`
			PRCount int    `json:"pr_count"`
		} `json:"release"`
		Channel struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"channel"`
		Issue struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"issue"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Release.ID == "" || resp.Release.Stage != "assembling" {
		t.Fatalf("expected assembling release, got %+v", resp.Release)
	}
	if resp.Release.PRCount != 2 {
		t.Fatalf("expected pr_count=2, got %d", resp.Release.PRCount)
	}
	// Channel auto-create was removed — releases ship without a
	// discussion channel by default. The slot is empty until the
	// user explicitly clicks "Open discussion channel" (covered by
	// TestRelease_OpenChannel below).
	if resp.Channel.ID != "" {
		t.Fatalf("expected empty channel slot (auto-create removed), got %+v", resp.Channel)
	}
	if resp.Issue.Title != "Memory KB rollout" {
		t.Fatalf("expected issue title to mirror release title, got %q", resp.Issue.Title)
	}
}

// TestRelease_OpenChannel covers the manual "Open discussion channel"
// flow that replaces the removed auto-create-on-CreateRelease path.
// First call creates + links the channel; second call is idempotent.
func TestRelease_OpenChannel(t *testing.T) {
	enableShipReleaseTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica-rel-chan")
	pr := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-rel-chan", 601)

	// Create the release (channel slot starts empty).
	body, _ := json.Marshal(map[string]any{
		"title":            "Channel-open test release",
		"pull_request_ids": []string{pr},
	})
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/releases", body)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateRelease(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateRelease: %d %s", w.Code, w.Body.String())
	}
	var createResp struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
		Channel *struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	json.NewDecoder(w.Body).Decode(&createResp)
	if createResp.Channel != nil && createResp.Channel.ID != "" {
		t.Fatalf("expected nil channel slot before manual open, got %+v", createResp.Channel)
	}

	// First open — creates + links a channel.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/releases/"+createResp.Release.ID+"/channel", nil)
	req = withURLParam(req, "id", createResp.Release.ID)
	testHandler.OpenReleaseChannel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("OpenReleaseChannel: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var openResp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	json.NewDecoder(w.Body).Decode(&openResp)
	if openResp.ID == "" || !strings.HasPrefix(openResp.Name, "release-") {
		t.Fatalf("expected new channel with release- prefix, got %+v", openResp)
	}

	// Second open — idempotent, returns the same channel.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/releases/"+createResp.Release.ID+"/channel", nil)
	req = withURLParam(req, "id", createResp.Release.ID)
	testHandler.OpenReleaseChannel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second OpenReleaseChannel: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var openResp2 struct {
		ID string `json:"id"`
	}
	json.NewDecoder(w.Body).Decode(&openResp2)
	if openResp2.ID != openResp.ID {
		t.Fatalf("expected idempotent return of same channel; got %s vs %s", openResp2.ID, openResp.ID)
	}
}

// TestRelease_Create_PRAlreadyInActiveRelease — a PR already in an
// active release cannot be added to a second one. The handler must
// return 409 Conflict, not a 500.
func TestRelease_Create_PRAlreadyInActiveRelease(t *testing.T) {
	enableShipReleaseTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica-conflict")
	pr := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-conflict", 201)

	// First release — succeeds.
	body, _ := json.Marshal(map[string]any{
		"title":            "First release",
		"pull_request_ids": []string{pr},
	})
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/releases", body)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateRelease(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first release: want 201, got %d: %s", w.Code, w.Body.String())
	}

	// Second release reusing the same PR — must 409.
	body2, _ := json.Marshal(map[string]any{
		"title":            "Second release",
		"pull_request_ids": []string{pr},
	})
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+projectID+"/releases", body2)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateRelease(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("second release: want 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRelease_Create_IneligiblePR — a draft PR cannot be added; the
// service rejects with 400 and an explanatory body.
func TestRelease_Create_IneligiblePR(t *testing.T) {
	enableShipReleaseTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica-ineligible")
	// Draft PR — manually constructed (seedReleasePR makes
	// non-draft only).
	var prID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			is_draft, author_login, base_ref, head_ref, head_sha, html_url,
			ci_status, mergeable, pr_created_at, pr_updated_at, risk_level
		) VALUES (
			$1, $2, 'https://github.com/multica-ai/multica-ineligible', 301, 'Draft PR', 'open',
			TRUE, 'alice', 'main', 'feat/x', 'sha', 'https://example.com/301',
			'success', 'MERGEABLE', NOW(), NOW(), 'medium'
		)
		RETURNING id
	`, testWorkspaceID, projectID).Scan(&prID); err != nil {
		t.Fatalf("seed draft PR: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"title":            "Includes a draft",
		"pull_request_ids": []string{prID},
	})
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/releases", body)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateRelease(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("draft PR: want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRelease_GetRelease_ReturnsPRsAndEvents — after creating a
// release, the GET endpoint hydrates the PR list, the channel link,
// the issue link, and the event timeline.
func TestRelease_GetRelease_ReturnsPRsAndEvents(t *testing.T) {
	enableShipReleaseTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica-detail")
	pr1 := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-detail", 401)

	// Create.
	body, _ := json.Marshal(map[string]any{
		"title":            "Detail page release",
		"pull_request_ids": []string{pr1},
	})
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/releases", body)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateRelease(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var createResp struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	json.NewDecoder(w.Body).Decode(&createResp)

	// Get.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/releases/"+createResp.Release.ID, nil)
	req = withURLParam(req, "id", createResp.Release.ID)
	testHandler.GetRelease(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	var getResp struct {
		Release      map[string]any   `json:"release"`
		PullRequests []map[string]any `json:"pull_requests"`
		Events       []map[string]any `json:"events"`
		Channel      map[string]any   `json:"channel"`
		Issue        map[string]any   `json:"issue"`
	}
	if err := json.NewDecoder(w.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if len(getResp.PullRequests) != 1 {
		t.Fatalf("expected 1 PR in detail, got %d", len(getResp.PullRequests))
	}
	// We expect at least 2 events: created + issue_created. The
	// channel auto-create on CreateRelease was removed in favor of
	// an explicit "Open discussion channel" affordance — most
	// releases ship without a chat channel. The third event
	// (`channel_created`) only appears after the user clicks the
	// manual button, which has its own coverage in
	// TestRelease_OpenChannel below.
	if len(getResp.Events) < 2 {
		t.Fatalf("expected at least 2 events (created + issue_created), got %d", len(getResp.Events))
	}
	// Channel slot is null by default — only populated after manual
	// open. TestRelease_OpenChannel covers the populated path.
	if getResp.Channel != nil {
		t.Fatalf("expected nil channel reference (channel auto-create removed), got %+v", getResp.Channel)
	}
	if getResp.Issue == nil || getResp.Issue["id"] == nil {
		t.Fatalf("expected issue reference in detail, got %+v", getResp.Issue)
	}
}

// TestRelease_AddRemovePR — once created, the assembling release
// accepts add/remove PR operations.
func TestRelease_AddRemovePR(t *testing.T) {
	enableShipReleaseTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica-mut")
	pr1 := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-mut", 501)
	pr2 := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-mut", 502)

	// Create with one PR.
	body, _ := json.Marshal(map[string]any{
		"title":            "Mutating release",
		"pull_request_ids": []string{pr1},
	})
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/releases", body)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateRelease(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var createResp struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	json.NewDecoder(w.Body).Decode(&createResp)
	releaseID := createResp.Release.ID

	// Add pr2.
	addBody, _ := json.Marshal(map[string]any{"pull_request_id": pr2})
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/releases/"+releaseID+"/pull_requests", addBody)
	req = withURLParam(req, "id", releaseID)
	testHandler.AddPullRequestToRelease(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("add PR: %d %s", w.Code, w.Body.String())
	}

	// Remove pr1.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/releases/"+releaseID+"/pull_requests/"+pr1, nil)
	req = withURLParam(req, "id", releaseID)
	req = withURLParam(req, "pr_id", pr1)
	testHandler.RemovePullRequestFromRelease(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("remove PR: %d %s", w.Code, w.Body.String())
	}

	// Verify final PR set is just pr2.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/releases/"+releaseID, nil)
	req = withURLParam(req, "id", releaseID)
	testHandler.GetRelease(w, req)
	var getResp struct {
		PullRequests []map[string]any `json:"pull_requests"`
	}
	json.NewDecoder(w.Body).Decode(&getResp)
	if len(getResp.PullRequests) != 1 {
		t.Fatalf("expected 1 PR after add+remove, got %d", len(getResp.PullRequests))
	}
}

// TestRelease_Cancel — cancellation flips the stage to 'cancelled',
// frees the PRs (is_active=false on join rows), and 409s subsequent
// add attempts.
func TestRelease_Cancel(t *testing.T) {
	enableShipReleaseTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica-cancel")
	pr := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-cancel", 601)

	body, _ := json.Marshal(map[string]any{
		"title":            "Cancel me",
		"pull_request_ids": []string{pr},
	})
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/releases", body)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateRelease(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var createResp struct {
		Release struct {
			ID string `json:"id"`
		} `json:"release"`
	}
	json.NewDecoder(w.Body).Decode(&createResp)
	releaseID := createResp.Release.ID

	// Cancel.
	cancelBody, _ := json.Marshal(map[string]any{"reason": "no longer needed"})
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/releases/"+releaseID+"/cancel", cancelBody)
	req = withURLParam(req, "id", releaseID)
	testHandler.CancelRelease(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	var cancelResp struct {
		Stage          string `json:"stage"`
		RollbackReason string `json:"rollback_reason"`
	}
	json.NewDecoder(w.Body).Decode(&cancelResp)
	if cancelResp.Stage != "cancelled" {
		t.Fatalf("expected cancelled stage, got %q", cancelResp.Stage)
	}

	// PR should now be free for a new release.
	body2, _ := json.Marshal(map[string]any{
		"title":            "After cancel",
		"pull_request_ids": []string{pr},
	})
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+projectID+"/releases", body2)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateRelease(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("recreate after cancel: %d %s", w.Code, w.Body.String())
	}
}

// TestRelease_ListWorkspaceActive — the workspace-wide rail returns
// only active releases; a cancelled release falls off the list.
func TestRelease_ListWorkspaceActive(t *testing.T) {
	enableShipReleaseTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica-rail")
	pr1 := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-rail", 701)
	pr2 := seedReleasePR(t, projectID, "https://github.com/multica-ai/multica-rail", 702)

	createRelease := func(title string, prs []string) string {
		body, _ := json.Marshal(map[string]any{
			"title":            title,
			"pull_request_ids": prs,
		})
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects/"+projectID+"/releases", body)
		req = withURLParam(req, "id", projectID)
		testHandler.CreateRelease(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", title, w.Code, w.Body.String())
		}
		var resp struct {
			Release struct {
				ID string `json:"id"`
			} `json:"release"`
		}
		json.NewDecoder(w.Body).Decode(&resp)
		return resp.Release.ID
	}

	rel1 := createRelease("Active 1", []string{pr1})
	rel2 := createRelease("Active 2", []string{pr2})

	// Cancel rel1.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/releases/"+rel1+"/cancel", []byte("{}"))
	req = withURLParam(req, "id", rel1)
	testHandler.CancelRelease(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel rel1: %d", w.Code)
	}

	// Workspace-active list should now contain only rel2.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/workspaces/"+testWorkspaceID+"/releases/active", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.ListWorkspaceActiveReleases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list active: %d %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Releases []map[string]any `json:"releases"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)
	if len(listResp.Releases) != 1 {
		t.Fatalf("expected 1 active, got %d (%+v)", len(listResp.Releases), listResp.Releases)
	}
	if id, _ := listResp.Releases[0]["id"].(string); id != rel2 {
		t.Fatalf("expected rel2 in list, got %v", listResp.Releases[0])
	}
}
