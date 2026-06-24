// CEREBRO-PATCH(inbox-thread-split-read): FIR-1854 — Slack-style independent
// read state for channel/DM threads. A reply that lives inside a thread keeps
// its own unread state, separate from the channel: it is excluded from the
// channel's unread badge and from channel mark-read, and it is cleared only by
// opening the thread itself. Kept in a cerebro-prefixed file so the upstream
// channel.go edits stay to single marked gate lines.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const cerebroInboxThreadSplitFlagKey = "cerebro_inbox_thread_split"

// cerebroChannelUnreadSmartFlagKey gates FIR-2010 chat-specific unread: a
// channel's badge counts only @mentions (other activity is a bold dot) and the
// channel is marked read on scroll-past, not on open. DMs are unaffected.
const cerebroChannelUnreadSmartFlagKey = "cerebro_channel_unread_smart"

// cerebroInboxThreadSplitEnabled reports whether the inbox thread-split feature
// is on for this workspace+user. Resolution mirrors
// packages/cerebro-feature-flags/store.ts precedence (locked workspace >
// personal > unlocked workspace > default) and defaults ON, so a missing row or
// any lookup failure preserves the shipped behavior.
func (h *Handler) cerebroInboxThreadSplitEnabled(ctx context.Context, workspaceID pgtype.UUID, requesterUserID string) bool {
	return h.cerebroResolveFlag(ctx, workspaceID, requesterUserID, cerebroInboxThreadSplitFlagKey, true)
}

// cerebroChannelUnreadSmartEnabled reports whether FIR-2010 smart channel
// unread is on for this workspace+user. Defaults OFF, so a missing row or any
// lookup failure preserves today's behavior (channel counts every message).
func (h *Handler) cerebroChannelUnreadSmartEnabled(ctx context.Context, workspaceID pgtype.UUID, requesterUserID string) bool {
	return h.cerebroResolveFlag(ctx, workspaceID, requesterUserID, cerebroChannelUnreadSmartFlagKey, false)
}

// cerebroResolveFlag resolves a cerebro feature flag for this workspace+user.
// Precedence mirrors packages/cerebro-feature-flags/store.ts (locked workspace
// > personal > unlocked workspace > default). On any lookup failure or missing
// row it returns def, so the shipped behavior is preserved.
func (h *Handler) cerebroResolveFlag(ctx context.Context, workspaceID pgtype.UUID, requesterUserID, flagKey string, def bool) bool {
	if h.CerebroQueries == nil {
		return def
	}
	var wsEnabled, wsLocked, wsFound bool
	wsRows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		slog.Warn("cerebro flag: workspace lookup failed", "flag", flagKey, "error", err)
		return def
	}
	for _, row := range wsRows {
		if row.FlagKey == flagKey {
			wsEnabled, wsLocked, wsFound = row.Enabled, row.Locked, true
			break
		}
	}
	if wsFound && wsLocked {
		return wsEnabled
	}
	if requesterUserID != "" {
		if userID, perr := util.ParseUUID(requesterUserID); perr == nil {
			on, gerr := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
				WorkspaceID: workspaceID,
				UserID:      userID,
				FlagKey:     flagKey,
			})
			if gerr == nil {
				return on
			}
			if !errors.Is(gerr, pgx.ErrNoRows) {
				slog.Warn("cerebro flag: personal lookup failed", "flag", flagKey, "error", gerr)
				return def
			}
		}
	}
	if wsFound {
		return wsEnabled
	}
	return def
}

// cerebroChannelUnreadCount drives a channel/DM's unread badge. When the
// thread-split feature is on, replies that live inside a thread are excluded —
// they surface on their own split inbox row instead of inflating the channel
// count. With the feature off it falls back to the upstream count.
func (h *Handler) cerebroChannelUnreadCount(ctx context.Context, workspaceID pgtype.UUID, userID string, issueID pgtype.UUID) int64 {
	if h.cerebroInboxThreadSplitEnabled(ctx, workspaceID, userID) {
		count, err := h.Queries.CountUnreadInboxForChannelExcludingThreads(ctx, db.CountUnreadInboxForChannelExcludingThreadsParams{
			RecipientID: parseUUID(userID),
			IssueID:     issueID,
		})
		if err != nil {
			return 0
		}
		return count
	}
	count, err := h.Queries.CountUnreadInboxForChannel(ctx, db.CountUnreadInboxForChannelParams{
		RecipientID: parseUUID(userID),
		IssueID:     issueID,
	})
	if err != nil {
		return 0
	}
	return count
}

