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

// enableShipHub flips workspace.ship_hub_enabled to TRUE for the shared
// test workspace, optionally seeds a github token, and registers a
// t.Cleanup that flips it back and removes any ship-hub rows the test
// created. Tests that exercise Ship Hub must call this; otherwise every
// endpoint correctly returns 404 (the default workspace state).
func enableShipHub(t *testing.T, withToken bool) {
	t.Helper()
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not yet applied; skipping")
	}
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET ship_hub_enabled = TRUE WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("enable ship hub: %v", err)
	}
	if withToken {
		// Seed a settings JSON with a token. jsonb_set with
		// create_missing=true only creates the LAST key in path —
		// intermediate objects must already exist. The legacy
		// `{ship_hub,github_token}` call silently no-ops when
		// settings was '{}'. Use jsonb concat instead so the whole
		// ship_hub container is created in one shot.
		if _, err := testPool.Exec(ctx, `
			UPDATE workspace
			SET settings = COALESCE(settings, '{}'::jsonb) ||
				jsonb_build_object('ship_hub', jsonb_build_object('github_token', 'test-token-xyz'))
			WHERE id = $1
		`, testWorkspaceID); err != nil {
			t.Fatalf("seed github token: %v", err)
		}
	}
	t.Cleanup(func() {
		// Order matters: drop child rows before flipping the flag back so
		// any in-flight reads see a consistent state.
		testPool.Exec(context.Background(), `DELETE FROM deploy WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM deploy_environment WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `UPDATE workspace SET ship_hub_enabled = FALSE, settings = '{}'::jsonb WHERE id = $1`, testWorkspaceID)
	})
}

// shipHubMigrationApplied returns true when the 079 migration has been
// applied. We use a column-existence probe so the harness can run on a
// CI checkout that hasn't yet migrated, without hard-failing every
// Ship Hub test.
func shipHubMigrationApplied(t *testing.T) bool {
	t.Helper()
	var exists bool
	err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'workspace' AND column_name = 'ship_hub_enabled'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("probe ship_hub migration: %v", err)
	}
	return exists
}

// createShipProject inserts a project + a github_repo project_resource and
// returns the project id as a UUID string.
func createShipProject(t *testing.T, repoURL string) string {
	t.Helper()
	ctx := context.Background()
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status, priority)
		VALUES ($1, $2, 'planned', 'medium')
		RETURNING id
	`, testWorkspaceID, "Ship Project "+repoURL).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, position)
		VALUES ($1, $2, 'github_repo', $3::jsonb, 0)
	`, projectID, testWorkspaceID, `{"url":"`+repoURL+`"}`); err != nil {
		t.Fatalf("insert project_resource: %v", err)
	}
	return projectID
}

// TestShip_Disabled404 — every Ship Hub endpoint must 404 when the
// workspace flag is off.
func TestShip_Disabled404(t *testing.T) {
	if !shipHubMigrationApplied(t) {
		t.Skip("ship hub migration not yet applied; skipping")
	}
	// Note: deliberately do NOT call enableShipHub.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/ship/projects", nil)
	testHandler.ListShipProjects(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("ListShipProjects with flag off: want 404, got %d: %s", w.Code, w.Body.String())
	}

	// Also verify a project-scoped endpoint behaves the same.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/dummy/pull_requests", nil)
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
	testHandler.ListProjectPullRequests(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("ListProjectPullRequests with flag off: want 404, got %d", w.Code)
	}
}

// TestShip_ListProjects_OnlyGithubRepoProjects — the Ship Hub list filters
// out projects without a github_repo resource. A workspace with a single
// non-Ship project + a single Ship project should return only the latter.
func TestShip_ListProjects_OnlyGithubRepoProjects(t *testing.T) {
	enableShipHub(t, false)

	// Project WITHOUT a github_repo.
	var nonShipID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title, status, priority)
		VALUES ($1, 'Non-ship', 'planned', 'medium') RETURNING id
	`, testWorkspaceID).Scan(&nonShipID); err != nil {
		t.Fatalf("insert non-ship project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, nonShipID)
	})
	// Project WITH a github_repo.
	shipID := createShipProject(t, "https://github.com/multica-ai/multica")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/ship/projects", nil)
	testHandler.ListShipProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListShipProjects: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Projects []map[string]any `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d (%+v)", len(resp.Projects), resp.Projects)
	}
	if id, _ := resp.Projects[0]["id"].(string); id != shipID {
		t.Fatalf("expected project %s, got %v", shipID, resp.Projects[0])
	}
}

// TestShip_ListPullRequests_DefaultsToOpen — without ?state= the handler
// returns only PRs in state='open'. Closed and merged PRs sit in the cache
// but should not appear by default.
func TestShip_ListPullRequests_DefaultsToOpen(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")

	// Seed three PRs: open, closed, merged.
	mustSeedPR(t, projectID, "https://github.com/multica-ai/multica", 1, "open")
	mustSeedPR(t, projectID, "https://github.com/multica-ai/multica", 2, "closed")
	mustSeedPR(t, projectID, "https://github.com/multica-ai/multica", 3, "merged")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+projectID+"/pull_requests", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ListProjectPullRequests(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectPullRequests: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		PullRequests []pullRequestResponse `json:"pull_requests"`
		Total        int                   `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 || resp.PullRequests[0].State != "open" {
		t.Fatalf("expected 1 open PR, got %d (%+v)", resp.Total, resp.PullRequests)
	}

	// ?state=all returns every state.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+projectID+"/pull_requests?state=all", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ListProjectPullRequests(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("?state=all: %d", w.Code)
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode all: %v", err)
	}
	if resp.Total != 3 {
		t.Fatalf("?state=all: expected 3 PRs, got %d", resp.Total)
	}
}

