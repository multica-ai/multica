// Phase 7d — Production promotion + rollback + health rollup tests.
//
// Mix of service-layer tests (PromoteRelease / LinkProductionDeploy /
// MarkReleaseRollback / MarkReleaseDone) and HTTP-handler tests for
// the gate behavior unique to the handler (rollback's owner/admin OR
// approver auth, mark_production_deployed's escape hatch).

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// shipPromotionMigrationApplied probes for the 089 migration so a
// pre-7d checkout skips these tests cleanly.
func shipPromotionMigrationApplied(t *testing.T) bool {
	t.Helper()
	var exists bool
	err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'ship_release' AND column_name = 'production_main_sha'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("probe phase 7d migration: %v", err)
	}
	return exists
}

func enablePromotionTest(t *testing.T) {
	t.Helper()
	if !shipPromotionMigrationApplied(t) {
		t.Skip("phase 7d migration not yet applied; skipping")
	}
	enableShipReleaseTest(t)
}

// seedReleaseVerifying inserts a release in stage='verifying' with a
// recorded merged_main_sha + qa_verified_at, mirroring the post-7c
// state right before the user clicks Promote.
func seedReleaseVerifying(t *testing.T, projectID, mergedSHA, riskLevel string) string {
	t.Helper()
	if riskLevel == "" {
		riskLevel = "low"
	}
	var releaseID string
	err := testPool.QueryRow(context.Background(),
		`INSERT INTO ship_release
			(workspace_id, project_id, title, risk_level, stage,
			 merged_at, merged_main_sha, qa_verified_at, qa_verified_by, staged_at)
		 VALUES ($1, $2, 'verifying release', $3, 'verifying',
			 NOW(), $4, NOW(), $5, NOW())
		 RETURNING id`,
		testWorkspaceID, projectID, riskLevel, mergedSHA, testUserID).Scan(&releaseID)
	if err != nil {
		t.Fatalf("seed verifying release: %v", err)
	}
	return releaseID
}

// readReleaseStage is shared with ship_release_merge_test.go.

func readReleasePromotedBy(t *testing.T, releaseID string) string {
	t.Helper()
	var u pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`SELECT promoted_by FROM ship_release WHERE id = $1`, releaseID).Scan(&u); err != nil {
		t.Fatalf("read promoted_by: %v", err)
	}
	if !u.Valid {
		return ""
	}
	return uuidToString(u)
}

// ---------------------------------------------------------------------------
// PromoteRelease — happy path + risk-tier guards
// ---------------------------------------------------------------------------

// TestPromoteRelease_LowRisk_AnyMember — low risk releases promotable
// by any workspace member.
func TestPromoteRelease_LowRisk_AnyMember(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/promote-low")
	releaseID := seedReleaseVerifying(t, projectID, "main-sha-7d-aaaa", "low")

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	updated, err := svc.PromoteRelease(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), ship.ApprovalContext{}, deps)
	if err != nil {
		t.Fatalf("PromoteRelease: %v", err)
	}
	if string(updated.Stage) != "promoting" {
		t.Fatalf("expected stage=promoting, got %q", updated.Stage)
	}
	if got := readReleasePromotedBy(t, releaseID); got != testUserID {
		t.Fatalf("expected promoted_by=%s, got %q", testUserID, got)
	}
}

