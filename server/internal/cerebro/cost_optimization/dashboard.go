// CEREBRO: cost-optimization dashboard handler (FIR-2325 phase 5) kept in a
// dedicated file so upstream-merges don't conflict.
package cost_optimization

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
)

// dashboardResponse is the GET /cost-optimization/dashboard shape: one entry per
// saving that has at least one measurement. Savings with no measurements are
// omitted (the frontend renders them from the registry as "no data yet").
type dashboardResponse struct {
	Savings []dashboardSaving `json:"savings"`
}

// dashboardSaving carries the numbers the dashboard shows per saving plus the
// run counts behind them. All money is US cents; the frontend divides by 100 for
// dollars. There are three saving figures, by how the run treated the saving:
//   - Estimated: the hypothetical would-save on runs where the saving was NOT
//     applied (shadow / measure-only runs and the held-out control arm).
//   - Applied: the REAL saving accrued on runs where the saving WAS applied
//     (mode=on, not held out). Calibrated per run from the run's own usage, so it
//     is a measured figure even at 100% rollout with no control arm. nil when no
//     applied runs yet (FIR-2572).
//   - Measured: the holdout A/B (control vs treatment avg cost); the most
//     rigorous figure, but it needs both arms and so is nil at 100% rollout.
type dashboardSaving struct {
	SavingKey string `json:"saving_key"`
	Metric    string `json:"metric"`

	ShadowRunCount    int64 `json:"shadow_run_count"`
	TreatmentRunCount int64 `json:"treatment_run_count"`
	ControlRunCount   int64 `json:"control_run_count"`

	// EstimatedSavedUnits is the hypothetical would-save (shadow + held-out
	// control runs) summed in the saving's native metric unit (platform calls,
	// context tokens, or cents for model_cost). EstimatedSavedCents is the same in
	// money, populated only where the metric is priced (model_cost, and context
	// tokens priced at the run's model rate); 0 for unpriced metrics like
	// platform_calls.
	EstimatedSavedUnits int64 `json:"estimated_saved_units"`
	EstimatedSavedCents int64 `json:"estimated_saved_cents"`

	// Applied is the real saving accrued on runs where the saving was applied
	// (mode=on, not held out). It is the number that fills the dashboard's
	// measured column at 100% rollout when there is no holdout A/B. nil when no
	// applied runs yet. FIR-2572.
	Applied *dashboardApplied `json:"applied"`

	// Measured is the trustworthy A/B saving from the random holdout: the control
	// arm (saving withheld) vs. the treatment arm (saving applied). nil when no
	// holdout comparison is available yet.
	Measured *dashboardMeasured `json:"measured"`
}

// dashboardApplied is the real per-run saving summed over the applied (treatment)
// runs of an "on" saving. SavedUnits is in the metric's native unit; SavedCents
// is the priced figure (0 for unpriced metrics like platform_calls). FIR-2572.
type dashboardApplied struct {
	SavedUnits int64 `json:"saved_units"`
	SavedCents int64 `json:"saved_cents"`
	RunCount   int64 `json:"run_count"`
}

// dashboardMeasured is the holdout A/B result for one saving, all in US cents.
type dashboardMeasured struct {
	TreatmentAvgCostCents int64 `json:"treatment_avg_cost_cents"`
	ControlAvgCostCents   int64 `json:"control_avg_cost_cents"`
	// SavedPerRunCents is control avg − treatment avg: the measured cost a run
	// avoids by applying the saving. Can be negative if the saving cost more.
	SavedPerRunCents int64 `json:"saved_per_run_cents"`
	// TotalSavedCents projects SavedPerRunCents across the treatment runs.
	TotalSavedCents int64 `json:"total_saved_cents"`
}

// savingAccumulator folds the per-arm rows of one saving into the dashboard
// numbers. Arms: shadow (mode=shadow), treatment (mode=on, not held out),
// control (mode=on, held out).
type savingAccumulator struct {
	metric           string
	estimatedUnits   int64 // shadow + held-out control: hypothetical would-save
	estimatedCents   int64
	appliedUnits     int64 // treatment (on, not held out): real accrued saving
	appliedCents     int64
	shadowRuns       int64
	treatmentRuns    int64
	treatmentCostSum int64
	controlRuns      int64
	controlCostSum   int64
}

