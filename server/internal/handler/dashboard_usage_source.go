package handler

import (
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DashboardUsageBySourceResponse is one (date, agent, autopilot, provider,
// model) token row. AutopilotID is null for manual runs. A run belongs to an
// autopilot when its task carries an autopilot run, or when its issue was
// created by one (see migration 760).
type DashboardUsageBySourceResponse struct {
	Date                     string  `json:"date"`
	AgentID                  string  `json:"agent_id"`
	AutopilotID              *string `json:"autopilot_id"`
	Provider                 string  `json:"provider"`
	Model                    string  `json:"model"`
	InputTokens              int64   `json:"input_tokens"`
	OutputTokens             int64   `json:"output_tokens"`
	CacheReadTokens          int64   `json:"cache_read_tokens"`
	CacheWriteTokens         int64   `json:"cache_write_tokens"`
	CostUSDTicks             int64   `json:"cost_usd_ticks"`
	UncostedInputTokens      int64   `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64   `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64   `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64   `json:"uncosted_cache_write_tokens"`
	TaskCount                int32   `json:"task_count"`
}

// usageBySourceKey keeps a folded restricted-agent row split by everything
// but the agent, so the client can still price it and attribute it.
type usageBySourceKey struct {
	date, autopilot, provider, model string
}

// GetDashboardUsageBySource returns per-day token rows split by autopilot,
// for the Usage page's manual / autopilot breakdown. Same filters, viewing
// tz, history limit and restricted-agent folding as the other usage rollups.
func (h *Handler) GetDashboardUsageBySource(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	squadScope, squadID, ok := parseSquadScopeParam(w, r)
	if !ok {
		return
	}
	if !h.dashboardProjectVisible(w, r, workspaceID, projectID) {
		return
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}
	tz := h.resolveViewingTZ(r)
	window := h.dashboardCutoff(w, r, workspaceID, tz, 0)

	rows, err := h.Queries.ListDashboardUsageBySource(r.Context(), db.ListDashboardUsageBySourceParams{
		WorkspaceID: parseUUID(workspaceID),
		Tz:          tz,
		Since:       window.since,
		ProjectID:   projectID,
		SquadScope:  squadScope,
		SquadID:     squadID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by source")
		return
	}
	resp := make([]DashboardUsageBySourceResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardUsageBySourceResponse{
			Date:                     row.Date.Time.Format("2006-01-02"),
			AgentID:                  uuidToString(row.AgentID),
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
			TaskCount:                row.TaskCount,
		}
		if row.AutopilotID.Valid {
			id := uuidToString(row.AutopilotID)
			resp[i].AutopilotID = &id
		}
	}
	writeJSON(w, http.StatusOK, foldRestrictedAgents(
		resp,
		restricted,
		func(row DashboardUsageBySourceResponse) string { return row.AgentID },
		func(row DashboardUsageBySourceResponse) (DashboardUsageBySourceResponse, usageBySourceKey) {
			key := usageBySourceKey{date: row.Date, provider: row.Provider, model: row.Model}
			if row.AutopilotID != nil {
				key.autopilot = *row.AutopilotID
			}
			row.AgentID = restrictedAgentsRowID
			return row, key
		},
		func(dst, src DashboardUsageBySourceResponse) DashboardUsageBySourceResponse {
			dst.InputTokens += src.InputTokens
			dst.OutputTokens += src.OutputTokens
			dst.CacheReadTokens += src.CacheReadTokens
			dst.CacheWriteTokens += src.CacheWriteTokens
			dst.CostUSDTicks += src.CostUSDTicks
			dst.UncostedInputTokens += src.UncostedInputTokens
			dst.UncostedOutputTokens += src.UncostedOutputTokens
			dst.UncostedCacheReadTokens += src.UncostedCacheReadTokens
			dst.UncostedCacheWriteTokens += src.UncostedCacheWriteTokens
			dst.TaskCount += src.TaskCount
			return dst
		},
	))
}
