package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
)

// CEREBRO-PATCH(task-mandate-enforcement-circuit-breaker): FIR-4220 keeps all
// local call-time Task Mandate checks behind the shared default-off workspace
// flag while issuance and observation continue independently.
func (h *Handler) taskMandateEnforcementEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if h == nil || h.CerebroQueries == nil {
		return false
	}
	return taskmandate.EnforcementEnabled(ctx, h.CerebroQueries, workspaceID)
}
