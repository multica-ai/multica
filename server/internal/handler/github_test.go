package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestDerivePRState(t *testing.T) {
	cases := []struct {
		state  string
		draft  bool
		merged bool
		want   string
	}{
		{"open", false, false, "open"},
		{"open", true, false, "draft"},
		{"closed", false, false, "closed"},
		{"closed", false, true, "merged"},
		{"closed", true, true, "merged"}, // merged trumps draft
	}
	for _, tc := range cases {
		got := derivePRState(tc.state, tc.draft, tc.merged)
		if got != tc.want {
			t.Errorf("derivePRState(%q, draft=%v, merged=%v) = %q, want %q",
				tc.state, tc.draft, tc.merged, got, tc.want)
		}
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "shared-secret"
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !verifyWebhookSignature(secret, good, body) {
		t.Error("expected valid signature to verify")
	}
	if verifyWebhookSignature(secret, "sha256=deadbeef", body) {
		t.Error("expected bad hex to fail")
	}
	if verifyWebhookSignature(secret, "", body) {
		t.Error("expected empty header to fail")
	}
	if verifyWebhookSignature(secret, "sha1=whatever", body) {
		t.Error("expected non-sha256 prefix to fail")
	}
	if verifyWebhookSignature("other-secret", good, body) {
		t.Error("expected wrong secret to fail")
	}
}

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret-123")
	wsID := "11111111-2222-3333-4444-555555555555"

	tok, err := signState(wsID)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	got, ok := verifyState(tok)
	if !ok {
		t.Fatal("verifyState rejected a freshly-signed token")
	}
	if got != wsID {
		t.Errorf("verifyState() = %q, want %q", got, wsID)
	}

	// Tampering with the workspace portion must fail (signature is bound
	// to it). Replace the leading UUID's first hex digit.
	tampered := "01111111" + tok[8:]
	if _, ok := verifyState(tampered); ok {
		t.Error("tampered state token should fail to verify")
	}

	// Wrong secret rejects.
	t.Setenv("GITHUB_WEBHOOK_SECRET", "different")
	if _, ok := verifyState(tok); ok {
		t.Error("token signed with old secret should fail under a new one")
	}
}

func TestSignStateRequiresSecret(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "")
	if _, err := signState("ws"); err == nil {
		t.Error("signState should error when secret is unset")
	}
}

// TestWebhook_MergedPR_AdvancesLinkedIssueToDone exercises the end-to-end
// auto-link + merge-sync path: install a workspace, fire a `pull_request`
// webhook with the issue identifier in the title, and verify (a) the PR row
// is upserted, (b) it is linked to the issue, (c) the issue transitions to
// 'done'. The system actor on that issue:updated event is what previously
// panicked the activity / notification listeners — having this test pass
// while listeners are wired up is the regression guard.
func TestWebhook_MergedPR_AdvancesLinkedIssueToDone(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "merge-sync-test-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	// Seed an issue we expect the webhook to close out.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR auto-merge test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	// Wire up an installation row for the webhook to attribute to.
	const installationID int64 = 99887766
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "merge-sync-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	// Build a minimal pull_request webhook payload referencing the issue.
	body := map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":     1234,
			"html_url":   "https://github.com/acme/widget/pull/1234",
			"title":      "Fix login " + created.Identifier,
			"body":       "Closes " + created.Identifier,
			"state":      "closed",
			"draft":      false,
			"merged":     true,
			"merged_at":  "2026-04-29T00:00:00Z",
			"closed_at":  "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": "fix/login"},
			"user":       map[string]any{"login": "octocat", "avatar_url": ""},
		},
		"repository": map[string]any{
			"name":  "widget",
			"owner": map[string]any{"login": "acme"},
		},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	w = httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(w, req2)
	if w.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	// Verify PR row + link + issue status.
	pr, err := testHandler.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		RepoOwner:   "acme",
		RepoName:    "widget",
		PrNumber:    1234,
	})
	if err != nil {
		t.Fatalf("GetGitHubPullRequest: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("expected pr state merged, got %q", pr.State)
	}

	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked PR, got %d", len(linked))
	}

	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "done" {
		t.Errorf("expected issue status 'done', got %q", updated.Status)
	}
}

// TestWebhook_MergedPR_PreservesCancelled guards the "do not stomp cancelled"
// rule: cancelling an issue then merging a linked PR must leave the issue
// cancelled.
func TestWebhook_MergedPR_PreservesCancelled(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "cancelled-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Already cancelled",
		"status": "cancelled",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 11223344
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "cancelled-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number": 7, "html_url": "https://x", "title": "Closes " + created.Identifier,
			"state": "closed", "merged": true, "draft": false,
			"merged_at": "2026-04-29T00:00:00Z", "closed_at": "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z", "updated_at": "2026-04-29T00:00:00Z",
			"head": map[string]any{"ref": "x"}, "user": map[string]any{"login": "u"},
		},
		"repository":   map[string]any{"name": "r", "owner": map[string]any{"login": "o"}},
		"installation": map[string]any{"id": installationID},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	w = httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(w, req2)

	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "cancelled" {
		t.Errorf("expected status to remain 'cancelled', got %q", updated.Status)
	}
}

// TestWebhook_UninstallReturnsWorkspaceForBroadcast guards #4: the uninstall
// path must look up the workspace_id BEFORE deleting the row so the
// resulting `github_installation:deleted` event is broadcast scoped to that
// workspace (the realtime listener drops events with empty workspace_id).
func TestWebhook_UninstallReturnsWorkspaceForBroadcast(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const installationID int64 = 55443322

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "uninstall-test",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
	})

	deleted, err := testHandler.Queries.DeleteGitHubInstallationByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("DeleteGitHubInstallationByInstallationID: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted binding, got %d", len(deleted))
	}
	if uuidToString(deleted[0].WorkspaceID) != testWorkspaceID {
		t.Errorf("expected returned workspace_id %s, got %s", testWorkspaceID, uuidToString(deleted[0].WorkspaceID))
	}
	// Re-deleting must return no rows so the handler skips the broadcast
	// (and does not panic).
	if again, err := testHandler.Queries.DeleteGitHubInstallationByInstallationID(ctx, installationID); err != nil {
		t.Errorf("second delete errored: %v", err)
	} else if len(again) != 0 {
		t.Errorf("expected 0 rows on second delete, got %d", len(again))
	}
}

// TestWebhook_MergedPR_WaitsForOpenSibling guards the multi-PR case: when an
// issue is linked to two PRs and only one is merged, the issue must stay in
// its current status. Only the merge that resolves the LAST in-flight PR
// closes the issue.
func TestWebhook_MergedPR_WaitsForOpenSibling(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "multi-pr-test-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Multi-PR auto-merge test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 55667788
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "multi-pr-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	// Helper to fire one pull_request webhook.
	fire := func(t *testing.T, repo string, prNumber int32, merged bool) {
		t.Helper()
		state := "open"
		if merged {
			state = "closed"
		}
		payload := map[string]any{
			"action": state,
			"pull_request": map[string]any{
				"number":     prNumber,
				"html_url":   "https://github.com/acme/" + repo + "/pull/1",
				"title":      "Fix " + created.Identifier,
				"body":       "",
				"state":      state,
				"draft":      false,
				"merged":     merged,
				"merged_at":  "2026-04-29T00:00:00Z",
				"closed_at":  "2026-04-29T00:00:00Z",
				"created_at": "2026-04-28T00:00:00Z",
				"updated_at": "2026-04-29T00:00:00Z",
				"head":       map[string]any{"ref": "fix/multi"},
				"user":       map[string]any{"login": "octocat"},
			},
			"repository": map[string]any{
				"name":  repo,
				"owner": map[string]any{"login": "acme"},
			},
			"installation": map[string]any{"id": installationID},
		}
		raw, _ := json.Marshal(payload)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(raw)
		sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

		rec := httptest.NewRecorder()
		hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
		hookReq.Header.Set("X-GitHub-Event", "pull_request")
		hookReq.Header.Set("X-Hub-Signature-256", sig)
		testHandler.HandleGitHubWebhook(rec, hookReq)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
		}
	}

	// Open PR A and PR B against two repos so the (workspace, owner, repo,
	// number) uniqueness on github_pull_request leaves room for both.
	fire(t, "repo-a", 1, false)
	fire(t, "repo-b", 2, false)

	// Sanity: both linked.
	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 2 {
		t.Fatalf("expected 2 linked PRs, got %d", len(linked))
	}

	// Merge PR A. Issue must stay in_progress because PR B is still open.
	fire(t, "repo-a", 1, true)
	issueAfterA, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issueAfterA.Status != "in_progress" {
		t.Errorf("issue should stay in_progress while sibling PR is open, got %q", issueAfterA.Status)
	}

	// Now merge PR B. Issue should advance to done — last sibling resolved.
	fire(t, "repo-b", 2, true)
	issueAfterB, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issueAfterB.Status != "done" {
		t.Errorf("expected issue 'done' after every linked PR merged, got %q", issueAfterB.Status)
	}
}

// firePullRequestWebhook is a shared helper for the multi-PR tests below: it
// fires one pull_request webhook for a given repo/number with a target state
// of open / closed / merged and asserts the handler accepts it. Centralizing
// here keeps the per-scenario tests focused on assertions.
func firePullRequestWebhook(t *testing.T, secret, identifier string, installationID int64, repo string, prNumber int32, prState string) {
	t.Helper()
	state := "open"
	merged := false
	switch prState {
	case "merged":
		state = "closed"
		merged = true
	case "closed":
		state = "closed"
	}
	payload := map[string]any{
		"action": state,
		"pull_request": map[string]any{
			"number":     prNumber,
			"html_url":   "https://github.com/acme/" + repo + "/pull/1",
			"title":      "Fix " + identifier,
			"body":       "",
			"state":      state,
			"draft":      false,
			"merged":     merged,
			"merged_at":  "2026-04-29T00:00:00Z",
			"closed_at":  "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": "fix/multi"},
			"user":       map[string]any{"login": "octocat"},
		},
		"repository": map[string]any{
			"name":  repo,
			"owner": map[string]any{"login": "acme"},
		},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	hookReq.Header.Set("X-GitHub-Event", "pull_request")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook %s pr=%d state=%s: expected 202, got %d (%s)",
			repo, prNumber, prState, rec.Code, rec.Body.String())
	}
}

