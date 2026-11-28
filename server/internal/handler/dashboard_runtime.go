package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Per-runtime usage on the workspace dashboard ("By machine" tab).
//
//   GET /api/dashboard/usage/by-runtime   per-runtime token usage + run count
//
// Takes the same ?days= / ?project_id= / ?squad_id= / ?tz= as usage/by-agent,
// on its exact-N window and under the plan's history limit (dashboardCutoff
// with trimDays 1). Tokens come from task_usage_hourly, the table behind the
// Cost and Tokens KPIs, so the rows add up to them. The run count reads raw
// task_usage (COUNT DISTINCT per runtime) on the same hour-aligned cutoff,
// because the rollup counts a run once per hourly bucket it touched. The
// client groups runtimes into machines the way the Runtimes page does.
//
// Access control: a runtime is named only to a caller who may read its usage
// on the runtime page, i.e. who passes the GET /api/runtimes/{id}/usage gate
// (service.RuntimeAllowsUser: the runtime's owner, anyone when it is public,
// the members it is shared with). Owners and admins included: they can list
// someone else's private runtime, but not read its usage. Every other row,
// and the rows of a runtime that no longer exists, is folded onto
// restrictedRuntimesRowID, keeping (provider, model) for pricing, so the
// breakdown still adds up to the workspace totals beside it without naming the
// runtime or splitting it from the others. There is no agent dimension, so no
// agent is named. Private projects (MUL-8472) follow the other rollups: a
// project_id the caller cannot see is answered like a missing project.

// restrictedRuntimesRowID is the synthetic runtime_id every runtime this
// response refuses to name is folded onto. Not a UUID, so it can never collide
// with a real runtime id.
const restrictedRuntimesRowID = "__restricted_runtimes__"

// DashboardRuntimeUsage is one (provider, model) usage aggregate, with the
// same authoritative/uncosted cost split as DashboardUsageByAgentResponse.
type DashboardRuntimeUsage struct {
	Provider                 string `json:"provider"`
	Model                    string `json:"model"`
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheReadTokens          int64  `json:"cache_read_tokens"`
	CacheWriteTokens         int64  `json:"cache_write_tokens"`
	CostUSDTicks             int64  `json:"cost_usd_ticks"`
	UncostedInputTokens      int64  `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64  `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64  `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64  `json:"uncosted_cache_write_tokens"`
}

// DashboardUsageByRuntimeResponse is one runtime's usage in the window.
// RuntimeID is restrictedRuntimesRowID for the folded bucket. TaskCount is the
// number of distinct runs with usage; a run that used two models counts once.
type DashboardUsageByRuntimeResponse struct {
	RuntimeID string                  `json:"runtime_id"`
	TaskCount int32                   `json:"task_count"`
	Usage     []DashboardRuntimeUsage `json:"usage"`
}

// GetDashboardUsageByRuntime returns per-runtime token aggregates and run
// counts for the workspace, optionally scoped to a project and/or squad.
func (h *Handler) GetDashboardUsageByRuntime(w http.ResponseWriter, r *http.Request) {
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
	tz := h.resolveViewingTZ(r)
	window := h.dashboardCutoff(w, r, workspaceID, tz, 1)
	wsUUID := parseUUID(workspaceID)

	usageRows, err := h.Queries.ListDashboardUsageByRuntime(r.Context(), db.ListDashboardUsageByRuntimeParams{
		WorkspaceID: wsUUID,
		Since:       window.since,
		ProjectID:   projectID,
		SquadScope:  squadScope,
		SquadID:     squadID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by runtime")
		return
	}
	countRows, err := h.Queries.ListDashboardTaskCountByRuntime(r.Context(), db.ListDashboardTaskCountByRuntimeParams{
		WorkspaceID: wsUUID,
		SinceHour:   usageHourCutoff(window.since),
		ProjectID:   projectID,
		SquadScope:  squadScope,
		SquadID:     squadID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by runtime")
		return
	}
	// Fails closed: without the runtime list nothing can be named, and an
	// unfiltered response is the one outcome this must never degrade to.
	runtimes, err := h.Queries.ListAgentRuntimes(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve runtime access")
		return
	}
	readable := make(map[pgtype.UUID]bool, len(runtimes))
	for _, rt := range runtimes {
		if service.RuntimeAllowsUser(rt, member.UserID) {
			readable[rt.ID] = true
		}
	}
	runtimeIDOf := func(id pgtype.UUID) string {
		if readable[id] {
			return uuidToString(id)
		}
		return restrictedRuntimesRowID
	}

	resp := make([]DashboardUsageByRuntimeResponse, 0)
	index := make(map[string]int)
	rowFor := func(runtimeID string) *DashboardUsageByRuntimeResponse {
		i, seen := index[runtimeID]
		if !seen {
			i = len(resp)
			index[runtimeID] = i
			resp = append(resp, DashboardUsageByRuntimeResponse{RuntimeID: runtimeID, Usage: []DashboardRuntimeUsage{}})
		}
		return &resp[i]
	}
	// The restricted bucket merges rows that collide on (provider, model), so
	// it carries no per-runtime split.
	restrictedUsage := make(map[providerModelKey]int)
	for _, row := range usageRows {
		rt := rowFor(runtimeIDOf(row.RuntimeID))
		u := DashboardRuntimeUsage{
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
		}
		if rt.RuntimeID != restrictedRuntimesRowID {
			rt.Usage = append(rt.Usage, u)
			continue
		}
		key := providerModelKey{provider: u.Provider, model: u.Model}
		if j, ok := restrictedUsage[key]; ok {
			dst := &rt.Usage[j]
			dst.InputTokens += u.InputTokens
			dst.OutputTokens += u.OutputTokens
			dst.CacheReadTokens += u.CacheReadTokens
			dst.CacheWriteTokens += u.CacheWriteTokens
			dst.CostUSDTicks += u.CostUSDTicks
			dst.UncostedInputTokens += u.UncostedInputTokens
			dst.UncostedOutputTokens += u.UncostedOutputTokens
			dst.UncostedCacheReadTokens += u.UncostedCacheReadTokens
			dst.UncostedCacheWriteTokens += u.UncostedCacheWriteTokens
			continue
		}
		restrictedUsage[key] = len(rt.Usage)
		rt.Usage = append(rt.Usage, u)
	}
	// A run has exactly one runtime, so the per-runtime distinct counts are
	// disjoint and the folded bucket's count is their sum.
	for _, row := range countRows {
		rowFor(runtimeIDOf(row.RuntimeID)).TaskCount += row.TaskCount
	}
	writeJSON(w, http.StatusOK, resp)
}
