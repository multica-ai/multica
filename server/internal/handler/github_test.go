package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
			name: "single_character_prefix",
			in:   []string{"H-412: fix widget parity"},
			want: []string{"H-412"},
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

func TestExtractClosingIdentifiers(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "single_closes",
			in:   []string{"", "Closes MUL-1"},
			want: []string{"MUL-1"},
		},
		{
			name: "single_character_prefix",
			in:   []string{"", "Closes H-412"},
			want: []string{"H-412"},
		},
		{
			name: "all_keyword_inflections",
			in: []string{
				"",
				"close MUL-1\nclosed MUL-2\ncloses MUL-3\nfix MUL-4\nfixes MUL-5\nfixed MUL-6\nresolve MUL-7\nresolves MUL-8\nresolved MUL-9",
			},
			want: []string{"MUL-1", "MUL-2", "MUL-3", "MUL-4", "MUL-5", "MUL-6", "MUL-7", "MUL-8", "MUL-9"},
		},
		{
			name: "case_insensitive_and_colon",
			in:   []string{"CLOSES: MUL-1", "Fixes:MUL-2 resolves   MUL-3"},
			want: []string{"MUL-1", "MUL-2", "MUL-3"},
		},
		{
			name: "bare_reference_does_not_close",
			// The bug-report repro: only ABC-1 carries closing intent.
			// ABC-2/ABC-3 are linked (extractIdentifiers) but must not
			// appear in the closing set.
			in:   []string{"ABC-1: Lorem Ipsum", "Closes ABC-1. Follow up work planned in ABC-2. Unblocks ABC-3."},
			want: []string{"ABC-1"},
		},
		{
			name: "keyword_not_adjacent_does_not_close",
			// "Fix login MUL-1" — keyword present but the identifier is
			// not adjacent. Consistent with GitHub's closing-keyword
			// grammar; matches via extractIdentifiers for linking only.
			in:   []string{"Fix login MUL-1", ""},
			want: []string{},
		},
		{
			name: "dedupe_across_fields",
			in:   []string{"Closes MUL-1", "fixes mul-1"},
			want: []string{"MUL-1"},
		},
		{
			name: "no_match_on_disclosed_or_foreclose",
			// Word-boundary guards against keyword fragments embedded
			// in larger words ("Disclosed MUL-1", "Foreclose MUL-1").
			in:   []string{"Disclosed MUL-1 in foreclose MUL-2", ""},
			want: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractClosingIdentifiers(tc.in...)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extractClosingIdentifiers() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPRClaimedIdentifiers: the title and branch link, a closing keyword in the
// title or body links and closes, and a bare body mention claims nothing.
func TestPRClaimedIdentifiers(t *testing.T) {
	idents, closing := prClaimedIdentifiers(
		"ABC-1: Lorem Ipsum",
		"Closes ABC-4. Follow up work planned in ABC-2.",
		"fix/abc-3-login",
	)
	if want := []string{"ABC-1", "ABC-3", "ABC-4"}; !reflect.DeepEqual(idents, want) {
		t.Errorf("idents = %v, want %v", idents, want)
	}
	if want := []string{"ABC-4"}; !reflect.DeepEqual(closing, want) {
		t.Errorf("closing = %v, want %v", closing, want)
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

func TestIssuePullRequestResponseHidesUnavailableSnapshot(t *testing.T) {
	fetchedAt := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	row := db.ListPullRequestsByIssueRow{
		State:               "open",
		HeadSha:             "B",
		SnapshotHeadSha:     "A",
		SnapshotFetchedAt:   fetchedAt,
		ApiMergeable:        pgtype.Text{String: "CONFLICTING", Valid: true},
		ApiMergeStateStatus: pgtype.Text{String: "DIRTY", Valid: true},
		ChecksRollupState:   pgtype.Text{String: "FAILURE", Valid: true},
		ChecksTotal:         1,
		ChecksFailed:        1,
		FailedCheckNames:    []string{"backend"},
	}

	// A synchronize webhook moved the row to B while the last stored snapshot
	// still belongs to A. Old data must not be presented as fresh B data.
	resp := issuePullRequestRowToResponse(row, true)
	if resp.SnapshotAvailable == nil || *resp.SnapshotAvailable {
		t.Fatal("mismatched-head snapshot must be marked unavailable")
	}
	if resp.Mergeable != nil || resp.ChecksRollup != nil || resp.ChecksFailed != 0 {
		t.Fatalf("mismatched-head snapshot leaked into response: %+v", resp)
	}

	// Even a current stored snapshot is hidden when no App private key is
	// configured. This covers deployments that disable the feature after data
	// was already written.
	row.SnapshotHeadSha = "B"
	resp = issuePullRequestRowToResponse(row, false)
	if resp.SnapshotAvailable == nil || *resp.SnapshotAvailable {
		t.Fatal("disabled snapshot feature must be marked unavailable")
	}
	if resp.Mergeable != nil || resp.ChecksRollup != nil || resp.ChecksFailed != 0 {
		t.Fatalf("disabled feature exposed last-known snapshot: %+v", resp)
	}

	resp = issuePullRequestRowToResponse(row, true)
	if resp.SnapshotAvailable == nil || !*resp.SnapshotAvailable {
		t.Fatal("enabled current-head snapshot must be available")
	}
	if resp.Mergeable == nil || *resp.Mergeable != "conflicting" || resp.ChecksFailed != 1 {
		t.Fatalf("current snapshot was not exposed: %+v", resp)
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
	if parts := strings.Split(tok, "."); len(parts) != 3 {
		t.Fatalf("default return state has %d parts, want legacy 3-part format", len(parts))
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

func TestStateRoundTripWithRepositoryReturnTarget(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret-123")
	wsID := "11111111-2222-3333-4444-555555555555"

	tok, err := signStateForReturn(wsID, githubReturnToRepositories)
	if err != nil {
		t.Fatalf("signStateForReturn: %v", err)
	}
	if parts := strings.Split(tok, "."); len(parts) != 4 {
		t.Fatalf("repository return state has %d parts, want 4", len(parts))
	}
	gotWorkspaceID, gotReturnTo, ok := verifyStateWithReturn(tok)
	if !ok {
		t.Fatal("verifyStateWithReturn rejected a freshly-signed token")
	}
	if gotWorkspaceID != wsID || gotReturnTo != githubReturnToRepositories {
		t.Errorf(
			"verifyStateWithReturn() = (%q, %q), want (%q, %q)",
			gotWorkspaceID,
			gotReturnTo,
			wsID,
			githubReturnToRepositories,
		)
	}

	tampered := strings.Replace(tok, ".repositories.", ".github.", 1)
	if _, _, ok := verifyStateWithReturn(tampered); ok {
		t.Error("tampered return target should fail verification")
	}
}

func TestGitHubConnectRepositoryReturnTarget(t *testing.T) {
	t.Setenv("GITHUB_APP_SLUG", "multica-test")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret-123")
	wsID := "11111111-2222-3333-4444-555555555555"

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/workspaces/"+wsID+"/github/connect?return_to=repositories",
		nil,
	)
	req = withURLParam(req, "id", wsID)
	rec := httptest.NewRecorder()
	(&Handler{}).GitHubConnect(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GitHubConnect: got %d (%s)", rec.Code, rec.Body.String())
	}
	var body GitHubConnectResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode connect response: %v", err)
	}
	installURL, err := url.Parse(body.URL)
	if err != nil {
		t.Fatalf("parse install URL: %v", err)
	}
	_, returnTo, ok := verifyStateWithReturn(installURL.Query().Get("state"))
	if !ok || returnTo != githubReturnToRepositories {
		t.Fatalf("signed return target = %q, valid=%v, want repositories", returnTo, ok)
	}

	badReq := httptest.NewRequest(
		http.MethodGet,
		"/api/workspaces/"+wsID+"/github/connect?return_to=https://evil.example",
		nil,
	)
	badReq = withURLParam(badReq, "id", wsID)
	badRec := httptest.NewRecorder()
	(&Handler{}).GitHubConnect(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid return target: got %d, want 400", badRec.Code)
	}
}

func TestGitHubSetupCallbackRepositoryReturnTarget(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret-123")
	t.Setenv("FRONTEND_ORIGIN", "https://app.multica.test/")
	wsID := "11111111-2222-3333-4444-555555555555"
	state, err := signStateForReturn(wsID, githubReturnToRepositories)
	if err != nil {
		t.Fatalf("signStateForReturn: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/github/setup?installation_id=not-a-number&state="+url.QueryEscape(state),
		nil,
	)
	rec := httptest.NewRecorder()
	(&Handler{}).GitHubSetupCallback(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("GitHubSetupCallback: got %d, want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "https://app.multica.test/settings?tab=repositories&github_error=bad_installation_id" {
		t.Fatalf("redirect = %q, want repository settings error", got)
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
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR auto-merge test",
		"status": "in_progress",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
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

	req2 := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	w = testutil.Call(t, testHandler.HandleGitHubWebhook, req2).Want(http.StatusAccepted)

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

	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Already cancelled",
		"status": "cancelled",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
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

	req2 := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	w = testutil.Call(t, testHandler.HandleGitHubWebhook, req2)

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

	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Multi-PR auto-merge test",
		"status": "in_progress",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
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

		hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
		hookReq.Header.Set("X-GitHub-Event", "pull_request")
		hookReq.Header.Set("X-Hub-Signature-256", sig)
		testutil.Call(t, testHandler.HandleGitHubWebhook, hookReq).Want(http.StatusAccepted)
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

	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	hookReq.Header.Set("X-GitHub-Event", "pull_request")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	rec := testutil.Call(t, testHandler.HandleGitHubWebhook, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook %s pr=%d state=%s: expected 202, got %d (%s)",
			repo, prNumber, prState, rec.Code, rec.Body.String())
	}
}

// TestWebhook_ClosedSiblingAfterMerge pins the MUL-7429 rule for a sibling
// that closes without merging: "all linked PRs merged" is the only completion
// condition, so the issue waits and shows the unmerged PR. Removing that PR
// from the issue is a PR event and completes it.
func TestWebhook_ClosedSiblingAfterMerge(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "closed-sibling-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Closed sibling after merge",
		"status": "in_progress",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request_exclusion WHERE issue_id = $1`, created.ID)
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

	// Close PR B WITHOUT merging — a closed PR is not a merged one, so the
	// issue keeps waiting and names the unmerged PR.
	firePullRequestWebhook(t, secret, created.Identifier, installationID, "repo-b", 2, "closed")
	afterClose, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if afterClose.Status != "in_progress" {
		t.Fatalf("a closed-unmerged sibling must block auto-complete, got %q", afterClose.Status)
	}
	if got := prAutoCompleteStateForTest(t, created.ID); got.State != prAutoCompleteNotMerged || len(got.PullRequestIDs) != 1 {
		t.Fatalf("auto_complete = %+v, want not_merged naming the closed PR", got)
	}

	// Removing the abandoned PR leaves only merged work behind.
	unlinkPRForTest(t, created.ID, githubPRIDForTest(t, "repo-b", 2))
	final, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if final.Status != "done" {
		t.Errorf("expected issue 'done' after the unmerged PR was removed, got %q", final.Status)
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

	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "All closed no merge",
		"status": "in_progress",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
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

	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := testutil.Call(t, testHandler.HandleGitHubWebhook, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook pr=%d: expected 202, got %d (%s)", prNumber, rec.Code, rec.Body.String())
	}
}

// ── PR auto-complete (MUL-7429) ────────────────────────────────────────────
//
// The rule these tests pin: a PR links to an issue when its title or branch
// carries the identifier (or a member links it by hand); when every linked PR
// is merged, the issue moves to Done. Keywords have no special meaning and the
// body is not scanned. The decision runs only on a PR event for the issue.

// prAutoCompleteTestIssue creates an in_progress issue plus a GitHub
// installation for the test workspace, with cleanup for everything the
// webhook and the link endpoints can write.
func prAutoCompleteTestIssue(t *testing.T, title string, installationID int64) IssueResponse {
	t.Helper()
	ctx := context.Background()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "in_progress",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM issue_pull_request_exclusion WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM issue_pr_automation WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(bg, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(bg, `DELETE FROM github_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(bg, `DELETE FROM issue WHERE id = $1`, created.ID)
	})
	if installationID != 0 {
		if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
			WorkspaceID:    parseUUID(testWorkspaceID),
			InstallationID: installationID,
			AccountLogin:   fmt.Sprintf("acct-%d", installationID),
			AccountType:    "User",
		}); err != nil {
			t.Fatalf("CreateGitHubInstallation: %v", err)
		}
	}
	return created
}

func issueStatusForTest(t *testing.T, issueID string) string {
	t.Helper()
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	return issue.Status
}

func linkedPRCountForTest(t *testing.T, issueID string) int {
	t.Helper()
	rows, err := testHandler.Queries.ListPullRequestsByIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	return len(rows)
}

type issuePullRequestsBodyForTest struct {
	PullRequests []GitHubPullRequestResponse `json:"pull_requests"`
	AutoComplete prAutoCompleteResponse      `json:"auto_complete"`
}

func listIssuePRsForTest(t *testing.T, issueID string) issuePullRequestsBodyForTest {
	t.Helper()
	req := withURLParam(newRequest("GET", "/api/issues/"+issueID+"/pull-requests", nil), "id", issueID)
	w := testutil.Call(t, testHandler.ListPullRequestsForIssue, req).Want(http.StatusOK)
	var out issuePullRequestsBodyForTest
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode pull requests: %v", err)
	}
	return out
}

func prAutoCompleteStateForTest(t *testing.T, issueID string) prAutoCompleteResponse {
	t.Helper()
	return listIssuePRsForTest(t, issueID).AutoComplete
}

func unlinkPRForTest(t *testing.T, issueID, prID string) {
	t.Helper()
	req := newRequest("DELETE", "/api/issues/"+issueID+"/pull-requests/"+prID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueID)
	rctx.URLParams.Add("prId", prID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testutil.Call(t, testHandler.UnlinkIssuePullRequest, req).Want(http.StatusOK)
}

// linkMergedGitHubPRForTest mirrors a merged PR in the test workspace and
// links it to issueID automatically with a closing keyword, for tests that
// drive the completion path without a webhook.
func linkMergedGitHubPRForTest(t *testing.T, issueID, repo string) {
	t.Helper()
	ctx := context.Background()
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	pr, err := testHandler.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), InstallationID: 1, RepoOwner: "acme", RepoName: repo,
		PrNumber: 1, Title: "merged work", State: "merged", HtmlUrl: "https://github.com/acme/" + repo + "/pull/1",
		PrCreatedAt: now, PrUpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("upsert pr: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, pr.ID)
		testPool.Exec(bg, `DELETE FROM github_pull_request WHERE id = $1`, pr.ID)
	})
	if _, err := testHandler.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{IssueID: parseUUID(issueID), PullRequestID: pr.ID}); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := testHandler.Queries.SyncPullRequestCloseIntent(ctx, db.SyncPullRequestCloseIntentParams{PullRequestID: pr.ID, ClosingIssueIds: []pgtype.UUID{parseUUID(issueID)}}); err != nil {
		t.Fatalf("close intent: %v", err)
	}
}