// TestPromoteRelease_HighRisk_RequiresApprover — high risk requires
// release.approver_id to equal the requesting user.
func TestPromoteRelease_HighRisk_RequiresApprover(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/promote-high")
	releaseID := seedReleaseVerifying(t, projectID, "main-sha-7d-bbbb", "high")

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	_, err := svc.PromoteRelease(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), ship.ApprovalContext{}, deps)
	if !errors.Is(err, ship.ErrApproverRequired) {
		t.Fatalf("expected ErrApproverRequired with no approver set, got %v", err)
	}

	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET approver_id = $1 WHERE id = $2`,
		testUserID, releaseID); err != nil {
		t.Fatalf("set approver: %v", err)
	}
	updated, err := svc.PromoteRelease(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), ship.ApprovalContext{}, deps)
	if err != nil {
		t.Fatalf("PromoteRelease after approver set: %v", err)
	}
	if string(updated.Stage) != "promoting" {
		t.Fatalf("expected stage=promoting, got %q", updated.Stage)
	}
}

// TestPromoteRelease_WrongStage_Rejects — Promote is only valid from
// verifying. Calling on in_staging returns the stage-mismatch sentinel.
func TestPromoteRelease_WrongStage_Rejects(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/promote-wrong-stage")
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-7d-cccc", "low")

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	_, err := svc.PromoteRelease(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), ship.ApprovalContext{}, deps)
	if !errors.Is(err, ship.ErrReleaseStageMismatch) {
		t.Fatalf("expected ErrReleaseStageMismatch, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// LinkProductionDeploy — webhook path
// ---------------------------------------------------------------------------

// TestLinkProductionDeploy_AdvancesToInProduction — a production
// deploy webhook for a promoting release advances it to in_production.
func TestLinkProductionDeploy_AdvancesToInProduction(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/link-prod")
	releaseID := seedReleaseVerifying(t, projectID, "main-sha-7d-dddd", "low")

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	// Promote first so we're in stage=promoting.
	if _, err := svc.PromoteRelease(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), ship.ApprovalContext{}, deps); err != nil {
		t.Fatalf("PromoteRelease setup: %v", err)
	}

	// Now seed a production deploy + invoke the linkage path.
	var deployID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO deploy_environment (workspace_id, project_id, kind, name, target_branch)
		 VALUES ($1, $2, 'production', 'production', 'main')
		 ON CONFLICT (project_id, kind) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`,
		testWorkspaceID, projectID).Scan(new(string)); err != nil {
		t.Fatalf("seed prod env: %v", err)
	}
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO deploy (workspace_id, environment_id, ref, sha, status)
		 SELECT $1, id, 'main', 'main-sha-7d-dddd', 'succeeded'
		 FROM deploy_environment WHERE workspace_id = $1 AND project_id = $2 AND kind = 'production'
		 RETURNING id`,
		testWorkspaceID, projectID).Scan(&deployID); err != nil {
		t.Fatalf("seed prod deploy: %v", err)
	}

	updated, err := svc.LinkProductionDeploy(context.Background(),
		parseUUID(releaseID), parseUUID(deployID), "main-sha-7d-dddd", deps)
	if err != nil {
		t.Fatalf("LinkProductionDeploy: %v", err)
	}
	if string(updated.Stage) != "in_production" {
		t.Fatalf("expected stage=in_production, got %q", updated.Stage)
	}
	var prodSHA pgtype.Text
	if err := testPool.QueryRow(context.Background(),
		`SELECT production_main_sha FROM ship_release WHERE id = $1`, releaseID).Scan(&prodSHA); err != nil {
		t.Fatalf("read production_main_sha: %v", err)
	}
	if prodSHA.String != "main-sha-7d-dddd" {
		t.Fatalf("expected production_main_sha=main-sha-7d-dddd, got %q", prodSHA.String)
	}
}

// ---------------------------------------------------------------------------
// MarkReleaseRollback — service-layer
// ---------------------------------------------------------------------------

// TestMarkReleaseRollback_HappyPath — rollback from in_production with
// at least one merged PR transitions to rolled_back, sets rolled_back_by
// and rollback_reason, and marks each merged PR's revert_state=pending.
func TestMarkReleaseRollback_HappyPath(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/rollback-happy")
	releaseID := seedReleaseVerifying(t, projectID, "main-sha-7d-eeee", "low")
	// Move to in_production manually.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET stage = 'in_production', promoted_at = NOW() WHERE id = $1`,
		releaseID); err != nil {
		t.Fatalf("seed in_production: %v", err)
	}
	// Seed a merged PR + membership row.
	prID := seedRollbackPR(t, projectID, releaseID, 1, "merged")

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	updated, err := svc.MarkReleaseRollback(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "broke prod", deps)
	if err != nil {
		t.Fatalf("MarkReleaseRollback: %v", err)
	}
	if string(updated.Stage) != "rolled_back" {
		t.Fatalf("expected stage=rolled_back, got %q", updated.Stage)
	}
	var reason pgtype.Text
	if err := testPool.QueryRow(context.Background(),
		`SELECT rollback_reason FROM ship_release WHERE id = $1`, releaseID).Scan(&reason); err != nil {
		t.Fatalf("read rollback_reason: %v", err)
	}
	if reason.String != "broke prod" {
		t.Fatalf("expected rollback_reason=broke prod, got %q", reason.String)
	}
	// PR's revert_state should be pending.
	var revState pgtype.Text
	if err := testPool.QueryRow(context.Background(),
		`SELECT revert_state::text FROM ship_release_pull_request
		 WHERE release_id = $1 AND pull_request_id = $2`,
		releaseID, prID).Scan(&revState); err != nil {
		t.Fatalf("read revert_state: %v", err)
	}
	if revState.String != "pending" {
		t.Fatalf("expected revert_state=pending, got %q", revState.String)
	}
}

