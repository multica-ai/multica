// Phase 7c — Staging deploy linkage + smoke tests + manual verify gate.
//
// These tests exercise the service layer (LinkStagingDeploy /
// RecordSmokeOutcome / RunSmokeTests / MarkSmokeManualPass /
// MarkVerified / Unverify) and the per-endpoint HTTP wiring against
// the real Postgres test pool. The fake GitHub client only needs to
// implement DispatchWorkflow for the smoke trigger paths.

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/ship"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// shipStagingMigrationApplied probes for the 087 migration. Mirrors
// the merge-migration probe so a checkout running pre-087 just skips
// the new tests instead of hard-failing.
func shipStagingMigrationApplied(t *testing.T) bool {
	t.Helper()
	var exists bool
	err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'ship_release' AND column_name = 'merged_main_sha'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("probe phase 7c migration: %v", err)
	}
	return exists
}

func enableStagingTest(t *testing.T) {
	t.Helper()
	if !shipStagingMigrationApplied(t) {
		t.Skip("phase 7c migration not yet applied; skipping")
	}
	enableShipReleaseTest(t)
}

// seedReleaseInStaging inserts a release in stage='in_staging' with a
// recorded merged_main_sha. Used as the entry-point fixture for every
// staging-stage test.
func seedReleaseInStaging(t *testing.T, projectID, mergedSHA, riskLevel string) string {
	t.Helper()
	if riskLevel == "" {
		riskLevel = "low"
	}
	var releaseID string
	err := testPool.QueryRow(context.Background(),
		`INSERT INTO ship_release
			(workspace_id, project_id, title, risk_level, stage, merged_at, merged_main_sha)
		 VALUES ($1, $2, 'staging release', $3, 'in_staging', NOW(), $4)
		 RETURNING id`,
		testWorkspaceID, projectID, riskLevel, mergedSHA).Scan(&releaseID)
	if err != nil {
		t.Fatalf("seed release: %v", err)
	}
	return releaseID
}

// readReleaseSmokeStatus pulls smoke_status (or empty when null).
func readReleaseSmokeStatus(t *testing.T, releaseID string) string {
	t.Helper()
	var s pgtype.Text
	if err := testPool.QueryRow(context.Background(),
		`SELECT smoke_status FROM ship_release WHERE id = $1`, releaseID).Scan(&s); err != nil {
		t.Fatalf("read smoke_status: %v", err)
	}
	if !s.Valid {
		return ""
	}
	return s.String
}

func readReleaseStagingDeploy(t *testing.T, releaseID string) string {
	t.Helper()
	var u pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`SELECT staging_deploy_id FROM ship_release WHERE id = $1`, releaseID).Scan(&u); err != nil {
		t.Fatalf("read staging_deploy_id: %v", err)
	}
	if !u.Valid {
		return ""
	}
	return uuidToString(u)
}

func readReleaseQAVerifiedBy(t *testing.T, releaseID string) string {
	t.Helper()
	var u pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`SELECT qa_verified_by FROM ship_release WHERE id = $1`, releaseID).Scan(&u); err != nil {
		t.Fatalf("read qa_verified_by: %v", err)
	}
	if !u.Valid {
		return ""
	}
	return uuidToString(u)
}

// seedDeployEnv inserts a staging env + a synthetic deploy row. Each
// project gets its own env (the (project_id, kind) UNIQUE allows one
// staging env per project). Cleanup is workspace-wide via the parent
// ship test fixture.
func seedDeployEnv(t *testing.T, projectID string) (envID, deployID, sha string) {
	t.Helper()
	sha = "main-sha-1234567890abcdef"
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO deploy_environment (workspace_id, project_id, kind, name, target_branch)
		 VALUES ($1, $2, 'staging', 'staging', 'main')
		 RETURNING id`,
		testWorkspaceID, projectID).Scan(&envID); err != nil {
		t.Fatalf("seed env: %v", err)
	}
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO deploy (workspace_id, environment_id, ref, sha, status)
		 VALUES ($1, $2, 'main', $3, 'succeeded')
		 RETURNING id`,
		testWorkspaceID, envID, sha).Scan(&deployID); err != nil {
		t.Fatalf("seed deploy: %v", err)
	}
	return envID, deployID, sha
}

