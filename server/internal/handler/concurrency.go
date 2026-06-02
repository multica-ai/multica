package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/dwickyfp/wallts/server/internal/util"
)

// ConcurrencyStatsResponse is the JSON shape returned by GetConcurrencyStats.
type ConcurrencyStatsResponse struct {
	WorkspaceID    string                   `json:"workspace_id"`
	ActiveCount    int                      `json:"active_count"`
	QueuedCount    int                      `json:"queued_count"`
	CompletedLastH int                      `json:"completed_last_hour"`
	FailedLastH    int                      `json:"failed_last_hour"`
	AgentDetails   []AgentConcurrencyDetail `json:"agent_details"`
}

// AgentConcurrencyDetail describes a single agent's concurrency utilization.
type AgentConcurrencyDetail struct {
	AgentID           string `json:"agent_id"`
	AgentName         string `json:"agent_name"`
	MaxConcurrentTasks int   `json:"max_concurrent_tasks"`
	RunningCount      int    `json:"running_count"`
	QueuedCount       int    `json:"queued_count"`
	AtCapacity        bool   `json:"at_capacity"`
}

// GetConcurrencyStats returns real-time concurrency metrics for a workspace.
// Exposed at GET /api/workspaces/{workspaceId}/concurrency.
//
// The response includes:
//   - Workspace-wide active/queued/completed/failed counts
//   - Per-agent concurrency utilization (running vs max_concurrent_tasks)
//
// Used by monitoring dashboards, the CLI `concurrency status` command, and
// ops tooling to detect serialization bottlenecks and capacity issues.
func (h *Handler) GetConcurrencyStats(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}

	// Workspace-wide aggregates.
	stats, err := h.Queries.GetWorkspaceConcurrencyStats(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get concurrency stats")
		return
	}

	// Per-agent breakdown.
	agentRows, err := h.Queries.GetWorkspaceAgentConcurrencyDetail(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent concurrency details")
		return
	}

	details := make([]AgentConcurrencyDetail, 0, len(agentRows))
	for _, row := range agentRows {
		atCap := row.MaxConcurrentTasks > 0 && row.RunningCount >= row.MaxConcurrentTasks
		details = append(details, AgentConcurrencyDetail{
			AgentID:            util.UUIDToString(row.AgentID),
			AgentName:          row.AgentName,
			MaxConcurrentTasks: int(row.MaxConcurrentTasks),
			RunningCount:       row.RunningCount,
			QueuedCount:        row.QueuedCount,
			AtCapacity:         atCap,
		})
	}

	resp := ConcurrencyStatsResponse{
		WorkspaceID:    workspaceID,
		ActiveCount:    stats.ActiveCount,
		QueuedCount:    stats.QueuedCount,
		CompletedLastH: stats.CompletedLastHour,
		FailedLastH:    stats.FailedLastHour,
		AgentDetails:   details,
	}

	writeJSON(w, http.StatusOK, resp)
}

