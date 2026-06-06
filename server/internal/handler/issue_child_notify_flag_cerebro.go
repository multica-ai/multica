// CEREBRO: TECH-3006 — workspace flag helpers for child-to-parent notifications.
// Kept in a cerebro-prefixed file so the upstream issue_child_done.go patch
// stays to a single marked gate line.
package handler

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// cerebroChildDoneNotifyParentEnabled reports whether the workspace flag
// cerebro_child_done_notify_parent is on. Default ON: a missing row (or any
// lookup failure) resolves to true, preserving current behavior on deploy.
func (h *Handler) cerebroChildDoneNotifyParentEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	return h.cerebroChildNotifyWorkspaceFlag(ctx, workspaceID, "cerebro_child_done_notify_parent")
}

// cerebroChildStatusNotifyParentEnabled reports whether the workspace flag
// cerebro_child_status_notify_parent is on. Default ON.
func (h *Handler) cerebroChildStatusNotifyParentEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	return h.cerebroChildNotifyWorkspaceFlag(ctx, workspaceID, "cerebro_child_status_notify_parent")
}

// cerebroChildNotifyWorkspaceFlag reads a workspace-level cerebro feature flag
// stored under the all-zero sentinel user_id. Returns true when no override row
// exists — these flags default ON so existing behavior is preserved on deploy.
func (h *Handler) cerebroChildNotifyWorkspaceFlag(ctx context.Context, workspaceID pgtype.UUID, key string) bool {
	if h.CerebroQueries == nil {
		return true
	}
	on, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Bytes: [16]byte{}, Valid: true}, // workspace sentinel (all-zero UUID)
		FlagKey:     key,
	})
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return true // default ON
	}
	return on
}