// TestShip_ListPullRequests_BadProjectID — feeding a malformed project id
// must produce a 400 (per parseUUIDOrBadRequest), not a 500.
func TestShip_ListPullRequests_BadProjectID(t *testing.T) {
	enableShipHub(t, false)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/garbage/pull_requests", nil)
	req = withURLParam(req, "id", "garbage")
	testHandler.ListProjectPullRequests(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// TestShip_SyncRequiresToken — sync must 400 when the workspace has no
// GitHub token configured, since unauthenticated reconcile would blow the
// rate limit immediately.
func TestShip_SyncRequiresToken(t *testing.T) {
	enableShipHub(t, false) // false = no token seeded
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/pull_requests/sync", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.SyncProjectPullRequests(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (no token), got %d: %s", w.Code, w.Body.String())
	}
}

// TestShip_DeployEnvironment_CRUD covers the create / list / patch round trip.
// One project, one staging environment, then a PATCH that flips
// auto_promote and the new value lands on the read.
func TestShip_DeployEnvironment_CRUD(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")

	// Create.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/deploy_environments", map[string]any{
		"kind":          "staging",
		"name":          "Staging",
		"target_branch": "main",
		"target_url":    "https://staging.example.com",
	})
	req = withURLParam(req, "id", projectID)
	testHandler.CreateProjectDeployEnvironment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create env: %d %s", w.Code, w.Body.String())
	}
	var env deployEnvironmentResponse
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Kind != "staging" || env.AutoPromote {
		t.Fatalf("unexpected env: %+v", env)
	}

	// List.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+projectID+"/deploy_environments", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ListProjectDeployEnvironments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list envs: %d", w.Code)
	}

	// Patch — flip auto_promote.
	tru := true
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/deploy_environments/"+env.ID, map[string]any{
		"auto_promote": tru,
	})
	req = withURLParam(req, "id", env.ID)
	testHandler.UpdateDeployEnvironment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch env: %d %s", w.Code, w.Body.String())
	}
	var patched deployEnvironmentResponse
	if err := json.NewDecoder(w.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patched: %v", err)
	}
	if !patched.AutoPromote {
		t.Fatalf("auto_promote should be true after PATCH, got %+v", patched)
	}
}

