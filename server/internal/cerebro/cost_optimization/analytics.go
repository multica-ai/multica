// CEREBRO: cost-optimization analytics drill-down (FIR-2765).
package cost_optimization

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
)

type analyticsSummaryResponse struct {
	Savings []analyticsSavingSummary `json:"savings"`
}

type analyticsSavingSummary struct {
	SavingKey      string `json:"saving_key"`
	Metric         string `json:"metric"`
	RunsToday      int64  `json:"runs_today"`
	SavedUnitsToday int64 `json:"saved_units_today"`
	SavedCentsToday int64 `json:"saved_cents_today"`
	Runs7d         int64  `json:"runs_7d"`
	SavedUnits7d   int64  `json:"saved_units_7d"`
	SavedCents7d   int64  `json:"saved_cents_7d"`
	Runs30d        int64  `json:"runs_30d"`
	SavedUnits30d  int64  `json:"saved_units_30d"`
	SavedCents30d  int64  `json:"saved_cents_30d"`
}

type analyticsIssuesResponse struct {
	Issues []analyticsIssueRow `json:"issues"`
}

type analyticsIssueRow struct {
	IssueID     string `json:"issue_id"`
	IssueNumber int32  `json:"issue_number"`
	IssueTitle  string `json:"issue_title"`
	RunCount    int64  `json:"run_count"`
	SavedUnits  int64  `json:"saved_units"`
	SavedCents  int64  `json:"saved_cents"`
}

type analyticsRunsResponse struct {
	Runs []analyticsRunRow `json:"runs"`
}

type analyticsRunRow struct {
	TaskID         string  `json:"task_id"`
	CreatedAt      string  `json:"created_at"`
	Mode           string  `json:"mode"`
	Applied        bool    `json:"applied"`
	HeldOut        bool    `json:"held_out"`
	Metric         string  `json:"metric"`
	BaselineValue  int64   `json:"baseline_value"`
	EffectiveValue int64   `json:"effective_value"`
	SavedUnits     int64   `json:"saved_units"`
	SavedCents     int64   `json:"saved_cents"`
	DetailJSON     []byte  `json:"detail_json,omitempty"`
}

// AnalyticsSummary returns per-saving totals for today / 7d / 30d.
func (h *Handler) AnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	rows, err := h.Queries.AnalyticsCostOptimizationBySaving(r.Context(), wsUUID)
	if err != nil {
		slog.Error("analytics cost optimization summary failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load analytics summary")
		return
	}
	out := make([]analyticsSavingSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, analyticsSavingSummary{
			SavingKey:       row.SavingKey,
			Metric:          row.Metric,
			RunsToday:       row.RunsToday,
			SavedUnitsToday: row.SavedUnitsToday,
			SavedCentsToday: row.SavedCentsToday,
			Runs7d:          row.Runs7d,
			SavedUnits7d:    row.SavedUnits7d,
			SavedCents7d:    row.SavedCents7d,
			Runs30d:         row.Runs30d,
			SavedUnits30d:   row.SavedUnits30d,
			SavedCents30d:   row.SavedCents30d,
		})
	}
	writeJSON(w, http.StatusOK, analyticsSummaryResponse{Savings: out})
}

// AnalyticsIssues returns the top issues for one saving.
func (h *Handler) AnalyticsIssues(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	savingKey := chi.URLParam(r, "key")
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	since := parseSinceQuery(r, 30)
	limit := parseLimitQuery(r, 20)

	rows, err := h.Queries.AnalyticsCostOptimizationTopIssues(r.Context(), cerebrodb.AnalyticsCostOptimizationTopIssuesParams{
		WorkspaceID: wsUUID,
		SavingKey:   savingKey,
		Since:       since,
		RowLimit:    limit,
	})
	if err != nil {
		slog.Error("analytics cost optimization issues failed", append(logger.RequestAttrs(r), "error", err, "saving_key", savingKey)...)
		writeError(w, http.StatusInternalServerError, "failed to load analytics issues")
		return
	}
	issues := make([]analyticsIssueRow, 0, len(rows))
	for _, row := range rows {
		issues = append(issues, analyticsIssueRow{
			IssueID:     util.UUIDToString(row.IssueID),
			IssueNumber: row.IssueNumber,
			IssueTitle:  row.IssueTitle,
			RunCount:    row.RunCount,
			SavedUnits:  row.SavedUnits,
			SavedCents:  row.SavedCents,
		})
	}
	writeJSON(w, http.StatusOK, analyticsIssuesResponse{Issues: issues})
}

// AnalyticsIssueRuns returns per-run measurements for one issue + saving.
func (h *Handler) AnalyticsIssueRuns(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	savingKey := chi.URLParam(r, "key")
	issueID := chi.URLParam(r, "issueId")
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return
	}
	since := parseSinceQuery(r, 90)
	limit := parseLimitQuery(r, 100)

	rows, err := h.Queries.AnalyticsCostOptimizationIssueRuns(r.Context(), cerebrodb.AnalyticsCostOptimizationIssueRunsParams{
		WorkspaceID: wsUUID,
		SavingKey:   savingKey,
		IssueID:     issueUUID,
		Since:       since,
		RowLimit:    limit,
	})
	if err != nil {
		slog.Error("analytics cost optimization runs failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load analytics runs")
		return
	}
	runs := make([]analyticsRunRow, 0, len(rows))
	for _, row := range rows {
		created := ""
		if row.CreatedAt.Valid {
			created = row.CreatedAt.Time.UTC().Format(time.RFC3339)
		}
		runs = append(runs, analyticsRunRow{
			TaskID:         util.UUIDToString(row.TaskID),
			CreatedAt:      created,
			Mode:           row.Mode,
			Applied:        row.Applied,
			HeldOut:        row.HeldOut,
			Metric:         row.Metric,
			BaselineValue:  row.BaselineValue,
			EffectiveValue: row.EffectiveValue,
			SavedUnits:     row.SavedUnits,
			SavedCents:     row.SavedCents,
			DetailJSON:     row.DetailJson,
		})
	}
	writeJSON(w, http.StatusOK, analyticsRunsResponse{Runs: runs})
}

func parseSinceQuery(r *http.Request, defaultDays int) pgtype.Timestamptz {
	days := defaultDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	return pgtype.Timestamptz{Time: since, Valid: true}
}

func parseLimitQuery(r *http.Request, defaultLimit int32) int32 {
	limit := defaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 200 {
			limit = int32(n)
		}
	}
	return limit
}
