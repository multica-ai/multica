package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type issueUnreadList struct {
	Items      []IssueUnreadResponse `json:"items"`
	NextCursor *string               `json:"next_cursor"`
}

func listIssueUnreadPageAs(t *testing.T, userID, cursor string) issueUnreadList {
	t.Helper()
	var out issueUnreadList
	req := newRequestAs(userID, "GET", "/api/issues/unread?cursor="+url.QueryEscape(cursor), nil)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	testutil.Call(t, testHandler.ListIssueUnread, req).
		Want(http.StatusOK).JSON(&out)
	return out
}

func listIssueUnreadAs(t *testing.T, userID string) []IssueUnreadResponse {
	t.Helper()
	page := listIssueUnreadPageAs(t, userID, "")
	if page.NextCursor != nil {
		t.Fatalf("expected a single page, got next_cursor %q", *page.NextCursor)
	}
	return page.Items
}

func setIssueUnreadAs(t *testing.T, userID, issueID string, unread bool) *testutil.Response {
	t.Helper()
	h, verb := testHandler.MarkIssueRead, "read"
	if unread {
		h, verb = testHandler.MarkIssueUnread, "unread"
	}
	req := withURLParam(newRequestAs(userID, "POST", "/api/issues/"+issueID+"/"+verb, nil), "id", issueID)
	return testutil.Call(t, h, req)
}

func TestIssueUnreadMarkAndClear(t *testing.T) {
	issueID := dbfx.Issue(t, "issue unread round trip")
	dbfx.Cleanup(t, `DELETE FROM issue_unread WHERE issue_id = $1`, issueID)

	setIssueUnreadAs(t, testUserID, issueID, true).Want(http.StatusOK)
	setIssueUnreadAs(t, testUserID, issueID, true).Want(http.StatusOK)
	items := listIssueUnreadAs(t, testUserID)
	if len(items) != 1 || items[0].IssueID != issueID || items[0].Outcome != "manual" || items[0].AgentID != nil ||
		items[0].TaskID != nil || items[0].CommentID != nil {
		t.Fatalf("want one manual unread row for the issue, got %+v", items)
	}

	setIssueUnreadAs(t, testUserID, issueID, false).Want(http.StatusOK)
	setIssueUnreadAs(t, testUserID, issueID, false).Want(http.StatusOK)
	if items := listIssueUnreadAs(t, testUserID); len(items) != 0 {
		t.Fatalf("want no unread rows after mark read, got %+v", items)
	}
}

func TestIssueUnreadPointsAtTheRunsLastComment(t *testing.T) {
	userID := dbfx.User(t, "Unread Landing", "issue-unread-landing@multica.ai")
	dbfx.Member(t, testWorkspaceID, userID, "member")
	dbfx.Cleanup(t, `DELETE FROM issue_unread WHERE user_id = $1`, userID)
	withComments := dbfx.Issue(t, "unread run with comments")
	silent := dbfx.Issue(t, "unread run without comments")
	elsewhere := dbfx.Issue(t, "issue the run also commented on")
	runID, silentRunID := uuid.NewString(), uuid.NewString()

	at := func(minutesAgo int) testutil.Raw {
		return testutil.Raw(fmt.Sprintf("now() - interval '%d minutes'", minutesAgo))
	}
	dbfx.Comment(t, withComments, "progress", testutil.Cols{"source_task_id": runID, "created_at": at(5)})
	result := dbfx.Comment(t, withComments, "result", testutil.Cols{"source_task_id": runID, "created_at": at(4)})
	dbfx.Comment(t, withComments, "deleted", testutil.Cols{"source_task_id": runID, "created_at": at(3), "deleted_at": at(2)})
	dbfx.Comment(t, withComments, "someone else", testutil.Cols{"created_at": at(2)})
	dbfx.Comment(t, elsewhere, "same run, other issue", testutil.Cols{"source_task_id": runID, "created_at": at(1)})

	dbfx.Exec(t, `
INSERT INTO issue_unread (workspace_id, user_id, issue_id, outcome, task_id)
VALUES ($1, $2, $3, 'completed', $4), ($1, $2, $5, 'failed', $6)`,
		testWorkspaceID, userID, withComments, runID, silent, silentRunID)

	byIssue := map[string]IssueUnreadResponse{}
	for _, item := range listIssueUnreadAs(t, userID) {
		byIssue[item.IssueID] = item
	}
	if got := byIssue[withComments]; got.TaskID == nil || *got.TaskID != runID || got.CommentID == nil || *got.CommentID != result {
		t.Fatalf("want the run's last live comment on this issue %s, got %+v", result, got)
	}
	if got := byIssue[silent]; got.TaskID == nil || *got.TaskID != silentRunID || got.CommentID != nil {
		t.Fatalf("want the run id and no comment for a run that left none, got %+v", got)
	}
}

