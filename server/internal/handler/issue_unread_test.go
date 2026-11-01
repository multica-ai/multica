package handler

import (
	"net/http"
	"net/url"
	"testing"

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
	if len(items) != 1 || items[0].IssueID != issueID || items[0].Outcome != "manual" || items[0].AgentID != nil {
		t.Fatalf("want one manual unread row for the issue, got %+v", items)
	}

	setIssueUnreadAs(t, testUserID, issueID, false).Want(http.StatusOK)
	setIssueUnreadAs(t, testUserID, issueID, false).Want(http.StatusOK)
	if items := listIssueUnreadAs(t, testUserID); len(items) != 0 {
		t.Fatalf("want no unread rows after mark read, got %+v", items)
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