// TestLinkStagingDeploy_NoSmoke_AdvancesToVerifying — without a
// configured smoke workflow, LinkStagingDeploy flips the release
// straight to verifying and marks smoke_status="skipped".
func TestLinkStagingDeploy_NoSmoke_AdvancesToVerifying(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/staging-no-smoke")
	_, deployIDStr, sha := seedDeployEnv(t, projectID)
	releaseID := seedReleaseInStaging(t, projectID, sha, "low")

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	updated, err := svc.LinkStagingDeploy(
		context.Background(),
		parseUUID(releaseID),
		parseUUID(deployIDStr),
		sha,
		"" /* no smoke workflow */, "https://github.com/multica-ai/staging-no-smoke",
		deps,
	)
	if err != nil {
		t.Fatalf("LinkStagingDeploy: %v", err)
	}
	if string(updated.Stage) != "verifying" {
		t.Fatalf("expected stage=verifying, got %q", updated.Stage)
	}
	if got := readReleaseSmokeStatus(t, releaseID); got != "skipped" {
		t.Fatalf("expected smoke_status=skipped, got %q", got)
	}
	if got := readReleaseStagingDeploy(t, releaseID); got != deployIDStr {
		t.Fatalf("expected staging_deploy_id=%s, got %q", deployIDStr, got)
	}
}

// TestLinkStagingDeploy_WithSmoke_QueuesAndDispatches — with a smoke
// workflow configured, linkage records smoke_status=queued, leaves
// the release in_staging, and dispatches the workflow.
func TestLinkStagingDeploy_WithSmoke_QueuesAndDispatches(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/staging-with-smoke")
	_, deployIDStr, sha := seedDeployEnv(t, projectID)
	releaseID := seedReleaseInStaging(t, projectID, sha, "low")

	var dispatchCalls atomic.Int32
	gh := &fakeShipGithub{
		dispatchFn: func(_ context.Context, _, _, workflowFile, _ string, _ map[string]string) error {
			dispatchCalls.Add(1)
			if workflowFile != "smoke.yml" {
				t.Fatalf("expected smoke.yml workflow, got %q", workflowFile)
			}
			return nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: gh}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	updated, err := svc.LinkStagingDeploy(
		context.Background(),
		parseUUID(releaseID),
		parseUUID(deployIDStr),
		sha,
		"smoke.yml",
		"https://github.com/multica-ai/staging-with-smoke",
		deps,
	)
	if err != nil {
		t.Fatalf("LinkStagingDeploy: %v", err)
	}
	if string(updated.Stage) != "in_staging" {
		t.Fatalf("expected stage=in_staging (smoke pending), got %q", updated.Stage)
	}
	if got := readReleaseSmokeStatus(t, releaseID); got != "queued" {
		t.Fatalf("expected smoke_status=queued, got %q", got)
	}
	if dispatchCalls.Load() != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", dispatchCalls.Load())
	}
}

// TestRecordSmokeOutcome_Success_AdvancesToVerifying — successful
// check_run flips release in_staging → verifying.
func TestRecordSmokeOutcome_Success_AdvancesToVerifying(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/smoke-pass")
	releaseID := seedReleaseInStaging(t, projectID, "abc1234", "low")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET smoke_status = 'queued', smoke_run_id = 'wf-99' WHERE id = $1`,
		releaseID); err != nil {
		t.Fatalf("seed smoke queued: %v", err)
	}

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	updated, err := svc.RecordSmokeOutcome(context.Background(), parseUUID(releaseID), "success", deps)
	if err != nil {
		t.Fatalf("RecordSmokeOutcome: %v", err)
	}
	if string(updated.Stage) != "verifying" {
		t.Fatalf("expected stage=verifying, got %q", updated.Stage)
	}
	if got := readReleaseSmokeStatus(t, releaseID); got != "completed_success" {
		t.Fatalf("expected smoke_status=completed_success, got %q", got)
	}
}

// TestRecordSmokeOutcome_Failure_StaysInStaging — failed smoke keeps
// the release in_staging; the user can retry or manually pass.
func TestRecordSmokeOutcome_Failure_StaysInStaging(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/smoke-fail")
	releaseID := seedReleaseInStaging(t, projectID, "def5678", "low")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET smoke_status = 'queued', smoke_run_id = 'wf-101' WHERE id = $1`,
		releaseID); err != nil {
		t.Fatalf("seed smoke queued: %v", err)
	}

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	updated, err := svc.RecordSmokeOutcome(context.Background(), parseUUID(releaseID), "failure", deps)
	if err != nil {
		t.Fatalf("RecordSmokeOutcome: %v", err)
	}
	if string(updated.Stage) != "in_staging" {
		t.Fatalf("expected stage stays in_staging, got %q", updated.Stage)
	}
	if got := readReleaseSmokeStatus(t, releaseID); got != "completed_failure" {
		t.Fatalf("expected smoke_status=completed_failure, got %q", got)
	}
}