func markIssueReadInboxCount(t *testing.T, userID, issueID string) int64 {
	t.Helper()
	var out struct {
		InboxRead int64 `json:"inbox_read"`
	}
	setIssueUnreadAs(t, userID, issueID, false).Want(http.StatusOK).JSON(&out)
	return out.InboxRead
}

func inboxRow(t *testing.T, recipientID, issueID, notifType, severity string, extra ...testutil.Cols) string {
	t.Helper()
	cols := testutil.Cols{
		"workspace_id":   testWorkspaceID,
		"recipient_type": "member",
		"recipient_id":   recipientID,
		"type":           notifType,
		"severity":       severity,
		"issue_id":       issueID,
		"title":          notifType,
	}
	for _, over := range extra {
		for k, v := range over {
			cols[k] = v
		}
	}
	return dbfx.Insert(t, "inbox_item", cols)
}

func inboxRowRead(t *testing.T, id string) bool {
	t.Helper()
	var read bool
	dbfx.QueryRow(t, `SELECT read FROM inbox_item WHERE id = $1`, id).Scan(&read)
	return read
}

func TestIssueReadClearsTheIssuesInfoInbox(t *testing.T) {
	issueID := dbfx.Issue(t, "issue read clears info inbox")
	otherIssueID := dbfx.Issue(t, "issue read leaves other issues")
	otherID := dbfx.User(t, "Inbox Other", "issue-read-inbox-other@multica.ai")
	dbfx.Member(t, testWorkspaceID, otherID, "member")

	comment := inboxRow(t, testUserID, issueID, "new_comment", "info")
	status := inboxRow(t, testUserID, issueID, "status_changed", "info")
	archived := inboxRow(t, testUserID, issueID, "new_comment", "info", testutil.Cols{"archived": true})
	otherIssue := inboxRow(t, testUserID, otherIssueID, "new_comment", "info")
	otherUser := inboxRow(t, otherID, issueID, "new_comment", "info")

	if n := markIssueReadInboxCount(t, testUserID, issueID); n != 2 {
		t.Fatalf("want 2 notifications read, got %d", n)
	}
	if !inboxRowRead(t, comment) || !inboxRowRead(t, status) {
		t.Fatal("the issue's info notifications were left unread")
	}
	if inboxRowRead(t, archived) || inboxRowRead(t, otherIssue) || inboxRowRead(t, otherUser) {
		t.Fatal("marked an archived row, another issue's row, or another member's row read")
	}
	if n := markIssueReadInboxCount(t, testUserID, issueID); n != 0 {
		t.Fatalf("a repeated read reported %d notifications", n)
	}
}

