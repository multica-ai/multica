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
// group's newest row and marks that one read, which reads the whole issue.
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
	f.markRead(items[0].ID)
}

func (f issueInboxFixture) markRead(id string) {
	f.t.Helper()
	testutil.Call(f.t, inboxWorkspaceHandler(testHandler.MarkInboxRead),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+id+"/read", f.workspaceID), "id", id)).
		Want(http.StatusOK)
}

func (f issueInboxFixture) markUnread(id string) {
	f.t.Helper()
	testutil.Call(f.t, inboxWorkspaceHandler(testHandler.MarkInboxUnread),
		withURLParam(inboxRequest(http.MethodPost, "/api/inbox/"+id+"/unread", f.workspaceID), "id", id)).
		Want(http.StatusOK)
}

// rawUnread is every unread active row the caller has in the workspace, the
// count GET /api/inbox/unread-count reports.
func (f issueInboxFixture) rawUnread() int64 {
	f.t.Helper()
	return int64(dbfx.Count(f.t,
		"SELECT count(*) FROM inbox_item WHERE workspace_id = $1 AND recipient_id = $2 AND archived = false AND read = false",
		f.workspaceID, testUserID))
}

// pendingRows is the issue's row in the Inbox badge: 1 while it has an active
// row the Inbox lists, read or not.
func (f issueInboxFixture) pendingRows() int64 {
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

// Items that need handling are read with the rest: opening the issue shows
// all of it (MUL-8618).
func TestMarkIssueInboxReadReadsItemsThatNeedHandling(t *testing.T) {
	for _, pending := range []struct{ notifType, severity string }{
		{"issue_assigned", "action_required"},
		{"task_failed", "action_required"},
		{"autopilot_paused", "attention"},
		{"mentioned", "info"},
	} {
		t.Run(pending.notifType, func(t *testing.T) {
			f := newIssueInboxFixture(t, "reads "+pending.notifType)
			pendingID := f.notify(pending.notifType, pending.severity, 2)
			comment := f.notify("new_comment", "info", 1)

			if got := f.openIssue(); got != 2 {
				t.Fatalf("count = %d, want both rows read", got)
			}
			if !f.read(pendingID) || !f.read(comment) || f.rawUnread() != 0 {
				t.Fatalf("opening the issue left %s unread", pending.notifType)
			}
			// Opening is not handling: the issue still waits in the Inbox.
			if f.pendingRows() != 1 {
				t.Fatalf("opening the issue took %s out of the Inbox", pending.notifType)
			}
		})
	}
}

// The MUL-8618 report: an assignment under a later comment. Opening the row
// in the inbox reads the comment it shows and the assignment under it, so no
// unread row is left behind where the inbox never shows it again.
func TestMarkInboxReadReadsEveryActiveRowOnTheIssue(t *testing.T) {
	f := newIssueInboxFixture(t, "inbox read reads the issue")
	assigned := f.notify("issue_assigned", "action_required", 3)
	comment := f.notify("new_comment", "info", 2)
	archived := f.notify("new_comment", "info", 4)
	dbfx.Exec(t, `UPDATE inbox_item SET archived = true WHERE id = $1`, archived)
	otherIssueID := dbfx.Issue(t, "Other issue", testutil.Cols{"workspace_id": f.workspaceID})
	other := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id":   f.workspaceID,
		"recipient_type": "member",
		"recipient_id":   testUserID,
		"type":           "new_comment",
		"severity":       "info",
		"issue_id":       otherIssueID,
		"title":          "another issue",
	})
	someoneElses := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id":   f.workspaceID,
		"recipient_type": "member",
		"recipient_id":   uuid.NewString(),
		"type":           "issue_assigned",
		"severity":       "action_required",
		"issue_id":       f.issueID,
		"title":          "someone else's",
	})

	f.markRead(comment)
	if !f.read(comment) || !f.read(assigned) {
		t.Fatal("reading the row the inbox shows left the assignment under it unread")
	}
	if f.read(archived) {
		t.Fatal("reading the row read an archived row on the issue")
	}
	if f.read(other) {
		t.Fatal("reading the row read another issue's row")
	}
	if f.read(someoneElses) {
		t.Fatal("reading the row read another recipient's row")
	}
	if got := f.rawUnread(); got != 1 {
		t.Fatalf("raw unread = %d, want only the other issue's row", got)
	}

	// Mark as unread flips only the row the inbox shows; reading it again
	// reads it, and a pending row that arrived since is read with it.
	f.markUnread(comment)
	if f.read(comment) || !f.read(assigned) {
		t.Fatal("mark as unread should flip only the shown row")
	}
	reassigned := f.notify("issue_assigned", "action_required", 1)
	f.markRead(comment)
	if !f.read(comment) || !f.read(reassigned) {
		t.Fatal("reading the row again left a newer assignment unread")
	}
}

