package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func seedIssueUnread(t *testing.T, workspaceID, userID, issueID, outcome string) {
	t.Helper()
	dbfx.InsertNoID(t, "issue_unread", testutil.Cols{
		"workspace_id": workspaceID, "user_id": userID, "issue_id": issueID, "outcome": outcome,
	}, "workspace_id = $1 AND user_id = $2 AND issue_id = $3", workspaceID, userID, issueID)
}

func TestInboxAcknowledgementClearsWorkUnread(t *testing.T) {
	for _, action := range []struct {
		name     string
		handler  http.HandlerFunc
		param    string
		read     bool
		terminal bool
	}{
		{"read", testHandler.MarkInboxRead, "id", false, false},
		{"read issue", testHandler.MarkIssueInboxRead, "issueId", false, false},
		{"read all", testHandler.MarkAllInboxRead, "", false, false},
		{"archive", testHandler.ArchiveInboxItem, "id", false, false},
		{"archive all", testHandler.ArchiveAllInbox, "", false, false},
		{"archive read", testHandler.ArchiveAllReadInbox, "", true, false},
		{"archive completed", testHandler.ArchiveCompletedInbox, "", false, true},
		{"delete", testHandler.DeleteInboxItem, "id", false, false},
	} {
		t.Run(action.name, func(t *testing.T) {
			f := newIssueInboxFixture(t, "Unread sync "+action.name)
			if action.terminal {
				dbfx.Exec(t, "UPDATE issue SET status = 'done' WHERE id = $1", f.issueID)
			}
			item := f.notify("new_comment", "info", 1)
			if action.read {
				dbfx.Exec(t, "UPDATE inbox_item SET read = true WHERE id = $1", item)
			}
			seedIssueUnread(t, f.workspaceID, testUserID, f.issueID, "completed")
			otherIssue := dbfx.Issue(t, "Unrelated unread issue", testutil.Cols{"workspace_id": f.workspaceID})
			if action.terminal {
				dbfx.Exec(t, "UPDATE issue SET status = 'done' WHERE id = $1", otherIssue)
			}
			seedIssueUnread(t, f.workspaceID, testUserID, otherIssue, "manual")
			otherUser := dbfx.User(t, "Other reader", "unread-sync-"+uuid.NewString()+"@example.test")
			seedIssueUnread(t, f.workspaceID, otherUser, f.issueID, "completed")
			// The same caller's unread state in another workspace must survive.
			seedIssueUnread(t, testWorkspaceID, testUserID, uuid.NewString(), "manual")

			req := inboxRequest(http.MethodPost, "/api/inbox", f.workspaceID)
			if action.param != "" {
				id := item
				if action.param == "issueId" {
					id = f.issueID
				}
				req = withURLParam(req, action.param, id)
			}
			testutil.Call(t, inboxWorkspaceHandler(action.handler), req).Want(http.StatusOK)
			if n := dbfx.Count(t, "SELECT count(*) FROM issue_unread WHERE workspace_id = $1 AND user_id = $2 AND issue_id = $3", f.workspaceID, testUserID, f.issueID); n != 0 {
				t.Fatalf("acknowledged issue still has %d Work unread marks", n)
			}
			wantOther := 1
			if action.name == "read all" {
				wantOther = 0
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM issue_unread WHERE workspace_id = $1 AND user_id = $2 AND issue_id = $3", f.workspaceID, testUserID, otherIssue); n != wantOther {
				t.Fatalf("unrelated issue unread = %d, want %d", n, wantOther)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM issue_unread WHERE workspace_id = $1 AND user_id = $2", f.workspaceID, otherUser); n != 1 {
				t.Fatal("acknowledging an issue changed another member's unread state")
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM issue_unread WHERE workspace_id = $1 AND user_id = $2", testWorkspaceID, testUserID); n != 1 {
				t.Fatal("acknowledging an issue changed another workspace's unread state")
			}
		})
	}
}

func TestIssueInboxAutoReadPreservesManualWorkReminder(t *testing.T) {
	f := newIssueInboxFixture(t, "Manual reminder while viewing")
	seedIssueUnread(t, f.workspaceID, testUserID, f.issueID, "manual")
	f.notify("new_comment", "info", 1)
	if count := f.openIssue(); count != 1 {
		t.Fatalf("read %d notifications, want 1", count)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_unread WHERE issue_id = $1 AND user_id = $2", f.issueID, testUserID); n != 1 {
		t.Fatal("background inbox acknowledgement erased a deliberate Work reminder")
	}
}

