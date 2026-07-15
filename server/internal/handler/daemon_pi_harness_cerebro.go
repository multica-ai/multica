package handler

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

const piHarnessFeatureFlag = "cerebro_pi_harness"

// piHarnessFeatureEnabled mirrors the client flag precedence: locked workspace,
// member override, workspace override, then the registry's default-on value.
// A database failure disables the harness so rollback never depends on spawn.
func (h *Handler) piHarnessFeatureEnabled(ctx context.Context, workspaceID, ownerID pgtype.UUID) bool {
	if h.CerebroQueries == nil {
		return false
	}
	rows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		return false
	}
	workspaceEnabled, workspaceSet, workspaceLocked := true, false, false
	for _, row := range rows {
		if row.FlagKey == piHarnessFeatureFlag {
			workspaceEnabled, workspaceSet, workspaceLocked = row.Enabled, true, row.Locked
			break
		}
	}
	if workspaceLocked {
		return workspaceEnabled
	}
	personal, err := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      ownerID,
		FlagKey:     piHarnessFeatureFlag,
	})
	if err == nil {
		return personal
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if workspaceSet {
		return workspaceEnabled
	}
	return true
}
