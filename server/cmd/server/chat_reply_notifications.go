package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type chatReplyMembership interface {
	IsMember(context.Context, string, string) (bool, error)
}

// Completion content keeps its workspace-member scope. Only this bounded alert
// reaches the owner's connections in other workspaces, after checking current
// membership of the source workspace. It never creates an Inbox item.
func registerChatReplyNotifications(bus *events.Bus, b realtime.Broadcaster, membership chatReplyMembership) {
	bus.Subscribe(protocol.EventChatDone, func(e events.Event) {
		p, ok := e.Payload.(protocol.ChatDonePayload)
		if !ok || p.Notification == nil || p.MessageID == "" || p.ChatSessionID == "" ||
			e.ChatSessionOwnerID == "" || e.WorkspaceID == "" || membership == nil ||
			p.Notification.WorkspaceID != e.WorkspaceID || p.ChatSessionID != e.ChatSessionID {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		member, err := membership.IsMember(ctx, e.ChatSessionOwnerID, e.WorkspaceID)
		if err != nil || !member {
			if err != nil {
				slog.Warn("chat reply alert membership lookup failed", "workspace_id", e.WorkspaceID, "error", err)
			}
			return
		}
		preview := []rune(strings.Join(strings.Fields(p.Content), " "))
		if len(preview) > 200 {
			preview = append(preview[:200], '…')
		}
		payload := protocol.ChatReplyNotificationPayload{
			WorkspaceID: e.WorkspaceID, ChatSessionID: p.ChatSessionID, MessageID: p.MessageID,
			Title: p.Notification.Title, Body: string(preview),
		}
		data, err := json.Marshal(map[string]any{
			"type": protocol.EventChatReplyNotification, "payload": payload,
			"actor_id": e.ActorID, "actor_type": e.ActorType,
		})
		if err != nil {
			return
		}
		realtime.M.RecordEvent(protocol.EventChatReplyNotification)
		b.SendToUser(e.ChatSessionOwnerID, data)
	})
}
