package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func registerChatReplyNotifications(bus *events.Bus, queries *db.Queries) {
	bus.Subscribe(protocol.EventChatDone, func(e events.Event) {
		payload, ok := e.Payload.(protocol.ChatDonePayload)
		if !ok || payload.MessageID == "" || payload.ChatSessionID == "" {
			return
		}
		ctx := context.Background()
		session, err := queries.GetChatSessionInWorkspace(ctx, db.GetChatSessionInWorkspaceParams{
			ID: parseUUID(payload.ChatSessionID), WorkspaceID: parseUUID(e.WorkspaceID),
		})
		if err != nil || session.Status != "active" || session.IsAgentIntro {
			return
		}
		// Read the persisted reply: lifecycle-only endings and channel replies
		// must not create a private-chat notification.
		message, err := queries.GetChatMessage(ctx, parseUUID(payload.MessageID))
		if err != nil || message.ChatSessionID != session.ID || message.Role != "assistant" || message.MessageKind != "message" {
			return
		}
		// Outbound provenance is recorded after chat:done. The input batch is
		// already sealed here, including for retries of a channel turn.
		origin, err := queries.GetTaskChannelOrigin(ctx, message.TaskID)
		if err != nil || origin.ChannelIngested {
			return
		}
		title := strings.TrimSpace(session.Title)
		if title == "" {
			title = "Chat reply"
		}
		preview := []rune(strings.Join(strings.Fields(message.Content), " "))
		if len(preview) > 200 {
			preview = append(preview[:200], '…')
		}
		details, _ := json.Marshal(map[string]string{
			"chat_session_id": payload.ChatSessionID,
			"message_id":      payload.MessageID,
		})
		notifyDirect(ctx, queries, bus, "member", util.UUIDToString(session.CreatorID), e.WorkspaceID,
			events.Event{ActorType: "agent", ActorID: util.UUIDToString(session.AgentID)},
			"", "", "chat_reply", "info", title, string(preview), details)
	})
}