func githubPRIDForTest(t *testing.T, repo string, number int32) string {
	t.Helper()
	pr, err := testHandler.Queries.GetGitHubPullRequest(context.Background(), db.GetGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), RepoOwner: "acme", RepoName: repo, PrNumber: number,
	})
	if err != nil {
		t.Fatalf("GetGitHubPullRequest %s#%d: %v", repo, number, err)
	}
	return uuidToString(pr.ID)
}

// TestWebhook_OnlyClosingKeywordCompletes is the repro from #3264: a title
// identifier links but does not complete, a body "Closes" links and completes,
// and a passing body mention ("Follow up in") links nothing.
func TestWebhook_OnlyClosingKeywordCompletes(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	secret := "body-mention-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 30264001
	primary := prAutoCompleteTestIssue(t, "primary work", installationID)
	closes := prAutoCompleteTestIssue(t, "closed only in body", 0)
	followUp := prAutoCompleteTestIssue(t, "follow up work", 0)

	title := primary.Identifier + ": Lorem Ipsum dolor sit amet"
	body := fmt.Sprintf("Closes %s. Follow up work planned in %s.", closes.Identifier, followUp.Identifier)
	fireBareWebhook(t, secret, installationID, 1, title, body, "fix/login")

	for _, issue := range []IssueResponse{primary, closes} {
		if n := linkedPRCountForTest(t, issue.ID); n != 1 {
			t.Errorf("%s should link, got %d rows", issue.Identifier, n)
		}
	}
	if got := issueStatusForTest(t, primary.ID); got != "in_progress" {
		t.Errorf("title-only issue: status = %q, want in_progress", got)
	}
	if got := prAutoCompleteStateForTest(t, primary.ID); got.State != prAutoCompleteNoCloseIntent {
		t.Errorf("title-only issue: auto_complete = %+v, want no_close_intent", got)
	}
	if got := issueStatusForTest(t, closes.ID); got != "done" {
		t.Errorf("closing-keyword issue: status = %q, want done", got)
	}
	if n := linkedPRCountForTest(t, followUp.ID); n != 0 {
		t.Errorf("a passing body mention must not link, got %d rows", n)
	}
	if got := issueStatusForTest(t, followUp.ID); got != "in_progress" {
		t.Errorf("mentioned issue: status = %q, want in_progress", got)
	}
}