// TestWebhook_ClosedSiblingAfterMerge guards the ordering bug GPT-Boy flagged
// on PR #2470: PR-A merges first (issue stays in_progress because PR-B is
// open), then PR-B closes WITHOUT merging. Because PR-A already delivered the
// work, that close event must re-evaluate the issue and advance it to done.
func TestWebhook_ClosedSiblingAfterMerge(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "closed-sibling-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Closed sibling after merge",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 66778899
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "closed-sibling-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	// Open both PRs.
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-a", 1, "open")
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-b", 2, "open")

	// Merge PR A — issue must stay in_progress because PR B is still open.
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-a", 1, "merged")
	intermediate, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if intermediate.Status != "in_progress" {
		t.Fatalf("issue should stay in_progress while sibling PR open, got %q", intermediate.Status)
	}

	// Close PR B WITHOUT merging — issue should now advance to done because
	// PR-A's merge already delivered the work.
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-b", 2, "closed")
	final, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if final.Status != "done" {
		t.Errorf("expected issue 'done' after sibling closed-without-merge follows a prior merge, got %q", final.Status)
	}
}

// TestWebhook_AllClosedWithoutMerge guards the "nothing was delivered" path:
// two PRs both close without merging. We must NOT auto-close the issue —
// closed-without-merge alone is not evidence the work landed, and the user
// should decide what to do.
func TestWebhook_AllClosedWithoutMerge(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "all-closed-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "All closed no merge",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 77889900
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "all-closed-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-a", 1, "open")
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-b", 2, "open")

	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-a", 1, "closed")
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-b", 2, "closed")

	final, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if final.Status != "in_progress" {
		t.Errorf("issue must stay in_progress when no linked PR ever merged, got %q", final.Status)
	}
}

// fireBareWebhook is a focused helper for the closing-keyword gate tests
// below: it fires a single merged-PR webhook with caller-controlled title,
// body, and branch so each test can exercise a specific PR-grammar shape
// (bare identifier, mixed closing/non-closing references, branch-only
// reference) without re-typing the full webhook envelope each time.
func fireBareWebhook(t *testing.T, secret string, installationID int64, prNumber int32, title, body, branch string) {
	t.Helper()
	payload := map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":     prNumber,
			"html_url":   fmt.Sprintf("https://github.com/acme/widget/pull/%d", prNumber),
			"title":      title,
			"body":       body,
			"state":      "closed",
			"draft":      false,
			"merged":     true,
			"merged_at":  "2026-04-29T00:00:00Z",
			"closed_at":  "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": branch},
			"user":       map[string]any{"login": "octocat"},
		},
		"repository":   map[string]any{"name": "widget", "owner": map[string]any{"login": "acme"}},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook pr=%d: expected 202, got %d (%s)", prNumber, rec.Code, rec.Body.String())
	}
}

// TestWebhook_MergedPR_OnlyClosesIdentifiersWithClosingKeyword is the repro
// from GitHub issue multica-ai/multica#3264: a PR that mentions three issues
// must only auto-complete the one declared with a closing keyword. Follow-up
// / unblocks references are linked but stay in their previous status.
func TestWebhook_MergedPR_OnlyClosesIdentifiersWithClosingKeyword(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "closing-keyword-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	// Three issues to mention in the same PR body.
	createIssue := func(title string) IssueResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":  title,
			"status": "in_progress",
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue %q: %d %s", title, w.Code, w.Body.String())
		}
		var out IssueResponse
		json.NewDecoder(w.Body).Decode(&out)
		return out
	}
	closes := createIssue("primary work")
	followUp := createIssue("follow up work")
	unblocks := createIssue("unblocked work")

	t.Cleanup(func() {
		for _, id := range []string{closes.ID, followUp.ID, unblocks.ID} {
			testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, id)
			testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, id)
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
		}
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
	})

	const installationID int64 = 30264001
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "closing-keyword-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	// PR title mirrors the reporter's repro shape — bare identifier prefix —
	// and body declares closing intent on `closes` only.
	title := closes.Identifier + ": Lorem Ipsum dolor sit amet"
	body := fmt.Sprintf(
		"Closes %s. Follow up work planned in %s. Unblocks %s.",
		closes.Identifier, followUp.Identifier, unblocks.Identifier,
	)
	fireBareWebhook(t, secret, installationID, 1, title, body, "fix/login")

	// The closing-keyword issue (also a bare title prefix) is a genuine target,
	// so it shows in the PR list. The follow-up / unblocks issues are matched
	// only by a bare body mention — auto-link still records the row (generous),
	// but the link is reference_only and excluded from the issue's PR list
	// (MUL-3739).
	listed, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(closes.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue(%s): %v", closes.Identifier, err)
	}
	if len(listed) != 1 {
		t.Errorf("expected %s (closing keyword) to show in the PR list, got %d rows", closes.Identifier, len(listed))
	}
	for _, issue := range []IssueResponse{followUp, unblocks} {
		listed, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(issue.ID))
		if err != nil {
			t.Fatalf("ListPullRequestsByIssue(%s): %v", issue.Identifier, err)
		}
		if len(listed) != 0 {
			t.Errorf("expected %s (bare body mention) to be hidden from the PR list, got %d rows", issue.Identifier, len(listed))
		}
		// The link row still exists — flagged reference_only, not deleted — so
		// close_intent stays trackable across later edits.
		var refOnly bool
		if err := testPool.QueryRow(ctx,
			`SELECT reference_only FROM issue_pull_request WHERE issue_id = $1`, issue.ID,
		).Scan(&refOnly); err != nil {
			t.Fatalf("query reference_only(%s): %v", issue.Identifier, err)
		}
		if !refOnly {
			t.Errorf("expected %s link to be reference_only, got false", issue.Identifier)
		}
	}

	// Only the closing-keyword identifier advances to done.
	wantStatus := map[string]string{
		closes.ID:   "done",
		followUp.ID: "in_progress",
		unblocks.ID: "in_progress",
	}
	for _, issue := range []IssueResponse{closes, followUp, unblocks} {
		got, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
		if err != nil {
			t.Fatalf("GetIssue(%s): %v", issue.Identifier, err)
		}
		if got.Status != wantStatus[issue.ID] {
			t.Errorf("issue %s: status = %q, want %q", issue.Identifier, got.Status, wantStatus[issue.ID])
		}
	}
}

// TestWebhook_MergedPR_TitlePrefixDoesNotClose locks in the design choice
// that a bare "MUL-X: foo" title (no closing keyword) links but never
// auto-completes. The user must write `Closes MUL-X` somewhere if they want
// the merge to flip the status.
func TestWebhook_MergedPR_TitlePrefixDoesNotClose(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "title-prefix-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "title-prefix repro",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264002
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "title-prefix-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	fireBareWebhook(t, secret, installationID, 2, created.Identifier+": fix something", "", "fix/login")

	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Errorf("expected 1 linked PR even without a closing keyword, got %d", len(linked))
	}

	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("expected issue to stay in_progress (title prefix alone is not closing intent), got %q", got.Status)
	}
}

// TestWebhook_MergedPR_BranchNameDoesNotClose guards the conservative design
// decision that identifiers extracted from the branch name link the PR but
// never auto-complete the issue — branch names are not natural-language
// fields and cannot carry a closing keyword.
func TestWebhook_MergedPR_BranchNameDoesNotClose(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "branch-name-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "branch-name repro",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264003
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "branch-name-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	branch := strings.ToLower(created.Identifier) + "/fix-login"
	fireBareWebhook(t, secret, installationID, 3, "Fix login flow", "", branch)

	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Errorf("expected branch-name reference to still link the PR, got %d link rows", len(linked))
	}

	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("expected issue to stay in_progress (branch-name reference is not closing intent), got %q", got.Status)
	}
}

// firePRWebhook fires a webhook for a single PR with caller-controlled
// title, body, branch, and lifecycle (open / merged / closed without
// merge). Tests below need the open→merged sequence so close_intent on
// one PR has to persist across multiple webhook events for a sibling PR.
func firePRWebhook(t *testing.T, secret string, installationID int64, prNumber int32, title, body, branch, lifecycle string) {
	t.Helper()
	var action, state string
	var merged bool
	var mergedAt, closedAt any
	switch lifecycle {
	case "opened":
		action, state, merged = "opened", "open", false
		mergedAt, closedAt = nil, nil
	case "edited":
		action, state, merged = "edited", "open", false
		mergedAt, closedAt = nil, nil
	case "merged":
		action, state, merged = "closed", "closed", true
		mergedAt, closedAt = "2026-04-29T00:00:00Z", "2026-04-29T00:00:00Z"
	case "edited_merged":
		action, state, merged = "edited", "closed", true
		mergedAt, closedAt = "2026-04-29T00:00:00Z", "2026-04-29T00:00:00Z"
	case "closed":
		action, state, merged = "closed", "closed", false
		mergedAt, closedAt = nil, "2026-04-29T00:00:00Z"
	default:
		t.Fatalf("firePRWebhook: unknown lifecycle %q", lifecycle)
	}
	payload := map[string]any{
		"action": action,
		"pull_request": map[string]any{
			"number":     prNumber,
			"html_url":   fmt.Sprintf("https://github.com/acme/widget/pull/%d", prNumber),
			"title":      title,
			"body":       body,
			"state":      state,
			"draft":      false,
			"merged":     merged,
			"merged_at":  mergedAt,
			"closed_at":  closedAt,
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": branch},
			"user":       map[string]any{"login": "octocat"},
		},
		"repository":   map[string]any{"name": "widget", "owner": map[string]any{"login": "acme"}},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook pr=%d (%s): expected 202, got %d (%s)", prNumber, lifecycle, rec.Code, rec.Body.String())
	}
}

