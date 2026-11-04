package handler

import (
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
