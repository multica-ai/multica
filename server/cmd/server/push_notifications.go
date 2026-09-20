package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/push"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type pushNotificationQueries interface {
	ListPushTargetsForNotification(context.Context, db.ListPushTargetsForNotificationParams) ([]db.ListPushTargetsForNotificationRow, error)
	DeletePushDeviceByToken(context.Context, string) error
}

// registerPushNotificationListeners bridges durable inbox creation to mobile
// push. The event bus is synchronous, so delivery runs outside the publisher's
// transaction/request path and has its own timeout.
func registerPushNotificationListeners(bus *events.Bus, queries pushNotificationQueries, sender push.Sender) {
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		item, ok := payload["item"].(map[string]any)
		if !ok || stringValue(item["recipient_type"]) != "member" {
			return
		}
		recipientID := stringValue(item["recipient_id"])
		inboxID := stringValue(item["id"])
		if recipientID == "" || inboxID == "" || e.WorkspaceID == "" {
			return
		}

		title := stringValue(item["title"])
		if title == "" {
			title = "Multica"
		}
		body := stringValue(item["body"])

		go deliverInboxPush(queries, sender, e.WorkspaceID, recipientID, inboxID, title, body)
	})
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func deliverInboxPush(
	queries pushNotificationQueries,
	sender push.Sender,
	workspaceID, recipientID, inboxID, title, body string,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	targets, err := queries.ListPushTargetsForNotification(ctx, db.ListPushTargetsForNotificationParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(recipientID),
	})
	if err != nil {
		slog.Error("push: list targets failed", "workspace_id", workspaceID, "recipient_id", recipientID, "error", err)
		return
	}
	if len(targets) == 0 {
		return
	}

	messages := make([]push.Message, 0, len(targets))
	for _, target := range targets {
		messages = append(messages, push.Message{
			To:       target.ExpoPushToken,
			Title:    title,
			Body:     body,
			Sound:    "default",
			Priority: "high",
			Data: map[string]any{
				"url": fmt.Sprintf("multica:///%s/inbox/%s", target.WorkspaceSlug, inboxID),
			},
		})
	}

	for start := 0; start < len(messages); start += 100 {
		end := min(start+100, len(messages))
		invalid, err := sender.Send(ctx, messages[start:end])
		if err != nil {
			slog.Error("push: Expo delivery failed", "workspace_id", workspaceID, "recipient_id", recipientID, "error", err)
			continue
		}
		for _, token := range invalid {
			if err := queries.DeletePushDeviceByToken(ctx, token); err != nil {
				slog.Warn("push: failed to remove invalid device", "error", err)
			}
		}
	}
}