// TestWebhook_TitleAndBranchLinkWithoutCompleting: a title prefix or a branch
// name links the PR, but only a closing keyword completes the issue — and a
// branch name is never read as one.
func TestWebhook_TitleAndBranchLinkWithoutCompleting(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	secret := "title-branch-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 30264002
	byTitle := prAutoCompleteTestIssue(t, "title link", installationID)
	byBranch := prAutoCompleteTestIssue(t, "branch link", 0)
	byTitleKeyword := prAutoCompleteTestIssue(t, "title keyword", 0)

	fireBareWebhook(t, secret, installationID, 2, byTitle.Identifier+": Fix login flow", "", "feat/login")
	fireBareWebhook(t, secret, installationID, 3, "Fix login flow", "", "fix/"+strings.ToLower(byBranch.Identifier)+"-login")
	fireBareWebhook(t, secret, installationID, 4, "Fixes "+byTitleKeyword.Identifier+": login flow", "", "feat/login")

	for _, issue := range []IssueResponse{byTitle, byBranch, byTitleKeyword} {
		if n := linkedPRCountForTest(t, issue.ID); n != 1 {
			t.Errorf("%s: expected 1 linked PR, got %d", issue.Identifier, n)
		}
	}
	for _, issue := range []IssueResponse{byTitle, byBranch} {
		if got := issueStatusForTest(t, issue.ID); got != "in_progress" {
			t.Errorf("%s: status = %q, want in_progress", issue.Identifier, got)
		}
	}
	if got := issueStatusForTest(t, byTitleKeyword.ID); got != "done" {
		t.Errorf("title closing keyword: status = %q, want done", got)
	}
	list := listIssuePRsForTest(t, byBranch.ID)
	if len(list.PullRequests) != 1 || list.PullRequests[0].LinkSource != "branch" {
		t.Errorf("link_source = %+v, want branch", list.PullRequests)
	}
}

// firePRWebhook fires a webhook for a single PR with caller-controlled
// title, body, branch, and lifecycle (open / edited / merged / closed without
// merge / edited after merge).
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

	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := testutil.Call(t, testHandler.HandleGitHubWebhook, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook pr=%d (%s): expected 202, got %d (%s)", prNumber, lifecycle, rec.Code, rec.Body.String())
	}
}

// TestWebhook_IdentifierRemovedBeforeMergeUnlinks: automatic links follow the
// PR's title and branch while it is open, so dropping the identifier removes
// the link and the later merge completes nothing.
func TestWebhook_IdentifierRemovedBeforeMergeUnlinks(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	secret := "identifier-removed-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 30264004
	created := prAutoCompleteTestIssue(t, "identifier removed", installationID)

	firePRWebhook(t, secret, installationID, 1, created.Identifier+": first attempt", "", "feat/attempt", "opened")
	if n := linkedPRCountForTest(t, created.ID); n != 1 {
		t.Fatalf("expected the title to link, got %d rows", n)
	}
	firePRWebhook(t, secret, installationID, 1, "Unrelated refactor", "", "feat/attempt", "edited")
	if n := linkedPRCountForTest(t, created.ID); n != 0 {
		t.Fatalf("removing the identifier while open should unlink, got %d rows", n)
	}
	firePRWebhook(t, secret, installationID, 1, "Unrelated refactor", "", "feat/attempt", "merged")
	if got := issueStatusForTest(t, created.ID); got != "in_progress" {
		t.Errorf("status = %q, want in_progress", got)
	}
}

// TestWebhook_CloseIntentFollowsPRTextUntilMerge: while the PR is open its
// close intent follows the text — downgrading "Closes" to a plain mention keeps
// the title link but not the completion, and adding it back restores it. It
// follows the PR on a manual link too.
func TestWebhook_CloseIntentFollowsPRTextUntilMerge(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	secret := "close-intent-follows-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 30264010
	byTitle := prAutoCompleteTestIssue(t, "close intent follows text", installationID)
	manual := prAutoCompleteTestIssue(t, "close intent on a manual link", 0)

	title := byTitle.Identifier + ": login flow"
	firePRWebhook(t, secret, installationID, 1, title, "Closes "+byTitle.Identifier, "feat/login", "opened")
	if got := prAutoCompleteStateForTest(t, byTitle.ID); got.State != prAutoCompleteWaiting {
		t.Fatalf("auto_complete = %+v, want waiting", got)
	}
	firePRWebhook(t, secret, installationID, 1, title, "Related to "+byTitle.Identifier, "feat/login", "edited")
	if got := prAutoCompleteStateForTest(t, byTitle.ID); got.State != prAutoCompleteNoCloseIntent {
		t.Fatalf("after dropping the keyword: auto_complete = %+v, want no_close_intent", got)
	}
	firePRWebhook(t, secret, installationID, 1, title, "Fixes "+byTitle.Identifier, "feat/login", "edited")
	firePRWebhook(t, secret, installationID, 1, title, "Fixes "+byTitle.Identifier, "feat/login", "merged")
	if got := issueStatusForTest(t, byTitle.ID); got != "done" {
		t.Errorf("keyword restored before merge: status = %q, want done", got)
	}

	firePRWebhook(t, secret, installationID, 2, "Session refactor", "", "refactor/session", "opened")
	req := withURLParam(newRequest("POST", "/api/issues/"+manual.ID+"/pull-requests", map[string]any{"url": "https://github.com/acme/widget/pull/2"}), "id", manual.ID)
	testutil.Call(t, testHandler.LinkIssuePullRequest, req).Want(http.StatusOK)
	firePRWebhook(t, secret, installationID, 2, "Session refactor", "Closes "+manual.Identifier, "refactor/session", "edited")
	firePRWebhook(t, secret, installationID, 2, "Session refactor", "Closes "+manual.Identifier, "refactor/session", "merged")
	if got := issueStatusForTest(t, manual.ID); got != "done" {
		t.Errorf("manual link whose PR says Closes: status = %q, want done", got)
	}
	if list := listIssuePRsForTest(t, manual.ID); len(list.PullRequests) != 1 || list.PullRequests[0].LinkSource != "manual" {
		t.Errorf("pull_requests = %+v, want the manual link kept", list.PullRequests)
	}
}