// cerebroChannelUnread computes a channel/DM's badge count and whether it has
// ANY unread activity (FIR-2010). When smart unread is on for a channel (not a
// DM), the badge counts only @mentions — so the red badge appears only on a
// mention — while hasActivity stays true for non-mention messages so the UI can
// still show a bold dot. DMs and the flag-off path keep the all-messages count.
func (h *Handler) cerebroChannelUnread(ctx context.Context, workspaceID pgtype.UUID, userID, kind string, issueID pgtype.UUID) (count int64, hasActivity bool) {
	all := h.cerebroChannelUnreadCount(ctx, workspaceID, userID, issueID)
	if kind == ChannelKindChannel && h.cerebroChannelUnreadSmartEnabled(ctx, workspaceID, userID) {
		mentions, err := h.Queries.CountUnreadInboxForChannelMentionsOnly(ctx, db.CountUnreadInboxForChannelMentionsOnlyParams{
			RecipientID: parseUUID(userID),
			IssueID:     issueID,
		})
		if err != nil {
			mentions = 0
		}
		return mentions, all > 0
	}
	return all, all > 0
}

// cerebroMarkChannelRead marks a channel/DM read on open. When the thread-split
// feature is on, replies that live inside a thread are left untouched so an
// unread thread row survives until the user opens the thread itself.
func (h *Handler) cerebroMarkChannelRead(ctx context.Context, workspaceID, userID pgtype.UUID, issueID pgtype.UUID) (int64, error) {
	if h.cerebroInboxThreadSplitEnabled(ctx, workspaceID, util.UUIDToString(userID)) {
		return h.Queries.MarkInboxReadByIssueExcludingThreads(ctx, db.MarkInboxReadByIssueExcludingThreadsParams{
			WorkspaceID:   workspaceID,
			RecipientType: "member",
			RecipientID:   userID,
			IssueID:       issueID,
		})
	}
	return h.Queries.MarkInboxReadByIssue(ctx, db.MarkInboxReadByIssueParams{
		WorkspaceID:   workspaceID,
		RecipientType: "member",
		RecipientID:   userID,
		IssueID:       issueID,
	})
}

// MarkThreadRead handles POST /api/channels/{id}/threads/{rootId}/read. It marks
// only the inbox rows that belong to one thread (matched on
// details.thread_root_id) read, so opening a thread clears just that thread and
// leaves the rest of the channel — and any other thread — untouched.
func (h *Handler) MarkThreadRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	channelID := chi.URLParam(r, "id")
	rootID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "rootId"), "rootId")
	if !ok {
		return
	}

	issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          parseUUID(channelID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if issue.Kind != ChannelKindChannel && issue.Kind != ChannelKindDM {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}

	subscribed, err := h.Queries.IsIssueSubscriber(r.Context(), db.IsIssueSubscriberParams{
		IssueID:  issue.ID,
		UserType: "member",
		UserID:   parseUUID(userID),
	})
	if err != nil || !subscribed {
		writeError(w, http.StatusForbidden, "not a participant of this channel")
		return
	}

	count, err := h.Queries.MarkInboxReadByThreadRoot(r.Context(), db.MarkInboxReadByThreadRootParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(userID),
		IssueID:       issue.ID,
		ThreadRootID:  util.UUIDToString(rootID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark thread read")
		return
	}

	slog.Info("channel: mark thread read", append(logger.RequestAttrs(r), "user_id", userID, "channel_id", channelID, "thread_root_id", util.UUIDToString(rootID), "count", count)...)
	h.publish(protocol.EventInboxBatchRead, workspaceID, "member", userID, map[string]any{
		"recipient_id":   userID,
		"issue_id":       channelID,
		"thread_root_id": util.UUIDToString(rootID),
		"count":          count,
	})

	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}