// TestMarkReleaseRollback_NoMergedPRs_Rejects — a release with no
// merged PRs (degenerate case after every PR was skipped) returns
// ErrReleaseRollbackNoTarget.
func TestMarkReleaseRollback_NoMergedPRs_Rejects(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/rollback-empty")
	releaseID := seedReleaseVerifying(t, projectID, "main-sha-7d-ffff", "low")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET stage = 'in_production', promoted_at = NOW() WHERE id = $1`,
		releaseID); err != nil {
		t.Fatalf("seed in_production: %v", err)
	}

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	_, err := svc.MarkReleaseRollback(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "no targets", deps)
	if !errors.Is(err, ship.ErrReleaseRollbackNoTarget) {
		t.Fatalf("expected ErrReleaseRollbackNoTarget, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

// TestRollbackRelease_HTTP_RequiresReason — empty reason → 400.
func TestRollbackRelease_HTTP_RequiresReason(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/rollback-http-reason")
	releaseID := seedReleaseVerifying(t, projectID, "main-sha-7d-gggg", "low")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET stage = 'in_production', promoted_at = NOW() WHERE id = $1`,
		releaseID); err != nil {
		t.Fatalf("seed in_production: %v", err)
	}

	body := strings.NewReader(`{"reason":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/releases/"+releaseID+"/rollback", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", releaseID)
	w := httptest.NewRecorder()
	testHandler.RollbackRelease(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty reason, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestMarkReleaseProductionDeployed_LinksDeploy — the manual escape
// hatch creates a deploy row + invokes the linkage path. End state:
// release is in_production with the synthesized production deploy
// linked.
func TestMarkReleaseProductionDeployed_LinksDeploy(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/mark-prod")
	releaseID := seedReleaseVerifying(t, projectID, "main-sha-7d-hhhh", "low")
	// Move to promoting (the user clicked Promote).
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET stage = 'promoting', promoted_at = NOW(), promoted_by = $1 WHERE id = $2`,
		testUserID, releaseID); err != nil {
		t.Fatalf("seed promoting: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/releases/"+releaseID+"/mark_production_deployed",
		strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", releaseID)
	w := httptest.NewRecorder()
	testHandler.MarkReleaseProductionDeployed(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := readReleaseStage(t, releaseID); got != "in_production" {
		t.Fatalf("expected stage=in_production, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// MarkReleaseDone + Health rollup
// ---------------------------------------------------------------------------

// TestMarkReleaseDone_FromInProduction — explicit fast-forward.
func TestMarkReleaseDone_FromInProduction(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/done-fast")
	releaseID := seedReleaseVerifying(t, projectID, "main-sha-7d-iiii", "low")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET stage = 'in_production', promoted_at = NOW() WHERE id = $1`,
		releaseID); err != nil {
		t.Fatalf("seed in_production: %v", err)
	}

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	updated, err := svc.MarkReleaseDone(context.Background(),
		parseUUID(releaseID), deps)
	if err != nil {
		t.Fatalf("MarkReleaseDone: %v", err)
	}
	if string(updated.Stage) != "done" {
		t.Fatalf("expected stage=done, got %q", updated.Stage)
	}
}

// TestUpsertReleaseHealth_Idempotent — two writes for the same release
// produce one row with the latest values.
func TestUpsertReleaseHealth_Idempotent(t *testing.T) {
	enablePromotionTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/health-rollup")
	releaseID := seedReleaseVerifying(t, projectID, "main-sha-7d-jjjj", "low")

	q := testHandler.Queries
	if _, err := q.UpsertReleaseHealth(context.Background(), db.UpsertReleaseHealthParams{
		ReleaseID:               parseUUID(releaseID),
		WorkspaceID:             parseUUID(testWorkspaceID),
		InboxIssuesSincePromote: 0,
		OverallStatus:           "ok",
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if _, err := q.UpsertReleaseHealth(context.Background(), db.UpsertReleaseHealthParams{
		ReleaseID:               parseUUID(releaseID),
		WorkspaceID:             parseUUID(testWorkspaceID),
		InboxIssuesSincePromote: 5,
		OverallStatus:           "warning",
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := q.GetReleaseHealth(context.Background(), parseUUID(releaseID))
	if err != nil {
		t.Fatalf("GetReleaseHealth: %v", err)
	}
	if got.OverallStatus != "warning" {
		t.Fatalf("expected overall_status=warning, got %q", got.OverallStatus)
	}
	if got.InboxIssuesSincePromote != 5 {
		t.Fatalf("expected inbox_issues=5, got %d", got.InboxIssuesSincePromote)
	}

	// One row exists.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ship_release_health WHERE release_id = $1`,
		releaseID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row after two upserts, got %d", count)
	}
}

// TestReleaseResponseShape_Phase7d — the JSON response carries the new
// Phase 7d fields. Same contract test as Phase 7c.
func TestReleaseResponseShape_Phase7d(t *testing.T) {
	enablePromotionTest(t)
	resp := releaseToResponse(db.ShipRelease{
		ID:                pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Stage:             db.ReleaseStageInProduction,
		ProductionMainSha: pgtype.Text{String: "prod-1234", Valid: true},
		PromotedBy:        pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
	}, 0)
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(out)
	for _, field := range []string{
		`"production_main_sha":"prod-1234"`,
		`"promoted_by":`,
		`"rolled_back_completed_at":null`,
	} {
		if !strings.Contains(body, field) {
			t.Fatalf("response missing %s; got %s", field, body)
		}
	}
}

// seedRollbackPR inserts a pull_request + membership row. Used for the
// rollback tests that need at least one "merged" PR present.
func seedRollbackPR(t *testing.T, projectID, releaseID string, position int, mergeState string) string {
	t.Helper()
	var prID string
	err := testPool.QueryRow(context.Background(),
		`INSERT INTO pull_request
			(workspace_id, project_id, repo_url, pr_number, title, state, is_draft,
			 author_login, author_avatar_url, base_ref, head_ref, head_sha, html_url,
			 body, ci_status, review_decision, mergeable,
			 additions, deletions, changed_files, labels)
		 VALUES ($1, $2, 'https://github.com/example/example', $3, 'rollback test', 'open',
			 false, 'tester', '', 'main', 'feat', 'sha-feat', 'https://example.com',
			 '', 'success', 'APPROVED', 'MERGEABLE', 0, 0, 0, '[]'::jsonb)
		 RETURNING id`,
		testWorkspaceID, projectID, position+9000).Scan(&prID)
	if err != nil {
		t.Fatalf("seed pr: %v", err)
	}
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO ship_release_pull_request
			(release_id, pull_request_id, position, is_active, merge_state, merged_sha, merged_at)
		 VALUES ($1, $2, $3, TRUE, $4, $5, NOW())`,
		releaseID, prID, position, mergeState, "sha-merged-"+prID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	return prID
}