// TestWebhook_RemovedKeywordStopsCounting: dropping "Closes" from the PR text
// clears close intent on every link of the PR, not only on identifiers the PR
// still claims — a manual link, or an automatic link whose merge event (which
// carries the final text) arrives before the edit event (PR #8794 review).
func TestWebhook_RemovedKeywordStopsCounting(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	for _, tc := range []struct {
		name            string
		manual          bool
		editBeforeMerge bool
	}{
		{"manual link, keyword edited to a mention", true, true},
		{"automatic link, merge arrives before the edit", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "removed-keyword-secret"
			const installationID int64 = 30264011
			t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
			issue := prAutoCompleteTestIssue(t, tc.name, installationID)

			firePRWebhook(t, secret, installationID, 1, "Session refactor", "Closes "+issue.Identifier, "refactor/session", "opened")
			if tc.manual {
				req := withURLParam(newRequest("POST", "/api/issues/"+issue.ID+"/pull-requests", map[string]any{"url": "https://github.com/acme/widget/pull/1"}), "id", issue.ID)
				testutil.Call(t, testHandler.LinkIssuePullRequest, req).Want(http.StatusOK)
			}
			if tc.editBeforeMerge {
				firePRWebhook(t, secret, installationID, 1, "Session refactor", "Related to "+issue.Identifier, "refactor/session", "edited")
				if got := prAutoCompleteStateForTest(t, issue.ID); got.State != prAutoCompleteNoCloseIntent {
					t.Errorf("after removing the keyword: auto_complete = %+v, want no_close_intent", got)
				}
			}
			firePRWebhook(t, secret, installationID, 1, "Session refactor", "Related to "+issue.Identifier, "refactor/session", "merged")
			if got := issueStatusForTest(t, issue.ID); got != "in_progress" {
				t.Errorf("merged without a closing keyword: status = %q, want in_progress", got)
			}
		})
	}
}

// TestWebhook_AutoLinkOffStillReadsCloseIntent: turning auto-link off stops
// new links, not the reading of existing links' keywords — otherwise a
// keyword removed afterwards would still complete the issue (PR #8794 review).
func TestWebhook_AutoLinkOffStillReadsCloseIntent(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	const secret = "auto-link-off-close-intent-secret"
	const installationID int64 = 30264012
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	issue := prAutoCompleteTestIssue(t, "auto-link off after linking", installationID)
	var previous []byte
	dbfx.QueryRow(t, `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previous, testWorkspaceID)
	})

	title := issue.Identifier + ": session refactor"
	firePRWebhook(t, secret, installationID, 1, title, "Closes "+issue.Identifier, "refactor/session", "opened")
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"github_auto_link_prs_enabled": false}'::jsonb WHERE id = $1`, testWorkspaceID)
	firePRWebhook(t, secret, installationID, 1, title, "", "refactor/session", "edited")
	firePRWebhook(t, secret, installationID, 1, title, "", "refactor/session", "merged")
	if got := issueStatusForTest(t, issue.ID); got != "in_progress" {
		t.Errorf("keyword removed while auto-link was off: status = %q, want in_progress", got)
	}
}

// TestWebhook_PostMergeEditLinksWithoutCompleting: adding a closing keyword to
// a merged PR links it, but close intent is decided at merge time, so the
// issue does not complete. Removing it again after merge keeps the link: the
// work landed.
func TestWebhook_PostMergeEditLinksWithoutCompleting(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	secret := "post-merge-edit-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 30264005
	created := prAutoCompleteTestIssue(t, "post-merge link", installationID)

	firePRWebhook(t, secret, installationID, 1, "Dependency bump", "", "chore/bump", "merged")
	if n := linkedPRCountForTest(t, created.ID); n != 0 {
		t.Fatalf("PR without the identifier must not link, got %d rows", n)
	}
	firePRWebhook(t, secret, installationID, 1, "Dependency bump", "Closes "+created.Identifier, "chore/bump", "edited_merged")
	if n := linkedPRCountForTest(t, created.ID); n != 1 {
		t.Fatalf("post-merge edit should link, got %d rows", n)
	}
	if got := issueStatusForTest(t, created.ID); got != "in_progress" {
		t.Errorf("status = %q, want in_progress", got)
	}
	if got := prAutoCompleteStateForTest(t, created.ID); got.State != prAutoCompleteNoCloseIntent {
		t.Errorf("auto_complete = %+v, want no_close_intent", got)
	}
	firePRWebhook(t, secret, installationID, 1, "Dependency bump", "", "chore/bump", "edited_merged")
	if n := linkedPRCountForTest(t, created.ID); n != 1 {
		t.Errorf("a post-merge edit must not take delivered work off the issue, got %d rows", n)
	}
}

// TestWebhook_ReopenedIssueStaysOpenUntilNewPRMerges is the reason the
// decision is edge-triggered. A reopened issue whose PRs are all merged must
// not bounce back to Done on the next unrelated event of a merged PR; the next
// fix PR's merge completes it again — with no hidden per-issue switch flipped
// behind the user's back.
func TestWebhook_ReopenedIssueStaysOpenUntilNewPRMerges(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "reopen-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 30264006
	created := prAutoCompleteTestIssue(t, "reopen flow", installationID)

	firePRWebhook(t, secret, installationID, 1, "First fix", "Closes "+created.Identifier, "fix/one", "opened")
	firePRWebhook(t, secret, installationID, 1, "First fix", "Closes "+created.Identifier, "fix/one", "merged")
	if got := issueStatusForTest(t, created.ID); got != "done" {
		t.Fatalf("status after first merge = %q, want done", got)
	}

	if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: parseUUID(created.ID), Status: "in_progress", WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	// A later event on the already-merged PR (label, edit, redelivery).
	firePRWebhook(t, secret, installationID, 1, "First fix (edited)", "Closes "+created.Identifier, "fix/one", "edited_merged")
	if got := issueStatusForTest(t, created.ID); got != "in_progress" {
		t.Fatalf("reopened issue bounced back to %q on an event of a merged PR", got)
	}
	if got := prAutoCompleteStateForTest(t, created.ID); got.State != prAutoCompleteAllMerged || got.IssueDisabled {
		t.Fatalf("auto_complete = %+v, want all_merged with the issue still enabled", got)
	}

	firePRWebhook(t, secret, installationID, 2, "Follow-up fix", "Fixes "+created.Identifier, "fix/two", "opened")
	if got := prAutoCompleteStateForTest(t, created.ID); got.State != prAutoCompleteWaiting {
		t.Fatalf("auto_complete = %+v, want waiting on the new PR", got)
	}
	firePRWebhook(t, secret, installationID, 2, "Follow-up fix", "Fixes "+created.Identifier, "fix/two", "merged")
	if got := issueStatusForTest(t, created.ID); got != "done" {
		t.Errorf("status after the follow-up merged = %q, want done", got)
	}
}

