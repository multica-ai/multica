// Tests for the Phase 7d follow-up deploy workflow poller.
//
// Run only when DATABASE_URL points at a working test DB (see
// integration_test.go for the harness). Each test seeds a workspace +
// project + release, points the GitHub client at an httptest server,
// drives runShipHubDeployWorkflowPollOnce once, then asserts on the
// release row's post-link state.

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// pollerMigrationApplied probes for the 091 migration so a checkout
// running pre-091 just skips the new tests.
func pollerMigrationApplied(t *testing.T) bool {
	t.Helper()
	if testPool == nil {
		return false
	}
	var exists bool
	err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'workspace' AND column_name = 'ship_hub_deploy_workflow_staging'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("probe phase 7d follow-up migration: %v", err)
	}
	return exists
}

// fakeGitHubServer returns an httptest server that responds to the
// /actions/workflows/{file}/runs endpoint with the supplied runs and
// 200 OK. Any other path returns 404 so a wrong call shows up in the
// test failure rather than silently succeeding.
func fakeGitHubServer(t *testing.T, runs []gh.WorkflowRun) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/actions/workflows/") || !strings.HasSuffix(r.URL.Path, "/runs") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":   len(runs),
			"workflow_runs": runs,
		})
	}))
}

// seedPollerFixture creates a workspace (with ship_hub_enabled=TRUE +
// the configured workflows), a project + a github_repo resource, and
// a release in the supplied stage with merged_main_sha=sha. Returns
// the IDs the test will assert against.
//
// Cleanup is registered on t so the fixture rows are removed after the
// test regardless of failure path.
type pollerFixture struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
}

func seedPollerFixture(t *testing.T, repoURL string, stagingWf, prodWf string) pollerFixture {
	t.Helper()
	ctx := context.Background()
	var wsID, projID, releaseID, channelID string

	// Workspace — we INSERT a brand-new one (not the shared
	// testWorkspaceID) so we can freely write columns without
	// stomping on parallel tests.
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, ship_hub_enabled,
			ship_hub_deploy_workflow_staging, ship_hub_deploy_workflow_production,
			settings)
		VALUES ($1, $2, TRUE, $3, $4, $5)
		RETURNING id`,
		"Poller Test", "poller-test-"+t.Name(),
		nullableText(stagingWf), nullableText(prodWf),
		[]byte(`{"ship_hub":{"github_token":"test-token"}}`),
	).Scan(&wsID)
	if err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	// Project + github_repo resource. The poller reads the URL out of
	// the resource_ref JSONB blob to discover what repo to query.
	// project.status check constraint admits the planning-style values
	// (see migrations/034); 'in_progress' is the closest analogue to
	// "this project is being worked on right now".
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status)
		VALUES ($1, 'Poller Project', 'in_progress')
		RETURNING id`, wsID).Scan(&projID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label)
		VALUES ($1, $2, 'github_repo', $3, 'main')`,
		projID, wsID, []byte(`{"url":"`+repoURL+`"}`)); err != nil {
		t.Fatalf("insert resource: %v", err)
	}

	// Release channel — required because the service-layer post path
	// derefs ChannelID; we use a real channels row to keep the pgx
	// scan happy. The channel table requires kind, visibility, and
	// the created_by_* polymorphic columns; we use 'channel' / 'public'
	// and a synthetic UUID for created_by_id because no real user/agent
	// row needs to exist for the FK (the column is just a UUID).
	if err := testPool.QueryRow(ctx, `
		INSERT INTO channel (
			workspace_id, name, kind, visibility,
			created_by_type, created_by_id
		)
		VALUES ($1, $2, 'channel', 'public', 'system', gen_random_uuid())
		RETURNING id`, wsID, "release-poller-"+t.Name()).Scan(&channelID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	// channelID + releaseID variables exist purely so the SELECTs
	// above scan into something — neither is consumed by the
	// fixture struct, since the test seeds the release via
	// seedPollerReleaseInStaging / seedPollerReleaseInPromoting.
	_ = channelID
	_ = releaseID
	return pollerFixture{
		WorkspaceID: parseUUIDString(t, wsID),
		ProjectID:   parseUUIDString(t, projID),
	}
}

// seedReleaseInStaging — minimal release row in stage='in_staging' with
// the supplied merged_main_sha. Returns the release UUID for assertions.
func seedPollerReleaseInStaging(t *testing.T, fix pollerFixture, sha string) pgtype.UUID {
	t.Helper()
	var releaseID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO ship_release (workspace_id, project_id, title, risk_level, stage, merged_at, merged_main_sha)
		VALUES ($1, $2, 'Poller release', 'low', 'in_staging', NOW(), $3)
		RETURNING id`,
		uuidPgToString(fix.WorkspaceID), uuidPgToString(fix.ProjectID), sha,
	).Scan(&releaseID)
	if err != nil {
		t.Fatalf("insert release: %v", err)
	}
	return parseUUIDString(t, releaseID)
}