func TestWebhook_CloseKeywordRemovedBeforeMergeDoesNotClose(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "close-intent-removal-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "close intent can be removed",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264005
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "close-intent-removal-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	firePRWebhook(t, secret, installationID, 1, "Implement removal path", "Closes "+created.Identifier, "feat/remove-close-intent", "opened")
	firePRWebhook(t, secret, installationID, 1, "Implement removal path", "Related "+created.Identifier, "feat/remove-close-intent", "edited")
	firePRWebhook(t, secret, installationID, 1, "Implement removal path", "Related "+created.Identifier, "feat/remove-close-intent", "merged")

	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after merge: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("after closing keyword was removed before merge: status = %q, want in_progress", got.Status)
	}
	counts, err := testHandler.Queries.GetIssuePullRequestCloseAggregate(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssuePullRequestCloseAggregate: %v", err)
	}
	if counts.MergedWithCloseIntentCount != 0 {
		t.Fatalf("merged_with_close_intent_count = %d, want 0", counts.MergedWithCloseIntentCount)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "post merge close keyword is link only",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue second: %d %s", w.Code, w.Body.String())
	}
	var second IssueResponse
	json.NewDecoder(w.Body).Decode(&second)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, second.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, second.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, second.ID)
	})

	// Adding a closing keyword after the merge must not rewrite the
	// merge-time decision and retroactively close either an existing link
	// or a newly mentioned issue.
	firePRWebhook(t, secret, installationID, 1, "Implement removal path", "Closes "+created.Identifier+"\nCloses "+second.Identifier, "feat/remove-close-intent", "edited_merged")
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after post-merge edit: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("after adding closing keyword post-merge: status = %q, want in_progress", got.Status)
	}
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(second.ID))
	if err != nil {
		t.Fatalf("GetIssue second after post-merge edit: %v", err)
	}
	if got.Status != "in_progress" {
		t.Errorf("second issue after post-merge closing keyword: status = %q, want in_progress", got.Status)
	}
}

// TestWebhook_LinkOnlySiblingMergeAfterCloseKeywordPR is the regression
// guard for the multi-PR sibling case Elon flagged on the first attempt
// of this fix. Scenario:
//
//  1. PR A declares closing intent (`Closes MUL-X`) and is opened.
//  2. PR B references the same issue (link-only — no closing keyword)
//     and is opened.
//  3. PR A merges. The issue stays in_progress because PR B is open.
//  4. PR B merges later. PR B's webhook has no closing keyword, so the
//     previous implementation skipped re-evaluating the issue and the
//     issue stayed stuck in_progress forever.
//
// The persisted close_intent column on issue_pull_request fixes this:
// the aggregate sees PR A's merged+close_intent row regardless of which
// webhook drives the re-evaluation, so PR B's merge advances the issue.
func TestWebhook_LinkOnlySiblingMergeAfterCloseKeywordPR(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "link-only-sibling-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "needs two prs",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264004
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "link-only-sibling-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	// 1) PR A opens with closing intent.
	firePRWebhook(t, secret, installationID, 1, "Implement primary path", "Closes "+created.Identifier, "feat/primary", "opened")
	// 2) PR B opens link-only — title prefix mention, no closing keyword.
	firePRWebhook(t, secret, installationID, 2, created.Identifier+": follow-up cleanup", "", "feat/cleanup", "opened")

	// Sanity: issue is still in_progress (both PRs open).
	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after open: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("after both PRs opened: status = %q, want in_progress", got.Status)
	}

	// 3) PR A merges. PR B still open → issue stays in_progress.
	firePRWebhook(t, secret, installationID, 1, "Implement primary path", "Closes "+created.Identifier, "feat/primary", "merged")
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after A merge: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("after PR A merged with PR B still open: status = %q, want in_progress", got.Status)
	}

	// 4) PR B merges (link-only, no closing keyword). The persisted
	// close_intent on PR A's link must still carry the advance.
	firePRWebhook(t, secret, installationID, 2, created.Identifier+": follow-up cleanup", "", "feat/cleanup", "merged")
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after B merge: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("after both PRs merged (A with close_intent, B link-only): status = %q, want done", got.Status)
	}
}

// TestWebhook_BareBodyMentionHiddenFromPRList is the regression guard for
// MUL-3739: a PR that only mentions an issue identifier in its body (no closing
// keyword, no title prefix, no branch reference) must not appear in that
// issue's PR list. Editing the body to add/remove a closing keyword flips the
// PR's visibility, because reference_only follows the live title/body parse
// while the PR is still open.
func TestWebhook_BareBodyMentionHiddenFromPRList(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "bare-mention-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "mentioned in passing",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264006
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "bare-mention-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	listLen := func() int {
		t.Helper()
		rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
		if err != nil {
			t.Fatalf("ListPullRequestsByIssue: %v", err)
		}
		return len(rows)
	}

	// 1) Opened with only a bare body mention → hidden from the PR list.
	firePRWebhook(t, secret, installationID, 1, "Unrelated cleanup", "Context for reviewers: see "+created.Identifier, "feat/cleanup", "opened")
	if n := listLen(); n != 0 {
		t.Errorf("bare body mention should be hidden from PR list, got %d rows", n)
	}

	// 2) Edited to declare closing intent → now a genuine target, shown.
	firePRWebhook(t, secret, installationID, 1, "Unrelated cleanup", "Closes "+created.Identifier, "feat/cleanup", "edited")
	if n := listLen(); n != 1 {
		t.Errorf("after adding a closing keyword the PR should show, got %d rows", n)
	}

	// 3) Edited back to a bare mention → hidden again.
	firePRWebhook(t, secret, installationID, 1, "Unrelated cleanup", "Reverting: just referencing "+created.Identifier, "feat/cleanup", "edited")
	if n := listLen(); n != 0 {
		t.Errorf("after removing the closing keyword the PR should be hidden again, got %d rows", n)
	}
}

// TestWebhook_HiddenBodyMentionDoesNotBlockAutoAdvance guards the P1 the code
// review flagged on PR #4611: a reference_only link (a PR that only mentions the
// issue in its body) is hidden from the PR list, so it must not silently gate
// auto-advance either. Here PR B stays open with a bare body mention while PR A
// merges with a closing keyword — the issue must still reach `done`, because
// the invisible PR B is excluded from the close aggregate's open_count.
func TestWebhook_HiddenBodyMentionDoesNotBlockAutoAdvance(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "hidden-mention-gate-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "closing PR plus invisible mention",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	const installationID int64 = 30264007
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "hidden-mention-gate-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	// PR B opens with only a bare body mention → reference_only, hidden, open.
	firePRWebhook(t, secret, installationID, 1, "Unrelated cleanup", "Context: see "+created.Identifier, "feat/cleanup", "opened")
	// PR A opens with a closing keyword → genuine closing PR.
	firePRWebhook(t, secret, installationID, 2, "Primary work", "Closes "+created.Identifier, "feat/primary", "opened")

	// Only PR A shows in the list; PR B is hidden.
	listed, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected only the closing PR to show, got %d rows", len(listed))
	}

	// Sanity: issue is still in_progress (PR A open).
	got, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after open: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("after both PRs opened: status = %q, want in_progress", got.Status)
	}

	// PR A merges. PR B is still open but reference_only, so it must NOT count
	// toward open_count — the issue should advance to done.
	firePRWebhook(t, secret, installationID, 2, "Primary work", "Closes "+created.Identifier, "feat/primary", "merged")
	got, err = testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue after merge: %v", err)
	}
	if got.Status != "done" {
		t.Errorf("closing PR merged while only a hidden body-only mention is open: status = %q, want done", got.Status)
	}
}

// ── CI / mergeable_state tests ─────────────────────────────────────────────

func TestDerivePRMergeableState(t *testing.T) {
	cases := []struct {
		name           string
		action         string
		payload        string
		baseRefChanged bool
		wantValid      bool
		wantStr        string
		wantClear      bool
	}{
		{"opened_clears", "opened", "clean", false, false, "", true},
		{"synchronize_clears", "synchronize", "clean", false, false, "", true},
		{"reopened_clears", "reopened", "dirty", false, false, "", true},
		{"edited_base_changed_clears", "edited", "clean", true, false, "", true},
		{"edited_title_only_keeps_value", "edited", "clean", false, true, "clean", false},
		{"labeled_keeps_value", "labeled", "clean", false, true, "clean", false},
		{"labeled_empty_payload_preserves", "labeled", "", false, false, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, clear := derivePRMergeableState(tc.action, tc.payload, tc.baseRefChanged)
			if got.Valid != tc.wantValid {
				t.Errorf("Valid=%v want %v", got.Valid, tc.wantValid)
			}
			if got.String != tc.wantStr {
				t.Errorf("String=%q want %q", got.String, tc.wantStr)
			}
			if clear != tc.wantClear {
				t.Errorf("clear=%v want %v", clear, tc.wantClear)
			}
		})
	}
}

func TestAggregateChecksConclusion(t *testing.T) {
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	cases := []struct {
		name                           string
		failed, passed, pending, total int64
		want                           string
	}{
		{"no_suites_nil", 0, 0, 0, 0, "<nil>"},
		{"any_failure_wins", 1, 5, 0, 6, "failed"},
		{"failure_beats_pending", 1, 0, 3, 4, "failed"},
		{"pending_when_no_failure", 0, 1, 2, 3, "pending"},
		{"all_passed", 0, 3, 0, 3, "passed"},
		{"counts_zero_but_total_nonzero_returns_nil", 0, 0, 0, 1, "<nil>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := aggregateChecksConclusion(tc.failed, tc.passed, tc.pending, tc.total)
			if str(got) != tc.want {
				t.Errorf("aggregateChecksConclusion = %s, want %s", str(got), tc.want)
			}
		})
	}
}

// firePullRequestWebhookWithHead is like firePullRequestWebhook but lets the
// caller control the head SHA and mergeable_state on the payload. The CI
// tests need both knobs to exercise head-change semantics.
func firePullRequestWebhookWithHead(t *testing.T, secret, identifier string, installationID int64, repo string, prNumber int32, action, headSHA, mergeableState string) {
	t.Helper()
	payload := map[string]any{
		"action": action,
		"pull_request": map[string]any{
			"number":          prNumber,
			"html_url":        "https://github.com/acme/" + repo + "/pull/1",
			"title":           "Fix " + identifier,
			"body":            "",
			"state":           "open",
			"draft":           false,
			"merged":          false,
			"merged_at":       nil,
			"closed_at":       nil,
			"created_at":      "2026-04-28T00:00:00Z",
			"updated_at":      "2026-04-29T00:00:00Z",
			"mergeable_state": mergeableState,
			"head":            map[string]any{"ref": "fix/foo", "sha": headSHA},
			"user":            map[string]any{"login": "octocat"},
		},
		"repository": map[string]any{
			"name":  repo,
			"owner": map[string]any{"login": "acme"},
		},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	hookReq.Header.Set("X-GitHub-Event", "pull_request")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook %s pr=%d action=%s: expected 202, got %d (%s)",
			repo, prNumber, action, rec.Code, rec.Body.String())
	}
}