// Dashboard returns the per-saving estimated-vs-measured rollup for a workspace.
// Member-level read (same audience as List): the numbers are workspace-wide, not
// per-user. Defaults and labels live on the frontend; this only returns measured
// data, so a saving with no runs is simply absent from the response.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace id required")
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	rows, err := h.Queries.DashboardCerebroCostOptimization(r.Context(), wsUUID)
	if err != nil {
		slog.Error("dashboard cerebro cost optimization failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load cost optimization dashboard")
		return
	}

	writeJSON(w, http.StatusOK, dashboardResponse{Savings: aggregateDashboard(rows)})
}

// aggregateDashboard folds the per-arm DB rows into the per-saving dashboard
// numbers. Pure (no request/DB), so the estimated-vs-measured and holdout A/B
// math is unit-tested directly.
func aggregateDashboard(rows []cerebrodb.DashboardCerebroCostOptimizationRow) []dashboardSaving {
	byKey := make(map[string]*savingAccumulator)
	for _, row := range rows {
		acc := byKey[row.SavingKey]
		if acc == nil {
			acc = &savingAccumulator{metric: row.Metric}
			byKey[row.SavingKey] = acc
		}
		switch {
		case row.Mode == "on" && row.HeldOut:
			// Held-out control: the saving was withheld, so its baseline−effective
			// is a hypothetical would-save → estimated, like shadow.
			acc.controlRuns += row.RunCount
			acc.controlCostSum += row.TotalActualCostCents
			acc.estimatedUnits += row.TotalSavedUnits
			acc.estimatedCents += row.TotalSavedCents
		case row.Mode == "on":
			// Treatment: the saving was applied, so baseline−effective is the real
			// saving this run accrued → applied (works at 100% with no control arm).
			acc.treatmentRuns += row.RunCount
			acc.treatmentCostSum += row.TotalActualCostCents
			acc.appliedUnits += row.TotalSavedUnits
			acc.appliedCents += row.TotalSavedCents
		default: // shadow / measure-only: hypothetical would-save → estimated.
			acc.shadowRuns += row.RunCount
			acc.estimatedUnits += row.TotalSavedUnits
			acc.estimatedCents += row.TotalSavedCents
		}
	}

	savings := make([]dashboardSaving, 0, len(byKey))
	for key, acc := range byKey {
		savings = append(savings, acc.toResponse(key))
	}
	// Stable output order so the dashboard doesn't reshuffle between polls.
	sortSavingsByKey(savings)
	return savings
}

// toResponse builds the API shape for one saving, computing the holdout A/B only
// when both arms have runs and the metric is model_cost or context_tokens (the
// metrics whose actual run cost reflects the saving — see
// DashboardCerebroCostOptimization). Both now produce held-out control runs, so
// the A/B is computed identically for either from actual_cost_cents.
func (a *savingAccumulator) toResponse(key string) dashboardSaving {
	s := dashboardSaving{
		SavingKey:           key,
		Metric:              a.metric,
		ShadowRunCount:      a.shadowRuns,
		TreatmentRunCount:   a.treatmentRuns,
		ControlRunCount:     a.controlRuns,
		EstimatedSavedUnits: a.estimatedUnits,
		EstimatedSavedCents: a.estimatedCents,
	}
	// The real saving accrued where the saving was applied. Present whenever there
	// is at least one treatment run, so the dashboard shows a measured number at
	// 100% rollout even though no holdout A/B exists.
	if a.treatmentRuns > 0 {
		s.Applied = &dashboardApplied{
			SavedUnits: a.appliedUnits,
			SavedCents: a.appliedCents,
			RunCount:   a.treatmentRuns,
		}
	}
	if (a.metric == "model_cost" || a.metric == "context_tokens") && a.treatmentRuns > 0 && a.controlRuns > 0 {
		treatmentAvg := a.treatmentCostSum / a.treatmentRuns
		controlAvg := a.controlCostSum / a.controlRuns
		perRun := controlAvg - treatmentAvg
		s.Measured = &dashboardMeasured{
			TreatmentAvgCostCents: treatmentAvg,
			ControlAvgCostCents:   controlAvg,
			SavedPerRunCents:      perRun,
			TotalSavedCents:       perRun * a.treatmentRuns,
		}
	}
	return s
}

// sortSavingsByKey orders savings by saving_key. Small N (<= number of savings),
// so an insertion sort keeps the dependency surface tiny.
func sortSavingsByKey(s []dashboardSaving) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].SavingKey > s[j].SavingKey; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
