package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestNotification_ChatReply(t *testing.T) {
	for _, name := range []string{
		"completed", "attachment only", "comments muted", "system muted", "chat muted", "not a member",
		"other workspace", "other session", "archived", "intro", "no response", "channel", "lifecycle only",
	} {
		t.Run(name, func(t *testing.T) {
			fx := workspaceFixture(t)
			owner := fx.User(t, "Chat owner", "chat-notification@example.test")
			if name != "not a member" {
				fx.Member(t, testWorkspaceID, owner, "member")
			}
			runtime := fx.Runtime(t, "Reply runtime")
			agent := fx.Agent(t, "Reply agent", runtime)
			sessionCols := testutil.Cols{"creator_id": owner, "title": "Review the changes"}
			if name == "archived" {
				sessionCols["status"] = "archived"
			}
			if name == "intro" {
				sessionCols["is_agent_intro"] = true
			}
			session := fx.ChatSession(t, agent, sessionCols)
			task := fx.Task(t, agent, testutil.Cols{"chat_session_id": session, "runtime_id": runtime, "status": "completed"})
			fx.Exec(t, "UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1", task)
			fx.Insert(t, "chat_message", testutil.Cols{
				"chat_session_id": session, "role": "user", "content": "Review changes", "task_id": task,
				"channel_ingested": name == "channel",
			})
			messageCols := testutil.Cols{
				"chat_session_id": session, "role": "assistant", "message_kind": "message",
				"content": strings.Repeat("回复", 120), "task_id": task,
			}
			if name == "no response" {
				messageCols["message_kind"] = "no_response"
			}
			if name == "attachment only" {
				messageCols["content"] = ""
			}
			if name == "other session" {
				messageCols["chat_session_id"] = fx.ChatSession(t, agent)
			}
			message := fx.Insert(t, "chat_message", messageCols)
			prefs := map[string]string{}
			switch name {
			case "comments muted":
				prefs["comments"] = "muted"
			case "system muted":
				prefs["system_notifications"] = "muted"
			case "chat muted":
				prefs["chat_replies"] = "muted"
			}
			prefJSON, _ := json.Marshal(prefs)
			fx.Insert(t, "notification_preference", testutil.Cols{
				"workspace_id": testWorkspaceID, "user_id": owner, "preferences": prefJSON,
			})
			payload := protocol.ChatDonePayload{ChatSessionID: session, MessageID: message}
			if name == "lifecycle only" {
				payload.MessageID = ""
			}
			workspaceID := testWorkspaceID
			if name == "other workspace" {
				workspaceID = fx.Workspace(t, "Other", "chat-notification-other")
			}
			queries := db.New(testPool)
			bus := newNotificationBus(t, queries)
			var delivered []events.Event
			bus.Subscribe(protocol.EventInboxNew, func(e events.Event) { delivered = append(delivered, e) })
			bus.Publish(events.Event{
				Type: protocol.EventChatDone, WorkspaceID: workspaceID,
				ChatSessionID: session, ChatSessionOwnerID: owner, Payload: payload,
			})
			items := inboxItemsForRecipient(t, queries, owner)
			wantDelivery := name == "completed" || name == "attachment only" || name == "comments muted" || name == "system muted"
			if !wantDelivery {
				if len(items) != 0 || len(delivered) != 0 {
					t.Fatalf("unexpected notification: items=%d events=%d", len(items), len(delivered))
				}
				return
			}
			if len(items) != 1 || len(delivered) != 1 {
				t.Fatalf("expected one notification: items=%d events=%d", len(items), len(delivered))
			}
			item := items[0]
			t.Cleanup(func() { fx.Exec(t, "DELETE FROM inbox_item WHERE id = $1", util.UUIDToString(item.ID)) })
			previewLength := 201
			if name == "attachment only" {
				previewLength = 0
			}
			if item.Type != "chat_reply" || item.IssueID.Valid || item.Read || item.Title != "Review the changes" ||
				item.ActorType.String != "agent" || util.UUIDToString(item.ActorID) != agent || len([]rune(item.Body.String)) != previewLength {
				t.Fatalf("unexpected notification: %+v", item)
			}
			var details map[string]string
			if err := json.Unmarshal(item.Details, &details); err != nil || details["chat_session_id"] != session || details["message_id"] != message {
				t.Fatalf("invalid chat destination: %s (%v)", item.Details, err)
			}
			if others := inboxItemsForRecipient(t, queries, testUserID); len(others) != 0 {
				t.Fatalf("private reply leaked to another member: %d items", len(others))
			}
		})
	}
}