// TestRunSmokeTests_DispatchesWorkflow — manual run_smoke_tests
// dispatches the workflow and stamps smoke_status=queued.
func TestRunSmokeTests_DispatchesWorkflow(t *testing.T) {
	enableStagingTest(t)
	repoURL := "https://github.com/multica-ai/smoke-rerun"
	projectID := createShipProject(t, repoURL)
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-aaaa", "low")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE workspace SET ship_hub_smoke_workflow = 'smoke.yml' WHERE id = $1`,
		testWorkspaceID); err != nil {
		t.Fatalf("set smoke_workflow: %v", err)
	}

	var calls atomic.Int32
	gh := &fakeShipGithub{
		dispatchFn: func(_ context.Context, _, _, _, _ string, _ map[string]string) error {
			calls.Add(1)
			return nil
		},
	}
	svc := &ship.Service{Q: testHandler.Queries, Github: gh}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	if _, err := svc.RunSmokeTests(
		context.Background(),
		parseUUID(releaseID), parseUUID(testUserID),
		ship.RunSmokeTestsParams{
			WorkspaceID:   parseUUID(testWorkspaceID),
			SmokeWorkflow: "smoke.yml",
			RepoURL:       repoURL,
		},
		deps,
	); err != nil {
		t.Fatalf("RunSmokeTests: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", calls.Load())
	}
	if got := readReleaseSmokeStatus(t, releaseID); got != "queued" {
		t.Fatalf("expected smoke_status=queued after manual run, got %q", got)
	}
}

// TestRunSmokeTests_NoWorkflow_Errors — no configured workflow ⇒
// ErrSmokeNotConfigured.
func TestRunSmokeTests_NoWorkflow_Errors(t *testing.T) {
	enableStagingTest(t)
	repoURL := "https://github.com/multica-ai/smoke-noworkflow"
	projectID := createShipProject(t, repoURL)
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-bbbb", "low")

	gh := &fakeShipGithub{}
	svc := &ship.Service{Q: testHandler.Queries, Github: gh}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	_, err := svc.RunSmokeTests(
		context.Background(),
		parseUUID(releaseID), parseUUID(testUserID),
		ship.RunSmokeTestsParams{
			WorkspaceID:   parseUUID(testWorkspaceID),
			SmokeWorkflow: "",
			RepoURL:       repoURL,
		},
		deps,
	)
	if !errors.Is(err, ship.ErrSmokeNotConfigured) {
		t.Fatalf("expected ErrSmokeNotConfigured, got %v", err)
	}
}

// TestMarkSmokeManualPass_FlipsStatusAndStage — owner/admin override
// flips smoke_status to manual_pass and advances stage to verifying.
func TestMarkSmokeManualPass_FlipsStatusAndStage(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/smoke-manual")
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-cccc", "low")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET smoke_status = 'completed_failure' WHERE id = $1`,
		releaseID); err != nil {
		t.Fatalf("seed smoke failed: %v", err)
	}

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	updated, err := svc.MarkSmokeManualPass(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "QA passed manually", deps)
	if err != nil {
		t.Fatalf("MarkSmokeManualPass: %v", err)
	}
	if string(updated.Stage) != "verifying" {
		t.Fatalf("expected stage=verifying, got %q", updated.Stage)
	}
	if got := readReleaseSmokeStatus(t, releaseID); got != "manual_pass" {
		t.Fatalf("expected smoke_status=manual_pass, got %q", got)
	}
}

// TestMarkVerified_LowRisk_AnyMember — low risk releases verifiable
// by any workspace member.
func TestMarkVerified_LowRisk_AnyMember(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/verify-low")
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-dddd", "low")

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	updated, err := svc.MarkVerified(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "looks good", ship.ApprovalContext{}, deps)
	if err != nil {
		t.Fatalf("MarkVerified: %v", err)
	}
	if string(updated.Stage) != "verifying" {
		t.Fatalf("expected stage=verifying, got %q", updated.Stage)
	}
	if got := readReleaseQAVerifiedBy(t, releaseID); got != testUserID {
		t.Fatalf("expected qa_verified_by=%s, got %q", testUserID, got)
	}
}

