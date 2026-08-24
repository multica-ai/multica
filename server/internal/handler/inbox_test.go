package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func inboxRequest(method, path, workspaceID string) *http.Request {
	return testutil.WithHeaders(
		testutil.JSONRequest(method, path, nil),
		"X-User-ID", testUserID,
		"X-Workspace-ID", workspaceID,
	)
}

func inboxWorkspaceHandler(handler http.HandlerFunc) http.HandlerFunc {
	return middleware.RequireWorkspaceMember(testHandler.Queries)(handler).ServeHTTP
}

func TestListInboxProjectsCurrentIssueStatusAndPriority(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Inbox filter projections", "inbox-filter-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	issueID := dbfx.Issue(t, "Filtered issue", testutil.Cols{
		"workspace_id": workspaceID,
		"status":       "in_review",
		"priority":     "high",
	})
	dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id":   workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           "status_changed",
		"severity":       "info",
		"issue_id":       issueID,
		"title":          "Projected issue",
	})

	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox", workspaceID)).
		Want(http.StatusOK).
		JSON(&items)

	if len(items) != 1 {
		t.Fatalf("inbox items = %d, want 1: %+v", len(items), items)
	}
	if items[0].IssueStatus == nil || *items[0].IssueStatus != "in_review" {
		t.Errorf("issue_status = %v, want in_review", items[0].IssueStatus)
	}
	if items[0].IssuePriority == nil || *items[0].IssuePriority != "high" {
		t.Errorf("issue_priority = %v, want high", items[0].IssuePriority)
	}
}

func TestListArchivedInboxLimitsIssueGroupsNotRows(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Archived inbox groups", "archived-groups-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	noisyIssueID := dbfx.Issue(t, "Noisy archived issue", testutil.Cols{"workspace_id": workspaceID})
	olderIssueID := dbfx.Issue(t, "Older archived issue", testutil.Cols{"workspace_id": workspaceID})

	base := time.Now().UTC().Add(-time.Minute)
	for i := 0; i < 200; i++ {
		cols := testutil.Cols{
			"workspace_id":   workspaceID,
			"recipient_type": "member",
			"recipient_id":   testUserID,
			"type":           "status_changed",
			"severity":       "info",
			"issue_id":       noisyIssueID,
			"title":          fmt.Sprintf("noisy-%03d", i),
			"archived":       true,
			"created_at":     base.Add(-time.Duration(i) * time.Millisecond),
		}
		if i == 199 {
			// The bounded response keeps this row as the group's comment anchor,
			// even though the newest status row is the one the UI renders.
			cols["details"] = testutil.Raw(`'{"comment_id":"comment-1"}'::jsonb`)
		}
		dbfx.Insert(t, "inbox_item", cols)
	}
	dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id":   workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           "new_comment",
		"severity":       "info",
		"issue_id":       olderIssueID,
		"title":          "older-group",
		"archived":       true,
		"created_at":     base.Add(-time.Hour),
	})

	var items []InboxItemResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInbox),
		inboxRequest(http.MethodGet, "/api/inbox/archived", workspaceID)).
		Want(http.StatusOK).
		JSON(&items)

	var noisyRows int
	var sawNoisyNewest, sawCommentAnchor, sawOlderGroup bool
	for _, item := range items {
		switch {
		case item.IssueID != nil && *item.IssueID == noisyIssueID:
			noisyRows++
			sawNoisyNewest = sawNoisyNewest || item.Title == "noisy-000"
			sawCommentAnchor = sawCommentAnchor || strings.Contains(string(item.Details), `"comment_id":"comment-1"`)
		case item.IssueID != nil && *item.IssueID == olderIssueID:
			sawOlderGroup = true
		}
	}
	if noisyRows != 2 || !sawNoisyNewest || !sawCommentAnchor {
		t.Fatalf("noisy group rows = %d, newest=%v anchor=%v; items=%+v",
			noisyRows, sawNoisyNewest, sawCommentAnchor, items)
	}
	if !sawOlderGroup {
		t.Fatal("raw-row limit let one issue hide another archived issue group")
	}
}

func TestArchiveAllReadInboxUsesNewestIssueRow(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Archive read groups", "archive-read-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	readIssueID := dbfx.Issue(t, "Newest row is read", testutil.Cols{"workspace_id": workspaceID})
	unreadIssueID := dbfx.Issue(t, "Newest row is unread", testutil.Cols{"workspace_id": workspaceID})

	insert := func(issueID, title string, read bool, createdAt testutil.Raw) {
		t.Helper()
		dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id":   workspaceID,
			"recipient_type": "member",
			"recipient_id":   testUserID,
			"type":           "status_changed",
			"severity":       "info",
			"issue_id":       issueID,
			"title":          title,
			"read":           read,
			"archived":       false,
			"created_at":     createdAt,
		})
	}
	insert(readIssueID, "older unread", false, "now() - interval '2 minutes'")
	insert(readIssueID, "newest read", true, "now() - interval '1 minute'")
	insert(unreadIssueID, "older read", true, "now() - interval '2 minutes'")
	insert(unreadIssueID, "newest unread", false, "now() - interval '1 minute'")

	testutil.Call(t, inboxWorkspaceHandler(testHandler.ArchiveAllReadInbox),
		inboxRequest(http.MethodPost, "/api/inbox/archive-all-read", workspaceID)).
		Want(http.StatusOK)

	if got := dbfx.Count(t,
		"SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND archived = true", readIssueID); got != 2 {
		t.Fatalf("archived rows in read issue = %d, want the whole two-row group", got)
	}
	if got := dbfx.Count(t,
		"SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND archived = true", unreadIssueID); got != 0 {
		t.Fatalf("archived rows in unread issue = %d, want the whole group untouched", got)
	}
}