func seedPollerReleaseInPromoting(t *testing.T, fix pollerFixture, sha string) pgtype.UUID {
	t.Helper()
	var releaseID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO ship_release (workspace_id, project_id, title, risk_level, stage, merged_at, merged_main_sha, qa_verified_at)
		VALUES ($1, $2, 'Poller release', 'low', 'promoting', NOW(), $3, NOW())
		RETURNING id`,
		uuidPgToString(fix.WorkspaceID), uuidPgToString(fix.ProjectID), sha,
	).Scan(&releaseID)
	if err != nil {
		t.Fatalf("insert release: %v", err)
	}
	return parseUUIDString(t, releaseID)
}

// readReleaseStagingDeployID returns the staging_deploy_id (or empty
// string when null).
func readReleaseStagingDeployID(t *testing.T, releaseID pgtype.UUID) string {
	t.Helper()
	var u pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`SELECT staging_deploy_id FROM ship_release WHERE id = $1`,
		uuidPgToString(releaseID)).Scan(&u); err != nil {
		t.Fatalf("read staging_deploy_id: %v", err)
	}
	if !u.Valid {
		return ""
	}
	return uuidPgToString(u)
}

func readReleaseProductionDeployID(t *testing.T, releaseID pgtype.UUID) string {
	t.Helper()
	var u pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`SELECT production_deploy_id FROM ship_release WHERE id = $1`,
		uuidPgToString(releaseID)).Scan(&u); err != nil {
		t.Fatalf("read production_deploy_id: %v", err)
	}
	if !u.Valid {
		return ""
	}
	return uuidPgToString(u)
}

func readReleaseStage(t *testing.T, releaseID pgtype.UUID) string {
	t.Helper()
	var s string
	if err := testPool.QueryRow(context.Background(),
		`SELECT stage::text FROM ship_release WHERE id = $1`,
		uuidPgToString(releaseID)).Scan(&s); err != nil {
		t.Fatalf("read stage: %v", err)
	}
	return s
}

// nullableText returns NULL for "" and the value otherwise. Used to
// keep the workspace.ship_hub_deploy_workflow_* columns honest with
// the "unset = NULL" contract the poller checks.
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// uuidPgToString — local copy of the helper that turns a pgtype.UUID
// into a hex string. We can't import handler.uuidToString from the
// cmd/server package.
func uuidPgToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	for i, j := 0, 0; i < 16; i++ {
		switch i {
		case 4, 6, 8, 10:
			out[j] = '-'
			j++
		}
		out[j] = hex[b[i]>>4]
		out[j+1] = hex[b[i]&0x0f]
		j += 2
	}
	return string(out)
}

func parseUUIDString(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid %q: %v", s, err)
	}
	return u
}

// drivePollerWithFakeGitHub runs runShipHubDeployWorkflowPollOnce with
// the GitHub client pointed at the supplied httptest server. The
// poller normally constructs its own gh.Client per workspace; for
// testability we replace the package-level NewClient call with a
// closure via the BaseURL override that gh.Client supports — same
// pattern the github_test.go uses.
//
// To inject the BaseURL we use a test seam: see the
// poller's pollEnvironmentForRelease — the gh.Client it gets is built
// from the workspace's token. We achieve URL injection by setting the
// global apiBase via test mutation (see ship_hub_deploy_workflow_poller_test_helper.go
// which exposes a hook), OR — cleaner — by driving the inner
// pollWorkspaceDeployWorkflows directly with a client whose BaseURL
// we've already overridden. We choose the latter to avoid global
// mutation.
func drivePollerWithFakeGitHub(t *testing.T, fix pollerFixture, server *httptest.Server, stagingWf, prodWf string) {
	t.Helper()
	ctx := context.Background()
	queries := db.New(testPool)

	// Re-fetch the workspace so we have the row state the poller sees.
	ws, err := queries.GetWorkspace(ctx, fix.WorkspaceID)
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	client := gh.NewClient("test-token")
	client.BaseURL = server.URL

	bus := events.New()
	pollWorkspaceDeployWorkflows(ctx, queries, bus, ws, client, stagingWf, prodWf)
}

func TestDeployWorkflowPoller_StagingMatch(t *testing.T) {
	if !pollerMigrationApplied(t) {
		t.Skip("phase 7d follow-up migration not applied")
	}
	const sha = "abcdef1234567890abcdef1234567890abcdef12"
	fix := seedPollerFixture(t, "https://github.com/owner/repo", "staging.yml", "")
	releaseID := seedPollerReleaseInStaging(t, fix, sha)

	srv := fakeGitHubServer(t, []gh.WorkflowRun{
		{
			ID:         101,
			HeadSHA:    sha,
			HeadBranch: "main",
			Status:     "completed",
			Conclusion: "success",
			HTMLURL:    "https://github.com/owner/repo/actions/runs/101",
		},
	})
	defer srv.Close()

	drivePollerWithFakeGitHub(t, fix, srv, "staging.yml", "")

	if got := readReleaseStagingDeployID(t, releaseID); got == "" {
		t.Fatal("expected staging_deploy_id to be set after poll")
	}
	// With no smoke workflow configured, LinkStagingDeploy advances to
	// verifying. (Poller's smokeWorkflow will be empty because the
	// test workspace doesn't set ship_hub_smoke_workflow.)
	if stage := readReleaseStage(t, releaseID); stage != "verifying" {
		t.Errorf("expected stage=verifying after no-smoke link, got %q", stage)
	}
}

func TestDeployWorkflowPoller_ProductionMatch(t *testing.T) {
	if !pollerMigrationApplied(t) {
		t.Skip("phase 7d follow-up migration not applied")
	}
	const sha = "fedcba0987654321fedcba0987654321fedcba09"
	fix := seedPollerFixture(t, "https://github.com/owner/repo", "", "production.yml")
	releaseID := seedPollerReleaseInPromoting(t, fix, sha)

	srv := fakeGitHubServer(t, []gh.WorkflowRun{
		{
			ID:         202,
			HeadSHA:    sha,
			HeadBranch: "main",
			Status:     "completed",
			Conclusion: "success",
			HTMLURL:    "https://github.com/owner/repo/actions/runs/202",
		},
	})
	defer srv.Close()

	drivePollerWithFakeGitHub(t, fix, srv, "", "production.yml")

	if got := readReleaseProductionDeployID(t, releaseID); got == "" {
		t.Fatal("expected production_deploy_id to be set after poll")
	}
	if stage := readReleaseStage(t, releaseID); stage != "in_production" {
		t.Errorf("expected stage=in_production after link, got %q", stage)
	}
}

// TestDeployWorkflowPoller_ProductionTimeBasedFallback verifies the
// fallback path that unsticks a promoting release when the prod
// deploy fires on a SHA that doesn't equal the release's
// merged_main_sha. This is the ROA-130 case from 2026-05-11: the
// merge train produced commit X, but production was deployed for a
// later commit Y that includes X in its ancestry. Strict SHA matching
// can't see that — the time-based fallback can.
func TestDeployWorkflowPoller_ProductionTimeBasedFallback(t *testing.T) {
	if !pollerMigrationApplied(t) {
		t.Skip("phase 7d follow-up migration not applied")
	}
	const releaseSHA = "aaaa1111bbbb2222cccc3333dddd4444eeee5555"
	const deploySHA = "9999ffff8888eeee7777dddd6666cccc5555bbbb"
	fix := seedPollerFixture(t, "https://github.com/owner/repo", "", "production.yml")
	releaseID := seedPollerReleaseInPromoting(t, fix, releaseSHA)

	// Workflow run's head_sha differs from the release's
	// merged_main_sha. UpdatedAt is well AFTER NOW(), so the
	// merged_at < deploy_time constraint passes (seedPollerReleaseInPromoting
	// stamps merged_at = NOW()).
	srv := fakeGitHubServer(t, []gh.WorkflowRun{
		{
			ID:         606,
			HeadSHA:    deploySHA,
			HeadBranch: "main",
			Status:     "completed",
			Conclusion: "success",
			UpdatedAt:  "2099-01-01T00:00:00Z",
			HTMLURL:    "https://github.com/owner/repo/actions/runs/606",
		},
	})
	defer srv.Close()

	drivePollerWithFakeGitHub(t, fix, srv, "", "production.yml")

	if got := readReleaseProductionDeployID(t, releaseID); got == "" {
		t.Fatal("expected production_deploy_id to be set via time-based fallback")
	}
	if stage := readReleaseStage(t, releaseID); stage != "in_production" {
		t.Errorf("expected stage=in_production after time-based fallback link, got %q", stage)
	}
}

// TestDeployWorkflowPoller_ProductionFallbackRespectsTime verifies the
// fallback DOES NOT link a deploy whose time predates the release's
// merged_at. A stale prod deploy from before the merge must not get
// re-linked to a fresh stuck release.
func TestDeployWorkflowPoller_ProductionFallbackRespectsTime(t *testing.T) {
	if !pollerMigrationApplied(t) {
		t.Skip("phase 7d follow-up migration not applied")
	}
	const releaseSHA = "1234567812345678123456781234567812345678"
	const oldDeploySHA = "fedcba98fedcba98fedcba98fedcba98fedcba98"
	fix := seedPollerFixture(t, "https://github.com/owner/repo", "", "production.yml")
	releaseID := seedPollerReleaseInPromoting(t, fix, releaseSHA)

	// Run finished a decade ago, well before the release's merged_at.
	srv := fakeGitHubServer(t, []gh.WorkflowRun{
		{
			ID:         707,
			HeadSHA:    oldDeploySHA,
			HeadBranch: "main",
			Status:     "completed",
			Conclusion: "success",
			UpdatedAt:  "2015-01-01T00:00:00Z",
			HTMLURL:    "https://github.com/owner/repo/actions/runs/707",
		},
	})
	defer srv.Close()

	drivePollerWithFakeGitHub(t, fix, srv, "", "production.yml")

	if got := readReleaseProductionDeployID(t, releaseID); got != "" {
		t.Fatalf("expected production_deploy_id to remain unset for pre-merge deploy, got %q", got)
	}
	if stage := readReleaseStage(t, releaseID); stage != "promoting" {
		t.Errorf("expected stage=promoting unchanged, got %q", stage)
	}
}

// TestDeployWorkflowPoller_NoMatchOnFailedRun verifies a workflow run
// with conclusion!=success is ignored even when the head_sha matches
// — defensive: a green-light pipeline that runs through a failure
// shouldn't mark the release as deployed.
func TestDeployWorkflowPoller_NoMatchOnFailedRun(t *testing.T) {
	if !pollerMigrationApplied(t) {
		t.Skip("phase 7d follow-up migration not applied")
	}
	const sha = "1111222233334444555566667777888899990000"
	fix := seedPollerFixture(t, "https://github.com/owner/repo", "staging.yml", "")
	releaseID := seedPollerReleaseInStaging(t, fix, sha)

	srv := fakeGitHubServer(t, []gh.WorkflowRun{
		{
			ID:         303,
			HeadSHA:    sha,
			HeadBranch: "main",
			Status:     "completed",
			Conclusion: "failure",
			HTMLURL:    "https://github.com/owner/repo/actions/runs/303",
		},
	})
	defer srv.Close()

	drivePollerWithFakeGitHub(t, fix, srv, "staging.yml", "")

	if got := readReleaseStagingDeployID(t, releaseID); got != "" {
		t.Fatalf("expected staging_deploy_id to remain unset on failed run, got %q", got)
	}
}

// TestDeployWorkflowPoller_NoMatchOnDifferentSHA — we list runs but
// none match a release. Release stays in_staging and unlinked.
func TestDeployWorkflowPoller_NoMatchOnDifferentSHA(t *testing.T) {
	if !pollerMigrationApplied(t) {
		t.Skip("phase 7d follow-up migration not applied")
	}
	fix := seedPollerFixture(t, "https://github.com/owner/repo", "staging.yml", "")
	releaseID := seedPollerReleaseInStaging(t, fix, "sha-of-this-release")

	srv := fakeGitHubServer(t, []gh.WorkflowRun{
		{
			ID:         404,
			HeadSHA:    "sha-of-some-unrelated-deploy",
			HeadBranch: "main",
			Status:     "completed",
			Conclusion: "success",
		},
	})
	defer srv.Close()

	drivePollerWithFakeGitHub(t, fix, srv, "staging.yml", "")

	if got := readReleaseStagingDeployID(t, releaseID); got != "" {
		t.Fatalf("expected staging_deploy_id to remain unset for non-matching sha, got %q", got)
	}
	if stage := readReleaseStage(t, releaseID); stage != "in_staging" {
		t.Errorf("expected stage=in_staging unchanged, got %q", stage)
	}
}

// TestDeployWorkflowPoller_IdempotentOnAlreadyLinked — running the
// poller again after a successful link is a no-op (no new deploy row,
// stage unchanged from the post-first-link state).
func TestDeployWorkflowPoller_IdempotentOnAlreadyLinked(t *testing.T) {
	if !pollerMigrationApplied(t) {
		t.Skip("phase 7d follow-up migration not applied")
	}
	const sha = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	fix := seedPollerFixture(t, "https://github.com/owner/repo", "staging.yml", "")
	releaseID := seedPollerReleaseInStaging(t, fix, sha)

	srv := fakeGitHubServer(t, []gh.WorkflowRun{
		{
			ID:         505,
			HeadSHA:    sha,
			HeadBranch: "main",
			Status:     "completed",
			Conclusion: "success",
		},
	})
	defer srv.Close()

	drivePollerWithFakeGitHub(t, fix, srv, "staging.yml", "")
	firstDeployID := readReleaseStagingDeployID(t, releaseID)
	if firstDeployID == "" {
		t.Fatal("expected staging_deploy_id to be set after first poll")
	}

	// Second tick — should be a no-op.
	drivePollerWithFakeGitHub(t, fix, srv, "staging.yml", "")
	secondDeployID := readReleaseStagingDeployID(t, releaseID)
	if secondDeployID != firstDeployID {
		t.Errorf("expected idempotent link; got %q -> %q", firstDeployID, secondDeployID)
	}
}
