package agentoffice

// HTTP layer for context observability (FIR-1775 Phase 4). Two read-only
// endpoints:
//
//   GET /api/agents/{id}/context/observability   one agent's overview
//   GET /api/agents/context/observability         workspace-wide overview
//
// Both reuse the Phase 3 lint (lint_handler.go) for the drift dimension so the
// "where does it drift" number is byte-identical to the lint surface.

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// ObservabilityAgent returns the overview for one agent.
func (h *Handler) ObservabilityAgent(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.loadAgent(w, r)
	if !ok {
		return
	}

	versionStats, err := h.Svc.Cerebro.AgentContextVersionStats(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load version stats: "+err.Error())
		return
	}

	crRows, err := h.Svc.Cerebro.ListAgentChangeRequestStats(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load change-request stats: "+err.Error())
		return
	}
	var counts ChangeRequestCounts
	for _, row := range crRows {
		foldChangeRequestCounts(&counts, row.Status, row.Count)
	}

	approverRows, err := h.Svc.Cerebro.ListAgentContextApproverStats(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load approver stats: "+err.Error())
		return
	}
	acc := newApproverAccumulator()
	for _, row := range approverRows {
		acc.add(util.UUIDToString(row.UserID), row.Name, row.Status, row.Count)
	}

	// Drift: reuse the Phase 3 lint so this number matches the lint surface.
	shared, err := h.loadLintSharedInputs(r, member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load lint inputs: "+err.Error())
		return
	}
	report, err := h.lintOneAgent(r, agent, shared)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lint agent: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, AgentObservabilityResponse{
		AgentID:         util.UUIDToString(agent.ID),
		AgentName:       agent.Name,
		ContextVersion:  agent.ContextVersion,
		VersionCount:    versionStats.VersionCount,
		VersionsLast30d: versionStats.VersionsLast30d,
		LastChangedAt:   util.TimestampToPtr(versionStats.LastChangedAt),
		ChangeRequests:  counts,
		Approvers:       acc.result(),
		Drift:           summarizeDrift(report.Findings),
	})
}

// ObservabilityWorkspace returns the overview for every non-archived agent in
// the workspace, plus workspace-wide totals and the approver leaderboard.
func (h *Handler) ObservabilityWorkspace(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	wsID := member.WorkspaceID

	versionRows, err := h.Svc.Cerebro.ListAgentContextVersionStatsByWorkspace(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load version stats: "+err.Error())
		return
	}

	// Change-request counts, grouped by agent.
	crRows, err := h.Svc.Cerebro.ListAgentChangeRequestStatsByWorkspace(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load change-request stats: "+err.Error())
		return
	}
	crByAgent := map[string]*ChangeRequestCounts{}
	for _, row := range crRows {
		id := util.UUIDToString(row.AgentID)
		counts, seen := crByAgent[id]
		if !seen {
			counts = &ChangeRequestCounts{}
			crByAgent[id] = counts
		}
		foldChangeRequestCounts(counts, row.Status, row.Count)
	}

	// Approver leaderboard (workspace-wide).
	approverRows, err := h.Svc.Cerebro.ListAgentContextApproverStatsByWorkspace(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load approver stats: "+err.Error())
		return
	}
	acc := newApproverAccumulator()
	for _, row := range approverRows {
		acc.add(util.UUIDToString(row.UserID), row.Name, row.Status, row.Count)
	}

	// Drift: sweep every agent once with shared inputs (same as the lint sweep).
	agents, err := h.Svc.Cerebro.ListAgentsForContextLint(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents: "+err.Error())
		return
	}
	shared, err := h.loadLintSharedInputs(r, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load lint inputs: "+err.Error())
		return
	}
	driftByAgent := map[string]int{}
	for _, a := range agents {
		report, err := h.lintOneAgent(r, a, shared)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lint agent "+a.Name+": "+err.Error())
			return
		}
		driftByAgent[util.UUIDToString(a.ID)] = len(report.Findings)
	}

	resp := WorkspaceObservabilityResponse{
		Totals:    WorkspaceObservabilityTotals{Agents: len(versionRows)},
		Agents:    make([]AgentObservabilityRow, 0, len(versionRows)),
		Approvers: acc.result(),
	}
	for _, v := range versionRows {
		row := versionStatsToRow(v, util.TimestampToPtr(v.LastChangedAt))
		if counts, ok := crByAgent[row.AgentID]; ok {
			row.ChangeRequests = *counts
		}
		row.DriftFindings = driftByAgent[row.AgentID]
		resp.Totals.VersionsLast30d += row.VersionsLast30d
		resp.Totals.PendingChangeRequests += row.ChangeRequests.Pending
		resp.Totals.DriftFindings += row.DriftFindings
		resp.Agents = append(resp.Agents, row)
	}
	writeJSON(w, http.StatusOK, resp)
}
