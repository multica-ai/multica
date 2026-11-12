package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatCompletionDoesNotCreateInboxNotification(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)
	var inboxEvents int
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) { inboxEvents++ })
	fx := workspaceFixture(t)
	agent := fx.Agent(t, "Chat agent", "")
	session := fx.ChatSession(t, agent)
	bus.Publish(events.Event{
		Type: protocol.EventChatDone, WorkspaceID: testWorkspaceID, ChatSessionOwnerID: testUserID,
		ChatSessionID: session,
		Payload: protocol.ChatDonePayload{
			ChatSessionID: session, MessageID: session, Content: "Reply finished",
			Notification: &protocol.ChatReplyNotification{WorkspaceID: testWorkspaceID, Title: "Chat reply"},
		},
	})
	if items := inboxItemsForRecipient(t, queries, testUserID); len(items) != 0 || inboxEvents != 0 {
		t.Fatalf("chat completion must stay out of Inbox: items=%d events=%d", len(items), inboxEvents)
	}
}
