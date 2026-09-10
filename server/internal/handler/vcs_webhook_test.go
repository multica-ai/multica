package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func withVCSBox(t *testing.T) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	prev := testHandler.VCSSecretBox
	testHandler.VCSSecretBox = box
	// The feature also requires the deployment-level switch; the box alone is
	// no longer sufficient. Enable it for the "configured" path tests.
	prevEnabled := testHandler.cfg.VCSIntegrationEnabled
	testHandler.cfg.VCSIntegrationEnabled = true
	t.Cleanup(func() {
		testHandler.VCSSecretBox = prev
		testHandler.cfg.VCSIntegrationEnabled = prevEnabled
	})
	return box
}

const vcsTestSecret = "vcs-webhook-secret"

func seedVCSConnection(t *testing.T, ctx context.Context, box *secretbox.Box, provider, instanceURL string) string {
	t.Helper()
	sealed, err := box.Seal([]byte(vcsTestSecret))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	tokenSealed, _ := box.Seal([]byte("tok"))
	conn, err := testHandler.Queries.UpsertVCSConnection(ctx, db.UpsertVCSConnectionParams{
		WorkspaceID:            parseUUID(testWorkspaceID),
		Provider:               provider,
		InstanceUrl:            instanceURL,
		AccountLogin:           "acme",
		AccessTokenEncrypted:   base64.StdEncoding.EncodeToString(tokenSealed),
		WebhookSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
		ConnectedByID:          pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("UpsertVCSConnection: %v", err)
	}
	return uuidToString(conn.ID)
}

func cleanupVCS(ctx context.Context, issueID string) {
	testPool.Exec(ctx, `DELETE FROM issue_vcs_pull_request WHERE issue_id = $1`, issueID)
	testPool.Exec(ctx, `DELETE FROM vcs_commit_status cs USING vcs_connection c WHERE cs.connection_id = c.id AND c.workspace_id = $1`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM vcs_pull_request WHERE workspace_id = $1`, testWorkspaceID)
	testPool.Exec(ctx, `DELETE FROM vcs_connection WHERE workspace_id = $1`, testWorkspaceID)
	if issueID != "" {
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	}
}

func newVCSIssue(t *testing.T, title string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": title, "status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	return created
}

func vcsWebhookReq(connID string, headers map[string]string, raw []byte) *http.Request {
	req := httptest.NewRequest("POST", "/api/webhooks/vcs/"+connID, bytes.NewReader(raw))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("connectionId", connID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func giteaSig(raw []byte) string {
	mac := hmac.New(sha256.New, []byte(vcsTestSecret))
	mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

func fireGitLabMRWebhook(t *testing.T, connID string, attrs map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{
		"object_kind":       "merge_request",
		"user":              map[string]any{"username": "alice"},
		"project":           map[string]any{"path_with_namespace": "acme/widget"},
		"object_attributes": attrs,
	})
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook", "X-Gitlab-Token": vcsTestSecret,
	}, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("GitLab MR webhook: expected 202, got %d (%s)", w.Code, w.Body.String())
	}
}

func gitLabMRAttrs(action, state, title, description, updatedAt string) map[string]any {
	return map[string]any{
		"iid": 42, "title": title, "description": description,
		"state": state, "action": action, "source_branch": "feat",
		"url":        "https://gitlab.test/acme/widget/-/merge_requests/42",
		"created_at": "2026-05-01 00:00:00 UTC", "updated_at": updatedAt,
		"last_commit": map[string]any{"id": "deadbeef"},
	}
}

func vcsLinkFlags(t *testing.T, ctx context.Context, issueID string) (bool, bool) {
	t.Helper()
	var closeIntent, referenceOnly bool
	if err := testPool.QueryRow(ctx,
		`SELECT close_intent, reference_only FROM issue_vcs_pull_request WHERE issue_id = $1`,
		issueID).Scan(&closeIntent, &referenceOnly); err != nil {
		t.Fatalf("select VCS link flags: %v", err)
	}
	return closeIntent, referenceOnly
}

func vcsPullRequestExists(t *testing.T, ctx context.Context, connID string, number int32) bool {
	t.Helper()
	var exists bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM vcs_pull_request WHERE connection_id = $1 AND pr_number = $2)`,
		connID, number).Scan(&exists); err != nil {
		t.Fatalf("select VCS PR exists #%d: %v", number, err)
	}
	return exists
}

func vcsLinkCount(t *testing.T, ctx context.Context, issueID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue_vcs_pull_request WHERE issue_id = $1`,
		issueID).Scan(&count); err != nil {
		t.Fatalf("select VCS link count: %v", err)
	}
	return count
}

func vcsPullRequestByNumber(t *testing.T, ctx context.Context, connID string, number int32) db.VcsPullRequest {
	t.Helper()
	var pr db.VcsPullRequest
	if err := testPool.QueryRow(ctx, `
		SELECT id, workspace_id, connection_id, provider, repo_owner, repo_name, pr_number, title, state,
		       html_url, branch, head_sha, author_login, author_avatar_url, merged_at, closed_at,
		       pr_created_at, pr_updated_at, additions, deletions, changed_files, created_at, updated_at
		FROM vcs_pull_request
		WHERE connection_id = $1 AND pr_number = $2
	`, connID, number).Scan(
		&pr.ID, &pr.WorkspaceID, &pr.ConnectionID, &pr.Provider, &pr.RepoOwner, &pr.RepoName, &pr.PrNumber,
		&pr.Title, &pr.State, &pr.HtmlUrl, &pr.Branch, &pr.HeadSha, &pr.AuthorLogin, &pr.AuthorAvatarUrl,
		&pr.MergedAt, &pr.ClosedAt, &pr.PrCreatedAt, &pr.PrUpdatedAt, &pr.Additions, &pr.Deletions,
		&pr.ChangedFiles, &pr.CreatedAt, &pr.UpdatedAt,
	); err != nil {
		t.Fatalf("select VCS PR #%d: %v", number, err)
	}
	return pr
}

func TestUpsertVCSPullRequestTerminalStateMonotonicity(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
	t.Cleanup(func() { cleanupVCS(ctx, "") })

	connUUID := parseUUID(connID)
	created := pgtype.Timestamptz{Time: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Valid: true}
	t1 := pgtype.Timestamptz{Time: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), Valid: true}
	t2 := pgtype.Timestamptz{Time: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC), Valid: true}

	upsert := func(number int32, state string, updated pgtype.Timestamptz, mergedAt, closedAt pgtype.Timestamptz) db.UpsertVCSPullRequestRow {
		t.Helper()
		row, err := testHandler.Queries.UpsertVCSPullRequest(ctx, db.UpsertVCSPullRequestParams{
			WorkspaceID: parseUUID(testWorkspaceID), ConnectionID: connUUID,
			Provider: "gitlab", RepoOwner: "acme", RepoName: "widget", PrNumber: number,
			Title: "PR", State: state, HtmlUrl: "https://gitlab.test/acme/widget/-/merge_requests/test",
			MergedAt: mergedAt, ClosedAt: closedAt, PrCreatedAt: created, PrUpdatedAt: updated,
			Additions: 1, Deletions: 2, ChangedFiles: 3, HeadSha: state + "-sha",
		})
		if err != nil {
			t.Fatalf("UpsertVCSPullRequest(%d, %s): %v", number, state, err)
		}
		return row
	}

	upsert(101, "merged", t1, t1, pgtype.Timestamptz{})
	row := upsert(101, "open", t2, pgtype.Timestamptz{}, pgtype.Timestamptz{})
	if row.State != "merged" || !row.MergedAt.Valid || !row.PrUpdatedAt.Time.Equal(t1.Time) || row.HeadSha != "merged-sha" {
		t.Fatalf("merged -> newer open regressed: state=%q merged_at=%v pr_updated_at=%v head_sha=%q", row.State, row.MergedAt.Valid, row.PrUpdatedAt.Time, row.HeadSha)
	}

	upsert(102, "closed", t1, pgtype.Timestamptz{}, t1)
	row = upsert(102, "open", t2, pgtype.Timestamptz{}, pgtype.Timestamptz{})
	if row.State != "closed" || !row.ClosedAt.Valid || !row.PrUpdatedAt.Time.Equal(t1.Time) || row.HeadSha != "closed-sha" {
		t.Fatalf("closed -> newer open regressed: state=%q closed_at=%v pr_updated_at=%v head_sha=%q", row.State, row.ClosedAt.Valid, row.PrUpdatedAt.Time, row.HeadSha)
	}

	row = upsert(101, "closed", t2, t1, t2)
	if row.State != "closed" || !row.MergedAt.Valid || !row.ClosedAt.Valid || !row.PrUpdatedAt.Time.Equal(t2.Time) || row.HeadSha != "closed-sha" {
		t.Fatalf("terminal -> terminal should advance: state=%q merged_at=%v closed_at=%v pr_updated_at=%v head_sha=%q", row.State, row.MergedAt.Valid, row.ClosedAt.Valid, row.PrUpdatedAt.Time, row.HeadSha)
	}
}

func TestVCSWebhookConcurrentFirstUpsertKeepsAcceptedEventLinkFlags(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Concurrent terminal")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	conn, err := testHandler.Queries.GetVCSConnectionByID(ctx, parseUUID(connID))
	if err != nil {
		t.Fatalf("GetVCSConnectionByID: %v", err)
	}

	openEv := vcs.PullRequestEvent{
		Action: "opened", RepoOwner: "acme", RepoName: "widget", Number: 88,
		Title: "Docs", Body: "Related " + issue.Identifier, State: "open",
		HTMLURL: "https://forgejo.test/acme/widget/pulls/88", Branch: "docs", HeadSHA: "open-sha",
		CreatedAt: "2026-05-01T00:00:00Z", UpdatedAt: "2026-05-01T00:00:00Z",
	}
	mergedEv := vcs.PullRequestEvent{
		Action: "closed", RepoOwner: "acme", RepoName: "widget", Number: 88,
		Title: "Fix " + issue.Identifier, Body: "Closes " + issue.Identifier, State: "merged",
		HTMLURL: "https://forgejo.test/acme/widget/pulls/88", Branch: "fix", HeadSHA: "merged-sha",
		MergedAt: "2026-05-02T00:00:00Z", ClosedAt: "2026-05-02T00:00:00Z",
		CreatedAt: "2026-05-01T00:00:00Z", UpdatedAt: "2026-05-02T00:00:00Z",
	}

	lockTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	defer lockTx.Rollback(ctx)
	if _, err := lockTx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", vcsPullRequestLockKey(conn, mergedEv)); err != nil {
		t.Fatalf("hold PR advisory lock: %v", err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, ev := range []vcs.PullRequestEvent{openEv, mergedEv} {
		ev := ev
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			testHandler.mirrorVCSPullRequest(ctx, conn, ev)
		}()
	}
	close(start)
	time.Sleep(50 * time.Millisecond)
	if vcsPullRequestExists(t, ctx, connID, 88) || vcsLinkCount(t, ctx, issue.ID) != 0 {
		t.Fatal("PR/link wrote while the per-PR advisory lock was held")
	}
	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf("release PR advisory lock: %v", err)
	}
	wg.Wait()

	wantMergedAt := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	pr := vcsPullRequestByNumber(t, ctx, connID, 88)
	if pr.State != "merged" || pr.HeadSha != "merged-sha" || !pr.PrUpdatedAt.Time.Equal(wantMergedAt) {
		t.Fatalf("concurrent first upsert persisted %q/%q/%v, want merged/merged-sha/2026-05-02", pr.State, pr.HeadSha, pr.PrUpdatedAt.Time)
	}
	closeIntent, referenceOnly := vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("concurrent link flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}

	rejected := openEv
	rejected.UpdatedAt = "2026-05-03T00:00:00Z"
	rejected.HeadSHA = "rejected-open-sha"
	testHandler.mirrorVCSPullRequest(ctx, conn, rejected)

	pr = vcsPullRequestByNumber(t, ctx, connID, 88)
	if pr.State != "merged" || pr.HeadSha != "merged-sha" || !pr.PrUpdatedAt.Time.Equal(wantMergedAt) {
		t.Fatalf("newer open replay after concurrent terminal persisted %q/%q/%v, want merged/merged-sha/2026-05-02", pr.State, pr.HeadSha, pr.PrUpdatedAt.Time)
	}
	closeIntent, referenceOnly = vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("rejected replay link flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}
}

func TestVCSWebhookPullRequestLockTimeoutRollsBack(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Lock timeout")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	conn, err := testHandler.Queries.GetVCSConnectionByID(ctx, parseUUID(connID))
	if err != nil {
		t.Fatalf("GetVCSConnectionByID: %v", err)
	}
	ev := vcs.PullRequestEvent{
		Action: "closed", RepoOwner: "acme", RepoName: "widget", Number: 89,
		Title: "Fix " + issue.Identifier, Body: "Closes " + issue.Identifier, State: "merged",
		HTMLURL: "https://forgejo.test/acme/widget/pulls/89", Branch: "fix", HeadSHA: "merged-sha",
		MergedAt: "2026-05-02T00:00:00Z", ClosedAt: "2026-05-02T00:00:00Z",
		CreatedAt: "2026-05-01T00:00:00Z", UpdatedAt: "2026-05-02T00:00:00Z",
	}

	lockTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	defer lockTx.Rollback(ctx)
	if _, err := lockTx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", vcsPullRequestLockKey(conn, ev)); err != nil {
		t.Fatalf("hold PR advisory lock: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	testHandler.mirrorVCSPullRequest(callCtx, conn, ev)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("PR mirror did not exit within bounded lock timeout: %s", elapsed)
	}
	if vcsPullRequestExists(t, ctx, connID, 89) {
		t.Fatal("PR row was written after lock timeout")
	}
	if count := vcsLinkCount(t, ctx, issue.ID); count != 0 {
		t.Fatalf("link rows after lock timeout = %d, want 0", count)
	}
}

func TestVCSWebhookRejectedTerminalReplayDoesNotRewriteLinkFlags(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Terminal replay")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	fireForgejo := func(action, state, updatedAt, title, body, branch string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{
			"action": action,
			"pull_request": map[string]any{
				"number": 77, "html_url": "https://forgejo.test/acme/widget/pulls/77",
				"title": title, "body": body, "state": state, "merged": state == "merged",
				"merged_at": "2026-05-02T00:00:00Z", "closed_at": "2026-05-02T00:00:00Z",
				"created_at": "2026-05-01T00:00:00Z", "updated_at": updatedAt,
				"head": map[string]any{"ref": branch, "sha": state + "-sha"},
				"user": map[string]any{"username": "octo"},
			},
			"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
		})
		w := httptest.NewRecorder()
		testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
			"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(raw),
		}, raw))
		if w.Code != http.StatusAccepted {
			t.Fatalf("Forgejo PR webhook: expected 202, got %d (%s)", w.Code, w.Body.String())
		}
	}

	fireForgejo("closed", "merged", "2026-05-02T00:00:00Z", "Fix "+issue.Identifier, "Closes "+issue.Identifier, "fix")
	closeIntent, referenceOnly := vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("merged link flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}

	fireForgejo("opened", "open", "2026-05-03T00:00:00Z", "Docs", "Related "+issue.Identifier, "docs")
	pr := vcsPullRequestByNumber(t, ctx, connID, 77)
	if pr.State != "merged" || !pr.MergedAt.Valid {
		t.Fatalf("newer terminal replay regressed PR: state=%q merged_at=%v", pr.State, pr.MergedAt.Valid)
	}
	closeIntent, referenceOnly = vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("rejected newer open rewrote link flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}

	fireForgejo("opened", "open", "not-a-time", "Docs", "Related "+issue.Identifier, "docs")
	pr = vcsPullRequestByNumber(t, ctx, connID, 77)
	if pr.State != "merged" || !pr.MergedAt.Valid {
		t.Fatalf("fallback-now terminal replay regressed PR: state=%q merged_at=%v", pr.State, pr.MergedAt.Valid)
	}
	closeIntent, referenceOnly = vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("fallback-now replay rewrote link flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}

	fireForgejo("opened", "open", "", "Docs", "Related "+issue.Identifier, "docs")
	pr = vcsPullRequestByNumber(t, ctx, connID, 77)
	if pr.State != "merged" || !pr.MergedAt.Valid {
		t.Fatalf("missing-timestamp terminal replay regressed PR: state=%q merged_at=%v", pr.State, pr.MergedAt.Valid)
	}
	closeIntent, referenceOnly = vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("missing-timestamp replay rewrote link flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}
}

func TestVCSWebhook_ForgejoMirrorsAndCloses(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Forgejo PR auto-merge")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	raw, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number": 7, "html_url": "https://forgejo.test/acme/widget/pulls/7",
			"title": "Fix login " + issue.Identifier, "body": "Closes " + issue.Identifier,
			"state": "closed", "merged": true,
			"merged_at": "2026-04-29T00:00:00Z", "closed_at": "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z", "updated_at": "2026-04-29T00:00:00Z",
			"head": map[string]any{"ref": "fix/login", "sha": "abc"},
			"user": map[string]any{"username": "octo"},
		},
		"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
	})
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(raw),
	}, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	rows, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("ListVCSPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 || rows[0].State != "merged" || rows[0].Provider != "forgejo" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	updated, _ := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if updated.Status != "done" {
		t.Errorf("expected issue done, got %q", updated.Status)
	}
}

// A bare body mention ("Related MUL-X", no closing keyword, not in title or
// branch) must link reference_only: excluded from the issue PR list and from
// the close gate, so it neither shows as a working PR nor blocks a genuine
// Closes sibling from advancing the issue. Mirrors the GitHub qualifying rule.
func TestVCSWebhook_ReferenceOnlyExcludedAndNonBlocking(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Reference-only mention")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	// PR #7: OPEN, mentions the issue only in the body with no closing keyword,
	// generic title/branch → reference_only.
	refRaw, _ := json.Marshal(map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 7, "html_url": "https://forgejo.test/acme/widget/pulls/7",
			"title": "Update docs", "body": "Related " + issue.Identifier,
			"state": "open", "merged": false,
			"created_at": "2026-04-28T00:00:00Z", "updated_at": "2026-04-28T00:00:00Z",
			"head": map[string]any{"ref": "docs", "sha": "ref7"},
			"user": map[string]any{"username": "octo"},
		},
		"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
	})
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(refRaw),
	}, refRaw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("ref PR: expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	// The link exists but is reference_only, so it is hidden from the PR list.
	var referenceOnly bool
	if err := testPool.QueryRow(ctx,
		`SELECT reference_only FROM issue_vcs_pull_request WHERE issue_id = $1`,
		issue.ID).Scan(&referenceOnly); err != nil {
		t.Fatalf("select reference_only: %v", err)
	}
	if !referenceOnly {
		t.Fatalf("body-only mention should be reference_only")
	}
	if rows, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID)); err != nil {
		t.Fatalf("list: %v", err)
	} else if len(rows) != 0 {
		t.Fatalf("reference_only PR must be excluded from the list, got %d rows", len(rows))
	}

	// PR #8: MERGED with a title reference + Closes keyword → qualifying,
	// close_intent. The still-open reference_only PR #7 must NOT block advance.
	closeRaw, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number": 8, "html_url": "https://forgejo.test/acme/widget/pulls/8",
			"title": "Fix " + issue.Identifier, "body": "Closes " + issue.Identifier,
			"state": "closed", "merged": true,
			"merged_at": "2026-04-29T00:00:00Z", "closed_at": "2026-04-29T00:00:00Z",
			"created_at": "2026-04-28T00:00:00Z", "updated_at": "2026-04-29T00:00:00Z",
			"head": map[string]any{"ref": "fix", "sha": "cls8"},
			"user": map[string]any{"username": "octo"},
		},
		"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
	})
	w = httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(closeRaw),
	}, closeRaw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("close PR: expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	updated, _ := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if updated.Status != "done" {
		t.Errorf("issue should advance despite the open reference_only PR, got %q", updated.Status)
	}
}

// The close gate must span providers: an issue with an OPEN GitHub PR and a
// MERGED close-intent VCS PR must report open_count > 0, so neither webhook
// auto-advances it out from under the still-open GitHub work (and vice versa).
func TestCombinedCloseAggregateSpansProviders(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
	issue := newVCSIssue(t, "Cross-provider close gate")
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		cleanupVCS(ctx, issue.ID)
	})

	// OPEN GitHub PR linked to the issue (installation_id carries no FK).
	ghPR, err := testHandler.Queries.UpsertGitHubPullRequest(ctx, db.UpsertGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), InstallationID: 987654,
		RepoOwner: "acme", RepoName: "gh", PrNumber: 3,
		Title: "WIP " + issue.Identifier, State: "open",
		HtmlUrl:     "https://github.com/acme/gh/pull/3",
		PrCreatedAt: now, PrUpdatedAt: now, HeadSha: "ghsha",
	})
	if err != nil {
		t.Fatalf("UpsertGitHubPullRequest: %v", err)
	}
	if err := testHandler.Queries.LinkIssueToPullRequest(ctx, db.LinkIssueToPullRequestParams{
		IssueID: parseUUID(issue.ID), PullRequestID: ghPR.ID, CloseIntent: false,
		ReferenceOnly: false, LinkedByType: strToText("system"),
	}); err != nil {
		t.Fatalf("LinkIssueToPullRequest: %v", err)
	}

	// MERGED close-intent VCS PR linked to the same issue.
	vcsPR, err := testHandler.Queries.UpsertVCSPullRequest(ctx, db.UpsertVCSPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID), ConnectionID: parseUUID(connID),
		Provider: "gitlab", RepoOwner: "acme", RepoName: "gl", PrNumber: 4,
		Title: "Fix " + issue.Identifier, State: "merged",
		HtmlUrl:     "https://gitlab.test/acme/gl/-/merge_requests/4",
		PrCreatedAt: now, PrUpdatedAt: now, HeadSha: "glsha",
	})
	if err != nil {
		t.Fatalf("UpsertVCSPullRequest: %v", err)
	}
	if err := testHandler.Queries.LinkIssueToVCSPullRequest(ctx, db.LinkIssueToVCSPullRequestParams{
		IssueID: parseUUID(issue.ID), PullRequestID: vcsPR.ID, CloseIntent: true,
		ReferenceOnly: false, LinkedByType: strToText("system"),
	}); err != nil {
		t.Fatalf("LinkIssueToVCSPullRequest: %v", err)
	}

	counts, err := testHandler.Queries.GetIssueCombinedPullRequestCloseAggregate(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("GetIssueCombinedPullRequestCloseAggregate: %v", err)
	}
	if counts.OpenCount != 1 {
		t.Errorf("open_count = %d, want 1 (the open GitHub PR must be seen)", counts.OpenCount)
	}
	if counts.MergedWithCloseIntentCount != 1 {
		t.Errorf("merged_with_close_intent_count = %d, want 1 (the VCS MR)", counts.MergedWithCloseIntentCount)
	}
}

// DeleteIssue's VCS-link cleanup must honour the same workspace guard as the
// issue delete: passing a foreign issue_id with a mismatched workspace_id must
// be a complete no-op, not silently drop the victim tenant's link rows (#1661).
func TestDeleteIssue_VCSLinkCleanupIsWorkspaceScoped(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Tenant-scoped delete")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	raw, _ := json.Marshal(map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 7, "html_url": "https://forgejo.test/acme/widget/pulls/7",
			"title": "Fix " + issue.Identifier, "state": "open", "merged": false,
			"created_at": "2026-05-01T00:00:00Z", "updated_at": "2026-05-01T00:00:00Z",
			"head": map[string]any{"ref": "fix", "sha": "abc"},
			"user": map[string]any{"username": "octo"},
		},
		"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
	})
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(raw),
	}, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("seed PR: %d %s", w.Code, w.Body.String())
	}

	linkCount := func() int {
		var n int
		testPool.QueryRow(ctx, `SELECT count(*) FROM issue_vcs_pull_request WHERE issue_id = $1`, issue.ID).Scan(&n)
		return n
	}
	if linkCount() != 1 {
		t.Fatalf("expected 1 link after seed, got %d", linkCount())
	}

	// Mismatched workspace_id → complete no-op: issue and link both survive.
	wrongWS := parseUUID("11111111-1111-1111-1111-111111111111")
	if err := testHandler.Queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: parseUUID(issue.ID), WorkspaceID: wrongWS}); err != nil {
		t.Fatalf("DeleteIssue(wrong ws): %v", err)
	}
	if _, err := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID)); err != nil {
		t.Errorf("issue must survive a mismatched-workspace delete: %v", err)
	}
	if linkCount() != 1 {
		t.Errorf("link rows must survive a mismatched-workspace delete, got %d", linkCount())
	}

	// Correct workspace_id → issue and its links both removed.
	if err := testHandler.Queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: parseUUID(issue.ID), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
		t.Fatalf("DeleteIssue(correct ws): %v", err)
	}
	if linkCount() != 0 {
		t.Errorf("link rows should be gone after correct delete, got %d", linkCount())
	}
}

// A redelivered older event must not rewrite the link metadata that a newer
// event already set. The PR-upsert monotonic guard protects the PR row; this
// covers the link (close_intent / reference_only).
func TestVCSWebhook_StaleEventDoesNotRewriteLink(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Out-of-order link guard")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	fire := func(action, state string, merged bool, title, body, updatedAt string) {
		raw, _ := json.Marshal(map[string]any{
			"action": action,
			"pull_request": map[string]any{
				"number": 7, "html_url": "https://forgejo.test/acme/widget/pulls/7",
				"title": title, "body": body, "state": state, "merged": merged,
				"created_at": "2026-05-01T00:00:00Z", "updated_at": updatedAt,
				"merged_at": "2026-05-02T00:00:00Z",
				"head":      map[string]any{"ref": "wip", "sha": "abc"},
				"user":      map[string]any{"username": "octo"},
			},
			"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
		})
		w := httptest.NewRecorder()
		testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
			"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(raw),
		}, raw))
		if w.Code != http.StatusAccepted {
			t.Fatalf("%s event: %d %s", action, w.Code, w.Body.String())
		}
	}

	// Newer terminal event: merged with a qualifying Closes → close_intent, not reference_only.
	fire("closed", "closed", true, "Fix "+issue.Identifier, "Closes "+issue.Identifier, "2026-05-02T00:00:00Z")
	// Older redelivered "opened" event: bare body mention, generic title/branch.
	// Without the guard this rewrites the link to close_intent=false, reference_only=true.
	fire("opened", "open", false, "WIP", "touches "+issue.Identifier, "2026-05-01T00:00:00Z")

	var closeIntent, referenceOnly bool
	if err := testPool.QueryRow(ctx,
		`SELECT close_intent, reference_only FROM issue_vcs_pull_request WHERE issue_id = $1`,
		issue.ID).Scan(&closeIntent, &referenceOnly); err != nil {
		t.Fatalf("select link: %v", err)
	}
	if !closeIntent || referenceOnly {
		t.Errorf("stale event rewrote link: close_intent=%v reference_only=%v, want true/false", closeIntent, referenceOnly)
	}
	// The PR row also stayed at the newer merged state.
	rows, _ := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID))
	if len(rows) != 1 || rows[0].State != "merged" {
		t.Errorf("PR row regressed: %+v", rows)
	}
}

func TestVCSWebhook_GitlabMergeRequest(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
	issue := newVCSIssue(t, "GitLab MR test")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	raw, _ := json.Marshal(map[string]any{
		"object_kind": "merge_request",
		"user":        map[string]any{"username": "alice"},
		"project":     map[string]any{"path_with_namespace": "acme/widget"},
		"object_attributes": map[string]any{
			"iid": 42, "title": "Add " + issue.Identifier, "description": "Closes " + issue.Identifier,
			"state": "merged", "action": "merge", "source_branch": "feat",
			"url":         "https://gitlab.test/acme/widget/-/merge_requests/42",
			"last_commit": map[string]any{"id": "deadbeef"},
		},
	})
	// GitLab authenticates by plaintext X-Gitlab-Token, not HMAC.
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitlab-Event": "Merge Request Hook", "X-Gitlab-Token": vcsTestSecret,
	}, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}

	rows, err := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID))
	if err != nil {
		t.Fatalf("ListVCSPullRequestsByIssue: %v", err)
	}
	if len(rows) != 1 || rows[0].Provider != "gitlab" || rows[0].RepoOwner != "acme" || rows[0].PrNumber != 42 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if rows[0].State != "merged" {
		t.Errorf("expected merged, got %q", rows[0].State)
	}
	updated, _ := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if updated.Status != "done" {
		t.Errorf("expected issue done, got %q", updated.Status)
	}
}

func TestVCSWebhook_GitlabMergedUpdateFirstUpsertClosesIssue(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
	issue := newVCSIssue(t, "GitLab merged update")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
		"update", "merged",
		"Add "+issue.Identifier,
		"Closes "+issue.Identifier,
		"2026-05-02 00:00:00 UTC",
	))

	closeIntent, referenceOnly := vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("link flags after first terminal upsert = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}
	updated, _ := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if updated.Status != "done" {
		t.Errorf("expected issue done, got %q", updated.Status)
	}
}

func TestVCSWebhook_GitlabFirstTerminalTransitionRecomputesCloseIntent(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
	issue := newVCSIssue(t, "GitLab transition update")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
		"update", "opened",
		"WIP "+issue.Identifier,
		"Related "+issue.Identifier,
		"2026-05-01 00:00:00 UTC",
	))
	closeIntent, referenceOnly := vcsLinkFlags(t, ctx, issue.ID)
	if closeIntent || referenceOnly {
		t.Fatalf("non-terminal update flags = close_intent:%v reference_only:%v, want false/false", closeIntent, referenceOnly)
	}

	fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
		"update", "merged",
		"Finish "+issue.Identifier,
		"Closes "+issue.Identifier,
		"2026-05-02 00:00:00 UTC",
	))
	closeIntent, referenceOnly = vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("first terminal transition flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}
	updated, _ := testHandler.Queries.GetIssue(ctx, parseUUID(issue.ID))
	if updated.Status != "done" {
		t.Errorf("expected issue done, got %q", updated.Status)
	}
}

func TestVCSWebhook_GitlabPostTerminalUpdatePreservesCloseIntent(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
	issue := newVCSIssue(t, "GitLab terminal preservation")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
		"merge", "merged",
		"Finish "+issue.Identifier,
		"Closes "+issue.Identifier,
		"2026-05-02 00:00:00 UTC",
	))
	fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
		"update", "merged",
		"Finish "+issue.Identifier,
		"Related "+issue.Identifier,
		"2026-05-03 00:00:00 UTC",
	))

	closeIntent, referenceOnly := vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("post-terminal update flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}
}

func TestVCSWebhook_GitlabClosedUpdateTerminalSemantics(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)

	t.Run("first upsert records close intent", func(t *testing.T) {
		connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
		issue := newVCSIssue(t, "GitLab closed update first upsert")
		t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

		fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
			"update", "closed",
			"Cancel "+issue.Identifier,
			"Closes "+issue.Identifier,
			"2026-05-02 00:00:00 UTC",
		))

		closeIntent, referenceOnly := vcsLinkFlags(t, ctx, issue.ID)
		if !closeIntent || referenceOnly {
			t.Fatalf("closed first upsert flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
		}
		rows, _ := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID))
		if len(rows) != 1 || rows[0].State != "closed" {
			t.Fatalf("closed first upsert row = %+v, want one closed row", rows)
		}
	})

	t.Run("first terminal transition recomputes close intent", func(t *testing.T) {
		connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
		issue := newVCSIssue(t, "GitLab closed update transition")
		t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

		fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
			"update", "opened",
			"WIP "+issue.Identifier,
			"Related "+issue.Identifier,
			"2026-05-03 00:00:00 UTC",
		))
		fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
			"update", "closed",
			"Cancel "+issue.Identifier,
			"Closes "+issue.Identifier,
			"2026-05-04 00:00:00 UTC",
		))

		closeIntent, referenceOnly := vcsLinkFlags(t, ctx, issue.ID)
		if !closeIntent || referenceOnly {
			t.Fatalf("closed transition flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
		}
	})

	t.Run("later terminal update preserves link flags", func(t *testing.T) {
		connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
		issue := newVCSIssue(t, "GitLab closed update preservation")
		t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

		fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
			"update", "closed",
			"Cancel "+issue.Identifier,
			"Closes "+issue.Identifier,
			"2026-05-05 00:00:00 UTC",
		))
		fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
			"update", "closed",
			"Cancel "+issue.Identifier,
			"Related "+issue.Identifier,
			"2026-05-06 00:00:00 UTC",
		))

		closeIntent, referenceOnly := vcsLinkFlags(t, ctx, issue.ID)
		if !closeIntent || referenceOnly {
			t.Fatalf("closed preservation flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
		}
	})
}

func TestVCSWebhook_GitlabEqualTimestampNonterminalReplayDoesNotClearTerminal(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "gitlab", "https://gitlab.test")
	issue := newVCSIssue(t, "GitLab equal timestamp replay")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	sameTimestamp := "2026-05-02 00:00:00 UTC"
	fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
		"update", "merged",
		"Finish "+issue.Identifier,
		"Closes "+issue.Identifier,
		sameTimestamp,
	))
	fireGitLabMRWebhook(t, connID, gitLabMRAttrs(
		"update", "opened",
		"WIP "+issue.Identifier,
		"Related "+issue.Identifier,
		sameTimestamp,
	))

	closeIntent, referenceOnly := vcsLinkFlags(t, ctx, issue.ID)
	if !closeIntent || referenceOnly {
		t.Fatalf("equal timestamp replay flags = close_intent:%v reference_only:%v, want true/false", closeIntent, referenceOnly)
	}
	rows, _ := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID))
	if len(rows) != 1 || rows[0].State != "merged" {
		t.Fatalf("equal timestamp replay regressed PR row: %+v", rows)
	}
}

func TestVCSWebhook_CommitStatusMirrors(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	issue := newVCSIssue(t, "Forgejo CI status")
	t.Cleanup(func() { cleanupVCS(ctx, issue.ID) })

	prRaw, _ := json.Marshal(map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 11, "html_url": "https://forgejo.test/acme/widget/pulls/11",
			"title": issue.Identifier + " feature", "state": "open",
			"created_at": "2026-04-28T00:00:00Z", "updated_at": "2026-04-28T00:00:00Z",
			"head": map[string]any{"ref": "feat", "sha": "deadbeef"},
			"user": map[string]any{"username": "octo"},
		},
		"repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}},
	})
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(prRaw),
	}, prRaw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("pr: expected 202, got %d", w.Code)
	}

	stRaw, _ := json.Marshal(map[string]any{"sha": "deadbeef", "context": "ci/woodpecker", "state": "success"})
	w = httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "status", "X-Gitea-Signature": giteaSig(stRaw),
	}, stRaw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status: expected 202, got %d", w.Code)
	}

	rows, _ := testHandler.Queries.ListVCSPullRequestsByIssue(ctx, parseUUID(issue.ID))
	if len(rows) != 1 || rows[0].ChecksTotal != 1 || rows[0].ChecksPassed != 1 {
		t.Fatalf("expected 1 passed check, got %+v", rows)
	}
}

func TestVCSWebhook_BadSignature(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	t.Cleanup(func() { cleanupVCS(ctx, "") })

	raw := []byte(`{"action":"opened","pull_request":{"number":1}}`)
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": "00",
	}, raw))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestVCSWebhook_UnknownConnection(t *testing.T) {
	withVCSBox(t)
	req := vcsWebhookReq("00000000-0000-0000-0000-000000000000", map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": "00",
	}, []byte(`{}`))
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestVCSWebhook_DisabledDeploymentReturns404 verifies the deployment-level
// switch is enforced server-side: with the integration off (the managed-cloud
// posture), even a valid, correctly-signed delivery to a real connection is
// rejected with a bare 404, so the feature is never processed and reveals
// nothing about config. The availability gate short-circuits before signature
// verification.
func TestVCSWebhook_DisabledDeploymentReturns404(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t) // sets the box AND enables the switch
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	t.Cleanup(func() { cleanupVCS(ctx, "") })

	// Now flip the deployment switch off (withVCSBox's cleanup restores it).
	testHandler.cfg.VCSIntegrationEnabled = false

	raw := []byte(`{"action":"opened","pull_request":{"number":1}}`)
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(raw),
	}, raw))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when integration disabled, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestVCSWebhook_MalformedTolerated(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	t.Cleanup(func() { cleanupVCS(ctx, "") })

	raw := []byte(`{"action":"opened","pull_request":"not-an-object"}`)
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{
		"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(raw),
	}, raw))
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", w.Code, w.Body.String())
	}
	var count int
	testPool.QueryRow(ctx, `SELECT count(*) FROM vcs_pull_request WHERE workspace_id = $1`, testWorkspaceID).Scan(&count)
	if count != 0 {
		t.Errorf("expected no PR rows, got %d", count)
	}
}
