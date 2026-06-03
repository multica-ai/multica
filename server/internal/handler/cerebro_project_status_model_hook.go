package handler

// CEREBRO-PATCH(project-create-default-status-model): FIR-2800 — auto-assign
// workspace default status model to every new project. Net-new cerebro file
// keeps the upstream project.go footprint to a single marked call site.

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// cerebroAutoAssignWorkspaceDefaultStatusModel runs after a project is
// successfully committed. When the workspace has a default status model
// configured (cerebro_status_model.workspace_default = true), the new project
// is automatically assigned that model so the admin does not have to
// visit Settings → Status models after every project create.
//
// This is intentionally best-effort: any error is logged and silently
// swallowed. Project creation never fails because of the auto-assignment.
func (h *Handler) cerebroAutoAssignWorkspaceDefaultStatusModel(
	ctx context.Context,
	projectID pgtype.UUID,
	wsUUID pgtype.UUID,
	actorIDStr string,
) {
	if h.CerebroQueries == nil {
		return
	}
	defaultModel, err := h.CerebroQueries.GetWorkspaceDefaultStatusModel(ctx, wsUUID)
	if err != nil {
		// pgx.ErrNoRows means no default is configured — silent, expected case.
		return
	}
	actorUUID, err := util.ParseUUID(actorIDStr)
	if err != nil {
		slog.Warn("cerebro: auto-assign default status model: invalid actor id", "actor", actorIDStr)
		return
	}
	if _, err := h.CerebroQueries.UpsertProjectStatusModel(ctx, cerebrodb.UpsertProjectStatusModelParams{
		ProjectID:      projectID,
		WorkspaceID:    wsUUID,
		StatusModelID:  defaultModel.ID,
		AssignedByID:   actorUUID,
		AssignedByType: "member",
	}); err != nil {
		slog.Warn("cerebro: auto-assign default status model failed",
			"project_id", util.UUIDToString(projectID),
			"model_id", util.UUIDToString(defaultModel.ID),
			"err", err,
		)
	}
}
