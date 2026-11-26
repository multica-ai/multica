package secretscan

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	testPool    *pgxpool.Pool
	testHandler *handler.Handler
	testService *Service
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	dbConfigured := dbURL != ""
	if !dbConfigured {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err == nil {
		err = pool.Ping(ctx)
	}
	if err != nil {
		if dbConfigured {
			fmt.Fprintln(os.Stderr, "secret scan tests: database from DATABASE_URL not reachable:", err)
			os.Exit(1)
		}
		fmt.Printf("Skipping tests: database not reachable: %v\n", err)
		os.Exit(0)
	}
	hub := realtime.NewHub()
	go hub.Run()
	testPool = pool
	testHandler = handler.New(db.New(pool), pool, hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, handler.Config{})
	testService = New(testHandler)
	testHandler.SetCommentScanner(testService)
	testService.subscribe(testHandler.Bus)
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// asm keeps fake secrets out of the source as whole strings, so push
// protection and secret scanners do not flag the fixtures.
func asm(parts ...string) string { return strings.Join(parts, "") }

var githubToken = asm("ghp_", "7Hq2LmZx9VbR4tKp1sWcY8nDf3GjU6aE0oQi")

// env is a workspace of its own with an owner, the author of the comments
// under test, and a member who did not write them.
type env struct {
	fx          *testutil.Fixture
	workspaceID string
	ownerID     string
	authorID    string
	otherID     string
	issueID     string
}

func newEnv(t *testing.T) env {
	t.Helper()
	fx := testutil.New(testPool, "", "")
	suffix := uuid.NewString()[:8]
	ownerID := fx.User(t, "Scan Owner", "scan-owner-"+suffix+"@multica.ai")
	authorID := fx.User(t, "Scan Author", "scan-author-"+suffix+"@multica.ai")
	otherID := fx.User(t, "Scan Other", "scan-other-"+suffix+"@multica.ai")
	workspaceID := fx.Workspace(t, "Scan Tests", "scan-tests-"+suffix)
	fx.Member(t, workspaceID, ownerID, "owner")
	fx.Member(t, workspaceID, authorID, "member")
	fx.Member(t, workspaceID, otherID, "member")
	fx = testutil.New(testPool, workspaceID, ownerID)
	fx.Cleanup(t, `DELETE FROM secret_scan_setting WHERE workspace_id = $1`, workspaceID)
	fx.Cleanup(t, `DELETE FROM secret_finding WHERE workspace_id = $1`, workspaceID)
	fx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
	fx.Cleanup(t, `DELETE FROM comment WHERE workspace_id = $1`, workspaceID)
	issueID := fx.Issue(t, "Payments webhook 502")
	return env{fx: fx, workspaceID: workspaceID, ownerID: ownerID, authorID: authorID, otherID: otherID, issueID: issueID}
}

func (e env) request(userID, method, path string, body any, params ...string) *http.Request {
	req := testutil.WithHeaders(testutil.JSONRequest(method, path, body), "X-User-ID", userID, "X-Workspace-ID", e.workspaceID)
	return testutil.WithURLParams(req, params...)
}

func (e env) comment(t *testing.T, userID, content string) handler.CommentResponse {
	t.Helper()
	var c handler.CommentResponse
	testutil.Call(t, testHandler.CreateComment, e.request(userID, http.MethodPost, "/api/issues/"+e.issueID+"/comments",
		map[string]any{"content": content}, "id", e.issueID)).Want(http.StatusCreated).JSON(&c)
	return c
}

func (e env) edit(t *testing.T, userID, commentID, content string) {
	t.Helper()
	testutil.Call(t, testHandler.UpdateComment, e.request(userID, http.MethodPut, "/api/comments/"+commentID,
		map[string]any{"content": content}, "commentId", commentID)).Want(http.StatusOK)
}

func (e env) content(t *testing.T, commentID string) string {
	t.Helper()
	var content string
	e.fx.QueryRow(t, `SELECT content FROM comment WHERE id = $1`, commentID).Scan(&content)
	return content
}

func (e env) issueFindings(t *testing.T, userID string) []FindingResponse {
	t.Helper()
	var resp struct {
		Findings []FindingResponse `json:"findings"`
	}
	testutil.Call(t, testService.ListIssueFindings, e.request(userID, http.MethodGet, "/api/secret-scan/issues/"+e.issueID+"/findings", nil, "issueId", e.issueID)).
		Want(http.StatusOK).JSON(&resp)
	return resp.Findings
}

func (e env) statusOf(t *testing.T, commentID string) string {
	t.Helper()
	var status string
	e.fx.QueryRow(t, `SELECT status FROM secret_finding WHERE comment_id = $1`, commentID).Scan(&status)
	return status
}

func (e env) setMode(t *testing.T, mode string) {
	t.Helper()
	testutil.Call(t, testService.UpdateSettings, e.request(e.ownerID, http.MethodPut, "/api/secret-scan/settings", map[string]any{"mode": mode})).Want(http.StatusOK)
}

func TestCommentWithSecretIsReportedToAuthorAndAdmins(t *testing.T) {
	e := newEnv(t)
	c := e.comment(t, e.authorID, "use this:\n\n```\nexport GITHUB_TOKEN="+githubToken+"\n```")

	if got := e.content(t, c.ID); !strings.Contains(got, githubToken) {
		t.Fatalf("notify mode changed the comment: %q", got)
	}
	author := e.issueFindings(t, e.authorID)
	if len(author) != 1 || author[0].Rule != "github_token" || author[0].Masked != "ghp_••••••••0oQi" || author[0].CommentID != c.ID {
		t.Fatalf("author findings = %+v", author)
	}
	if got := e.issueFindings(t, e.ownerID); len(got) != 1 {
		t.Fatalf("admin findings = %+v", got)
	}
	if got := e.issueFindings(t, e.otherID); len(got) != 0 {
		t.Fatalf("another member sees findings: %+v", got)
	}
	if n := e.fx.Count(t, `SELECT count(*) FROM secret_finding WHERE comment_id = $1 AND (masked LIKE '%' || $2 || '%' OR fingerprint = $2)`, c.ID, githubToken); n != 0 {
		t.Fatal("a finding stores the secret")
	}
}

func TestCommentWithoutSecretRecordsNothing(t *testing.T) {
	e := newEnv(t)
	e.comment(t, e.authorID, "set GITHUB_TOKEN in your shell, then run pnpm install")
	if n := e.fx.Count(t, `SELECT count(*) FROM secret_finding WHERE workspace_id = $1`, e.workspaceID); n != 0 {
		t.Fatalf("findings = %d, want 0", n)
	}
}

func TestRedactModeStoresPlaceholder(t *testing.T) {
	e := newEnv(t)
	e.setMode(t, ModeRedact)
	c := e.comment(t, e.authorID, "token "+githubToken)

	if want := "token [REDACTED GITHUB TOKEN]"; c.Content != want || e.content(t, c.ID) != want {
		t.Fatalf("content = %q / %q, want %q", c.Content, e.content(t, c.ID), want)
	}
	if got := e.statusOf(t, c.ID); got != StatusRedacted {
		t.Fatalf("status = %q, want redacted", got)
	}
	if got := e.issueFindings(t, e.authorID); len(got) != 0 {
		t.Fatalf("a redacted finding is still open: %+v", got)
	}
}

func TestRedactRewritesCommentAndNotificationCopies(t *testing.T) {
	e := newEnv(t)
	c := e.comment(t, e.authorID, "token "+githubToken+" for payments-sdk")
	e.fx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": e.workspaceID, "recipient_type": "member", "recipient_id": e.ownerID,
		"type": "new_comment", "issue_id": e.issueID, "title": "New comment",
		"body": "token " + githubToken + " for payments-sdk", "details": fmt.Sprintf(`{"comment_id":"%s"}`, c.ID),
	})
	findings := e.issueFindings(t, e.authorID)
	ids := []string{findings[0].ID}

	testutil.Call(t, testService.Redact, e.request(e.otherID, http.MethodPost, "/api/secret-scan/findings/redact", map[string]any{"finding_ids": ids})).
		Want(http.StatusNotFound)
	testutil.Call(t, testService.Redact, e.request(e.authorID, http.MethodPost, "/api/secret-scan/findings/redact", map[string]any{"finding_ids": ids})).
		Want(http.StatusOK)

	want := "token [REDACTED GITHUB TOKEN] for payments-sdk"
	if got := e.content(t, c.ID); got != want {
		t.Fatalf("comment = %q, want %q", got, want)
	}
	var body string
	e.fx.QueryRow(t, `SELECT body FROM inbox_item WHERE issue_id = $1`, e.issueID).Scan(&body)
	if body != want {
		t.Fatalf("inbox body = %q, want %q", body, want)
	}
	if got := e.statusOf(t, c.ID); got != StatusRedacted {
		t.Fatalf("status = %q, want redacted", got)
	}
}

