package main

// CEREBRO-PATCH(cerebro-reminder): FIR-394 — background sweeper that fires due
// reminders (the standalone reminder entity) and re-surfaces the source
// conversation in the user's inbox. Crucially the surfaced inbox row carries NO
// comment_id and is not the reminder itself, so firing a reminder can never
// re-introduce the DM thread lockout (FIR-249).

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	cerebroReminderSweepInterval = 30 * time.Second
	cerebroReminderSweepLimit    = 100
)

func runCerebroReminderSweeper(ctx context.Context, cerebro *cerebrodb.Queries, bus *events.Bus) {
	ticker := time.NewTicker(cerebroReminderSweepInterval)
	defer ticker.Stop()

	tickCerebroReminders(ctx, cerebro, bus)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCerebroReminders(ctx, cerebro, bus)
		}
	}
}

func tickCerebroReminders(ctx context.Context, cerebro *cerebrodb.Queries, bus *events.Bus) {
	due, err := cerebro.ClaimDueReminders(ctx, cerebroReminderSweepLimit)
	if err != nil {
		slog.Warn("cerebro reminder sweeper: failed to claim due reminders", "error", err)
		return
	}
	for _, rem := range due {
		surfaceFiredReminder(ctx, cerebro, bus, rem)
	}
}

// surfaceFiredReminder brings the reminder's source conversation back to the
// user's attention: un-archive the conversation (and any archived inbox rows for
// it), then ensure a manually-added inbox row exists and is unread so the
// reminder "lands in the inbox" linking to the original message's conversation.
func surfaceFiredReminder(ctx context.Context, cerebro *cerebrodb.Queries, bus *events.Bus, rem cerebrodb.ClaimDueRemindersRow) {
	if !rem.ConversationID.Valid {
		// Source conversation was deleted; the reminder still shows in the
		// overview as fired, but there is nothing to re-surface.
		return
	}

	// 1. Un-archive the conversation itself (per-user DM/channel archive) so a
	//    conversation the user archived reappears in their list.
	if err := cerebro.UnarchiveChannelForUser(ctx, cerebrodb.UnarchiveChannelForUserParams{
		ChannelID: rem.ConversationID,
		UserID:    rem.UserID,
	}); err != nil {
		slog.Warn("cerebro reminder sweeper: failed to unarchive channel", "error", err)
	}

	// 2. Restore any archived inbox rows the user had for the conversation.
	if _, err := cerebro.UnarchiveInboxByIssue(ctx, cerebrodb.UnarchiveInboxByIssueParams{
		WorkspaceID:   rem.WorkspaceID,
		RecipientType: "member",
		RecipientID:   rem.UserID,
		IssueID:       rem.ConversationID,
	}); err != nil {
		slog.Warn("cerebro reminder sweeper: failed to unarchive inbox rows", "error", err)
	}

	// 3. Ensure a visible inbox element linking to the conversation. Reuse the
	//    manually-added row (issue_id = conversation, no comment_id) so it opens
	//    the conversation without ever triggering the thread auto-open path.
	var inboxItemID pgtype.UUID
	if existing, err := cerebro.FindManualInboxItem(ctx, cerebrodb.FindManualInboxItemParams{
		WorkspaceID: rem.WorkspaceID,
		RecipientID: rem.UserID,
		IssueID:     rem.ConversationID,
	}); err == nil {
		inboxItemID = existing.ID
		if _, uerr := cerebro.SetInboxUnread(ctx, existing.ID); uerr != nil {
			slog.Warn("cerebro reminder sweeper: failed to mark inbox unread", "error", uerr)
		}
	} else {
		title := rem.Text
		if title == "" {
			title = "Reminder"
		}
		created, cerr := cerebro.CreateManualInboxItem(ctx, cerebrodb.CreateManualInboxItemParams{
			WorkspaceID: rem.WorkspaceID,
			RecipientID: rem.UserID,
			IssueID:     rem.ConversationID,
			Title:       title,
		})
		if cerr != nil {
			slog.Warn("cerebro reminder sweeper: failed to create inbox row", "error", cerr)
			return
		}
		inboxItemID = created.ID
	}

	// 4. Record the surfaced row so a re-fire can dedupe against it.
	if inboxItemID.Valid {
		if err := cerebro.SetReminderFiredInboxItem(ctx, cerebrodb.SetReminderFiredInboxItemParams{
			ID:               rem.ID,
			FiredInboxItemID: inboxItemID,
		}); err != nil {
			slog.Warn("cerebro reminder sweeper: failed to record fired inbox item", "error", err)
		}
	}

	// 5. Refresh the user's other sessions.
	if bus != nil {
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: util.UUIDToString(rem.WorkspaceID),
			ActorType:   "system",
			Payload: map[string]any{
				"recipient_id": util.UUIDToString(rem.UserID),
				"issue_id":     util.UUIDToString(rem.ConversationID),
				"type":         "reminder",
			},
		})
	}
}