func TestMarkAllInboxReadClearsUnreadWithoutNotifications(t *testing.T) {
	f := newIssueInboxFixture(t, "Unread without inbox")
	seedIssueUnread(t, f.workspaceID, testUserID, f.issueID, "completed")
	seedIssueUnread(t, f.workspaceID, testUserID, uuid.NewString(), "manual")
	var out struct {
		Count int64 `json:"count"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.MarkAllInboxRead), inboxRequest(http.MethodPost, "/api/inbox/read-all", f.workspaceID)).
		Want(http.StatusOK).JSON(&out)
	if out.Count != 0 {
		t.Fatalf("notification count changed contract: %d", out.Count)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_unread WHERE workspace_id = $1 AND user_id = $2", f.workspaceID, testUserID); n != 0 {
		t.Fatalf("mark all read left %d orphan/manual unread marks", n)
	}
}

func TestIssueInboxReadNotifiesWhenOnlyTheWorkMarkChanged(t *testing.T) {
	f := newIssueInboxFixture(t, "Unread-only acknowledgement")
	seedIssueUnread(t, f.workspaceID, testUserID, f.issueID, "completed")
	published := 0
	testHandler.Bus.Subscribe(protocol.EventInboxBatchRead, func(e events.Event) {
		if e.Payload.(map[string]any)["issue_id"] == f.issueID {
			published++
		}
	})
	if count := f.openIssue(); count != 0 {
		t.Fatalf("notification count = %d, want 0", count)
	}
	if published != 1 {
		t.Fatalf("acknowledged Work mark sent %d refresh events, want 1", published)
	}
	if count := f.openIssue(); count != 0 || published != 1 {
		t.Fatal("repeating a no-op acknowledgement sent another refresh")
	}
}

func TestInboxReadFailureRollsBackWorkAcknowledgement(t *testing.T) {
	f := newIssueInboxFixture(t, "Atomic unread acknowledgement")
	item := f.notify("new_comment", "info", 1)
	seedIssueUnread(t, f.workspaceID, testUserID, f.issueID, "completed")
	name := "unread_sync_fail_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	dbfx.Exec(t, "CREATE FUNCTION "+name+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected inbox write failure'; END $$")
	dbfx.Cleanup(t, "DROP FUNCTION "+name+"()")
	dbfx.Exec(t, "CREATE TRIGGER "+name+" BEFORE UPDATE ON inbox_item FOR EACH ROW WHEN (OLD.workspace_id = '"+f.workspaceID+"') EXECUTE FUNCTION "+name+"()")
	dbfx.Cleanup(t, "DROP TRIGGER "+name+" ON inbox_item")
	testutil.Call(t, inboxWorkspaceHandler(testHandler.MarkAllInboxRead), inboxRequest(http.MethodPost, "/api/inbox/mark-all-read", f.workspaceID)).Want(http.StatusInternalServerError)
	if f.read(item) {
		t.Fatal("failed acknowledgement marked the notification read")
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_unread WHERE workspace_id = $1 AND user_id = $2", f.workspaceID, testUserID); n != 1 {
		t.Fatal("failed acknowledgement cleared the Work mark")
	}
}

func TestWorkUnreadFollowsNewestActiveNotification(t *testing.T) {
	userID := dbfx.User(t, "Work reader", "work-reader-"+uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, userID, "member")
	for _, outcome := range []string{"completed", "failed", "manual"} {
		for _, state := range []string{"unread", "read", "archived", "missing", "older unread", "other recipient", "agent recipient", "other workspace"} {
			t.Run(outcome+"/"+state, func(t *testing.T) {
				issue := dbfx.Issue(t, "Unread visibility")
				seedIssueUnread(t, testWorkspaceID, userID, issue, outcome)
				if state != "missing" {
					cols := testutil.Cols{
						"workspace_id": testWorkspaceID, "recipient_type": "member", "recipient_id": userID,
						"issue_id": issue, "type": "new_comment", "severity": "info", "title": "Run result",
						"read": state == "read" || state == "older unread", "archived": state == "archived",
					}
					if state == "older unread" {
						dbfx.Insert(t, "inbox_item", testutil.Cols{
							"workspace_id": testWorkspaceID, "recipient_type": "member", "recipient_id": userID,
							"issue_id": issue, "type": "mentioned", "severity": "info", "title": "Old result",
							"created_at": testutil.Raw("now() - interval '1 minute'"),
						})
					}
					if state == "other recipient" {
						cols["recipient_id"] = testUserID
					}
					if state == "agent recipient" {
						cols["recipient_type"] = "agent"
					}
					if state == "other workspace" {
						cols["workspace_id"] = dbfx.Workspace(t, "Other inbox", "other-inbox-"+uuid.NewString())
					}
					dbfx.Insert(t, "inbox_item", cols)
				}
				want := 0
				if outcome == "manual" || state == "unread" {
					want = 1
				}
				items := listIssueUnreadAs(t, userID)
				if len(items) != want || (len(items) == 1 && items[0].IssueID != issue) {
					t.Fatalf("Work unread = %+v, want %d items", items, want)
				}
			})
		}
	}
}

func TestDeletingArchivedHistoryPreservesActiveWorkUnread(t *testing.T) {
	f := newIssueInboxFixture(t, "Archived history unread")
	history := f.notify("new_comment", "info", 2)
	dbfx.Exec(t, "UPDATE inbox_item SET archived = true WHERE id = $1", history)
	f.notify("new_comment", "info", 1)
	seedIssueUnread(t, f.workspaceID, testUserID, f.issueID, "completed")
	testutil.Call(t, inboxWorkspaceHandler(testHandler.DeleteInboxItem),
		withURLParam(inboxRequest(http.MethodDelete, "/api/inbox/"+history, f.workspaceID), "id", history)).Want(http.StatusOK)
	if n := dbfx.Count(t, "SELECT count(*) FROM issue_unread WHERE workspace_id = $1 AND user_id = $2 AND issue_id = $3", f.workspaceID, testUserID, f.issueID); n != 1 {
		t.Fatal("deleting archived history acknowledged the active result")
	}
}
