package main

// CEREBRO-PATCH(inbox-reminders-due): background sweeper for scheduled reminder activation.

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	reminderDueSweepInterval = 30 * time.Second
	reminderDueSweepLimit    = 100
)

func runReminderDueSweeper(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	ticker := time.NewTicker(reminderDueSweepInterval)
	defer ticker.Stop()

	tickDueReminders(ctx, queries, bus)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickDueReminders(ctx, queries, bus)
		}
	}
}

func tickDueReminders(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	items, err := queries.ClaimDueReminderInboxItems(ctx, reminderDueSweepLimit)
	if err != nil {
		slog.Warn("reminder sweeper: failed to claim due reminders", "error", err)
		return
	}
	for _, item := range items {
		workspaceID := util.UUIDToString(item.WorkspaceID)
		recipientID := util.UUIDToString(item.RecipientID)
		itemID := util.UUIDToString(item.ID)
		issueKey := itemID
		if item.IssueID.Valid {
			issueKey = util.UUIDToString(item.IssueID)
		}

		if bus != nil {
			bus.Publish(events.Event{
				Type:        protocol.EventInboxNew,
				WorkspaceID: workspaceID,
				ActorType:   "system",
				Payload:     map[string]any{"item": inboxItemToResponse(item)},
			})
		}

		if item.RecipientType == "member" &&
			resolveChannelChoice(ctx, queries, item.RecipientType, recipientID, channelMobile, "reminder") {
			pushReminderToMember(ctx, queries, recipientID, workspaceID, issueKey, itemID, item.Title, textString(item.Body), service.DeviceClassMobile, channelMobile)
		}

		// CEREBRO-PATCH(push-device-split): TECH-3548 — desktop channel also
		// fires a Web Push to the user's computer-class browsers.
		if item.RecipientType == "member" &&
			resolveChannelChoice(ctx, queries, item.RecipientType, recipientID, channelDesktop, "reminder") {
			pushReminderToMember(ctx, queries, recipientID, workspaceID, issueKey, itemID, item.Title, textString(item.Body), service.DeviceClassDesktop, channelDesktop)
		}

		if bus != nil && item.RecipientType == "member" &&
			resolveChannelChoice(ctx, queries, item.RecipientType, recipientID, channelDesktop, "reminder") {
			bus.Publish(events.Event{
				Type:        protocol.EventDesktopNotify,
				WorkspaceID: workspaceID,
				ActorType:   "system",
				Payload: map[string]any{
					"recipient_id": recipientID,
					"type":         "reminder",
					"severity":     item.Severity,
					"title":        item.Title,
					"body":         textString(item.Body),
					"issue_id":     issueKey,
				},
			})
		}
	}
}

func pushReminderToMember(ctx context.Context, queries *db.Queries, recipientID, workspaceID, issueKey, itemID, title, body, deviceClass, channel string) {
	if pushNotifier == nil || !pushNotifier.Enabled() {
		return
	}
	transport := resolveChannelTransport(ctx, queries, "member", recipientID, channel)
	pushNotifier.SendToUserForClass(ctx, recipientID, deviceClass, service.Payload{
		Title:   truncateForPush(title, pushTitleMaxRunes),
		Body:    truncateForPush(stripMentionMarkdownForPush(body), pushBodyMaxRunes),
		URL:     pushDeepLinkURL(ctx, queries, workspaceID, issueKey),
		Tag:     "reminder:" + itemID,
		IssueID: issueKey,
		Type:    "reminder",
		Badge:   transport.Badge,
		Silent:  !transport.Sound,
	})
}

func textString(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}