func TestIssueReadKeepsInboxThatNeedsHandling(t *testing.T) {
	for _, kept := range []struct{ notifType, severity string }{
		{"issue_assigned", "action_required"},
		{"task_failed", "action_required"},
		{"autopilot_paused", "attention"},
		{"mentioned", "info"},
	} {
		t.Run(kept.notifType, func(t *testing.T) {
			issueID := dbfx.Issue(t, "issue read keeps "+kept.notifType)
			keptID := inboxRow(t, testUserID, issueID, kept.notifType, kept.severity,
				testutil.Cols{"created_at": testutil.Raw("now() - interval '1 minute'")})
			comment := inboxRow(t, testUserID, issueID, "new_comment", "info")

			// The newest row is the comment, so reading it would turn the
			// issue's Inbox row read and bury the kept item.
			if n := markIssueReadInboxCount(t, testUserID, issueID); n != 0 {
				t.Fatalf("want nothing read while %s is unread, got %d", kept.notifType, n)
			}
			if inboxRowRead(t, keptID) || inboxRowRead(t, comment) {
				t.Fatalf("an issue with an unread %s had notifications marked read", kept.notifType)
			}

			dbfx.Exec(t, `UPDATE inbox_item SET read = true WHERE id = $1`, keptID)
			if n := markIssueReadInboxCount(t, testUserID, issueID); n != 1 || !inboxRowRead(t, comment) {
				t.Fatalf("want the comment read once %s is handled, got %d", kept.notifType, n)
			}
		})
	}
}

func TestIssueUnreadIsPerUser(t *testing.T) {
	issueID := dbfx.Issue(t, "issue unread per user")
	dbfx.Cleanup(t, `DELETE FROM issue_unread WHERE issue_id = $1`, issueID)
	otherID := dbfx.User(t, "Unread Other", "issue-unread-other@multica.ai")
	dbfx.Member(t, testWorkspaceID, otherID, "member")

	setIssueUnreadAs(t, testUserID, issueID, true).Want(http.StatusOK)
	if items := listIssueUnreadAs(t, otherID); len(items) != 0 {
		t.Fatalf("another member saw the caller's unread mark: %+v", items)
	}
	setIssueUnreadAs(t, otherID, issueID, false).Want(http.StatusOK)
	if items := listIssueUnreadAs(t, testUserID); len(items) != 1 {
		t.Fatalf("another member's mark-read cleared the caller's mark: %+v", items)
	}
}

func TestIssueUnreadRejectsIssueInAnotherWorkspace(t *testing.T) {
	otherWorkspaceID := dbfx.Workspace(t, "Unread Other WS", "issue-unread-other-ws")
	issueID := dbfx.Issue(t, "foreign issue", testutil.Cols{"workspace_id": otherWorkspaceID})
	dbfx.Cleanup(t, `DELETE FROM issue_unread WHERE issue_id = $1`, issueID)

	setIssueUnreadAs(t, testUserID, issueID, true).Want(http.StatusNotFound)
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_unread WHERE issue_id = $1`, issueID); n != 0 {
		t.Fatalf("an issue in another workspace was marked unread")
	}
}

func TestIssueUnreadPagesReachEveryRow(t *testing.T) {
	userID := dbfx.User(t, "Unread Pages", "issue-unread-pages@multica.ai")
	dbfx.Member(t, testWorkspaceID, userID, "member")
	dbfx.Cleanup(t, `DELETE FROM issue_unread WHERE user_id = $1`, userID)
	// Rows share timestamps in pairs, so a page boundary can fall between two
	// rows with the same created_at.
	const total = 2*issueUnreadPageSize + 1
	dbfx.Exec(t, `
INSERT INTO issue_unread (workspace_id, user_id, issue_id, outcome, created_at)
SELECT $1, $2, gen_random_uuid(), 'completed', now() - (n / 2) * interval '1 second'
FROM generate_series(1, $3::int) AS n`, testWorkspaceID, userID, total)

	seen := map[string]bool{}
	cursor, pages := "", 0
	for {
		page := listIssueUnreadPageAs(t, userID, cursor)
		pages++
		for _, item := range page.Items {
			if seen[item.IssueID] {
				t.Fatalf("issue %s returned on two pages", item.IssueID)
			}
			seen[item.IssueID] = true
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	if pages != 3 || len(seen) != total {
		t.Fatalf("want %d rows over 3 pages, got %d rows over %d pages", total, len(seen), pages)
	}

	req := newRequestAs(userID, "GET", "/api/issues/unread?cursor=nope", nil)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	testutil.Call(t, testHandler.ListIssueUnread, req).Want(http.StatusBadRequest)
}
