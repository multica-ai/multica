package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestMarkIssueInboxReadReadsOnlyTheCallersActiveRowsOnThatIssue(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Issue inbox read", "issue-inbox-read-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	viewedIssueID := dbfx.Issue(t, "Viewed issue", testutil.Cols{"workspace_id": workspaceID})
	otherIssueID := dbfx.Issue(t, "Other issue", testutil.Cols{"workspace_id": workspaceID})
	otherRecipientID := uuid.NewString()
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)

	insert := func(issueID, recipientID, title string, archived bool) {
		t.Helper()
		dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id":   workspaceID,
			"recipient_type": "member",
			"recipient_id":   recipientID,
			"type":           "new_comment",
			"severity":       "info",
			"issue_id":       issueID,
			"title":          title,
			"read":           false,
			"archived":       archived,
		})
	}
	insert(viewedIssueID, testUserID, "older", false)
	insert(viewedIssueID, testUserID, "newest", false)
	insert(viewedIssueID, testUserID, "archived", true)
	insert(viewedIssueID, otherRecipientID, "someone else's", false)
	insert(otherIssueID, testUserID, "another issue", false)

	// Bus handlers are never unsubscribed in this suite, so keep only this
	// issue's events. Publish is synchronous: the channel is final on return.
	published := make(chan map[string]any, 4)
	testHandler.Bus.Subscribe(protocol.EventInboxBatchRead, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok || payload["issue_id"] != viewedIssueID {
			return
		}
		select {
		case published <- payload:
		default:
		}
	})

	call := func() int64 {
		t.Helper()
		var resp struct {
			Count int64 `json:"count"`
		}
		testutil.Call(t, inboxWorkspaceHandler(testHandler.MarkIssueInboxRead),
			withURLParam(inboxRequest(http.MethodPost, "/api/inbox/issues/"+viewedIssueID+"/read", workspaceID), "issueId", viewedIssueID)).
			Want(http.StatusOK).
			JSON(&resp)
		return resp.Count
	}

	if got := call(); got != 2 {
		t.Fatalf("count = %d, want the caller's two active rows on the issue", got)
	}
	if got := dbfx.Count(t,
		"SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND recipient_id = $2 AND archived = false AND read = false",
		viewedIssueID, testUserID); got != 0 {
		t.Fatalf("caller's unread active rows on the issue = %d, want 0", got)
	}
	if got := dbfx.Count(t,
		"SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND recipient_id = $2 AND archived = true AND read = false",
		viewedIssueID, testUserID); got != 1 {
		t.Fatalf("caller's unread archived rows = %d, want the archived row untouched", got)
	}
	if got := dbfx.Count(t,
		"SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND recipient_id = $2 AND read = false",
		viewedIssueID, otherRecipientID); got != 1 {
		t.Fatalf("another recipient's unread rows = %d, want 1", got)
	}
	if got := dbfx.Count(t,
		"SELECT count(*) FROM inbox_item WHERE issue_id = $1 AND read = false", otherIssueID); got != 1 {
		t.Fatalf("unread rows on another issue = %d, want 1", got)
	}

	select {
	case payload := <-published:
		if payload["recipient_id"] != testUserID || payload["count"] != int64(2) {
			t.Fatalf("inbox:batch-read payload = %+v, want the caller and count 2", payload)
		}
	default:
		t.Fatal("a write that read rows published no inbox:batch-read")
	}

	// Reopening an issue with nothing unread is the common case: no rows and
	// no event, so other clients do not refetch their inbox.
	if got := call(); got != 0 {
		t.Fatalf("second count = %d, want 0", got)
	}
	select {
	case payload := <-published:
		t.Fatalf("a no-op write published inbox:batch-read: %+v", payload)
	default:
	}
}