// TestUnlinkIssuePullRequest_IsRememberedAndCompletes: a removed PR stays
// removed across webhook redeliveries, and removing the last unmerged PR is
// the PR event that completes the issue.
func TestUnlinkIssuePullRequest_IsRememberedAndCompletes(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	secret := "unlink-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 30264007
	created := prAutoCompleteTestIssue(t, "unlink flow", installationID)

	firePRWebhook(t, secret, installationID, 1, created.Identifier+": abandoned approach", "", "try/one", "opened")
	firePRWebhook(t, secret, installationID, 2, created.Identifier+": real fix", "Closes "+created.Identifier, "fix/two", "opened")
	firePRWebhook(t, secret, installationID, 2, created.Identifier+": real fix", "Closes "+created.Identifier, "fix/two", "merged")
	if got := issueStatusForTest(t, created.ID); got != "in_progress" {
		t.Fatalf("status = %q, want in_progress while #1 is open", got)
	}

	unlinkPRForTest(t, created.ID, githubPRIDForTest(t, "widget", 1))
	if got := issueStatusForTest(t, created.ID); got != "done" {
		t.Fatalf("status after removing the open PR = %q, want done", got)
	}

	firePRWebhook(t, secret, installationID, 1, created.Identifier+": abandoned approach", "", "try/one", "edited")
	if n := linkedPRCountForTest(t, created.ID); n != 1 {
		t.Errorf("a removed PR must not be re-linked by the next webhook, got %d rows", n)
	}
}

