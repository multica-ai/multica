package handler

import (
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

// GetGovernanceHealth returns the system health the Controls tab shows (spec
// 4.9): each scheduled job's last success and lag, failed deliveries, WeKnora's
// state, the budget gate's cache age and the risk lines. Any member may read it.
//
//	GET /api/governance/health
func (h *Handler) GetGovernanceHealth(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	svc := &service.HealthService{Queries: h.Queries, Knowledge: h.Knowledge}
	// The gate that admits runs is the task service's, so its cache is the one
	// health reads. Without a task service the gate is not reported.
	if h.TaskService != nil && h.TaskService.Budget != nil {
		svc.BudgetGate = h.TaskService.Budget
	}
	report, err := svc.Report(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("read governance health failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to read the system health")
		return
	}
	writeJSON(w, http.StatusOK, report)
}
