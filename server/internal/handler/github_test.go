package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestExtractIdentifiers(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "branch_name",
			in:   []string{"", "", "mul-1510/fix-login"},
			want: []string{"MUL-1510"},
		},
		{
			name: "title_and_body",
			in:   []string{"Fix MUL-82", "Closes MUL-1510 and ABC-7", ""},
			want: []string{"MUL-82", "MUL-1510", "ABC-7"},
		},
		{
			name: "dedupe_across_fields",
			in:   []string{"MUL-1", "MUL-1 again", "mul-1/branch"},
			want: []string{"MUL-1"},
		},
		{
			name: "ignore_email_and_versions",
			in:   []string{"reply@user-1 v1.2-3 here", "", ""},
			// Word-boundary regex still matches "user-1"; identifier prefix is
			// any 2..10 letters/digits, so this is intentional. The downstream
			// workspace prefix check in lookupIssueByIdentifier filters it.
			want: []string{"USER-1"},
		},
		{
			name: "no_match",
			in:   []string{"plain text", "no idents", ""},
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractIdentifiers(tc.in...)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extractIdentifiers() = %v, want %v", got, tc.want)
			}
		})
	}
}

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
			"body":       "",
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
	if uuidToString(deleted.WorkspaceID) != testWorkspaceID {
		t.Errorf("expected returned workspace_id %s, got %s", testWorkspaceID, uuidToString(deleted.WorkspaceID))
	}
	// Re-deleting must surface ErrNoRows so the handler can short-circuit
	// the broadcast (and not panic).
	if _, err := testHandler.Queries.DeleteGitHubInstallationByInstallationID(ctx, installationID); err == nil {
		t.Error("expected ErrNoRows on second delete, got nil")
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
		name                            string
		failed, passed, pending, total int64
		want                            string
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
	prRefs := make([]map[string]any, 0, len(prNumbers))
	for _, n := range prNumbers {
		prRefs = append(prRefs, map[string]any{"number": n})
	}
	payload := map[string]any{
		"action": "completed",
		"check_suite": map[string]any{
			"id":            suiteID,
			"head_sha":      headSHA,
			"status":        "completed",
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