// TestLinkIssuePullRequest_ByURL: a pasted PR URL (with trailing path/query)
// links a mirrored PR by hand, and manual links survive title edits. A manual
// link carries no close intent of its own, so linking a merged PR that never
// said "Closes" does not complete the issue. An unknown URL is 404.
func TestLinkIssuePullRequest_ByURL(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	secret := "manual-link-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 30264008
	created := prAutoCompleteTestIssue(t, "manual link", installationID)

	firePRWebhook(t, secret, installationID, 41, "Refactor session helper", "", "refactor/session", "merged")

	link := func(url string) *testutil.Response {
		req := withURLParam(newRequest("POST", "/api/issues/"+created.ID+"/pull-requests", map[string]any{"url": url}), "id", created.ID)
		return testutil.Call(t, testHandler.LinkIssuePullRequest, req)
	}
	if rec := link("https://github.com/acme/widget/pull/999"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown PR: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := link("github.com/Acme/widget/pull/41/files?w=1"); rec.Code != http.StatusOK {
		t.Fatalf("link: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	list := listIssuePRsForTest(t, created.ID)
	if len(list.PullRequests) != 1 || list.PullRequests[0].LinkSource != "manual" {
		t.Fatalf("pull_requests = %+v, want one manual link", list.PullRequests)
	}
	if got := issueStatusForTest(t, created.ID); got != "in_progress" {
		t.Errorf("linking a merged PR without a closing keyword: status = %q, want in_progress", got)
	}
	if list.AutoComplete.State != prAutoCompleteNoCloseIntent {
		t.Errorf("auto_complete = %+v, want no_close_intent", list.AutoComplete)
	}
	firePRWebhook(t, secret, installationID, 41, "Refactor session helper (renamed)", "", "refactor/session", "edited_merged")
	if n := linkedPRCountForTest(t, created.ID); n != 1 {
		t.Errorf("a manual link must survive webhook reconciliation, got %d rows", n)
	}
}

// TestPRAutoComplete_WorkspaceAndIssueSwitches: turning the workspace setting
// off keeps PRs linked but writes no status; turning it back on completes
// nothing retroactively. The per-issue switch does the same for one issue and
// is recorded on the timeline.
func TestPRAutoComplete_WorkspaceAndIssueSwitches(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "switches-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	const installationID int64 = 30264009
	wsOff := prAutoCompleteTestIssue(t, "workspace switch", installationID)
	issueOff := prAutoCompleteTestIssue(t, "issue switch", 0)

	var previous []byte
	dbfx.QueryRow(t, `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previous, testWorkspaceID)
	})
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"pr_auto_complete_enabled": false}'::jsonb WHERE id = $1`, testWorkspaceID)

	firePRWebhook(t, secret, installationID, 1, "Work", "Closes "+wsOff.Identifier, "feat/one", "merged")
	if n := linkedPRCountForTest(t, wsOff.ID); n != 1 {
		t.Fatalf("setting off must still link, got %d rows", n)
	}
	if got := issueStatusForTest(t, wsOff.ID); got != "in_progress" {
		t.Fatalf("setting off: status = %q, want in_progress", got)
	}
	if got := prAutoCompleteStateForTest(t, wsOff.ID); got.State != prAutoCompleteWorkspaceDisabled || got.WorkspaceEnabled {
		t.Fatalf("auto_complete = %+v, want workspace_disabled", got)
	}
	dbfx.Exec(t, `UPDATE workspace SET settings = settings || '{"pr_auto_complete_enabled": true}'::jsonb WHERE id = $1`, testWorkspaceID)
	if got := issueStatusForTest(t, wsOff.ID); got != "in_progress" {
		t.Fatalf("turning the setting on must not complete history, got %q", got)
	}

	put := withURLParam(newRequest("PUT", "/api/issues/"+issueOff.ID+"/pr-auto-complete", map[string]any{"disabled": true}), "id", issueOff.ID)
	testutil.Call(t, testHandler.SetIssuePRAutoComplete, put).Want(http.StatusOK)
	firePRWebhook(t, secret, installationID, 2, "Work", "Closes "+issueOff.Identifier, "feat/two", "merged")
	if got := issueStatusForTest(t, issueOff.ID); got != "in_progress" {
		t.Fatalf("issue switch off: status = %q, want in_progress", got)
	}
	if got := prAutoCompleteStateForTest(t, issueOff.ID); got.State != prAutoCompleteIssueDisabled || !got.IssueDisabled {
		t.Fatalf("auto_complete = %+v, want issue_disabled", got)
	}
	var recorded int
	dbfx.QueryRow(t, `SELECT count(*) FROM activity_log WHERE issue_id = $1 AND action = 'pr_auto_complete_changed'`, issueOff.ID).Scan(&recorded)
	if recorded != 1 {
		t.Errorf("expected the switch change on the timeline, got %d entries", recorded)
	}
	_ = ctx
}

func TestNormalizePullRequestURL(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"https://github.com/Acme/Widget/pull/12", "https://github.com/acme/widget/pull/12", true},
		{"github.com/acme/widget/pull/12/files?diff=split#r1", "https://github.com/acme/widget/pull/12", true},
		{"https://gitlab.example.com/group/sub/repo/-/merge_requests/7/diffs", "https://gitlab.example.com/group/sub/repo/-/merge_requests/7", true},
		{"https://git.example.com/acme/widget/pulls/3", "https://git.example.com/acme/widget/pulls/3", true},
		{"https://github.com/acme/widget/issues/12", "", false},
		{"not a url", "", false},
		{"", "", false},
	} {
		got, ok := normalizePullRequestURL(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("normalizePullRequestURL(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
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

	hookReq := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(raw))
	hookReq.Header.Set("X-GitHub-Event", "pull_request")
	hookReq.Header.Set("X-Hub-Signature-256", sig)
	rec := testutil.Call(t, testHandler.HandleGitHubWebhook, hookReq)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook %s pr=%d action=%s: expected 202, got %d (%s)",
			repo, prNumber, action, rec.Code, rec.Body.String())
	}
}

func setupPRTestIssue(t *testing.T, ctx context.Context, secret string) (IssueResponse, int64) {
	t.Helper()
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR CI test",
		"status": "in_progress",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
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
	_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email LIKE $1`, "github-routes-"+slug+"-%")

	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "GitHub Routes Role Gating",
		"slug":         slug,
		"description":  "github routes role gating",
		"issue_prefix": "GRG",
	})

	// Three workspace members + one outsider. We attach the requesting user
	// via the X-User-ID header so the middleware reads them off the auth
	// boundary just like a real request.
	mkUser := func(t *testing.T, label string) string {
		t.Helper()
		var id string
		email := fmt.Sprintf("github-routes-%s-%s@multica.ai", slug, label)
		dbfx.QueryRow(t, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "GHR "+label, email).Scan(&id)
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
		dbfx.Exec(t, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)
`, wsID, m.userID, m.role)
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
			r.Get("/github/installations/{installationId}/repositories", testHandler.ListGitHubInstallationRepositories)
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

	t.Run("GET repositories remains owner/admin only", func(t *testing.T) {
		path := "/api/workspaces/" + wsID + "/github/installations/" + uuidToString(createdInst.ID) + "/repositories"
		if code := exercise(t, http.MethodGet, path, memberUserID); code != http.StatusForbidden {
			t.Errorf("member GET repositories: want 403, got %d", code)
		}
		if code := exercise(t, http.MethodGet, path, outsiderUserID); code != http.StatusNotFound {
			t.Errorf("outsider GET repositories: want 404, got %d", code)
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
		dbfx.QueryRow(t, `SELECT COUNT(*) FROM github_installation WHERE id = $1`, uuidToString(createdInst.ID)).Scan(&remaining)
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
// reaches `done`, and that path goes through maybeAutoCompleteIssue — not the
// HTTP UpdateIssue / BatchUpdateIssues handlers that originally wired up
// notifyParentOfChildDone. Without the helper call inside maybeAutoCompleteIssue,
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
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR-merge parent " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	w := testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
	var parent IssueResponse
	json.NewDecoder(w.Body).Decode(&parent)

	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "PR-merge child " + time.Now().Format(time.RFC3339Nano),
		"status":          "in_progress",
		"parent_issue_id": parent.ID,
	})
	w = testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusCreated)
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

	req2 := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req2.Header.Set("X-GitHub-Event", "pull_request")
	req2.Header.Set("X-Hub-Signature-256", sig)
	w = testutil.Call(t, testHandler.HandleGitHubWebhook, req2).Want(http.StatusAccepted)

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
	dbfx.QueryRow(t,
		`SELECT count(*) FROM comment WHERE issue_id = $1 AND author_type = 'system'`,
		parent.ID,
	).Scan(&sysCount)
	if sysCount != 1 {
		t.Fatalf("expected 1 system comment on parent after PR-merge auto-done, got %d", sysCount)
	}

	var content string
	dbfx.QueryRow(t,
		`SELECT content FROM comment WHERE issue_id = $1 AND author_type = 'system' LIMIT 1`,
		parent.ID,
	).Scan(&content)
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

// generateTestRSAKeyPEM returns the shared RSA-2048 test key's PKCS#1 PEM
// encoding (the format GitHub hands operators when they create the App)
// and the parsed *rsa.PrivateKey for verification.
func generateTestRSAKeyPEM(t *testing.T) (pemBytes []byte, key *rsa.PrivateKey) {
	t.Helper()
	k := sharedTestRSAKey(t)
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

func TestFetchGitHubInstallationRepositories(t *testing.T) {
	pemBytes, key := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "424242")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))

	const installationID int64 = 314159
	var tokenRevoked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/314159/access_tokens":
			bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if bearer == "" {
				http.Error(w, "missing app jwt", http.StatusUnauthorized)
				return
			}
			if _, err := jwt.Parse(bearer, func(token *jwt.Token) (any, error) {
				return &key.PublicKey, nil
			}); err != nil {
				http.Error(w, "bad app jwt", http.StatusUnauthorized)
				return
			}
			var tokenRequest struct {
				Permissions map[string]string `json:"permissions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&tokenRequest); err != nil {
				http.Error(w, "bad token request", http.StatusBadRequest)
				return
			}
			if !reflect.DeepEqual(tokenRequest.Permissions, map[string]string{"metadata": "read"}) {
				http.Error(w, "overbroad token permissions", http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"token": "installation-secret"})
		case r.Method == http.MethodGet && r.URL.Path == "/installation/repositories":
			if got := r.Header.Get("Authorization"); got != "Bearer installation-secret" {
				http.Error(w, "bad installation token", http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "1" {
				http.Error(w, "bad pagination", http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"total_count": 3,
				"repositories": []map[string]any{{
					"id":             9,
					"full_name":      "acme/private-repo",
					"html_url":       "https://github.com/acme/private-repo",
					"clone_url":      "https://github.com/acme/private-repo.git",
					"description":    "Private repository",
					"private":        true,
					"archived":       false,
					"default_branch": "main",
				}},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/installation/token":
			tokenRevoked = r.Header.Get("Authorization") == "Bearer installation-secret"
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	got, err := fetchGitHubInstallationRepositories(
		context.Background(),
		installationID,
		2,
		1,
	)
	if err != nil {
		t.Fatalf("fetchGitHubInstallationRepositories: %v", err)
	}
	if len(got.Repositories) != 1 {
		t.Fatalf("repositories = %d, want 1", len(got.Repositories))
	}
	repository := got.Repositories[0]
	if repository.FullName != "acme/private-repo" || !repository.Private {
		t.Errorf("repository = %+v, want mapped private repository", repository)
	}
	if got.TotalCount != 3 || got.NextPage == nil || *got.NextPage != 3 {
		t.Errorf("pagination = total %d, next %v; want total 3, next 3", got.TotalCount, got.NextPage)
	}
	if !tokenRevoked {
		t.Error("installation token was not revoked after repository listing")
	}
}

func TestListGitHubInstallationRepositoriesRejectsCrossWorkspaceRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const installationID int64 = 818181
	row, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "cross-workspace-acct",
		AccountType:    "Organization",
	})
	if err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})

	otherWorkspaceID := "11111111-2222-3333-4444-555555555555"
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/workspaces/"+otherWorkspaceID+"/github/installations/"+uuidToString(row.ID)+"/repositories",
		nil,
	)
	rec := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Get(
		"/api/workspaces/{id}/github/installations/{installationId}/repositories",
		testHandler.ListGitHubInstallationRepositories,
	)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace row: got %d (%s), want 404", rec.Code, rec.Body.String())
	}
}

func TestGitHubWebhook_UnconfiguredDeploymentReturns404(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	(&Handler{}).HandleGitHubWebhook(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when webhook is unconfigured, got %d (%s)", rec.Code, rec.Body.String())
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

	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "installation")
	req.Header.Set("X-Hub-Signature-256", sig)
	testutil.Call(t, testHandler.HandleGitHubWebhook, req).Want(http.StatusAccepted)

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
	dbfx.QueryRow(t,
		`SELECT account_login FROM github_pending_installation WHERE installation_id = $1`,
		installationID,
	).Scan(&pendingLogin)
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
	dbfx.QueryRow(t,
		`SELECT count(*) FROM github_pending_installation WHERE installation_id = $1`,
		installationID,
	).Scan(&pendingCount)
	if pendingCount != 0 {
		t.Fatalf("pending installation row should be consumed, got count %d", pendingCount)
	}
}

// TestWebhook_PullRequest_FansOutToBoundWorkspaces is the MUL-4343 change: one
// GitHub App installation bound to several workspaces must deliver a repo's PR
// events to EVERY bound workspace. Each workspace mirrors the PR and auto-links
// it against its own issues (its own prefix), replacing the old single-workspace
// routing that dropped the event for every workspace but one.
func TestWebhook_PullRequest_FansOutToBoundWorkspaces(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "fanout-pr-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	// Workspace B is the shared test workspace (prefix HAN) and owns the issue.
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "fan-out PR test",
		"status": "in_progress",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	const repo = "fanout-repo"
	const prNumber int32 = 4343
	const installationID int64 = 778899101

	// Workspace A is bound to the SAME installation but has no matching issue;
	// it must still receive the PR mirror.
	testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, "fanout-pr-ws-a")
	wsA, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name: "fanout-pr-ws-a", Slug: "fanout-pr-ws-a", IssuePrefix: "FPA",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	// Bind the installation to BOTH workspaces.
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID: parseUUID(testWorkspaceID), InstallationID: installationID, AccountLogin: "acme", AccountType: "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation B: %v", err)
	}
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID: wsA.ID, InstallationID: installationID, AccountLogin: "acme", AccountType: "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation A: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE repo_owner = 'acme' AND repo_name = $1`, repo)
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsA.ID)
	})

	// One PR whose title references workspace B's issue.
	firePullRequestWebhook(t, secret, created.Identifier, installationID, repo, prNumber, "open")

	// The PR must be mirrored in BOTH bound workspaces.
	if _, err := testHandler.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), RepoOwner: "acme", RepoName: repo, PrNumber: prNumber,
	}); err != nil {
		t.Fatalf("expected PR mirrored in workspace B: %v", err)
	}
	prA, err := testHandler.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
		WorkspaceID: wsA.ID, RepoOwner: "acme", RepoName: repo, PrNumber: prNumber,
	})
	if err != nil {
		t.Fatalf("expected PR fanned out to workspace A: %v", err)
	}

	// Workspace B links its own issue; workspace A (no matching issue) does not.
	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked PR on workspace B's issue, got %d", len(linked))
	}
	if issues, _ := testHandler.Queries.ListIssueIDsForPullRequest(ctx, prA.ID); len(issues) != 0 {
		t.Fatalf("workspace A has no matching issue, expected 0 links, got %d", len(issues))
	}
}

