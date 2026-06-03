// CEREBRO-PATCH(member-default-group-placement): FIR-2732 — place new workspace
// members into the configured default cerebro group (invitation accept,
// manual member add). Best-effort: failures are logged but never block the
// membership response.
package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/groupplacement"
)

func (h *Handler) placeMemberInDefaultGroup(ctx context.Context, workspaceID, userID pgtype.UUID) {
	if h.CerebroQueries == nil {
		return
	}
	if err := groupplacement.PlaceUserInDefaultGroup(ctx, h.CerebroQueries, workspaceID, userID); err != nil {
		slog.Warn("default group placement failed",
			"workspace_id", uuidToString(workspaceID),
			"user_id", uuidToString(userID),
			"error", err)
	}
}