func TestEditingSecretOutResolvesFinding(t *testing.T) {
	e := newEnv(t)
	c := e.comment(t, e.authorID, "token "+githubToken)
	e.edit(t, e.authorID, c.ID, "token rotated, ask me in DM")
	if got := e.statusOf(t, c.ID); got != StatusResolved {
		t.Fatalf("status = %q, want resolved", got)
	}
}

func TestDismissedFindingStaysDismissedAcrossEdits(t *testing.T) {
	e := newEnv(t)
	c := e.comment(t, e.authorID, "token "+githubToken)
	findings := e.issueFindings(t, e.authorID)
	testutil.Call(t, testService.Dismiss, e.request(e.authorID, http.MethodPost, "/api/secret-scan/findings/dismiss",
		map[string]any{"finding_ids": []string{findings[0].ID}})).Want(http.StatusOK)

	e.edit(t, e.authorID, c.ID, "token "+githubToken+" (test fixture, not a real key)")
	if got := e.statusOf(t, c.ID); got != StatusDismissed {
		t.Fatalf("status = %q, want dismissed", got)
	}
}

func TestDeletingCommentDropsFindings(t *testing.T) {
	e := newEnv(t)
	c := e.comment(t, e.authorID, "token "+githubToken)
	testutil.Call(t, testHandler.DeleteComment, e.request(e.authorID, http.MethodDelete, "/api/comments/"+c.ID, nil, "commentId", c.ID)).
		Want(http.StatusNoContent)
	if n := e.fx.Count(t, `SELECT count(*) FROM secret_finding WHERE comment_id = $1`, c.ID); n != 0 {
		t.Fatalf("findings = %d after delete, want 0", n)
	}
}

