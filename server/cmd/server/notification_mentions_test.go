package main

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestNotification_MalformedMemberMentionPreservesValidRecipients(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mentionAll bool
		mapPayload bool
	}{
		{name: "explicit member"},
		{name: "explicit member map payload", mapPayload: true},
		{name: "all members", mentionAll: true},
		{name: "all members map payload", mentionAll: true, mapPayload: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := workspaceFixture(t)
			recipientID := fx.User(t, "Mention recipient", "mention-recipient@multica.ai")
			mutedID := fx.User(t, "Muted recipient", "mention-muted@multica.ai")
			fx.Member(t, testWorkspaceID, recipientID, "member")
			fx.Member(t, testWorkspaceID, mutedID, "member")
			fx.Insert(t, "notification_preference", testutil.Cols{
				"workspace_id": testWorkspaceID,
				"user_id":      mutedID,
				"preferences":  `{"mentions":"muted"}`,
			})
			issueID := fx.Issue(t, "Malformed mention")
			fx.Cleanup(t, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
			fx.Cleanup(t, `DELETE FROM issue_subscriber WHERE issue_id = $1`, issueID)

			// These IDs match the markdown parser but are not UUIDs. Keep a
			// valid recipient on either side to exercise the whole batch.
			content := "[@Recipient](mention://member/" + recipientID + ") " +
				"[@Bad](mention://member/deadbeef) [@Bad all](mention://member/all) " +
				"[@Recipient again](mention://member/" + recipientID + ") " +
				"[@Muted](mention://member/" + mutedID + ") " +
				"[@Self](mention://member/" + testUserID + ")"
			if tc.mentionAll {
				content = "[@Bad](mention://member/deadbeef) [@All](mention://all/all)"
			}
			commentID := fx.Comment(t, issueID, content)
			comment := handler.CommentResponse{
				ID: commentID, IssueID: issueID, AuthorType: "member",
				AuthorID: testUserID, Content: content, Type: "comment",
			}
			var commentPayload any = comment
			if tc.mapPayload {
				commentPayload = map[string]any{
					"id": commentID, "issue_id": issueID, "author_type": "member",
					"author_id": testUserID, "content": content, "type": "comment",
				}
			}

			queries := db.New(testPool)
			bus := newNotificationBus(t, queries)
			var inboxEvents []events.Event
			bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
				inboxEvents = append(inboxEvents, e)
			})
			bus.Publish(events.Event{
				Type: protocol.EventCommentCreated, WorkspaceID: testWorkspaceID,
				ActorType: "member", ActorID: testUserID,
				Payload: map[string]any{
					"comment": commentPayload, "issue_title": "Malformed mention", "issue_status": "todo",
				},
			})

			items := inboxItemsForRecipient(t, queries, recipientID)
			if len(items) != 1 {
				t.Fatalf("valid recipient got %d inbox items, want 1", len(items))
			}
			if items[0].Type != "mentioned" || util.UUIDToString(items[0].IssueID) != issueID {
				t.Fatalf("unexpected mention inbox item: %+v", items[0])
			}
			var details map[string]string
			if err := json.Unmarshal(items[0].Details, &details); err != nil || details["comment_id"] != commentID {
				t.Fatalf("mention details = %s, error = %v; want comment_id %s", items[0].Details, err, commentID)
			}
			if count := fx.Count(t, `SELECT count(*) FROM inbox_item WHERE issue_id = $1`, issueID); count != 1 {
				t.Fatalf("issue has %d inbox items, want only the valid, unmuted recipient", count)
			}
			if len(inboxEvents) != 1 {
				t.Fatalf("got %d inbox:new events, want 1", len(inboxEvents))
			}
			item := inboxEvents[0].Payload.(map[string]any)["item"].(map[string]any)
			if item["recipient_id"] != recipientID || item["type"] != "mentioned" || item["issue_status"] != "todo" {
				t.Fatalf("unexpected inbox:new item: %+v", item)
			}
		})
	}
}

func TestNotifyMentionedMembersOnlyMalformedIDs(t *testing.T) {
	// Call the boundary directly so the event bus cannot hide a panic. With
	// no valid recipients, neither the database nor the bus is needed.
	notifyMentionedMembers(nil, nil, events.Event{},
		parseMentions("[@Bad](mention://member/deadbeef) [@Bad all](mention://member/all)"),
		"", "", "", "", nil, nil)
}