func fireCheckSuiteWebhook(t *testing.T, secret string, installationID int64, repo string, prNumbers []int32, suiteID, appID int64, headSHA, conclusion, updatedAt string) {
	t.Helper()
	fireCheckSuiteWebhookWithStatus(t, secret, installationID, repo, prNumbers,
		suiteID, appID, headSHA, "completed", "completed", conclusion, updatedAt)
}

// fireCheckSuiteWebhookWithStatus is the parametric form of
// fireCheckSuiteWebhook. Tests covering the `requested`/`rerequested` and
// `queued`/`in_progress` matrix use it directly; the legacy completed-only
// helper above wraps it for existing call sites.
func fireCheckSuiteWebhookWithStatus(t *testing.T, secret string, installationID int64, repo string, prNumbers []int32, suiteID, appID int64, headSHA, action, status, conclusion, updatedAt string) {
	t.Helper()
	prRefs := make([]map[string]any, 0, len(prNumbers))
	for _, n := range prNumbers {
		prRefs = append(prRefs, map[string]any{"number": n})
	}
	payload := map[string]any{
		"action": action,
		"check_suite": map[string]any{
			"id":            suiteID,
			"head_sha":      headSHA,
			"status":        status,
			"conclusion":    conclusion,
			"updated_at":    updatedAt,
			"app":           map[string]any{"id": appID},
			"pull_requests": prRefs,
		},
		"repository": map[string]any{
			"name":  repo,
			"owner": map[string]any{"login": "acme"},
		},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	hookReq.Header.Set("X-GitHub-Event", "check_suite")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("check_suite webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func setupPRTestIssue(t *testing.T, ctx context.Context, secret string) (IssueResponse, int64) {
	t.Helper()
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR CI test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	installationID := int64(33445566) + int64(time.Now().UnixNano()%1000000)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_pull_request_check_suite WHERE pr_id IN (SELECT id FROM github_pull_request WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_pending_check_suite WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "ci-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	return created, installationID
}

// TestWebhook_CheckSuite_AggregatesAcrossApps ensures the list query reports
// "failed" when one app's latest suite is a failure and another app's is a
// success on the same head. Without per-app aggregation, the last-completed
// suite would silently flip the verdict.
func TestWebhook_CheckSuite_AggregatesAcrossApps(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-aggregate-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	head := "abc1234567890"
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-a", 11, "opened", head, "")
	// App A → success, App B → failure. The list query must report failed.
	fireCheckSuiteWebhook(t, secret, installationID, "ci-repo-a", []int32{11}, 1001, 7001, head, "success", "2026-05-01T00:00:00Z")
	fireCheckSuiteWebhook(t, secret, installationID, "ci-repo-a", []int32{11}, 1002, 7002, head, "failure", "2026-05-01T00:01:00Z")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 PR row, got %d", len(rows))
	}
	got := aggregateChecksConclusion(rows[0].ChecksFailed, rows[0].ChecksPassed, rows[0].ChecksPending, rows[0].ChecksTotal)
	if got == nil || *got != "failed" {
		t.Errorf("expected aggregate failed, got %v (counts: failed=%d passed=%d pending=%d total=%d)",
			got, rows[0].ChecksFailed, rows[0].ChecksPassed, rows[0].ChecksPending, rows[0].ChecksTotal)
	}
}

// TestWebhook_CheckSuite_OldHeadIgnored asserts that a late-arriving
// check_suite for a stale head SHA doesn't contaminate the current head's
// pending view. Without the head_sha filter in the aggregation query, the
// new head would inherit the old head's "passed" verdict.
func TestWebhook_CheckSuite_OldHeadIgnored(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-oldhead-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	oldHead := "old1111111111"
	newHead := "new2222222222"

	// First: open the PR at old head, run a passing suite.
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-b", 22, "opened", oldHead, "")
	fireCheckSuiteWebhook(t, secret, installationID, "ci-repo-b", []int32{22}, 2001, 8001, oldHead, "success", "2026-05-01T00:00:00Z")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	got := aggregateChecksConclusion(rows[0].ChecksFailed, rows[0].ChecksPassed, rows[0].ChecksPending, rows[0].ChecksTotal)
	if got == nil || *got != "passed" {
		t.Fatalf("setup: expected passed on old head, got %v", got)
	}

	// Then: synchronize to new head — no new suite yet. Then a late suite
	// for the OLD head fires (e.g. a delayed delivery). The current aggregate
	// must be nil (no suite for the new head).
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-b", 22, "synchronize", newHead, "")
	fireCheckSuiteWebhook(t, secret, installationID, "ci-repo-b", []int32{22}, 2002, 8001, oldHead, "success", "2026-05-01T00:05:00Z")

	rows, err = testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	got = aggregateChecksConclusion(rows[0].ChecksFailed, rows[0].ChecksPassed, rows[0].ChecksPending, rows[0].ChecksTotal)
	if got != nil {
		t.Errorf("expected no aggregate (nil) after head change, got %v", got)
	}
}

// TestWebhook_CheckSuite_LateOlderEventIgnored guards the single-row ordering
// rule: for the same (pr_id, suite_id) the upsert must not let a later-
// delivered older event overwrite the latest one. We send the newer state
// (failure) first and then the older (success) and assert the row still
// reads failure.
func TestWebhook_CheckSuite_LateOlderEventIgnored(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-ordering-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	head := "ord1234567890"
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-c", 33, "opened", head, "")
	// Latest event first.
	fireCheckSuiteWebhook(t, secret, installationID, "ci-repo-c", []int32{33}, 3001, 9001, head, "failure", "2026-05-01T01:00:00Z")
	// Late-arriving older event for the same suite.
	fireCheckSuiteWebhook(t, secret, installationID, "ci-repo-c", []int32{33}, 3001, 9001, head, "success", "2026-05-01T00:00:00Z")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	got := aggregateChecksConclusion(rows[0].ChecksFailed, rows[0].ChecksPassed, rows[0].ChecksPending, rows[0].ChecksTotal)
	if got == nil || *got != "failed" {
		t.Errorf("expected failure to win against later-delivered older success, got %v", got)
	}
}

// TestWebhook_CheckSuite_QueuedCountsAsPending covers the "CI 跑到一半" path:
// GitHub fires `check_suite.requested` with status `queued` and an empty
// conclusion while CI is still spinning up. The handler must persist these
// non-terminal events so the per-PR `checks_pending` count reflects work in
// progress; otherwise the frontend falls through to the "checks not
// reported yet" placeholder until the first completed suite arrives.
func TestWebhook_CheckSuite_QueuedCountsAsPending(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-pending-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	head := "pending1234567"
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-pending", 55, "opened", head, "")
	// CI just kicked off — `requested` action, status=queued, no conclusion.
	fireCheckSuiteWebhookWithStatus(t, secret, installationID, "ci-repo-pending", []int32{55}, 4001, 6001, head, "requested", "queued", "", "2026-05-01T00:00:00Z")
	// A second app's suite starts a moment later with status=in_progress.
	fireCheckSuiteWebhookWithStatus(t, secret, installationID, "ci-repo-pending", []int32{55}, 4002, 6002, head, "requested", "in_progress", "", "2026-05-01T00:00:30Z")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 PR row, got %d", len(rows))
	}
	if rows[0].ChecksPending != 2 || rows[0].ChecksTotal != 2 ||
		rows[0].ChecksFailed != 0 || rows[0].ChecksPassed != 0 {
		t.Fatalf("expected pending=2 total=2 failed=0 passed=0, got pending=%d total=%d failed=%d passed=%d",
			rows[0].ChecksPending, rows[0].ChecksTotal, rows[0].ChecksFailed, rows[0].ChecksPassed)
	}
	got := aggregateChecksConclusion(rows[0].ChecksFailed, rows[0].ChecksPassed, rows[0].ChecksPending, rows[0].ChecksTotal)
	if got == nil || *got != "pending" {
		t.Errorf("expected aggregate pending while CI is running, got %v", got)
	}

	// Now one app completes successfully — pending count drops to 1 and the
	// aggregate stays pending until the second app finishes.
	fireCheckSuiteWebhookWithStatus(t, secret, installationID, "ci-repo-pending", []int32{55}, 4001, 6001, head, "completed", "completed", "success", "2026-05-01T00:05:00Z")
	rows, err = testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if rows[0].ChecksPending != 1 || rows[0].ChecksPassed != 1 || rows[0].ChecksTotal != 2 {
		t.Fatalf("expected pending=1 passed=1 total=2 after one suite completes, got pending=%d passed=%d total=%d",
			rows[0].ChecksPending, rows[0].ChecksPassed, rows[0].ChecksTotal)
	}
}

// TestWebhook_CheckSuite_OutOfOrderReplaysOnPRUpsert covers the out-of-order
// path: a `check_suite` event arrives before the matching `pull_request`
// row has been mirrored locally (e.g. webhook reordering, or the PR was
// linked to an installation that was suspended/resumed). The handler must
// stash the suite and replay it when the PR upsert arrives, otherwise the
// PR's first observed suite is silently lost and `checks_pending` stays at
// 0 until the next suite ships.
func TestWebhook_CheckSuite_OutOfOrderReplaysOnPRUpsert(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-oooreplay-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	head := "oo01234567890"
	// Suite event lands FIRST — the PR row does not exist yet.
	fireCheckSuiteWebhookWithStatus(t, secret, installationID, "ci-repo-ooo", []int32{66}, 5001, 7501, head, "requested", "in_progress", "", "2026-05-01T00:00:00Z")

	// Verify nothing landed on the PR table yet (no PR row to land on).
	if rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID)); err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	} else if len(rows) != 0 {
		t.Fatalf("expected 0 PR rows before PR webhook, got %d", len(rows))
	}

	// Now the pull_request webhook arrives. The handler must drain the
	// pending stash and replay it onto this PR.
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-ooo", 66, "opened", head, "")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 PR row after PR webhook, got %d", len(rows))
	}
	if rows[0].ChecksPending != 1 || rows[0].ChecksTotal != 1 {
		t.Fatalf("expected pending=1 total=1 after replay, got pending=%d total=%d",
			rows[0].ChecksPending, rows[0].ChecksTotal)
	}

	// The next PR upsert (a no-op metadata edit) must NOT re-apply or fail
	// — the drain is one-shot, so the second pull_request webhook drains
	// an empty pending list.
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-ooo", 66, "edited", head, "")
	rows, err = testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if rows[0].ChecksPending != 1 || rows[0].ChecksTotal != 1 {
		t.Fatalf("expected pending=1 total=1 after no-op edit, got pending=%d total=%d",
			rows[0].ChecksPending, rows[0].ChecksTotal)
	}
}