func TestSettingsAreForAdmins(t *testing.T) {
	e := newEnv(t)
	testutil.Call(t, testService.GetSettings, e.request(e.authorID, http.MethodGet, "/api/secret-scan/settings", nil)).Want(http.StatusForbidden)
	testutil.Call(t, testService.ListFindings, e.request(e.authorID, http.MethodGet, "/api/secret-scan/findings", nil)).Want(http.StatusForbidden)
	testutil.Call(t, testService.UpdateSettings, e.request(e.ownerID, http.MethodPut, "/api/secret-scan/settings", map[string]any{"mode": "delete"})).
		Want(http.StatusBadRequest)

	var s SettingsResponse
	testutil.Call(t, testService.GetSettings, e.request(e.ownerID, http.MethodGet, "/api/secret-scan/settings", nil)).Want(http.StatusOK).JSON(&s)
	if s.Mode != ModeNotify || s.Backfill.Status != "idle" {
		t.Fatalf("default settings = %+v", s)
	}
}

func TestBackfillFindsSecretsInExistingComments(t *testing.T) {
	e := newEnv(t)
	old := e.fx.Comment(t, e.issueID, "old token "+githubToken, testutil.Cols{"author_id": e.authorID})
	e.fx.Comment(t, e.issueID, "nothing to see")

	testutil.Call(t, testService.StartBackfill, e.request(e.ownerID, http.MethodPost, "/api/secret-scan/backfill", nil)).Want(http.StatusAccepted)

	var s SettingsResponse
	for deadline := time.Now().Add(10 * time.Second); ; {
		testutil.Call(t, testService.GetSettings, e.request(e.ownerID, http.MethodGet, "/api/secret-scan/settings", nil)).Want(http.StatusOK).JSON(&s)
		if s.Backfill.Status != "running" || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if s.Backfill.Status != "done" || s.Backfill.Scanned != 2 {
		t.Fatalf("backfill = %+v, want done after 2 comments", s.Backfill)
	}
	if got := e.statusOf(t, old); got != StatusOpen {
		t.Fatalf("status = %q, want open", got)
	}
	if got := e.content(t, old); !strings.Contains(got, githubToken) {
		t.Fatal("backfill rewrote a comment")
	}

	var list ListFindingsResponse
	testutil.Call(t, testService.ListFindings, e.request(e.ownerID, http.MethodGet, "/api/secret-scan/findings", nil)).Want(http.StatusOK).JSON(&list)
	if list.Counts.Open != 1 || len(list.Findings) != 1 || list.Findings[0].IssueTitle != "Payments webhook 502" || list.Findings[0].IssueIdentifier == "" {
		t.Fatalf("list = %+v", list)
	}
}
