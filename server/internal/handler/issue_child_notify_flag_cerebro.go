// CEREBRO-PATCH(child-done-notify-flag): TECH-3006 — flag helpers for child-to-parent notifications.
// Kept in a cerebro-prefixed file so the upstream issue_child_done.go patch
// stays to a single marked gate line.
package handler

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// cerebroChildDoneNotifyParentEnabled reports whether the workspace flag
// cerebro_child_done_notify_parent is on for this request actor. Default ON:
// a missing row (or any lookup failure) resolves to true, preserving current
// behavior on deploy.
func (h *Handler) cerebroChildDoneNotifyParentEnabled(ctx context.Context, workspaceID pgtype.UUID, requesterUserID string) bool {
	return h.cerebroChildNotifyFlag(ctx, workspaceID, requesterUserID, "cerebro_child_done_notify_parent")
}

// CEREBRO-PATCH(child-done-notify-flag): TECH-3006 — per-status notify flags.
// childStatusNotifyFlagKeys maps each non-done child status that wakes the
// parent to its own per-status feature flag, split from the original combined
// `cerebro_child_status_notify_parent` flag so in_review and blocked can be
// silenced independently.
var childStatusNotifyFlagKeys = map[string]string{
	"in_review": "cerebro_child_in_review_notify_parent",
	"blocked":   "cerebro_child_blocked_notify_parent",
}

// cerebroChildStatusNotifyParentEnabled reports whether the per-status
// notification flag for `status` (in_review / blocked) is on. Default ON: an
// unknown status or any lookup failure resolves to true, preserving current
// behavior.
func (h *Handler) cerebroChildStatusNotifyParentEnabled(ctx context.Context, workspaceID pgtype.UUID, requesterUserID, status string) bool {
	key, ok := childStatusNotifyFlagKeys[status]
	if !ok {
		return true
	}
	return h.cerebroChildNotifyFlag(ctx, workspaceID, requesterUserID, key)
}

// cerebroChildNotifyFlag resolves the effective flag value using the same
// precedence as packages/cerebro-feature-flags/store.ts:
// locked workspace > personal > unlocked workspace > default.
func (h *Handler) cerebroChildNotifyFlag(ctx context.Context, workspaceID pgtype.UUID, requesterUserID string, key string) bool {
	if h.CerebroQueries == nil {
		return true
	}

	var wsEnabled, wsLocked, wsFound bool
	wsRows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		slog.Warn("child notify flag: workspace flag lookup failed", "error", err, "flag_key", key)
		return true
	}
	for _, row := range wsRows {
		if row.FlagKey == key {
			wsEnabled, wsLocked, wsFound = row.Enabled, row.Locked, true
			break
		}
	}
	if wsFound && wsLocked {
		return wsEnabled
	}

	if requesterUserID != "" {
		userID, parseErr := util.ParseUUID(requesterUserID)
		if parseErr == nil {
			on, perr := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
				WorkspaceID: workspaceID,
				UserID:      userID,
				FlagKey:     key,
			})
			if perr == nil {
				return on
			}
			if !errors.Is(perr, pgx.ErrNoRows) {
				slog.Warn("child notify flag: personal flag lookup failed", "error", perr, "flag_key", key)
				return true
			}
		}
	}

	if wsFound {
		return wsEnabled
	}
	return true
}