// TestWebhook_CheckSuite_OutOfOrderStashKeepsNewer guards the pending
// stash against the same out-of-order trap the live table already
// handles: while the PR row is still missing, an older event for the
// same suite_id must not overwrite a newer payload that was stashed
// first. Without the suite_updated_at guard on UpsertPendingCheckSuite,
// a late `requested/in_progress` arriving after `completed/success`
// would roll the stash back to pending; the subsequent PR upsert would
// then replay the stale state and the PR card would stay stuck on
// "pending" until the next suite shipped.
func TestWebhook_CheckSuite_OutOfOrderStashKeepsNewer(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-stash-order-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	head := "stash01234567"
	// Newer event lands FIRST while the PR row does not exist yet.
	fireCheckSuiteWebhookWithStatus(t, secret, installationID, "ci-repo-stash", []int32{77}, 6001, 8001, head, "completed", "completed", "success", "2026-05-01T00:05:00Z")
	// Older event for the SAME suite arrives later (webhook reorder). The
	// pending stash must keep the newer payload.
	fireCheckSuiteWebhookWithStatus(t, secret, installationID, "ci-repo-stash", []int32{77}, 6001, 8001, head, "requested", "in_progress", "", "2026-05-01T00:00:00Z")

	// PR webhook arrives — drain replays the (still newer) stash.
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-stash", 77, "opened", head, "")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 PR row after PR webhook, got %d", len(rows))
	}
	if rows[0].ChecksPassed != 1 || rows[0].ChecksPending != 0 || rows[0].ChecksTotal != 1 {
		t.Fatalf("expected passed=1 pending=0 total=1 (newer stash preserved), got passed=%d pending=%d total=%d",
			rows[0].ChecksPassed, rows[0].ChecksPending, rows[0].ChecksTotal)
	}
}

// TestWebhook_PullRequest_SynchronizeClearsMergeable verifies that
// `synchronize` sets mergeable_state to NULL even when the payload still
// carries the previous "clean" verdict — the old answer no longer applies
// to the new head SHA.
func TestWebhook_PullRequest_SynchronizeClearsMergeable(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-mergeable-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	// Open with no mergeable verdict, then a metadata event populates clean.
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-d", 44, "opened", "head1", "")
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-d", 44, "labeled", "head1", "clean")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if !rows[0].MergeableState.Valid || rows[0].MergeableState.String != "clean" {
		t.Fatalf("setup: expected mergeable_state=clean, got %+v", rows[0].MergeableState)
	}

	// Synchronize — payload still claims clean, but we must blank it.
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-d", 44, "synchronize", "head2", "clean")

	rows, err = testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if rows[0].MergeableState.Valid {
		t.Errorf("expected mergeable_state cleared on synchronize, got %q", rows[0].MergeableState.String)
	}
	if rows[0].HeadSha != "head2" {
		t.Errorf("expected head_sha updated to head2, got %q", rows[0].HeadSha)
	}
}

// TestWebhook_PullRequest_MetadataPreservesMergeable verifies that a
// metadata-only event (labeled/assigned/edited-without-base-swap) whose
// payload omits mergeable_state does NOT clobber an existing clean/dirty
// verdict. GitHub re-computes mergeability lazily and metadata events ship
// with the field empty even when the previous verdict is still accurate;
// silently overwriting it with NULL would drop a real signal.
func TestWebhook_PullRequest_MetadataPreservesMergeable(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const secret = "ci-mergeable-preserve-secret"
	created, installationID := setupPRTestIssue(t, ctx, secret)

	// Open, then set a known verdict via a labeled event carrying clean.
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-e", 55, "opened", "headA", "")
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-e", 55, "labeled", "headA", "clean")

	rows, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if !rows[0].MergeableState.Valid || rows[0].MergeableState.String != "clean" {
		t.Fatalf("setup: expected mergeable_state=clean, got %+v", rows[0].MergeableState)
	}

	// A second labeled event arrives with mergeable_state empty (typical for
	// metadata events). The existing clean must survive.
	firePullRequestWebhookWithHead(t, secret, created.Identifier, installationID, "ci-repo-e", 55, "labeled", "headA", "")

	rows, err = testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if !rows[0].MergeableState.Valid || rows[0].MergeableState.String != "clean" {
		t.Errorf("expected mergeable_state preserved as clean after metadata event, got %+v", rows[0].MergeableState)
	}
}

// TestListGitHubInstallations_RoleGating covers the read-only relaxation
// in MUL-2413: the endpoint is now reachable by any workspace member, but
// the handler strips the numeric installation_id and reports `can_manage`
// based on the caller's role. Admins / owners still receive the full row.
func TestListGitHubInstallations_RoleGating(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()

	const installationID int64 = 42424242
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "role-gating-acct",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
	})

	call := func(t *testing.T, role string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/github/installations", nil)
		req = withURLParam(req, "id", testWorkspaceID)
		req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{Role: role}))
		w := httptest.NewRecorder()
		testHandler.ListGitHubInstallations(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListGitHubInstallations(%s): %d %s", role, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body (%s): %v", role, err)
		}
		return body
	}

	t.Run("admin sees installation_id + can_manage true", func(t *testing.T) {
		body := call(t, "admin")
		if got, _ := body["can_manage"].(bool); !got {
			t.Errorf("can_manage = %v, want true", body["can_manage"])
		}
		installs, _ := body["installations"].([]any)
		if len(installs) == 0 {
			t.Fatalf("expected at least one installation row, got %v", installs)
		}
		row, _ := installs[0].(map[string]any)
		gotID, ok := row["installation_id"].(float64)
		if !ok {
			t.Fatalf("admin response missing installation_id: %v", row)
		}
		if int64(gotID) != installationID {
			t.Errorf("installation_id = %v, want %d", gotID, installationID)
		}
	})

	t.Run("owner sees installation_id + can_manage true", func(t *testing.T) {
		body := call(t, "owner")
		if got, _ := body["can_manage"].(bool); !got {
			t.Errorf("can_manage = %v, want true", body["can_manage"])
		}
		installs, _ := body["installations"].([]any)
		row, _ := installs[0].(map[string]any)
		if _, ok := row["installation_id"]; !ok {
			t.Errorf("owner response missing installation_id: %v", row)
		}
	})

	t.Run("member sees row without installation_id and can_manage false", func(t *testing.T) {
		body := call(t, "member")
		canManage, _ := body["can_manage"].(bool)
		if canManage {
			t.Errorf("can_manage = true, want false for non-admin member")
		}
		installs, _ := body["installations"].([]any)
		if len(installs) == 0 {
			t.Fatalf("member should still see installation rows, got %v", installs)
		}
		row, _ := installs[0].(map[string]any)
		if _, present := row["installation_id"]; present {
			t.Errorf("installation_id must be omitted for non-admin members, row=%v", row)
		}
		// Display fields the read-only view still needs must round-trip.
		if got, _ := row["account_login"].(string); got != "role-gating-acct" {
			t.Errorf("account_login = %q, want role-gating-acct", got)
		}
	})

	t.Run("guest is treated as read-only and can_manage is false", func(t *testing.T) {
		body := call(t, "guest")
		if canManage, _ := body["can_manage"].(bool); canManage {
			t.Errorf("can_manage = true, want false for guest")
		}
		installs, _ := body["installations"].([]any)
		row, _ := installs[0].(map[string]any)
		if _, present := row["installation_id"]; present {
			t.Errorf("installation_id must be omitted for guest, row=%v", row)
		}
	})
}

// TestGitHubRoutes_RoleGating exercises the router-level middleware split
// introduced in MUL-2413: GET installations runs under
// RequireWorkspaceMemberFromURL while connect / delete remain behind
// RequireWorkspaceRoleFromURL(owner, admin). The handler-level tests above
// inject a member into context directly and so do not cover the middleware
// itself — a future routing change that accidentally moved one of the
// admin-only routes into the member group would slip past them.
func TestGitHubRoutes_RoleGating(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()

	const slug = "github-routes-role-gating"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "GitHub Routes Role Gating", slug, "github routes role gating", "GRG").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// Three workspace members + one outsider. We attach the requesting user
	// via the X-User-ID header so the middleware reads them off the auth
	// boundary just like a real request.
	mkUser := func(t *testing.T, label string) string {
		t.Helper()
		var id string
		email := fmt.Sprintf("github-routes-%s-%s@multica.ai", slug, label)
		if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "GHR "+label, email).Scan(&id); err != nil {
			t.Fatalf("create user %s: %v", label, err)
		}
		return id
	}
	adminUserID := mkUser(t, "admin")
	memberUserID := mkUser(t, "member")
	outsiderUserID := mkUser(t, "outsider")

	for _, m := range []struct {
		userID, role string
	}{
		{adminUserID, "admin"},
		{memberUserID, "member"},
	} {
		if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)