// TestShip_LogDeploy_BumpsCurrentSHA — logging a 'succeeded' deploy must
// update the parent environment's current_sha so the "what's running" read
// is a single column lookup.
func TestShip_LogDeploy_BumpsCurrentSHA(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")

	// Set up env via DB so we don't depend on the create handler in this test.
	var envID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO deploy_environment (workspace_id, project_id, kind, name, target_branch)
		VALUES ($1, $2, 'production', 'Production', 'main') RETURNING id
	`, testWorkspaceID, projectID).Scan(&envID); err != nil {
		t.Fatalf("insert env: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/deploy_environments/"+envID+"/deploys", map[string]any{
		"sha":     "deadbeef",
		"status":  "succeeded",
		"log_url": "https://ci.example.com/run/1",
	})
	req = withURLParam(req, "id", envID)
	testHandler.LogDeploy(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("log deploy: %d %s", w.Code, w.Body.String())
	}

	var current string
	if err := testPool.QueryRow(context.Background(),
		`SELECT current_sha FROM deploy_environment WHERE id = $1`, envID).Scan(&current); err != nil {
		t.Fatalf("read current_sha: %v", err)
	}
	if current != "deadbeef" {
		t.Fatalf("current_sha not bumped: got %q", current)
	}
}

// TestShip_LogDeploy_RejectsBadStatus — the status enum is gated by
// normalizeDeployStatus so a typo lands as a 400, not a 500.
func TestShip_LogDeploy_RejectsBadStatus(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")
	var envID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO deploy_environment (workspace_id, project_id, kind, name)
		VALUES ($1, $2, 'staging', 'Staging') RETURNING id
	`, testWorkspaceID, projectID).Scan(&envID); err != nil {
		t.Fatalf("insert env: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/deploy_environments/"+envID+"/deploys", map[string]any{
		"sha":    "abc",
		"status": "exploded",
	})
	req = withURLParam(req, "id", envID)
	testHandler.LogDeploy(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestShip_ListDeploys_NewestFirst — verifies the LIMIT default and the
// DESC ordering. Three deploys go in oldest-first; the response must come
// back newest-first.
func TestShip_ListDeploys_NewestFirst(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")
	var envID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO deploy_environment (workspace_id, project_id, kind, name)
		VALUES ($1, $2, 'production', 'Production') RETURNING id
	`, testWorkspaceID, projectID).Scan(&envID); err != nil {
		t.Fatalf("insert env: %v", err)
	}
	for i, sha := range []string{"sha-1", "sha-2", "sha-3"} {
		age := fmt.Sprintf("%d seconds", i)
		_, _ = testPool.Exec(context.Background(), `
			INSERT INTO deploy (workspace_id, environment_id, ref, sha, status, triggered_at)
			VALUES ($1, $2, 'main', $3, 'succeeded', now() + ($4)::interval)
		`, testWorkspaceID, envID, sha, age)
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/deploy_environments/"+envID+"/deploys", nil)
	req = withURLParam(req, "id", envID)
	testHandler.ListDeploys(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list deploys: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Deploys []deployResponse `json:"deploys"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Deploys) != 3 || resp.Deploys[0].SHA != "sha-3" || resp.Deploys[2].SHA != "sha-1" {
		t.Fatalf("unexpected order: %+v", resp.Deploys)
	}
}

// mustSeedPR inserts a row directly into pull_request. Used by tests that
// don't want to depend on the GitHub-talking sync path.
//
// PG 17 won't reuse a single parameter as both INT (for pr_number) and TEXT
// (for url/interval string concatenation) — explicit ::text casts aren't
// enough, so the URL + age offset are formatted in Go and passed as their
// own parameters.
func mustSeedPR(t *testing.T, projectID, repoURL string, number int, state string) {
	t.Helper()
	url := fmt.Sprintf("https://example.com/%d", number)
	age := fmt.Sprintf("%d seconds", number)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			author_login, base_ref, head_ref, head_sha, html_url,
			pr_created_at, pr_updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6::pull_request_state,
			'alice', 'main', 'feat/x', 'sha', $7,
			now(), now() + ($8)::interval
		)
	`, testWorkspaceID, projectID, repoURL, number, "PR "+state, state, url, age); err != nil {
		t.Fatalf("seed PR %d: %v", number, err)
	}
}

// TestShip_GitHubTokenNotLeakedInResponse — the workspace-level token
// must never round-trip through the workspace response. We seed a token,
// fetch the workspace, and assert the raw token doesn't appear anywhere
// while github_token_set=true reflects the presence flag.
func TestShip_GitHubTokenNotLeakedInResponse(t *testing.T) {
	enableShipHub(t, true) // seed token

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspaces/"+testWorkspaceID, nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.GetWorkspace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetWorkspace: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "test-token-xyz") {
		t.Fatalf("raw token leaked in workspace response: %s", body)
	}
	if !strings.Contains(body, `"github_token_set":true`) {
		t.Fatalf("github_token_set should be true: %s", body)
	}
}