// TestWebhook_PullRequest_AmbiguousCloseAcrossWorkspaces is the #6804
// regression under MUL-7429. Two workspaces bound to the same installation share
// an issue prefix (permitted by #2797) and both own a real issue at the same
// number, so a PR titled "Fix AMB-<n>" resolves in BOTH after the #5183
// fan-out. A link now means "this PR delivers the issue" and a merged link set
// completes it, so an identifier nothing in the event can attribute must not
// link in either workspace — otherwise the merge would complete an issue in a
// workspace that has nothing to do with the PR. A member links it by hand.
func TestWebhook_PullRequest_AmbiguousCloseAcrossWorkspaces(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "ambiguous-close-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)

	// Both workspaces answer to "AMB" — the collision #2797 allows.
	setWorkspaceIssuePrefixForTest(t, "AMB")

	// Workspace B is the shared test workspace and owns the issue the PR is
	// really for.
	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "ambiguous close test",
		"status": "in_progress",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issueB IssueResponse
	json.NewDecoder(w.Body).Decode(&issueB)

	const repo = "ambiguous-close-repo"
	const prNumber int32 = 6804
	const installationID int64 = 660480000

	testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, "ambiguous-close-ws-a")
	wsA, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name: "ambiguous-close-ws-a", Slug: "ambiguous-close-ws-a", IssuePrefix: "AMB",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	// Workspace A's own issue at the SAME number. `number` is an explicit
	// column, so we can place the collision directly instead of pumping the
	// workspace's issue counter up to B's.
	issueA, err := testHandler.Queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: wsA.ID,
		Title:       "unrelated issue that happens to share a number",
		Status:      "in_progress",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   parseUUID(testUserID),
		Number:      issueB.Number,
	})
	if err != nil {
		t.Fatalf("CreateIssue in workspace A: %v", err)
	}

	for _, wsID := range []pgtype.UUID{parseUUID(testWorkspaceID), wsA.ID} {
		if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
			WorkspaceID: wsID, InstallationID: installationID, AccountLogin: "acme", AccountType: "User",
		}); err != nil {
			t.Fatalf("CreateGitHubInstallation: %v", err)
		}
	}

	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM issue_pull_request WHERE issue_id = ANY($1)`,
			[]string{issueB.ID, uuidToString(issueA.ID)})
		testPool.Exec(bg, `DELETE FROM github_pull_request WHERE repo_owner = 'acme' AND repo_name = $1`, repo)
		testPool.Exec(bg, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(bg, `DELETE FROM activity_log WHERE issue_id = ANY($1)`,
			[]string{issueB.ID, uuidToString(issueA.ID)})
		testPool.Exec(bg, `DELETE FROM issue WHERE id = ANY($1)`,
			[]string{issueB.ID, uuidToString(issueA.ID)})
		testPool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, wsA.ID)
	})

	// A stale automatic link from before this rule must not survive either.
	firePullRequestWebhook(t, secret, issueB.Identifier, installationID, repo, prNumber, "open")
	prB := githubPRIDForTest(t, repo, prNumber)
	dbfx.Exec(t, `INSERT INTO issue_pull_request (issue_id, pull_request_id, linked_by_type) VALUES ($1, $2, 'system')`, issueB.ID, prB)
	firePullRequestWebhook(t, secret, issueB.Identifier, installationID, repo, prNumber, "merged")

	for _, tc := range []struct {
		name    string
		issueID pgtype.UUID
	}{
		{"workspace B (owns the PR)", parseUUID(issueB.ID)},
		{"workspace A (collision)", issueA.ID},
	} {
		linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, tc.issueID)
		if err != nil {
			t.Fatalf("%s: ListPullRequestsByIssue: %v", tc.name, err)
		}
		if len(linked) != 0 {
			t.Fatalf("%s: an ambiguous identifier must not auto-link, got %d links", tc.name, len(linked))
		}

		issue, err := testHandler.Queries.GetIssue(ctx, tc.issueID)
		if err != nil {
			t.Fatalf("%s: GetIssue: %v", tc.name, err)
		}
		if issue.Status == "done" {
			t.Errorf("%s: issue was auto-advanced to done on an ambiguous close (#6804)", tc.name)
		}
	}
}

// TestPRLinkPolicyPermits pins the fail-closed invariant at the type level:
// the policy is an allowlist, so anything it was not able to prove is denied.
func TestPRLinkPolicyPermits(t *testing.T) {
	const wsA, wsB = "workspace-a", "workspace-b"

	for _, tc := range []struct {
		name   string
		policy prLinkPolicy
		ws     string
		want   bool
	}{
		{
			name:   "zero value denies",
			policy: prLinkPolicy{},
			ws:     wsA,
			want:   false,
		},
		{
			name:   "single-binding delivery is unrestricted",
			policy: prLinkPolicy{unrestricted: true},
			ws:     wsA,
			want:   true,
		},
		{
			name:   "recorded owner may link",
			policy: prLinkPolicy{owner: map[string]string{"ABC-100": wsA}},
			ws:     wsA,
			want:   true,
		},
		{
			// A workspace that grew a same-numbered issue after the scan is not
			// the recorded owner, so it still cannot link.
			name:   "workspace that is not the recorded owner may not link",
			policy: prLinkPolicy{owner: map[string]string{"ABC-100": wsA}},
			ws:     wsB,
			want:   false,
		},
		{
			name:   "ambiguous identifier is denied everywhere",
			policy: prLinkPolicy{owner: map[string]string{}, ambiguous: map[string]bool{"ABC-100": true}},
			ws:     wsA,
			want:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.permits("ABC-100", tc.ws); got != tc.want {
				t.Errorf("permits(ABC-100, %s) = %v, want %v", tc.ws, got, tc.want)
			}
		})
	}
}

// TestPRLinkPolicyPermitsClose: auto-link decides which links get created, not
// whether a closing keyword counts on links a workspace already has. A
// workspace that is the only resolver acts with auto-link off; a resolver
// that shares the identifier with an auto-linking owner does not.
func TestPRLinkPolicyPermitsClose(t *testing.T) {
	const wsA, wsB = "workspace-a", "workspace-b"

	for _, tc := range []struct {
		name   string
		policy prLinkPolicy
		ws     string
		want   bool
	}{
		{"zero value denies", prLinkPolicy{}, wsA, false},
		{"single-binding delivery is unrestricted", prLinkPolicy{unrestricted: true}, wsA, true},
		{"auto-linking owner acts", prLinkPolicy{owner: map[string]string{"ABC-100": wsA}}, wsA, true},
		{"sole resolver with auto-link off acts", prLinkPolicy{sole: map[string]string{"ABC-100": wsA}}, wsA, true},
		{"resolver beside an auto-linking owner does not act", prLinkPolicy{owner: map[string]string{"ABC-100": wsA}}, wsB, false},
		{"ambiguous identifier is denied everywhere", prLinkPolicy{ambiguous: map[string]bool{"ABC-100": true}}, wsA, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.permitsClose("ABC-100", tc.ws); got != tc.want {
				t.Errorf("permitsClose(ABC-100, %s) = %v, want %v", tc.ws, got, tc.want)
			}
		})
	}
}

// TestWebhook_AutoLinkOffKeepsValidClosingKeyword: turning auto-link off in a
// workspace that shares its installation with another must not drop a closing
// keyword only this workspace resolves — the merge still completes the issue
// (PR #8794 re-review).
func TestWebhook_AutoLinkOffKeepsValidClosingKeyword(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	for _, shared := range []bool{false, true} {
		name := "single workspace"
		if shared {
			name = "shared installation, unique prefix"
		}
		t.Run(name, func(t *testing.T) {
			const secret = "auto-link-off-keeps-keyword-secret"
			const installationID int64 = 30264013
			t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
			setWorkspaceIssuePrefixForTest(t, "RVA")
			if shared {
				bindSecondWorkspaceForTest(t, "auto-link-off-other-workspace", "RVB", installationID)
			}
			issue := prAutoCompleteTestIssue(t, name, installationID)
			var previous []byte
			dbfx.QueryRow(t, `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous)
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previous, testWorkspaceID)
			})

			firePRWebhook(t, secret, installationID, 1, "Session refactor", "Closes "+issue.Identifier, "refactor/session", "opened")
			if got := prAutoCompleteStateForTest(t, issue.ID); got.State != prAutoCompleteWaiting {
				t.Fatalf("before turning auto-link off: auto_complete = %+v, want waiting", got)
			}
			dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"github_auto_link_prs_enabled": false}'::jsonb WHERE id = $1`, testWorkspaceID)
			firePRWebhook(t, secret, installationID, 1, "Session refactor", "Closes "+issue.Identifier, "refactor/session", "merged")
			if got := issueStatusForTest(t, issue.ID); got != "done" {
				t.Errorf("keyword kept, auto-link off: status = %q, want done (auto_complete = %+v)", got, prAutoCompleteStateForTest(t, issue.ID))
			}
		})
	}
}