`, wsID, m.userID, m.role); err != nil {
			t.Fatalf("insert member (%s): %v", m.role, err)
		}
	}

	const installationID int64 = 90909090
	createdInst, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(wsID),
		InstallationID: installationID,
		AccountLogin:   "routes-acct",
		AccountType:    "User",
	})
	if err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		for _, uid := range []string{adminUserID, memberUserID, outsiderUserID} {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uid)
		}
	})

	// Build a router subtree mirroring the production wiring at
	// server/cmd/server/router.go for the workspace-scoped GitHub routes.
	// Mounting the real middleware is what makes this a routing-level test —
	// the role split has to come from the chi groups, not from the handler.
	router := chi.NewRouter()
	router.Route("/api/workspaces/{id}", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMemberFromURL(testHandler.Queries, "id"))
			r.Get("/github/installations", testHandler.ListGitHubInstallations)
		})
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceRoleFromURL(testHandler.Queries, "id", "owner", "admin"))
			r.Get("/github/connect", testHandler.GitHubConnect)
			r.Delete("/github/installations/{installationId}", testHandler.DeleteGitHubInstallation)
		})
	})

	exercise := func(t *testing.T, method, path, userID string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if userID != "" {
			req.Header.Set("X-User-ID", userID)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("GET installations is reachable by members", func(t *testing.T) {
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/installations", memberUserID); code != http.StatusOK {
			t.Errorf("member GET installations: want 200, got %d", code)
		}
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/installations", adminUserID); code != http.StatusOK {
			t.Errorf("admin GET installations: want 200, got %d", code)
		}
	})

	t.Run("GET installations rejects non-members", func(t *testing.T) {
		// Outsider hits the workspace middleware before the handler — the
		// middleware translates a missing membership row into 404.
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/installations", outsiderUserID); code != http.StatusNotFound {
			t.Errorf("outsider GET installations: want 404, got %d", code)
		}
	})

	t.Run("GET connect remains owner/admin only", func(t *testing.T) {
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/connect", adminUserID); code != http.StatusOK {
			t.Errorf("admin GET connect: want 200, got %d", code)
		}
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/connect", memberUserID); code != http.StatusForbidden {
			t.Errorf("member GET connect: want 403, got %d", code)
		}
		if code := exercise(t, http.MethodGet, "/api/workspaces/"+wsID+"/github/connect", outsiderUserID); code != http.StatusNotFound {
			t.Errorf("outsider GET connect: want 404, got %d", code)
		}
	})

	t.Run("DELETE installation remains owner/admin only", func(t *testing.T) {
		// Member: 403 — middleware rejects before the handler runs.
		if code := exercise(t, http.MethodDelete, "/api/workspaces/"+wsID+"/github/installations/"+uuidToString(createdInst.ID), memberUserID); code != http.StatusForbidden {
			t.Errorf("member DELETE installation: want 403, got %d", code)
		}
		// Outsider: 404 — workspace not found.
		if code := exercise(t, http.MethodDelete, "/api/workspaces/"+wsID+"/github/installations/"+uuidToString(createdInst.ID), outsiderUserID); code != http.StatusNotFound {
			t.Errorf("outsider DELETE installation: want 404, got %d", code)
		}
		// Admin: 204 and the row goes away.
		if code := exercise(t, http.MethodDelete, "/api/workspaces/"+wsID+"/github/installations/"+uuidToString(createdInst.ID), adminUserID); code != http.StatusNoContent {
			t.Errorf("admin DELETE installation: want 204, got %d", code)
		}
		var remaining int
		if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM github_installation WHERE id = $1`, uuidToString(createdInst.ID)).Scan(&remaining); err != nil {
			t.Fatalf("verify deletion: %v", err)
		}
		if remaining != 0 {
			t.Errorf("expected installation row gone after admin DELETE, got %d remaining", remaining)
		}
	})
}

// TestGitHubInstallationBroadcastRedaction guards Emacs' finding on PR #2886:
// the realtime payloads we publish on installation create / uninstall must
// not carry the numeric `installation_id`. The frontend uses these events
// only to invalidate the installations query, so an admin client recovers
// the management handle via the list endpoint — which already gates the
// numeric id by role.
func TestGitHubInstallationBroadcastRedaction(t *testing.T) {
	inst := db.GithubInstallation{
		InstallationID: 123456789,
		AccountLogin:   "broadcast-acct",
		AccountType:    "User",
	}
	got := githubInstallationToBroadcast(inst)
	if got.InstallationID != nil {
		t.Errorf("broadcast payload must omit installation_id, got %v", *got.InstallationID)
	}
	if got.AccountLogin != "broadcast-acct" {
		t.Errorf("expected account_login preserved, got %q", got.AccountLogin)
	}

	// Sanity: the JSON encoding actually drops the field (omitempty + nil
	// pointer). A future change to the response shape could re-introduce
	// the field through a different name; the JSON check is the real
	// assertion against the wire format clients see.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal broadcast payload: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("unmarshal broadcast payload: %v", err)
	}
	if _, present := generic["installation_id"]; present {
		t.Errorf("installation_id leaked into broadcast JSON: %s", string(raw))
	}
}

// TestWebhook_MergedPR_ChildWithParent_NotifiesParent guards the MUL-2538
// must-fix: a merged PR is the dominant path by which a sub-issue actually
// reaches `done`, and that path goes through advanceIssueToDone — not the
// HTTP UpdateIssue / BatchUpdateIssues handlers that originally wired up
// notifyParentOfChildDone. Without the helper call inside advanceIssueToDone,
// the parent receives nothing when a child is closed by merging its PR.
// This test fires a `pull_request closed merged` webhook against a child
// issue and verifies the parent gets exactly one platform-generated system
// comment with the child's real workspace identifier.
func TestWebhook_MergedPR_ChildWithParent_NotifiesParent(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "merge-parent-notify-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	// Create parent (open) + child (in_progress) pair.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR-merge parent " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue parent: %d %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	json.NewDecoder(w.Body).Decode(&parent)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "PR-merge child " + time.Now().Format(time.RFC3339Nano),
		"status":          "in_progress",
		"parent_issue_id": parent.ID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue child: %d %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	json.NewDecoder(w.Body).Decode(&child)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id IN ($1, $2)`, child.ID, parent.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id IN ($1, $2)`, child.ID, parent.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id IN ($1, $2)`, child.ID, parent.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, child.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parent.ID)
	})

	const installationID int64 = 88990011
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "merge-parent-acct",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":     4242,
			"html_url":   "https://github.com/acme/widget/pull/4242",
			"title":      "Fix " + child.Identifier,
			"body":       "",
			"state":      "closed",
			"draft":      false,
			"merged":     true,
			"merged_at":  "2026-04-29T00:00:00Z",
			"closed_at":  "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-29T00:00:00Z",
			"head":       map[string]any{"ref": "fix/child"},
			"user":       map[string]any{"login": "octocat", "avatar_url": ""},
		},
		"repository": map[string]any{
			"name":  "widget",
			"owner": map[string]any{"login": "acme"},
		},
		"installation": map[string]any{"id": installationID},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	w = httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(w, req2)
	if w.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	// Child must now be done (sanity check — the existing path).
	updatedChild, err := testHandler.Queries.GetIssue(ctx, parseUUID(child.ID))
	if err != nil {
		t.Fatalf("GetIssue child: %v", err)
	}
	if updatedChild.Status != "done" {
		t.Fatalf("expected child status 'done', got %q", updatedChild.Status)
	}

	// Parent must have received exactly one platform-generated system comment.
	var sysCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM comment WHERE issue_id = $1 AND author_type = 'system'`,
		parent.ID,
	).Scan(&sysCount); err != nil {
		t.Fatalf("count system comments on parent: %v", err)
	}
	if sysCount != 1 {
		t.Fatalf("expected 1 system comment on parent after PR-merge auto-done, got %d", sysCount)
	}

	var content string
	if err := testPool.QueryRow(ctx,
		`SELECT content FROM comment WHERE issue_id = $1 AND author_type = 'system' LIMIT 1`,
		parent.ID,
	).Scan(&content); err != nil {
		t.Fatalf("read system comment: %v", err)
	}
	if !strings.Contains(content, child.Identifier) {
		t.Errorf("system comment should reference child identifier %q, got: %s", child.Identifier, content)
	}
	// Parent has no assignee in this fixture, so the routing mentions stay
	// absent. Behavior for assigned parents is covered in
	// issue_child_done_test.go (MUL-2538 Option C).
	for _, banned := range []string{"mention://agent/", "mention://member/", "mention://squad/"} {
		if strings.Contains(content, banned) {
			t.Errorf("system comment must not include %q mention (parent unassigned), got: %s", banned, content)
		}
	}
}

// generateTestRSAKeyPEM mints an RSA-2048 key, returns its PKCS#1 PEM
// encoding (the format GitHub hands operators when they create the App)
// and the parsed *rsa.PrivateKey for verification.
func generateTestRSAKeyPEM(t *testing.T) (pemBytes []byte, key *rsa.PrivateKey) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(k)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}), k
}

// TestSignGitHubAppJWT_NotConfigured pins the contract that missing env
// vars produce ("", nil) — a soft "App auth not available" signal that
// fetchInstallationAccount uses to fall through to its unauthenticated
// path. Returning an error here would force every install on a vanilla
// self-host to log a noisy warning even though the deployment is
// intentionally not running App-authenticated calls.
func TestSignGitHubAppJWT_NotConfigured(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	tok, err := signGitHubAppJWT(time.Now())
	if err != nil {
		t.Fatalf("expected nil error when env not set, got %v", err)
	}
	if tok != "" {
		t.Errorf("expected empty token when env not set, got %q", tok)
	}

	// Half-configured (one var set, the other empty) is treated the same
	// as fully unset — we never want a partial config to claim the App
	// is wired up.
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	tok, err = signGitHubAppJWT(time.Now())
	if err != nil || tok != "" {
		t.Errorf("partial config should return empty token, got tok=%q err=%v", tok, err)
	}
}

// TestSignGitHubAppJWT_InvalidPEM proves that a malformed private key is
// surfaced as an error, not silently swallowed. The setup-callback path
// catches and logs this so the operator gets a breadcrumb instead of an
// install that quietly never enriches the row.
func TestSignGitHubAppJWT_InvalidPEM(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "not a real PEM block")
	if _, err := signGitHubAppJWT(time.Now()); err == nil {
		t.Error("expected error for malformed private key, got nil")
	}
}

// TestSignGitHubAppJWT_ClaimsAndSignature signs a token with a known key
// and verifies (a) the claims GitHub requires (`iss`, `iat`, `exp`) carry
// the values we set, (b) iat is back-dated for clock skew, (c) exp stays
// inside GitHub's 10-minute cap, and (d) the signature verifies against
// the matching public key.
func TestSignGitHubAppJWT_ClaimsAndSignature(t *testing.T) {
	pemBytes, key := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "424242")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))

	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	tok, err := signGitHubAppJWT(now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token when fully configured")
	}

	// Inject the same `now` into the parser's clock so default exp/nbf
	// validation is anchored to the test-time, not real wall clock —
	// otherwise the test becomes a time bomb that fails for real once
	// the real time crosses the token's exp (now + 9m).
	parsed, err := jwt.Parse(
		tok,
		func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return &key.PublicKey, nil
		},
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil || !parsed.Valid {
		t.Fatalf("verify token: err=%v valid=%v", err, parsed != nil && parsed.Valid)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims type: %T", parsed.Claims)
	}
	if got, _ := claims["iss"].(string); got != "424242" {
		t.Errorf("iss = %q, want 424242", got)
	}
	iat := int64(claims["iat"].(float64))
	exp := int64(claims["exp"].(float64))
	if iat != now.Add(-60*time.Second).Unix() {
		t.Errorf("iat = %d, want %d (now - 60s for clock skew)", iat, now.Add(-60*time.Second).Unix())
	}
	if exp != now.Add(9*time.Minute).Unix() {
		t.Errorf("exp = %d, want %d (now + 9m, inside GitHub's 10m cap)", exp, now.Add(9*time.Minute).Unix())
	}
	if exp-iat > int64(10*time.Minute/time.Second) {
		t.Errorf("exp-iat = %d s, exceeds GitHub's 10m max", exp-iat)
	}
}

