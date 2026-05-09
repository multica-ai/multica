// Phase 5 Ship Hub handler tests — pre-flight gate, summary endpoint,
// time-machine snapshot. Each test seeds rows directly via testPool so
// the spec is one DB transaction → one HTTP call → one assertion.
//
// We deliberately mirror the Phase 1/2/3/4 test layout (newRequest,
// withURLParam, enableShipHub) rather than introducing a new harness
// — the existing one already handles workspace gating and cleanup.

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestShip_GetSummary_BasicCounts seeds two open PRs (one staging, one
// failing CI) and asserts the summary endpoint returns the expected
// segment counts.
func TestShip_GetSummary_BasicCounts(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")

	mustSeedPR(t, projectID, "https://github.com/multica-ai/multica", 11, "open")
	mustSeedPR(t, projectID, "https://github.com/multica-ai/multica", 12, "open")
	// Mark PR #12 as failing CI so the "failing" segment increments.
	if _, err := testPool.Exec(context.Background(), `
		UPDATE pull_request SET ci_status = 'failure'
		WHERE workspace_id = $1 AND pr_number = $2
	`, testWorkspaceID, 12); err != nil {
		t.Fatalf("seed ci_status: %v", err)
	}

	// Seed a staging env where current_sha matches PR #11's head_sha.
	var stagingID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO deploy_environment (workspace_id, project_id, kind, name, target_branch, current_sha)
		VALUES ($1, $2, 'staging', 'staging', 'main', 'sha')
		RETURNING id
	`, testWorkspaceID, projectID).Scan(&stagingID); err != nil {
		t.Fatalf("seed env: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM deploy_environment WHERE id = $1`, stagingID)
	})

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/ship_hub/summary", nil)
	testHandler.GetShipHubSummary(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetShipHubSummary: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		InStaging      int64 `json:"in_staging"`
		AwaitingReview int64 `json:"awaiting_review"`
		Failing        int64 `json:"failing"`
		OpenPRTotal    int64 `json:"open_pr_total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.InStaging != 2 {
		// Both PRs share head_sha 'sha' from mustSeedPR, so both land
		// in the staging segment given the seeded current_sha.
		t.Fatalf("in_staging: want 2, got %d", resp.InStaging)
	}
	if resp.AwaitingReview < 1 {
		t.Fatalf("awaiting_review: want >= 1, got %d", resp.AwaitingReview)
	}
	if resp.Failing < 1 {
		t.Fatalf("failing: want >= 1 (CI failure), got %d", resp.Failing)
	}
	if resp.OpenPRTotal != 2 {
		t.Fatalf("open_pr_total: want 2, got %d", resp.OpenPRTotal)
	}
}

// TestShip_Preflight_GateBlocksWithoutSmokeTests seeds an env+sha with
// a high-risk PR, opens a preflight, and verifies the gate is "blocked"
// until the user toggles every required field.
func TestShip_Preflight_GateBlocksHighRisk(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")

	// Seed a HIGH-risk PR with head_sha = "deadbeef".
	mustSeedPR(t, projectID, "https://github.com/multica-ai/multica", 21, "open")
	if _, err := testPool.Exec(context.Background(), `
		UPDATE pull_request
		SET head_sha = 'deadbeef', risk_level = 'high'
		WHERE workspace_id = $1 AND pr_number = 21
	`, testWorkspaceID); err != nil {
		t.Fatalf("update PR: %v", err)
	}

	// Production env to attach the preflight to.
	var envID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO deploy_environment (workspace_id, project_id, kind, name, target_branch)
		VALUES ($1, $2, 'production', 'production', 'main')
		RETURNING id
	`, testWorkspaceID, projectID).Scan(&envID); err != nil {
		t.Fatalf("seed env: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM deploy_environment WHERE id = $1`, envID)
	})

	// Open the preflight — gate should be blocked, required level high.
	body := strings.NewReader(`{"target_sha":"deadbeef"}`)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/deploy_environments/"+envID+"/preflight", body)
	req = withURLParam(req, "id", envID)
	testHandler.CreateOrGetDeployPreflight(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create preflight: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		ID                 string   `json:"id"`
		RequiredRiskLevel  string   `json:"required_risk_level"`
		GateStatus         string   `json:"gate_status"`
		GateBlockedReasons []string `json:"gate_blocked_reasons"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RequiredRiskLevel != "high" {
		t.Fatalf("required_risk_level: want high, got %q", resp.RequiredRiskLevel)
	}
	if resp.GateStatus != "blocked" {
		t.Fatalf("gate_status: want blocked, got %q", resp.GateStatus)
	}
	// High-risk preflights need: smoke, qa, migrations, rollback,
	// approver. All five reasons should be present.
	wantContain := []string{"smoke_tests_ok", "qa_verified", "migrations_ok", "rollback_plan", "approver"}
	for _, w := range wantContain {
		ok := false
		for _, r := range resp.GateBlockedReasons {
			if strings.Contains(r, w) {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("expected blocker containing %q in %v", w, resp.GateBlockedReasons)
		}
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM deploy_preflight WHERE id = $1`, resp.ID)
	})
}

// TestShip_Snapshot_TimeFiltersPRsCorrectly seeds two PRs with
// different created_at, and verifies the snapshot endpoint excludes
// the newer one for an `at` value before its creation.
func TestShip_Snapshot_TimeFiltersPRsCorrectly(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")

	// Seed two PRs at distinct timestamps.
	now := time.Now().UTC()
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-10 * time.Minute)

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO pull_request (
			workspace_id, project_id, repo_url, pr_number, title, state,
			author_login, base_ref, head_ref, head_sha, html_url,
			pr_created_at, pr_updated_at
		) VALUES
			($1, $2, $3, 31, 'old PR', 'open', 'alice', 'main', 'feat/old', 's1', 'u1', $4, $4),
			($1, $2, $3, 32, 'new PR', 'open', 'alice', 'main', 'feat/new', 's2', 'u2', $5, $5)
	`,
		testWorkspaceID, projectID, "https://github.com/multica-ai/multica",
		older, newer,
	); err != nil {
		t.Fatalf("seed PRs: %v", err)
	}

	atParam := now.Add(-1 * time.Hour).Format(time.RFC3339)
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+projectID+"/ship_snapshot?at="+atParam, nil)
	req = withURLParam(req, "id", projectID)
	testHandler.GetProjectShipSnapshot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("snapshot: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		PullRequests []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"pull_requests"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Only the older PR (#31) should be present at the chosen `at`.
	if len(resp.PullRequests) != 1 {
		t.Fatalf("expected 1 PR at `at`, got %d (%+v)", len(resp.PullRequests), resp.PullRequests)
	}
	if resp.PullRequests[0].Number != 31 {
		t.Fatalf("expected PR #31 only, got #%d", resp.PullRequests[0].Number)
	}
}

// TestShip_Snapshot_RejectsFutureAt is a small guard test for the
// time-validation path — protects against a typo wasting a slow scan.
func TestShip_Snapshot_RejectsFutureAt(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")

	atParam := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+projectID+"/ship_snapshot?at="+atParam, nil)
	req = withURLParam(req, "id", projectID)
	testHandler.GetProjectShipSnapshot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("future at: want 400, got %d", w.Code)
	}
}
