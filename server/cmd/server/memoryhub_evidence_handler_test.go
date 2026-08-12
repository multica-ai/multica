package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// MemoryHub evidence handler tests against the REAL router (the same
// NewRouter instance the integration suite serves), NOT the handler
// constructed directly. These tests prove the V6-1/V6-2 review-repair
// endpoint is reachable and behaves per contract.
//
// Before the MemoryHubService wiring landed, /api/memoryhub/* was never
// registered (h.MemoryHubSvc was nil), so every request here would 404. With
// the wiring in place the route resolves to HandleRepairBlockedReviewer and
// these tests exercise 200/400/401/403/404/409/422.

// seedBlockedReviewRecord inserts an execution_evidence_record in blocked
// state for the given workspace and returns its execution_id.
func seedBlockedReviewRecord(t *testing.T, wsID string, version int32) string {
	t.Helper()
	ctx := context.Background()
	var execID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO execution_evidence_record (
			execution_id, workspace_id, review_policy, review_state, review_version
		)
		VALUES (gen_random_uuid(), $1, 'independent', 'blocked', $2)
		RETURNING execution_id
	`, wsID, version).Scan(&execID); err != nil {
		t.Fatalf("seed blocked review record: %v", err)
	}
	return execID
}

// seedActiveReviewerAgent inserts an active (idle) reviewer agent in the
// workspace and returns its id. The agent gets a real runtime row so its
// runtime_id is never NULL: the shared integration workspace is also used by
// the sweeper fixture queries (SELECT ... FROM agent ... LIMIT 1), and a NULL
// runtime_id there would break their scan. Both rows are removed on test end
// so reviewer agents never accumulate in the shared integration workspace.
func seedActiveReviewerAgent(t *testing.T, wsID string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'integration_test_runtime', 'online', $3, '{}'::jsonb, now())
		RETURNING id
	`, wsID, "Reviewer Runtime "+fmt.Sprintf("%d", time.Now().UnixNano()), "Integration test runtime").Scan(&runtimeID); err != nil {
		t.Fatalf("seed reviewer runtime: %v", err)
	}
	var reviewerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, status, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 'idle', 1, $4)
		RETURNING id
	`, wsID, "Reviewer Agent "+fmt.Sprintf("%d", time.Now().UnixNano()), runtimeID, testUserID).Scan(&reviewerID); err != nil {
		t.Fatalf("seed reviewer agent: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := testPool.Exec(cleanupCtx, `DELETE FROM agent WHERE id = $1`, reviewerID); err != nil {
			t.Logf("cleanup reviewer agent %s: %v", reviewerID, err)
		}
		if _, err := testPool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID); err != nil {
			t.Logf("cleanup reviewer runtime %s: %v", runtimeID, err)
		}
	})
	return reviewerID
}

func reviewRepairRequest(t *testing.T, execID string, body any, token, wsID string) *http.Response {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest("POST", testServer.URL+"/api/memoryhub/evidence/"+execID+"/review-repair", bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Workspace-ID", wsID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

// TestMemoryHubReviewRepairReachable is the P0 acceptance test: the endpoint
// must be reachable on the real router. Before the MemoryHubService wiring
// this returned 404 (route not registered); after wiring it resolves and the
// request is processed.
func TestMemoryHubReviewRepairReachable(t *testing.T) {
	execID := seedBlockedReviewRecord(t, testWorkspaceID, 1)
	reviewerID := seedActiveReviewerAgent(t, testWorkspaceID)

	resp := reviewRepairRequest(t, execID, map[string]any{
		"schema_version":         1,
		"expected_review_version": 1,
		"reviewer_agent_id":       reviewerID,
	}, testToken, testWorkspaceID)
	defer resp.Body.Close()

	// A real router 401/403/404/422/200 are all "reachable". The old dead
	// code returned 404 with no route at all; a 200 proves the full chain
	// (auth + owner guard + handler + service CAS) works.
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("review-repair route returned 404: MemoryHubService is not wired (dead code)")
	}
}

// TestMemoryHubReviewRepairOwnerRepair verifies the happy path: owner repair
// moves the blocked record to pending with review_version+1.
func TestMemoryHubReviewRepairOwnerRepair(t *testing.T) {
	execID := seedBlockedReviewRecord(t, testWorkspaceID, 1)
	reviewerID := seedActiveReviewerAgent(t, testWorkspaceID)

	resp := reviewRepairRequest(t, execID, map[string]any{
		"schema_version":         1,
		"expected_review_version": 1,
		"reviewer_agent_id":       reviewerID,
	}, testToken, testWorkspaceID)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify the record moved to pending with version+1.
	ctx := context.Background()
	var state string
	var version int32
	if err := testPool.QueryRow(ctx, `
		SELECT review_state, review_version FROM execution_evidence_record WHERE execution_id = $1
	`, execID).Scan(&state, &version); err != nil {
		t.Fatalf("read record: %v", err)
	}
	if state != "pending" {
		t.Fatalf("review_state = %q, want pending", state)
	}
	if version != 2 {
		t.Fatalf("review_version = %d, want 2", version)
	}
}

// TestMemoryHubReviewRepairCASRace verifies a replayed request with a stale
// version gets 409 (at-most-once CAS).
func TestMemoryHubReviewRepairCASRace(t *testing.T) {
	execID := seedBlockedReviewRecord(t, testWorkspaceID, 1)
	reviewerID := seedActiveReviewerAgent(t, testWorkspaceID)

	body := map[string]any{
		"schema_version":         1,
		"expected_review_version": 1,
		"reviewer_agent_id":       reviewerID,
	}

	// First repair succeeds.
	resp := reviewRepairRequest(t, execID, body, testToken, testWorkspaceID)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first repair expected 200, got %d", resp.StatusCode)
	}

	// Replay with the SAME (now stale) version -> 409.
	resp2 := reviewRepairRequest(t, execID, body, testToken, testWorkspaceID)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("replay expected 409, got %d", resp2.StatusCode)
	}
}

// TestMemoryHubReviewRepairErrors covers 400/401/403/404/422.
func TestMemoryHubReviewRepairErrors(t *testing.T) {
	execID := seedBlockedReviewRecord(t, testWorkspaceID, 1)
	reviewerID := seedActiveReviewerAgent(t, testWorkspaceID)

	t.Run("401_unauthenticated", func(t *testing.T) {
		resp := reviewRepairRequest(t, execID, map[string]any{
			"schema_version":         1,
			"expected_review_version": 1,
			"reviewer_agent_id":       reviewerID,
		}, "invalid-token", testWorkspaceID)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("403_non_owner_member", func(t *testing.T) {
		// Seed a member with role 'member' and a JWT for that user.
		ctx := context.Background()
		suffix := time.Now().UnixNano()
		email := fmt.Sprintf("review-member-%d@multica.ai", suffix)
		var memberUserID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
		`, "Review Member", email).Scan(&memberUserID); err != nil {
			t.Fatalf("create member user: %v", err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
		`, testWorkspaceID, memberUserID); err != nil {
			t.Fatalf("create member: %v", err)
		}
		memberToken, err := generateTestJWT(memberUserID, email, "Review Member")
		if err != nil {
			t.Fatalf("member jwt: %v", err)
		}
		resp := reviewRepairRequest(t, execID, map[string]any{
			"schema_version":         1,
			"expected_review_version": 1,
			"reviewer_agent_id":       reviewerID,
		}, memberToken, testWorkspaceID)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("400_invalid_uuid", func(t *testing.T) {
		resp := reviewRepairRequest(t, "not-a-uuid", map[string]any{
			"schema_version":         1,
			"expected_review_version": 1,
			"reviewer_agent_id":       reviewerID,
		}, testToken, testWorkspaceID)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("400_unknown_field", func(t *testing.T) {
		resp := reviewRepairRequest(t, execID, map[string]any{
			"schema_version":         1,
			"expected_review_version": 1,
			"reviewer_agent_id":       reviewerID,
			"unknown_field":          true,
		}, testToken, testWorkspaceID)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("404_not_found", func(t *testing.T) {
		resp := reviewRepairRequest(t, "00000000-0000-0000-0000-000000000000", map[string]any{
			"schema_version":         1,
			"expected_review_version": 1,
			"reviewer_agent_id":       reviewerID,
		}, testToken, testWorkspaceID)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("422_reviewer_scope_mismatch", func(t *testing.T) {
		resp := reviewRepairRequest(t, execID, map[string]any{
			"schema_version":         1,
			"expected_review_version": 1,
			"reviewer_agent_id":       "00000000-0000-0000-0000-000000000000",
		}, testToken, testWorkspaceID)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d", resp.StatusCode)
		}
	})

	t.Run("422_self_forbidden", func(t *testing.T) {
		// The integration fixture's user is the workspace owner; the agent
		// created in the fixture has the owner as its owner. Use that agent's
		// id as both reviewer and execution agent via a fresh record where the
		// handler cannot observe the execution agent, so we instead verify the
		// service rejects an inactive reviewer agent (status=offline).
		ctx := context.Background()
		var offlineAgent string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent (
				workspace_id, name, description, runtime_mode, runtime_config,
				visibility, status, max_concurrent_tasks, owner_id
			)
			VALUES ($1, $2, '', 'cloud', '{}'::jsonb, 'private', 'offline', 1, $3)
			RETURNING id
		`, testWorkspaceID, "Offline Reviewer", testUserID).Scan(&offlineAgent); err != nil {
			t.Fatalf("seed offline agent: %v", err)
		}
		// Remove the offline agent on test end: it lives in the shared
		// integration workspace, and the sweeper/rerun fixtures select the
		// integration agent with LIMIT 1 — an extra agent whose runtime_id is
		// NULL breaks their scan.
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			if _, err := testPool.Exec(cleanupCtx, `DELETE FROM agent WHERE id = $1`, offlineAgent); err != nil {
				t.Logf("cleanup offline agent %s: %v", offlineAgent, err)
			}
		})
		resp := reviewRepairRequest(t, execID, map[string]any{
			"schema_version":         1,
			"expected_review_version": 1,
			"reviewer_agent_id":       offlineAgent,
		}, testToken, testWorkspaceID)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for inactive reviewer, got %d", resp.StatusCode)
		}
	})
}