// TestFetchInstallationAccount_AuthenticatedPopulatesRow simulates the
// GitHub `/app/installations/{id}` endpoint with a JWT-gated mock and
// verifies that fetchInstallationAccount, when fully configured,
// (a) sends a Bearer JWT, (b) parses the JSON response, and (c) returns
// the real account login instead of the "unknown" placeholder. This is
// the assertion that nails down the bug fix for MUL-3078.
func TestFetchInstallationAccount_AuthenticatedPopulatesRow(t *testing.T) {
	pemBytes, key := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "11111")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))

	const wantInstallationID int64 = 7777777
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		expectedPath := fmt.Sprintf("/app/installations/%d", wantInstallationID)
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path: got %q want %q", r.URL.Path, expectedPath)
		}
		// Verify JWT signature using the matching public key — this is
		// what GitHub does on the real endpoint.
		bearer := strings.TrimPrefix(sawAuth, "Bearer ")
		if bearer == sawAuth {
			http.Error(w, "missing Bearer prefix", http.StatusUnauthorized)
			return
		}
		if _, err := jwt.Parse(bearer, func(token *jwt.Token) (any, error) {
			return &key.PublicKey, nil
		}); err != nil {
			http.Error(w, "bad jwt: "+err.Error(), http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"account": map[string]any{
				"login":      "octocat",
				"type":       "Organization",
				"avatar_url": "https://example.com/o.png",
			},
		})
	}))
	t.Cleanup(srv.Close)

	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	login, accountType, avatar := fetchInstallationAccount(context.Background(), wantInstallationID)
	if login != "octocat" {
		t.Errorf("login = %q, want %q (the bug repro: stayed as 'unknown' before the fix)", login, "octocat")
	}
	if accountType != "Organization" {
		t.Errorf("accountType = %q, want Organization", accountType)
	}
	if avatar == nil || *avatar != "https://example.com/o.png" {
		t.Errorf("avatar = %v, want pointer to https://example.com/o.png", avatar)
	}
	if !strings.HasPrefix(sawAuth, "Bearer ") {
		t.Errorf("expected Bearer auth header, got %q", sawAuth)
	}
}

// TestFetchInstallationAccount_UnauthenticatedFallsBack documents the
// degraded path: when the operator hasn't set GITHUB_APP_ID/PRIVATE_KEY,
// the call is made unauthenticated, GitHub returns 401, and the function
// returns the "unknown" placeholder. This is the input the webhook then
// upserts over once GitHub delivers `installation.created`.
func TestFetchInstallationAccount_UnauthenticatedFallsBack(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "auth required", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"account": map[string]any{"login": "should-not-see"}})
	}))
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	login, _, _ := fetchInstallationAccount(context.Background(), 999)
	if login != "unknown" {
		t.Errorf("login = %q, want unknown placeholder when auth not configured", login)
	}
}

// TestFetchInstallationAccount_EmptyAccountKeepsPlaceholder pins that a 200
// response with a missing `account.login` (e.g. GitHub returned a partial
// payload) still yields the safe "unknown" placeholder rather than writing
// an empty string — the frontend renders the literal value, so an empty
// string would surface as "已连接到 " (the bug we're fixing, in a different
// shape).
func TestFetchInstallationAccount_EmptyAccountKeepsPlaceholder(t *testing.T) {
	pemBytes, _ := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "1")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"account": map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	login, accountType, avatar := fetchInstallationAccount(context.Background(), 12)
	if login != "unknown" {
		t.Errorf("expected 'unknown' placeholder for empty account.login, got %q", login)
	}
	if accountType != "User" {
		t.Errorf("expected default 'User' accountType, got %q", accountType)
	}
	if avatar != nil {
		t.Errorf("expected nil avatar, got %v", *avatar)
	}
}

// TestWebhook_InstallationCreatedRefreshesUnknownLogin guards the fix for
// MUL-3078: when the setup callback persists a row with the "unknown"
// placeholder (because the operator hasn't configured App JWT auth, or
// the API call failed), the subsequent `installation.created` webhook
// must (a) overwrite account_login with the real value from the payload
// and (b) broadcast a `github_installation:created` event so any open
// Settings → GitHub tab re-queries without needing a manual refresh.
func TestWebhook_InstallationCreatedRefreshesUnknownLogin(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "installation-refresh-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	const installationID int64 = 71717171
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})

	// Seed the row the way the setup callback does today when App JWT
	// auth isn't available: account_login = "unknown".
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "unknown",
		AccountType:    "User",
	}); err != nil {
		t.Fatalf("seed installation row: %v", err)
	}

	// Subscribe to the bus BEFORE firing the webhook so we can assert the
	// broadcast actually fired. Bus.Subscribe is per-event-type, which
	// matches the realtime hub's downstream filter.
	gotEvent := make(chan events.Event, 1)
	testHandler.Bus.Subscribe(protocol.EventGitHubInstallationCreated, func(e events.Event) {
		select {
		case gotEvent <- e:
		default:
		}
	})

	body, _ := json.Marshal(map[string]any{
		"action": "created",
		"installation": map[string]any{
			"id": installationID,
			"account": map[string]any{
				"login":      "real-octocat",
				"type":       "Organization",
				"avatar_url": "https://example.com/avatar.png",
			},
		},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "installation")
	req.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	// (a) The row's account_login must be the real login, not "unknown".
	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 installation row, got %d", len(rows))
	}
	got := rows[0]
	if got.AccountLogin != "real-octocat" {
		t.Errorf("account_login = %q, want %q (refresh did not overwrite the unknown placeholder)",
			got.AccountLogin, "real-octocat")
	}
	if got.AccountType != "Organization" {
		t.Errorf("account_type = %q, want Organization", got.AccountType)
	}

	// (b) A broadcast must have been emitted on the installation:created
	// channel so the frontend re-queries the list. The realtime listener
	// drops events with empty workspace_id, so we verify both the type
	// AND the workspace scope.
	select {
	case ev := <-gotEvent:
		if ev.WorkspaceID != testWorkspaceID {
			t.Errorf("broadcast WorkspaceID = %q, want %q", ev.WorkspaceID, testWorkspaceID)
		}
		// The payload must carry the redacted installation shape so
		// non-admin clients on the workspace channel can't extract the
		// numeric installation_id from the broadcast itself.
		payload, ok := ev.Payload.(map[string]any)
		if !ok {
			t.Fatalf("broadcast payload type: %T", ev.Payload)
		}
		inst, ok := payload["installation"].(GitHubInstallationResponse)
		if !ok {
			t.Fatalf("installation payload type: %T", payload["installation"])
		}
		if inst.AccountLogin != "real-octocat" {
			t.Errorf("broadcast account_login = %q, want real-octocat", inst.AccountLogin)
		}
		if inst.InstallationID != nil {
			t.Errorf("broadcast must redact installation_id, got %v", *inst.InstallationID)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("expected github_installation:created broadcast after webhook refresh, got none in 2s")
	}
}

// TestSetupCallback_ConsumesPendingInstallationCreated covers the inverse
// race to TestWebhook_InstallationCreatedRefreshesUnknownLogin: GitHub can
// deliver installation.created before the setup callback has created the local
// workspace binding. The webhook cannot broadcast yet, but it must not be lost;
// the callback consumes the pending account metadata even if its direct GitHub
// API lookup falls back to the "unknown" placeholder.
func TestSetupCallback_ConsumesPendingInstallationCreated(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "pending-installation-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.test")

	const installationID int64 = 81818181
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM github_pending_installation WHERE installation_id = $1`, installationID)
	})

	// Force fetchInstallationAccount to take its degraded path. This pins that
	// the final real account name comes from the earlier webhook, not the
	// setup callback's synchronous GitHub API lookup.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "auth required", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	body, _ := json.Marshal(map[string]any{
		"action": "created",
		"installation": map[string]any{
			"id": installationID,
			"account": map[string]any{
				"login":      "pending-octocat",
				"type":       "Organization",
				"avatar_url": "https://example.com/pending.png",
			},
		},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	hookReq.Header.Set("X-GitHub-Event", "installation")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	var pendingLogin string
	if err := testPool.QueryRow(ctx,
		`SELECT account_login FROM github_pending_installation WHERE installation_id = $1`,
		installationID,
	).Scan(&pendingLogin); err != nil {
		t.Fatalf("pending installation row not stored: %v", err)
	}
	if pendingLogin != "pending-octocat" {
		t.Fatalf("pending account_login = %q, want pending-octocat", pendingLogin)
	}

	state, err := signState(testWorkspaceID)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	setupReq := httptest.NewRequest("GET",
		fmt.Sprintf("/api/github/setup?installation_id=%d&state=%s", installationID, state),
		nil,
	)
	setupRec := httptest.NewRecorder()
	testHandler.GitHubSetupCallback(setupRec, setupReq)
	if setupRec.Code != http.StatusFound {
		t.Fatalf("setup callback: expected 302, got %d (%s)", setupRec.Code, setupRec.Body.String())
	}
	if loc := setupRec.Header().Get("Location"); !strings.Contains(loc, "github_connected=1") {
		t.Fatalf("setup callback redirect = %q, want github_connected=1", loc)
	}

	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 installation row, got %d", len(rows))
	}
	got := rows[0]
	if got.AccountLogin != "pending-octocat" {
		t.Errorf("account_login = %q, want pending-octocat (callback left the unknown placeholder)", got.AccountLogin)
	}
	if got.AccountType != "Organization" {
		t.Errorf("account_type = %q, want Organization", got.AccountType)
	}
	if got.AccountAvatarUrl.String != "https://example.com/pending.png" || !got.AccountAvatarUrl.Valid {
		t.Errorf("account_avatar_url = %+v, want pending avatar", got.AccountAvatarUrl)
	}

	var pendingCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM github_pending_installation WHERE installation_id = $1`,
		installationID,
	).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending installation: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending installation row should be consumed, got count %d", pendingCount)
	}
}

func TestRepoIdentityFromURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://github.com/octocat/hello-world", "github.com/octocat/hello-world"},
		{"https://github.com/octocat/hello-world.git", "github.com/octocat/hello-world"},
		{"https://github.com/octocat/hello-world/", "github.com/octocat/hello-world"},
		{"https://github.com/octocat/hello-world.git/", "github.com/octocat/hello-world"}, // .git + trailing slash
		{"git@github.com:octocat/hello-world.git", "github.com/octocat/hello-world"},
		{"git@github.com:octocat/hello-world", "github.com/octocat/hello-world"},
		{"ssh://git@github.com/octocat/hello-world.git", "github.com/octocat/hello-world"},
		{"https://github.com/Octocat/Hello-World", "github.com/octocat/hello-world"}, // case-folded
		{"  https://github.com/octocat/hello-world  ", "github.com/octocat/hello-world"},
		{"git@gitlab.com:octocat/hello-world.git", "gitlab.com/octocat/hello-world"}, // host kept
		{"https://bitbucket.org/octocat/hello-world", "bitbucket.org/octocat/hello-world"},
		{"", ""},
		{"single-segment", ""},
		{"github.com/onlyowner", ""}, // needs host + 2 path segments
	}
	for _, c := range cases {
		if got := repoIdentityFromURL(c.in); got != c.want {
			t.Errorf("repoIdentityFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestWebhook_RoutesToRepoOwningWorkspace is the regression test for the bug
// where one GitHub App installation serving repos in several workspaces filed
// every PR under the installation's single workspace — so a PR's identifier
// was scanned against the wrong prefix and never linked. The fix routes by the
// repository's owning workspace (workspace.repos registry).
func TestWebhook_RoutesToRepoOwningWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "route-by-repo-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	// The issue lives in the repo-OWNING workspace B (the shared test workspace).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "route-by-repo test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	const repoOwner, repoName = "route-acme", "route-widget"
	repoURL := "https://github.com/" + repoOwner + "/" + repoName

	// Register the repo in workspace B's registry; remember the prior value.
	var prevRepos []byte
	if err := testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&prevRepos); err != nil {
		t.Fatalf("read repos: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET repos = $1 WHERE id = $2`,
		[]byte(`[{"url":"`+repoURL+`","description":"route test"}]`), testWorkspaceID); err != nil {
		t.Fatalf("set repos: %v", err)
	}

	// A DIFFERENT workspace A owns the App installation that delivers the
	// webhook; the repo is NOT registered here. Pre-delete any leftover from an
	// aborted run so the slug is free.
	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, "route-test-install-ws")
	wsA, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "route-test-install-ws",
		Slug:        "route-test-install-ws",
		IssuePrefix: "RTW",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	const installationID int64 = 778899001
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    wsA.ID,
		InstallationID: installationID,
		// Account owns the repo (owner == accountLogin); the registry override
		// is only honored for the delivering account.
		AccountLogin: repoOwner,
		AccountType:  "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE repo_owner = $1 AND repo_name = $2`, repoOwner, repoName)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
		testPool.Exec(ctx, `UPDATE workspace SET repos = $1 WHERE id = $2`, prevRepos, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsA.ID)
	})

	const prNumber = 4242
	body := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number":     prNumber,
			"html_url":   repoURL + "/pull/4242",
			"title":      "Implement thing " + created.Identifier,
			"body":       "Closes " + created.Identifier,
			"state":      "open",
			"draft":      false,
			"merged":     false,
			"created_at": "2026-04-28T00:00:00Z",
			"updated_at": "2026-04-28T00:00:00Z",
			"head":       map[string]any{"ref": "feat/thing"},
			"user":       map[string]any{"login": "octocat", "avatar_url": ""},
		},
		"repository": map[string]any{
			"name":  repoName,
			"owner": map[string]any{"login": repoOwner},
		},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	rec := httptest.NewRecorder()
	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	hookReq.Header.Set("X-GitHub-Event", "pull_request")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The PR must be filed under the repo-owning workspace B, not installation
	// workspace A.
	if _, err := testHandler.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		RepoOwner:   repoOwner,
		RepoName:    repoName,
		PrNumber:    prNumber,
	}); err != nil {
		t.Fatalf("expected PR filed under the repo-owning workspace: %v", err)
	}
	if _, err := testHandler.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
		WorkspaceID: wsA.ID,
		RepoOwner:   repoOwner,
		RepoName:    repoName,
		PrNumber:    prNumber,
	}); err == nil {
		t.Fatalf("PR should NOT be filed under the installation's workspace")
	}

	// And it must be linked to the repo-owning workspace's issue.
	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked PR on the repo-owning workspace issue, got %d", len(linked))
	}
}

// TestWebhook_RegistryDoesNotCaptureForeignAccount guards the security gate: a
// workspace that registers a repo whose owner is NOT the delivering
// installation's account must not capture that repo's webhooks.
func TestWebhook_RegistryDoesNotCaptureForeignAccount(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "foreign-acct-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	const repoOwner, repoName = "victim-org", "victim-repo"
	repoURL := "https://github.com/" + repoOwner + "/" + repoName

	// Squatter workspace B (the shared test workspace) registers the repo and
	// has an issue, but the delivery is for a DIFFERENT account.
	var prevRepos []byte
	testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&prevRepos)
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET repos = $1 WHERE id = $2`,
		[]byte(`[{"url":"`+repoURL+`"}]`), testWorkspaceID); err != nil {
		t.Fatalf("set repos: %v", err)
	}
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": "foreign", "status": "todo"}))
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	// The installation maps to its OWN workspace (not the squatter), so when the
	// gate refuses the registry it falls back here — a workspace with no
	// matching issue — and the squatter genuinely gets zero links.
	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, "foreign-acct-install-ws")
	wsA, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name: "foreign-acct-install-ws", Slug: "foreign-acct-install-ws", IssuePrefix: "FAW",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	const installationID int64 = 778899002
	// Installation account != repo owner — the gate must refuse the registry.
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID: wsA.ID, InstallationID: installationID,
		AccountLogin: "some-other-account", AccountType: "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE repo_owner = $1 AND repo_name = $2`, repoOwner, repoName)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
		testPool.Exec(ctx, `UPDATE workspace SET repos = $1 WHERE id = $2`, prevRepos, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsA.ID)
	})

	body := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 5, "html_url": repoURL + "/pull/5", "title": "x " + created.Identifier,
			"body": "Closes " + created.Identifier, "state": "open", "draft": false, "merged": false,
			"created_at": "2026-04-28T00:00:00Z", "updated_at": "2026-04-28T00:00:00Z",
			"head": map[string]any{"ref": "x"}, "user": map[string]any{"login": "octocat"},
		},
		"repository":   map[string]any{"name": repoName, "owner": map[string]any{"login": repoOwner}},
		"installation": map[string]any{"id": installationID},
	}
	raw, _ := json.Marshal(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	rec := httptest.NewRecorder()
	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	hookReq.Header.Set("X-GitHub-Event", "pull_request")
	hookReq.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	testHandler.HandleGitHubWebhook(rec, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The gate refused the registry, so the squatter workspace got nothing.
	if linked, _ := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID)); len(linked) != 0 {
		t.Fatalf("foreign-account repo must not link to the squatter workspace, got %d links", len(linked))
	}
}

// TestSecondWorkspaceBindDoesNotUnbindFirst is the #4823 regression: binding
// the same GitHub App installation in a second workspace must NOT overwrite the
// first workspace's binding. Both bindings coexist, and re-binding an existing
// (workspace, installation) pair upserts its row in place.
func TestSecondWorkspaceBindDoesNotUnbindFirst(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const installationID int64 = 909090909

	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, "multi-bind-ws-b")
	wsB, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "multi-bind-ws-b",
		Slug:        "multi-bind-ws-b",
		IssuePrefix: "MBB",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsB.ID)
	})

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "shared-org",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("bind workspace A: %v", err)
	}
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    wsB.ID,
		InstallationID: installationID,
		AccountLogin:   "shared-org",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("bind workspace B: %v", err)
	}

	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 bindings to coexist (silent unbind regression), got %d", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[uuidToString(r.WorkspaceID)] = true
	}
	if !seen[testWorkspaceID] || !seen[uuidToString(wsB.ID)] {
		t.Errorf("both workspaces must retain a binding; got %v", seen)
	}

	// Re-binding workspace A must upsert its own row in place, not add a third.
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "shared-org-renamed",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("re-bind workspace A: %v", err)
	}
	rows, err = testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations after re-bind: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("re-binding an existing (workspace, installation) must upsert, got %d rows", len(rows))
	}
}

// TestWebhook_UninstallDeletesAllBindings verifies a GitHub-side app uninstall
// drops every workspace binding for the installation and broadcasts to each
// affected workspace so their Settings tabs refresh.
func TestWebhook_UninstallDeletesAllBindings(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "uninstall-all-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 707070707

	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, "uninstall-all-ws-b")
	wsB, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        "uninstall-all-ws-b",
		Slug:        "uninstall-all-ws-b",
		IssuePrefix: "UAB",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsB.ID)
	})

	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "shared-org",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("bind workspace A: %v", err)
	}
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    wsB.ID,
		InstallationID: installationID,
		AccountLogin:   "shared-org",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("bind workspace B: %v", err)
	}

	gotWS := make(chan string, 2)
	testHandler.Bus.Subscribe(protocol.EventGitHubInstallationDeleted, func(e events.Event) {
		select {
		case gotWS <- e.WorkspaceID:
		default:
		}
	})

	body, _ := json.Marshal(map[string]any{
		"action":       "deleted",
		"installation": map[string]any{"id": installationID},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "installation")
	req.Header.Set("X-Hub-Signature-256", sig)
	testHandler.HandleGitHubWebhook(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected all bindings deleted, got %d", len(rows))
	}

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case ws := <-gotWS:
			seen[ws] = true
		case <-deadline:
			t.Fatalf("expected 2 deleted broadcasts (one per workspace), saw %v", seen)
		}
	}
	if !seen[testWorkspaceID] || !seen[uuidToString(wsB.ID)] {
		t.Errorf("deleted broadcasts must cover both workspaces; saw %v", seen)
	}
}