// Opening an archived row reads that row and the issue's active rows, the same
// set opening the issue anywhere else reads.
func TestMarkInboxReadOnAnArchivedRowReadsTheIssuesActiveRows(t *testing.T) {
	f := newIssueInboxFixture(t, "inbox read on an archived row")
	archivedOlder := f.notify("new_comment", "info", 3)
	archivedNewest := f.notify("new_comment", "info", 2)
	dbfx.Exec(t, `UPDATE inbox_item SET archived = true WHERE id = ANY($1::uuid[])`, []string{archivedOlder, archivedNewest})
	active := f.notify("issue_assigned", "action_required", 1)

	f.markRead(archivedNewest)
	if !f.read(archivedNewest) || !f.read(active) {
		t.Fatal("want the archived row and the issue's active row read")
	}
	if f.read(archivedOlder) {
		t.Fatal("an archived sibling should be left as it is")
	}
}

func TestMarkInboxReadWithoutAnIssueReadsOnlyTheItem(t *testing.T) {
	f := newIssueInboxFixture(t, "inbox read without an issue")
	insert := func(title string) string {
		t.Helper()
		return dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id":   f.workspaceID,
			"recipient_type": "member",
			"recipient_id":   testUserID,
			"type":           "autopilot_paused",
			"severity":       "attention",
			"title":          title,
		})
	}
	first := insert("first")
	second := insert("second")

	f.markRead(first)
	if !f.read(first) || f.read(second) {
		t.Fatal("reading an item without an issue should read only that item")
	}
}

func (f issueInboxFixture) archived(id string) bool {
	f.t.Helper()
	var archived bool
	dbfx.QueryRow(f.t, `SELECT archived FROM inbox_item WHERE id = $1`, id).Scan(&archived)
	return archived
}

// A notice asks nothing of the member, so opening the issue takes it out of
// the Inbox, from the inbox row and from the issue page alike.
func TestOpeningAnIssueArchivesItsNotices(t *testing.T) {
	for _, open := range []struct {
		name string
		run  func(f issueInboxFixture)
	}{
		{"in the inbox", func(f issueInboxFixture) { f.openInInbox() }},
		{"outside the inbox", func(f issueInboxFixture) { f.openIssue() }},
	} {
		t.Run(open.name, func(t *testing.T) {
			f := newIssueInboxFixture(t, "notice "+open.name)
			reaction := f.notify("reaction_added", "info", 1)
			if f.pendingRows() != 1 {
				t.Fatal("a notice should count in the Inbox until it is opened")
			}
			open.run(f)
			if !f.archived(reaction) || f.pendingRows() != 0 {
				t.Fatal("opening the issue left its notice in the Inbox")
			}
		})
	}
}

// Only the notices leave: a row that waits on the member keeps the issue in
// the Inbox after it is opened.
func TestOpeningAnIssueKeepsRowsThatWaitOnTheMember(t *testing.T) {
	f := newIssueInboxFixture(t, "notice beside a mention")
	mention := f.notify("mentioned", "info", 2)
	reaction := f.notify("reaction_added", "info", 1)

	f.openInInbox()
	if !f.archived(reaction) {
		t.Fatal("opening the issue left its notice in the Inbox")
	}
	if f.archived(mention) || f.pendingRows() != 1 {
		t.Fatal("opening the issue archived a row that waits on the member")
	}
}