func TestMarkIssueInboxReadRejectsInvalidIssueID(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Issue inbox read invalid", "issue-inbox-read-invalid-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")

	testutil.Call(t, inboxWorkspaceHandler(testHandler.MarkIssueInboxRead),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/issues/nope/read", workspaceID), "issueId", "nope")).
		Want(http.StatusBadRequest)
}

type issueInboxFixture struct {
	t           *testing.T
	workspaceID string
	issueID     string
}

func newIssueInboxFixture(t *testing.T, name string) issueInboxFixture {
	t.Helper()
	workspaceID := dbfx.Workspace(t, name, "issue-inbox-mixed-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	issueID := dbfx.Issue(t, name, testutil.Cols{"workspace_id": workspaceID})
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
	return issueInboxFixture{t: t, workspaceID: workspaceID, issueID: issueID}
}

func (f issueInboxFixture) notify(notifType, severity string, minutesAgo int) string {
	f.t.Helper()
	return dbfx.Insert(f.t, "inbox_item", testutil.Cols{
		"workspace_id":   f.workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           notifType,
		"severity":       severity,
		"issue_id":       f.issueID,
		"title":          notifType,
		"created_at":     testutil.Raw(fmt.Sprintf("now() - interval '%d minutes'", minutesAgo)),
	})
}

// openIssue is opening the issue outside the inbox.
func (f issueInboxFixture) openIssue() int64 {
	f.t.Helper()
	var resp struct {
		Count int64 `json:"count"`
	}
	testutil.Call(f.t, inboxWorkspaceHandler(testHandler.MarkIssueInboxRead),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/issues/"+f.issueID+"/read", f.workspaceID), "issueId", f.issueID)).
		Want(http.StatusOK).
		JSON(&resp)
	return resp.Count
}

// openInInbox is selecting the issue's row in the inbox: it renders the
// group's newest row and marks that one read.
func (f issueInboxFixture) openInInbox() {
	f.t.Helper()
	var items []InboxItemResponse
	testutil.Call(f.t, inboxWorkspaceHandler(testHandler.ListInbox),
		inboxRequest(http.MethodGet, "/api/inbox", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&items)
	if len(items) == 0 {
		f.t.Fatal("the inbox lists nothing for the issue")
	}
	newest := items[0].ID
	testutil.Call(f.t, inboxWorkspaceHandler(testHandler.MarkInboxRead),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+newest+"/read", f.workspaceID), "id", newest)).
		Want(http.StatusOK)
}

// unreadRows is the issue's row in the unread badge: 1 while its newest
// active row is unread.
func (f issueInboxFixture) unreadRows() int64 {
	f.t.Helper()
	var summary []InboxWorkspaceUnreadResponse
	testutil.Call(f.t, inboxWorkspaceHandler(testHandler.UnreadInboxSummary),
		inboxRequest(http.MethodGet, "/api/inbox/unread-summary", f.workspaceID)).
		Want(http.StatusOK).
		JSON(&summary)
	for _, s := range summary {
		if s.WorkspaceID == f.workspaceID {
			return s.Count
		}
	}
	return 0
}

func (f issueInboxFixture) read(id string) bool {
	f.t.Helper()
	var read bool
	dbfx.QueryRow(f.t, `SELECT read FROM inbox_item WHERE id = $1`, id).Scan(&read)
	return read
}

func TestMarkIssueInboxReadKeepsPendingItemsThatNeedHandling(t *testing.T) {
	for _, kept := range []struct{ notifType, severity string }{
		{"issue_assigned", "action_required"},
		{"task_failed", "action_required"},
		{"autopilot_paused", "attention"},
		{"mentioned", "info"},
	} {
		t.Run(kept.notifType, func(t *testing.T) {
			f := newIssueInboxFixture(t, "keeps "+kept.notifType)
			keptID := f.notify(kept.notifType, kept.severity, 2)
			comment := f.notify("new_comment", "info", 1)

			// The comment is the row the inbox shows: reading it would turn the
			// row read with the kept item still waiting behind it.
			if got := f.openIssue(); got != 0 {
				t.Fatalf("count = %d, want nothing read while %s is pending", got, kept.notifType)
			}
			if f.read(keptID) || f.read(comment) || f.unreadRows() != 1 {
				t.Fatalf("opening the issue read past a pending %s", kept.notifType)
			}
		})
	}

	t.Run("kept row is the newest", func(t *testing.T) {
		f := newIssueInboxFixture(t, "keeps the newest row")
		comment := f.notify("new_comment", "info", 2)
		assigned := f.notify("issue_assigned", "action_required", 1)
		if got := f.openIssue(); got != 0 || f.read(comment) || f.read(assigned) {
			t.Fatalf("count = %d, want the assignment row left unread as it is", got)
		}
	})
}

func TestMarkIssueInboxReadIsNotBlockedByAnAssignmentTheInboxReadPast(t *testing.T) {
	f := newIssueInboxFixture(t, "assignment the inbox read past")
	assigned := f.notify("issue_assigned", "action_required", 3)
	f.notify("new_comment", "info", 2)

	// The inbox shows and reads only the newest comment; the assignment under
	// it stays unread for good.
	f.openInInbox()
	if f.read(assigned) || f.unreadRows() != 0 {
		t.Fatal("setup: want the inbox row read with the assignment still unread under it")
	}

	next := f.notify("new_comment", "info", 1)
	if f.unreadRows() != 1 {
		t.Fatal("setup: a new comment should make the issue's row unread again")
	}
	if got := f.openIssue(); got != 1 || !f.read(next) {
		t.Fatalf("count = %d, want the new comment read", got)
	}
	if f.unreadRows() != 0 {
		t.Fatal("the issue's inbox row is still unread after opening the issue")
	}
	if f.read(assigned) {
		t.Fatal("opening the issue read the assignment")
	}

	// An assignment that arrives after that is pending again.
	reassigned := f.notify("issue_assigned", "action_required", 0)
	f.notify("new_comment", "info", 0)
	if got := f.openIssue(); got != 0 || f.read(reassigned) || f.unreadRows() != 1 {
		t.Fatalf("count = %d, want a new assignment to keep the row unread", got)
	}
}
