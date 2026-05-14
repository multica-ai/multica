package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Workspace / Project dashboard
//
// Three read endpoints power the workspace dashboard:
//
//   GET /api/dashboard/usage/daily       per-(date, model) token rows
//   GET /api/dashboard/usage/by-agent    per-(agent, model) token rows
//   GET /api/dashboard/agent-runtime     per-agent run-time + task counts
//
// All three accept ?days=N (defaults to 30, capped at 365) and an optional
// ?project_id=<uuid> to scope the rollup to a single project. With no
// project_id the data spans the whole workspace.
//
// Cost is computed client-side from a per-model pricing table — the model
// dimension is intentionally preserved on the wire (same convention as the
// per-runtime usage endpoints).
//
// Access control: workspace membership only — we don't filter by per-agent
// visibility on the dashboard because token spend / run time are workspace-
// level operational metrics. Agent-detail pages still gate on per-agent
// access (see GetWorkspaceAgentRunCounts).
// ---------------------------------------------------------------------------

// parseProjectIDParam reads ?project_id=<uuid> off the URL. Returns a
// pgtype.UUID with Valid=false when the param is absent so sqlc's nullable
// argument resolves to SQL NULL and the WHERE clause degrades to "no
// project filter". On a malformed UUID it writes a 400 and returns
// ok=false; callers must return immediately.
func parseProjectIDParam(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := r.URL.Query().Get("project_id")
	if raw == "" {
		return pgtype.UUID{}, true
	}
	u, err := util.ParseUUID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project_id")
		return pgtype.UUID{}, false
	}
	return u, true
}

// DashboardUsageDailyResponse is one (date, model) bucket. Cost-side math
// happens on the client from a per-model pricing table; model stays on the
// wire for that reason.
type DashboardUsageDailyResponse struct {
	Date             string `json:"date"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	TaskCount        int32  `json:"task_count"`
}

// GetDashboardUsageDaily returns per-(date, model) token rows for the
// workspace, optionally scoped to a project. When the dashboard rollup
// is enabled (USAGE_DASHBOARD_ROLLUP_ENABLED=true) reads come from
// `task_usage_dashboard_daily` (migration 084); otherwise from the raw
// task_usage stream.
func (h *Handler) GetDashboardUsageDaily(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	since := parseSinceParam(r, 30)

	resp, err := h.listDashboardUsageDaily(r.Context(), parseUUID(workspaceID), since, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listDashboardUsageDaily(
	ctx context.Context,
	workspaceID pgtype.UUID,
	since pgtype.Timestamptz,
	projectID pgtype.UUID,
) ([]DashboardUsageDailyResponse, error) {
	if h.cfg.UseDailyRollupForDashboard {
		rows, err := h.Queries.ListDashboardUsageDailyRollup(ctx, db.ListDashboardUsageDailyRollupParams{
			WorkspaceID: workspaceID,
			Since:       since,
			ProjectID:   projectID,
		})
		if err != nil {
			return nil, err
		}
		resp := make([]DashboardUsageDailyResponse, len(rows))
		for i, row := range rows {
			resp[i] = DashboardUsageDailyResponse{
				Date:             row.Date.Time.Format("2006-01-02"),
				Model:            row.Model,
				InputTokens:      row.InputTokens,
				OutputTokens:     row.OutputTokens,
				CacheReadTokens:  row.CacheReadTokens,
				CacheWriteTokens: row.CacheWriteTokens,
				TaskCount:        row.TaskCount,
			}
		}
		return resp, nil
	}
	rows, err := h.Queries.ListDashboardUsageDaily(ctx, db.ListDashboardUsageDailyParams{
		WorkspaceID: workspaceID,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, err
	}
	resp := make([]DashboardUsageDailyResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardUsageDailyResponse{
			Date:             row.Date.Time.Format("2006-01-02"),
			Model:            row.Model,
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			CacheWriteTokens: row.CacheWriteTokens,
			TaskCount:        row.TaskCount,
		}
	}
	return resp, nil
}

// DashboardUsageByAgentResponse is one (agent, model) row.
type DashboardUsageByAgentResponse struct {
	AgentID          string `json:"agent_id"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	TaskCount        int32  `json:"task_count"`
}

// GetDashboardUsageByAgent returns per-(agent, model) token aggregates for
// the workspace, optionally scoped to a project. Switches to the rollup
// table when UseDailyRollupForDashboard is on (same gating as the daily
// endpoint above).
func (h *Handler) GetDashboardUsageByAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	since := parseSinceParam(r, 30)

	resp, err := h.listDashboardUsageByAgent(r.Context(), parseUUID(workspaceID), since, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by agent")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listDashboardUsageByAgent(
	ctx context.Context,
	workspaceID pgtype.UUID,
	since pgtype.Timestamptz,
	projectID pgtype.UUID,
) ([]DashboardUsageByAgentResponse, error) {
	if h.cfg.UseDailyRollupForDashboard {
		rows, err := h.Queries.ListDashboardUsageByAgentRollup(ctx, db.ListDashboardUsageByAgentRollupParams{
			WorkspaceID: workspaceID,
			Since:       since,
			ProjectID:   projectID,
		})
		if err != nil {
			return nil, err
		}
		resp := make([]DashboardUsageByAgentResponse, len(rows))
		for i, row := range rows {
			resp[i] = DashboardUsageByAgentResponse{
				AgentID:          uuidToString(row.AgentID),
				Model:            row.Model,
				InputTokens:      row.InputTokens,
				OutputTokens:     row.OutputTokens,
				CacheReadTokens:  row.CacheReadTokens,
				CacheWriteTokens: row.CacheWriteTokens,
				TaskCount:        row.TaskCount,
			}
		}
		return resp, nil
	}
	rows, err := h.Queries.ListDashboardUsageByAgent(ctx, db.ListDashboardUsageByAgentParams{
		WorkspaceID: workspaceID,
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, err
	}
	resp := make([]DashboardUsageByAgentResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardUsageByAgentResponse{
			AgentID:          uuidToString(row.AgentID),
			Model:            row.Model,
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CacheReadTokens,
			CacheWriteTokens: row.CacheWriteTokens,
			TaskCount:        row.TaskCount,
		}
	}
	return resp, nil
}

// DashboardAgentRunTimeResponse is one agent's total terminal-task run time
// over the window. Includes failed tasks so the dashboard can surface how
// much execution time was spent on runs that didn't succeed.
type DashboardAgentRunTimeResponse struct {
	AgentID      string `json:"agent_id"`
	TotalSeconds int64  `json:"total_seconds"`
	TaskCount    int32  `json:"task_count"`
	FailedCount  int32  `json:"failed_count"`
}

// GetDashboardAgentRunTime returns per-agent total task run time (seconds)
// and task counts for the workspace, optionally scoped to a project. Only
// terminal tasks (completed or failed) with both started_at and
// completed_at populated contribute, since queued/running tasks have no
// finite duration.
func (h *Handler) GetDashboardAgentRunTime(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	since := parseSinceParam(r, 30)

	rows, err := h.Queries.ListDashboardAgentRunTime(r.Context(), db.ListDashboardAgentRunTimeParams{
		WorkspaceID: parseUUID(workspaceID),
		Since:       since,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent runtime")
		return
	}

	resp := make([]DashboardAgentRunTimeResponse, len(rows))
	for i, row := range rows {
		resp[i] = DashboardAgentRunTimeResponse{
			AgentID:      uuidToString(row.AgentID),
			TotalSeconds: row.TotalSeconds,
			TaskCount:    row.TaskCount,
			FailedCount:  row.FailedCount,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
