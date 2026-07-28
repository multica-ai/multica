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

// surfaceFiredReminder makes a fired member reminder land in the inbox. Every
// reminder type — free, project, issue-, comment-, or chat-anchored — gets a
// standalone unread `reminder` inbox row so it always shows in the inbox's
// Reminders section (FIR-2278), with one exception: a reminder set by snoozing
// an inbox row re-surfaces that row instead, because it comes back by itself and
// would otherwise show the same message twice (FIR-3918). Before either, we bring
// the reminder's source back into view when there is one: un-archive the linked
// issue/conversation, or re-surface the chat in the inbox's chat list.
//
// The reminder row carries issue_id (the conversation) when anchored to an
// issue/comment, NULL otherwise. It never carries a comment_id, so firing a
// reminder can never re-introduce the DM thread lockout (FIR-249).
func surfaceFiredReminder(ctx context.Context, cerebro *cerebrodb.Queries, bus *events.Bus, rem cerebrodb.ClaimDueRemindersRow) {
	switch {
	case rem.AnchorType == "chat_message":
		// A chat is not an issue: re-surface it by stamping its chat session
		// unread, which makes the chat reappear in the inbox's chat list
		// (FIR-394). The reminder row below then also lists it under Reminders.
		if rem.ChatMessageID.Valid {
			if err := cerebro.MarkChatSessionUnreadByMessage(ctx, rem.ChatMessageID); err != nil {
				slog.Warn("cerebro reminder sweeper: failed to mark chat session unread", "error", err)
			}
		}
	case rem.ConversationID.Valid:
		// Un-archive the linked conversation (per-user DM/channel archive) and
		// restore any archived inbox rows for it, so the reminder's source is
		// visible when the user opens the reminder.
		if err := cerebro.UnarchiveChannelForUser(ctx, cerebrodb.UnarchiveChannelForUserParams{
			ChannelID: rem.ConversationID,
			UserID:    rem.RecipientID,
		}); err != nil {
			slog.Warn("cerebro reminder sweeper: failed to unarchive channel", "error", err)
		}
		if _, err := cerebro.UnarchiveInboxByIssue(ctx, cerebrodb.UnarchiveInboxByIssueParams{
			WorkspaceID:   rem.WorkspaceID,
			RecipientType: "member",
			RecipientID:   rem.RecipientID,
			IssueID:       rem.ConversationID,
		}); err != nil {
			slog.Warn("cerebro reminder sweeper: failed to unarchive inbox rows", "error", err)
		}
	}

	// FIR-3918: a reminder set by snoozing an inbox row must not come back as two
	// rows. That row was only hidden (muted_until) and resurfaces on its own, so
	// when it is still there it IS the reminder — it already renders the reminder
	// mark from its overdue muted_until. Bring it back unread and stop; only when
	// the link is missing or the row is gone do we fall through to a standalone
	// row (a free/project/comment/chat reminder always does).
	if resurfacedSnoozedInboxRow(ctx, cerebro, bus, rem) {
		return
	}

	surfaceReminderInboxRow(ctx, cerebro, bus, rem)
}

// resurfacedSnoozedInboxRow re-surfaces the inbox row a reminder was snoozed from
// and reports whether it did, so the caller can skip the standalone reminder row
// (FIR-3918). The row un-mutes by itself when muted_until passes; what it needs
// from us is to arrive unread, the way any fired reminder does — otherwise the
// reminder returns as a grey, already-read row and never rings.
//
// Unread is set per issue because muting is per issue (SetInboxMutedUntilByIssue):
// every sibling shares one muted_until, so the row the deduplicated inbox shows is
// not necessarily the one that was clicked. The inbox groups by issue, so this
// stays one unread signal.
//
// fired_inbox_item_id is deliberately NOT set here: the reminder handler archives
// that row on done/snooze/delete, and this row is the user's own message — quite
// possibly unread — not something we created.
func resurfacedSnoozedInboxRow(ctx context.Context, cerebro *cerebrodb.Queries, bus *events.Bus, rem cerebrodb.ClaimDueRemindersRow) bool {
	if !rem.SourceInboxItemID.Valid {
		return false
	}
	row, err := cerebro.FindLiveInboxItemForRecipient(ctx, cerebrodb.FindLiveInboxItemForRecipientParams{
		ID:          rem.SourceInboxItemID,
		RecipientID: rem.RecipientID,
	})
	if err != nil {
		// Archived, deleted, or no longer this member's row — fall back to the
		// standalone reminder row so the reminder is never silently lost.
		return false
	}
	if !row.IssueID.Valid {
		return false
	}
	if _, err := cerebro.SetInboxUnreadByIssue(ctx, cerebrodb.SetInboxUnreadByIssueParams{
		WorkspaceID:   row.WorkspaceID,
		RecipientType: "member",
		RecipientID:   rem.RecipientID,
		IssueID:       row.IssueID,
	}); err != nil {
		slog.Warn("cerebro reminder sweeper: failed to mark snoozed inbox row unread", "error", err)
		return false
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
	return true
}

// surfaceReminderInboxRow drops a fresh, unread `reminder` inbox row carrying
// the reminder text, so every fired member reminder lands in the inbox's
// Reminders section at its planned time (FIR-2154, FIR-2278). issue_id is the
// conversation for an issue-/comment-anchored reminder and NULL for a free,
// project, or chat reminder.
func surfaceReminderInboxRow(ctx context.Context, cerebro *cerebrodb.Queries, bus *events.Bus, rem cerebrodb.ClaimDueRemindersRow) {
	title := rem.Text
	if title == "" {
		title = "Reminder"
	}
	created, err := cerebro.CreateFiredReminderInboxItem(ctx, cerebrodb.CreateFiredReminderInboxItemParams{
		WorkspaceID: rem.WorkspaceID,
		RecipientID: rem.RecipientID,
		IssueID:     rem.ConversationID, // invalid → NULL for free/project/chat reminders
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
