package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestDeleteInboxItem(t *testing.T) {
	ws := dbfx.Workspace(t, "Inbox deletion", "inbox-delete-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	issue := dbfx.Issue(t, "Keep the issue", testutil.Cols{"workspace_id": ws})
	otherWS := dbfx.Workspace(t, "Other inbox", "inbox-delete-other-"+uuid.NewString())
	dbfx.Member(t, otherWS, testUserID, "owner")
	otherUser := dbfx.User(t, "Other recipient", "inbox-delete-"+uuid.NewString()+"@example.test")
	dbfx.Member(t, ws, otherUser, "member")

	insert := func(workspace, recipient string, issueID any, archived bool) string {
		return dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id": workspace, "recipient_type": "member", "recipient_id": recipient,
			"type": "task_failed", "severity": "info", "title": "Failed run",
			"issue_id": issueID, "archived": archived,
		})
	}
	target := insert(ws, testUserID, issue, false)
	sibling := insert(ws, testUserID, issue, false)
	history := insert(ws, testUserID, issue, true)
	otherRecipient := insert(ws, otherUser, issue, false)
	foreign := insert(otherWS, testUserID, nil, false)
	standalone := insert(ws, testUserID, nil, false)
	anotherStandalone := insert(ws, testUserID, nil, false)
	agent := dbfx.Agent(t, "Deletion test agent", "", testutil.Cols{"workspace_id": ws})
	task := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "failed", "completed_at": time.Now()})

	published := 0
	testHandler.Bus.Subscribe(protocol.EventInboxDeleted, func(e events.Event) {
		payload := e.Payload.(map[string]any)
		if payload["item_id"] == target {
			published++
			if payload["recipient_id"] != testUserID || payload["count"] != int64(2) {
				t.Errorf("deletion payload = %+v", payload)
			}
		}
	})

	remove := func(id, workspace string, status int) int64 {
		var out struct {
			Count int64 `json:"count"`
		}
		call := testutil.Call(t, inboxWorkspaceHandler(testHandler.DeleteInboxItem),
			withURLParam(inboxRequest(http.MethodDelete, "/api/inbox/"+id, workspace), "id", id)).Want(status)
		if status == http.StatusOK {
			call.JSON(&out)
		}
		return out.Count
	}
	remove(otherRecipient, ws, http.StatusNotFound)
	remove(foreign, ws, http.StatusNotFound)
	remove("not-a-uuid", ws, http.StatusBadRequest)
	remove(uuid.NewString(), ws, http.StatusNotFound)
	listArchived := func() []InboxItemResponse {
		var items []InboxItemResponse
		testutil.Call(t, inboxWorkspaceHandler(testHandler.ListArchivedInbox),
			inboxRequest(http.MethodGet, "/api/inbox/archived", ws)).Want(http.StatusOK).JSON(&items)
		return items
	}
	if items := listArchived(); len(items) != 0 {
		t.Fatalf("issue with active notifications appeared in archive: %+v", items)
	}
	if count := remove(target, ws, http.StatusOK); count != 2 {
		t.Fatalf("deleted %d notifications, want 2", count)
	}
	if published != 1 {
		t.Fatalf("deletion events = %d, want 1", published)
	}
	if items := listArchived(); len(items) != 1 || items[0].ID != history {
		t.Fatalf("archived history should reappear after deleting active notifications: %+v", items)
	}
	for _, id := range []string{target, sibling} {
		if n := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1", id); n != 0 {
			t.Errorf("deleted notification %s remains", id)
		}
	}
	for _, id := range []string{history, otherRecipient, foreign, standalone, anotherStandalone} {
		if n := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1", id); n != 1 {
			t.Errorf("unrelated notification %s removed", id)
		}
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM issue WHERE id = $1", issue); n != 1 {
		t.Error("underlying issue removed")
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE id = $1", task); n != 1 {
		t.Error("underlying run removed")
	}
	remove(target, ws, http.StatusNotFound)
	if count := remove(standalone, ws, http.StatusOK); count != 1 {
		t.Errorf("standalone delete count = %d", count)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1", anotherStandalone); n != 1 {
		t.Error("deleting a standalone notification removed another")
	}
	if count := remove(history, ws, http.StatusOK); count != 1 {
		t.Errorf("archived delete count = %d", count)
	}
}

func TestDeleteInboxItemPrivateProjectAfterRevocation(t *testing.T) {
	for _, archived := range []bool{false, true} {
		name := "active"
		if archived {
			name = "archived"
		}
		t.Run(name, func(t *testing.T) {
			f := newPrivateProjectFixture(t)
			id := dbfx.Insert(t, "inbox_item", testutil.Cols{
				"workspace_id": testWorkspaceID, "recipient_type": "member", "recipient_id": f.insider,
				"type": "mentioned", "severity": "info", "title": "Private notification",
				"issue_id": f.secretIssue, "archived": archived,
			})
			removeProjectMemberAsOwner(t, f, f.insider)
			published := 0
			testHandler.Bus.Subscribe(protocol.EventInboxDeleted, func(e events.Event) {
				if e.Payload.(map[string]any)["item_id"] == id {
					published++
				}
			})
			remove := func(itemID string, want int) *testutil.Response {
				return testutil.Call(t, inboxWorkspaceHandler(testHandler.DeleteInboxItem),
					withURLParam(asUser(inboxRequest(http.MethodDelete, "/api/inbox/"+itemID, testWorkspaceID), f.insider), "id", itemID)).Want(want)
			}
			hiddenBody := remove(id, http.StatusNotFound).Text()
			missingBody := remove(uuid.NewString(), http.StatusNotFound).Text()
			if hiddenBody != missingBody {
				t.Fatalf("hidden notification differs from a missing one: %s vs %s", hiddenBody, missingBody)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1", id); n != 1 {
				t.Error("hidden notification was deleted")
			}
			if published != 0 {
				t.Error("hidden notification deletion published an event")
			}
			addProjectMemberRow(t, f.projectID, f.insider)
			remove(id, http.StatusOK)
			if n := dbfx.Count(t, "SELECT count(*) FROM inbox_item WHERE id = $1", id); n != 0 {
				t.Error("project member could not delete their notification")
			}
		})
	}
}
