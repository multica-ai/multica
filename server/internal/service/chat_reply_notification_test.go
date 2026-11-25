package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatReplyNotificationContext(t *testing.T) {
	for _, name := range []string{"reply", "attachment only", "archived", "intro", "no response", "channel", "lifecycle only"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			pool := sharedTestPool(t)
			fx := testutil.New(pool, "", "")
			owner := fx.User(t, "Chat owner", "chat-os-notification@example.test")
			workspace := fx.Workspace(t, "Chat alerts", "chat-os-"+owner[:8])
			fx.UserID, fx.WorkspaceID = owner, workspace
			fx.Member(t, workspace, owner, "member")
			runtime := fx.Runtime(t, "Reply runtime")
			agent := fx.Agent(t, "Reply agent", runtime)
			cols := testutil.Cols{"title": "Review changes"}
			if name == "archived" {
				cols["status"] = "archived"
			}
			if name == "intro" {
				cols["is_agent_intro"] = true
			}
			session := fx.ChatSession(t, agent, cols)
			taskID := fx.Task(t, agent, testutil.Cols{"runtime_id": runtime, "chat_session_id": session, "status": "completed"})
			fx.Exec(t, "UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1", taskID)
			fx.Insert(t, "chat_message", testutil.Cols{
				"chat_session_id": session, "role": "user", "content": "Please review", "task_id": taskID,
				"channel_ingested": name == "channel",
			})
			messageCols := testutil.Cols{
				"chat_session_id": session, "role": "assistant", "content": "Done", "task_id": taskID, "message_kind": "message",
			}
			if name == "attachment only" {
				messageCols["content"] = ""
			}
			if name == "no response" {
				messageCols["message_kind"] = "no_response"
			}
			messageID := fx.Insert(t, "chat_message", messageCols)
			queries := db.New(pool)
			task, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			message, err := queries.GetChatMessage(ctx, util.MustParseUUID(messageID))
			if err != nil {
				t.Fatal(err)
			}
			bus := events.New()
			var got events.Event
			bus.Subscribe(protocol.EventChatDone, func(e events.Event) { got = e })
			svc := &TaskService{Queries: queries, Bus: bus}
			msg := &message
			if name == "lifecycle only" {
				msg = nil
			}
			svc.broadcastChatDone(ctx, task, msg, false)
			payload, ok := got.Payload.(protocol.ChatDonePayload)
			if !ok || got.ChatSessionOwnerID != owner || got.WorkspaceID != workspace {
				t.Fatalf("reply lost its private source scope: %+v", got)
			}
			wantAlert := name == "reply" || name == "attachment only"
			if !wantAlert {
				if payload.Notification != nil {
					t.Fatalf("unexpected OS notification context: %+v", payload.Notification)
				}
				return
			}
			if payload.Notification == nil || payload.Notification.WorkspaceID != workspace || payload.Notification.Title != "Review changes" || payload.MessageID != messageID {
				t.Fatalf("incorrect OS notification context: %+v", payload)
			}
		})
	}
}
