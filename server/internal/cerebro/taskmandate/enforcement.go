package taskmandate

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

const EnforcementFlagKey = "cerebro_task_mandate_enforcement"

type EnforcementFlagReader interface {
	ListCerebroWorkspaceFeatureFlags(context.Context, pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error)
}

// EnforcementEnabled is the shared fail-open rollout circuit breaker. Task
// Mandates may still be issued and observed while this returns false, but they
// cannot reject a local or Firtal Gateway call until the workspace explicitly
// enables the flag.
func EnforcementEnabled(ctx context.Context, flags EnforcementFlagReader, workspaceID pgtype.UUID) bool {
	if flags == nil || !workspaceID.Valid {
		return false
	}
	rows, err := flags.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.FlagKey == EnforcementFlagKey {
			return row.Enabled
		}
	}
	return false
}