// TestMarkVerified_HighRisk_RequiresApprover — high risk needs the
// release.approver_id to equal the requesting user.
func TestMarkVerified_HighRisk_RequiresApprover(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/verify-high")
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-eeee", "high")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET smoke_status = 'completed_success' WHERE id = $1`,
		releaseID); err != nil {
		t.Fatalf("seed smoke success: %v", err)
	}

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}

	_, err := svc.MarkVerified(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "", ship.ApprovalContext{}, deps)
	if !errors.Is(err, ship.ErrApproverRequired) {
		t.Fatalf("expected ErrApproverRequired with no approver set, got %v", err)
	}

	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET approver_id = $1 WHERE id = $2`,
		testUserID, releaseID); err != nil {
		t.Fatalf("set approver: %v", err)
	}
	updated, err := svc.MarkVerified(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "approved", ship.ApprovalContext{}, deps)
	if err != nil {
		t.Fatalf("MarkVerified after approver set: %v", err)
	}
	if string(updated.Stage) != "verifying" {
		t.Fatalf("expected stage=verifying, got %q", updated.Stage)
	}
}

// TestMarkVerified_HighRisk_NoSmoke_Blocks — high risk requires
// smoke_status to be passing/manual_pass/skipped before verifying.
func TestMarkVerified_HighRisk_NoSmoke_Blocks(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/verify-high-nosmoke")
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-ffff", "high")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET approver_id = $1, smoke_status = 'completed_failure' WHERE id = $2`,
		testUserID, releaseID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	_, err := svc.MarkVerified(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "", ship.ApprovalContext{}, deps)
	if !errors.Is(err, ship.ErrSmokeNotFinished) {
		t.Fatalf("expected ErrSmokeNotFinished, got %v", err)
	}
}

// TestUnverify_ReturnsToInStaging — unverify reverses MarkVerified.
func TestUnverify_ReturnsToInStaging(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/unverify")
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-gggg", "low")
	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	if _, err := svc.MarkVerified(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "", ship.ApprovalContext{}, deps); err != nil {
		t.Fatalf("MarkVerified for unverify seed: %v", err)
	}

	updated, err := svc.Unverify(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "found a bug", deps)
	if err != nil {
		t.Fatalf("Unverify: %v", err)
	}
	if string(updated.Stage) != "in_staging" {
		t.Fatalf("expected stage=in_staging after unverify, got %q", updated.Stage)
	}
	if got := readReleaseQAVerifiedBy(t, releaseID); got != "" {
		t.Fatalf("expected qa_verified_by cleared, got %q", got)
	}
}

// TestUnverify_RequiresVerifyingStage — unverify on in_staging
// rejects.
func TestUnverify_RequiresVerifyingStage(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/unverify-wrong-stage")
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-hhhh", "low")

	svc := &ship.Service{Q: testHandler.Queries}
	deps := &ship.StagingDeps{Publisher: &recordingPublisher{}, ParentCtx: context.Background()}
	_, err := svc.Unverify(context.Background(),
		parseUUID(releaseID), parseUUID(testUserID), "stage mismatch", deps)
	if !errors.Is(err, ship.ErrReleaseNotInVerifying) {
		t.Fatalf("expected ErrReleaseNotInVerifying, got %v", err)
	}
}

// TestUnverify_HTTPHandler_RequiresReason — the HTTP layer rejects an
// empty reason with 400.
func TestUnverify_HTTPHandler_RequiresReason(t *testing.T) {
	enableStagingTest(t)
	projectID := createShipProject(t, "https://github.com/multica-ai/unverify-http")
	releaseID := seedReleaseInStaging(t, projectID, "main-sha-iiii", "low")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE ship_release SET stage = 'verifying', qa_verified_at = NOW(), qa_verified_by = $1 WHERE id = $2`,
		testUserID, releaseID); err != nil {
		t.Fatalf("seed verifying: %v", err)
	}
	body := strings.NewReader(`{"reason":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/releases/"+releaseID+"/unverify", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", releaseID)
	w := httptest.NewRecorder()
	testHandler.UnverifyRelease(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty reason, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestReleaseResponseShape_Phase7c — confirms the JSON response
// carries the new Phase 7c fields. This is the contract older
// Electron builds cache against.
func TestReleaseResponseShape_Phase7c(t *testing.T) {
	enableStagingTest(t)
	resp := releaseToResponse(db.ShipRelease{
		ID:            pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Stage:         db.ReleaseStageInStaging,
		SmokeStatus:   pgtype.Text{String: "queued", Valid: true},
		SmokeRunID:    pgtype.Text{String: "wf-1", Valid: true},
		MergedMainSha: pgtype.Text{String: "abc1234", Valid: true},
		MergePaused:   false,
	}, 0)
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(out)
	for _, field := range []string{
		`"smoke_status":"queued"`,
		`"smoke_run_id":"wf-1"`,
		`"merged_main_sha":"abc1234"`,
	} {
		if !strings.Contains(body, field) {
			t.Fatalf("response missing %s; got %s", field, body)
		}
	}
}
