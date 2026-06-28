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
		// Fan out by recipient: an agent recipient fires a wakeup (a scheduled
		// agent run); a member recipient re-surfaces the source in the inbox.
		if rem.RecipientType == "agent" {
			fireAgentReminder(ctx, cerebro, rem)
			continue
		}
		surfaceFiredReminder(ctx, cerebro, bus, rem)
	}
}

// fireAgentReminder turns a due agent-recipient reminder into a cerebro agent
// wakeup: a one-shot 'time' wakeup due now, which the existing wakeup dispatcher
// claims and runs. creator_id carries the human origin a real agent run needs;
// the wakeup needs an issue context, guaranteed by the create-time validation
// that an agent reminder is anchored to a message or an issue (conversation_id).
func fireAgentReminder(ctx context.Context, cerebro *cerebrodb.Queries, rem cerebrodb.ClaimDueRemindersRow) {
	if !rem.ConversationID.Valid {
		slog.Warn("cerebro reminder sweeper: agent reminder has no issue context, skipping", "reminder_id", util.UUIDToString(rem.ID))
		return
	}
	prompt := rem.Text
	if prompt == "" {
		prompt = "Reminder"
	}
	if _, err := cerebro.CreateCerebroAgentWakeup(ctx, cerebrodb.CreateCerebroAgentWakeupParams{
		WorkspaceID:     rem.WorkspaceID,
		AgentID:         rem.RecipientID,
		IssueID:         rem.ConversationID,
		Prompt:          prompt,
		TriggerType:     "time",
		FireAt:          pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CreatedByID:     rem.CreatorID,
		OriginCommentID: rem.MessageID,
	}); err != nil {
		slog.Warn("cerebro reminder sweeper: failed to create agent wakeup", "error", err)
	}
}

// surfaceFiredReminder brings the reminder's source conversation back to the
// user's attention: un-archive the conversation (and any archived inbox rows for
// it), then ensure a manually-added inbox row exists and is unread so the
// reminder "lands in the inbox" linking to the original message's conversation.
func surfaceFiredReminder(ctx context.Context, cerebro *cerebrodb.Queries, bus *events.Bus, rem cerebrodb.ClaimDueRemindersRow) {
	// A chat-message reminder has no conversation (a chat is not an issue); it
	// re-surfaces by stamping its chat session unread, which makes the chat
	// reappear in the inbox's chat list (FIR-394).
	if rem.AnchorType == "chat_message" {
		surfaceFiredChatReminder(ctx, cerebro, bus, rem)
		return
	}
	if !rem.ConversationID.Valid {
		// No conversation to re-surface: a free "remind me at X" (anchor
		// 'none'), a project reminder, or one whose source was deleted. These
		// must still notify at their planned time, so drop a standalone reminder
		// row straight into the inbox (FIR-2154) instead of silently swallowing
		// it — the bug where free reminders never fired into the inbox.
		surfaceFiredAnchorlessReminder(ctx, cerebro, bus, rem)
		return
	}

	// 1. Un-archive the conversation itself (per-user DM/channel archive) so a
	//    conversation the user archived reappears in their list.
	if err := cerebro.UnarchiveChannelForUser(ctx, cerebrodb.UnarchiveChannelForUserParams{
		ChannelID: rem.ConversationID,
		UserID:    rem.RecipientID,
	}); err != nil {
		slog.Warn("cerebro reminder sweeper: failed to unarchive channel", "error", err)
	}

	// 2. Restore any archived inbox rows the user had for the conversation.
	if _, err := cerebro.UnarchiveInboxByIssue(ctx, cerebrodb.UnarchiveInboxByIssueParams{
		WorkspaceID:   rem.WorkspaceID,
		RecipientType: "member",
		RecipientID:   rem.RecipientID,
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
		RecipientID: rem.RecipientID,
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
			RecipientID: rem.RecipientID,
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
				"recipient_id": util.UUIDToString(rem.RecipientID),
				"issue_id":     util.UUIDToString(rem.ConversationID),
				"type":         "reminder",
			},
		})
	}
}

// surfaceFiredAnchorlessReminder handles a fired reminder that has no
// conversation to bring back: a free "remind me at X", a project reminder, or
// one whose source was deleted. It creates a fresh, unread reminder row in the
// inbox carrying the reminder text so the reminder still lands at its planned
// time (FIR-2154). issue_id is NULL for a free/project reminder.
func surfaceFiredAnchorlessReminder(ctx context.Context, cerebro *cerebrodb.Queries, bus *events.Bus, rem cerebrodb.ClaimDueRemindersRow) {
	title := rem.Text
	if title == "" {
		title = "Reminder"
	}
	created, err := cerebro.CreateFiredReminderInboxItem(ctx, cerebrodb.CreateFiredReminderInboxItemParams{
		WorkspaceID: rem.WorkspaceID,
		RecipientID: rem.RecipientID,
		IssueID:     rem.ConversationID, // invalid → NULL for free/project reminders
		Title:       title,
	})
	if err != nil {
		slog.Warn("cerebro reminder sweeper: failed to create standalone reminder inbox row", "error", err)
		return
	}
	if err := cerebro.SetReminderFiredInboxItem(ctx, cerebrodb.SetReminderFiredInboxItemParams{
		ID:               rem.ID,
		FiredInboxItemID: created.ID,
	}); err != nil {
		slog.Warn("cerebro reminder sweeper: failed to record fired inbox item", "error", err)
	}
	if bus != nil {
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: util.UUIDToString(rem.WorkspaceID),
			ActorType:   "system",
			Payload: map[string]any{
				"recipient_id": util.UUIDToString(rem.RecipientID),
				"type":         "reminder",
			},
		})
	}
}

// surfaceFiredChatReminder re-surfaces a fired chat-message reminder: it stamps
// the chat session unread (idempotent), which makes the chat reappear in the
// inbox's chat list. A chat is not an issue, so none of the conversation/inbox
// machinery above applies.
func surfaceFiredChatReminder(ctx context.Context, cerebro *cerebrodb.Queries, bus *events.Bus, rem cerebrodb.ClaimDueRemindersRow) {
	if !rem.ChatMessageID.Valid {
		// The chat message was deleted; the reminder still shows fired in the
		// overview, but there is no chat to re-surface.
		return
	}
	if err := cerebro.MarkChatSessionUnreadByMessage(ctx, rem.ChatMessageID); err != nil {
		slog.Warn("cerebro reminder sweeper: failed to mark chat session unread", "error", err)
		return
	}
	if bus != nil {
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: util.UUIDToString(rem.WorkspaceID),
			ActorType:   "system",
			Payload: map[string]any{
				"recipient_id": util.UUIDToString(rem.RecipientID),
				"type":         "reminder",
			},
		})
	}
}