// TestWebhook_PullRequest_UniqueResolverAmongBindingsStillAutoCompletes is the
// other half of the #6804 fix: withholding links must be scoped to the
// identifiers we could not attribute. Two workspaces share an installation and
// a prefix, but only one of them actually has an issue at that number — that is
// a proven unique owner, so auto-complete must still fire for it.
func TestWebhook_PullRequest_UniqueResolverAmongBindingsStillAutoCompletes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "unique-resolver-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	setWorkspaceIssuePrefixForTest(t, "UNQ")

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "unique resolver test",
		"status": "in_progress",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issueB IssueResponse
	json.NewDecoder(w.Body).Decode(&issueB)

	const repo = "unique-resolver-repo"
	const prNumber int32 = 6809
	const installationID int64 = 660480001

	// Workspace A shares the prefix but has no issue at all, so it can never be
	// a second resolver.
	bindSecondWorkspaceForTest(t, "unique-resolver-ws-a", "UNQ", installationID)
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID: parseUUID(testWorkspaceID), InstallationID: installationID, AccountLogin: "acme", AccountType: "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation B: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM issue_pull_request WHERE issue_id = $1`, issueB.ID)
		testPool.Exec(bg, `DELETE FROM github_pull_request WHERE repo_owner = 'acme' AND repo_name = $1`, repo)
		testPool.Exec(bg, `DELETE FROM activity_log WHERE issue_id = $1`, issueB.ID)
		testPool.Exec(bg, `DELETE FROM issue WHERE id = $1`, issueB.ID)
	})

	firePullRequestWebhook(t, secret, issueB.Identifier, installationID, repo, prNumber, "open")
	firePullRequestWebhook(t, secret, issueB.Identifier, installationID, repo, prNumber, "merged")

	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueB.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Status != "done" {
		t.Errorf("issue status = %q, want done — a proven unique resolver must keep auto-complete", issue.Status)
	}
}

// TestWebhook_PullRequest_UnreadableWorkspaceWithholdsCloseIntent covers the
// fail-closed half of resolvePRLinkPolicy. The setup is identical to the
// unique-resolver test above — one real resolver, which would auto-complete —
// except that a bound workspace's settings cannot be parsed. That workspace
// might or might not be a second resolver, and since we cannot rule it out, the
// delivery changes no link and no status: a transient read failure must never
// promote an ambiguous identifier into an auto-complete.
func TestWebhook_PullRequest_UnreadableWorkspaceWithholdsCloseIntent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "unreadable-ws-secret"
	t.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	setWorkspaceIssuePrefixForTest(t, "UNR")

	w := httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "unreadable workspace test",
		"status": "in_progress",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issueB IssueResponse
	json.NewDecoder(w.Body).Decode(&issueB)

	const repo = "unreadable-ws-repo"
	const prNumber int32 = 6810
	const installationID int64 = 660480002

	wsA := bindSecondWorkspaceForTest(t, "unreadable-ws-a", "UNR", installationID)
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID: parseUUID(testWorkspaceID), InstallationID: installationID, AccountLogin: "acme", AccountType: "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation B: %v", err)
	}
	// Valid JSONB, but the auto-link flag is not a bool — the settings blob
	// parses in Postgres and fails in the handler, which is exactly the
	// "we could not find out" case.
	dbfx.Exec(t,
		`UPDATE workspace SET settings = '{"github_auto_link_prs_enabled": 5}'::jsonb WHERE id = $1`, wsA,
	)

	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM issue_pull_request WHERE issue_id = $1`, issueB.ID)
		testPool.Exec(bg, `DELETE FROM github_pull_request WHERE repo_owner = 'acme' AND repo_name = $1`, repo)
		testPool.Exec(bg, `DELETE FROM activity_log WHERE issue_id = $1`, issueB.ID)
		testPool.Exec(bg, `DELETE FROM issue WHERE id = $1`, issueB.ID)
	})

	firePullRequestWebhook(t, secret, issueB.Identifier, installationID, repo, prNumber, "open")
	firePullRequestWebhook(t, secret, issueB.Identifier, installationID, repo, prNumber, "merged")

	if n := linkedPRCountForTest(t, issueB.ID); n != 0 {
		t.Errorf("no link may be written when a bound workspace could not be inspected, got %d", n)
	}

	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueB.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue.Status == "done" {
		t.Error("issue was auto-advanced despite an unreadable bound workspace — the scan failed open")
	}
}

// bindSecondWorkspaceForTest creates a throwaway workspace with the given issue
// prefix, binds it to installationID, and registers cleanup for both. Returns
// the new workspace id.
func bindSecondWorkspaceForTest(t *testing.T, slug, prefix string, installationID int64) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	ws, err := testHandler.Queries.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name: slug, Slug: slug, IssuePrefix: prefix,
	})
	if err != nil {
		t.Fatalf("CreateWorkspace %s: %v", slug, err)
	}
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID: ws.ID, InstallationID: installationID, AccountLogin: "acme", AccountType: "User",
	}); err != nil {
		t.Fatalf("CreateGitHubInstallation %s: %v", slug, err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		testPool.Exec(bg, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
		testPool.Exec(bg, `DELETE FROM workspace WHERE id = $1`, ws.ID)
	})
	return ws.ID
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
	req := httptest.NewRequest("POST", "/api/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "installation")
	req.Header.Set("X-Hub-Signature-256", sig)
	testutil.Call(t, testHandler.HandleGitHubWebhook, req).Want(http.StatusAccepted)

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
