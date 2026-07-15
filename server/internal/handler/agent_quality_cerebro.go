package handler

// CEREBRO-PATCH(agent-quality): FIR-3212 — net-new fork handler exposing
// quality and satisfaction aggregated per agent config version.
//
// The sample base is the immutable run prompt snapshots, so every number is
// attributed to the config version the run actually read at claim time.
// The response reports counts with explicit sample sizes and forms an average
// only when at least one measurement carried that value — missing data is nil,
// never zero (FIR-3212 hard requirement; the reopen-after-done signal was
// dropped by review decision, satisfaction is reactions + approval/rejection).

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

type agentQualitySolution struct {
	MeasuredRuns  int64    `json:"measured_runs"`
	Measurements  int64    `json:"measurements"`
	Passes        int64    `json:"passes"`
	Scored        int64    `json:"scored"`
	AvgScore      *float64 `json:"avg_score"`
	Confident     int64    `json:"confident"`
	AvgConfidence *float64 `json:"avg_confidence"`
}

type agentQualitySatisfaction struct {
	MeasuredRuns int64 `json:"measured_runs"`
	Reactions    int64 `json:"reactions"`
	Approvals    int64 `json:"approvals"`
	Rejections   int64 `json:"rejections"`
}

type agentQualityVersionResponse struct {
	AgentContextVersion   string                   `json:"agent_context_version"`
	AgentContextVersionID *string                  `json:"agent_context_version_id"`
	Runs                  int64                    `json:"runs"`
	FirstRunAt            *time.Time               `json:"first_run_at"`
	LastRunAt             *time.Time               `json:"last_run_at"`
	Solution              agentQualitySolution     `json:"solution"`
	Satisfaction          agentQualitySatisfaction `json:"satisfaction"`
}

func agentQualityVersion(row cerebrodb.AggregateAgentQualityByVersionRow) agentQualityVersionResponse {
	v := agentQualityVersionResponse{
		AgentContextVersion: row.AgentContextVersion,
		Runs:                row.Runs,
		Solution: agentQualitySolution{
			MeasuredRuns: row.SolutionMeasuredRuns,
			Measurements: row.SolutionMeasurements,
			Passes:       row.SolutionPasses,
			Scored:       row.SolutionScored,
			Confident:    row.SolutionConfident,
		},
		Satisfaction: agentQualitySatisfaction{
			MeasuredRuns: row.SatisfactionMeasuredRuns,
			Reactions:    row.Reactions,
			Approvals:    row.Approvals,
			Rejections:   row.Rejections,
		},
	}
	if row.AgentContextVersionID.Valid {
		id := uuidToString(row.AgentContextVersionID)
		v.AgentContextVersionID = &id
	}
	if row.FirstRunAt.Valid {
		t := row.FirstRunAt.Time
		v.FirstRunAt = &t
	}
	if row.LastRunAt.Valid {
		t := row.LastRunAt.Time
		v.LastRunAt = &t
	}
	if row.SolutionScored > 0 {
		avg := row.SolutionScoreSum / float64(row.SolutionScored)
		v.Solution.AvgScore = &avg
	}
	if row.SolutionConfident > 0 {
		avg := row.SolutionConfidenceSum / float64(row.SolutionConfident)
		v.Solution.AvgConfidence = &avg
	}
	return v
}

// GetAgentQuality returns quality/satisfaction per agent config version.
// GET /api/agents/{id}/quality
func (h *Handler) GetAgentQuality(w http.ResponseWriter, r *http.Request) {
	agentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(agent.WorkspaceID)); !ok {
		return
	}
	if h.CerebroQueries == nil {
		writeError(w, http.StatusServiceUnavailable, "quality store unavailable")
		return
	}
	rows, err := h.CerebroQueries.AggregateAgentQualityByVersion(r.Context(), agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to aggregate agent quality")
		return
	}
	versions := make([]agentQualityVersionResponse, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, agentQualityVersion(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}
