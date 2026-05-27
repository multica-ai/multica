package handler

// CEREBRO-PATCH(daemon-bundled-saving): claim-time half of the "bundled_read"
// cost saving (FIR-2384 step 2). The other half — the actual bundled endpoint
// and its real-usage measurement — lives in issue_context_cerebro.go.
//
// Three states, mapped to a tool-based saving:
//   - "off"    — start prompt steers the agent to the separate `multica issue
//                get` + `comment list` reads (today's behaviour). No measurement.
//   - "shadow" — behaviour UNCHANGED (separate reads). At claim time we record
//                the would-save: bundling these reads into one `issue context`
//                call would collapse bundledReadBaseline calls into 1. This is
//                the "Kun måling" arm — measured without changing behaviour.
//   - "on"     — the start prompt is flipped to point at `multica issue context`
//                (BundleContextHint). The real save is then recorded at the
//                endpoint when the agent actually calls it — not assumed here.
//
// snapshot_prompt takes precedence: when it already inlined the issue+thread
// into the prompt this run, the reads are gone and bundled_read defers entirely
// (no prompt change, no measurement) so the two savings never double-count.

import (
	"context"
	"log/slog"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	costSavingBundledKey = "bundled_read"

	// bundledReadBaseline is how many separate platform reads the single
	// `multica issue context` call replaces: issue get + comment list +
	// workspace members + issue labels. This mirrors the registry definition
	// ("Collapses 4-5 calls into 1") and the gateway's bundled_read baseline.
	// Effective is always 1 (the one bundled call).
	bundledReadBaseline = 4
)

// applyBundledReadSaving wires the bundled_read saving into a daemon task claim.
// Best-effort: any failure leaves the claim untouched so a measurement bug can
// never block real work. Must run AFTER applySnapshotSaving so it can see
// whether a snapshot was already inlined this run.
func (h *Handler) applyBundledReadSaving(ctx context.Context, resp *AgentTaskResponse, issue db.Issue, taskID pgtype.UUID) {
	if h.CerebroQueries == nil || !issue.WorkspaceID.Valid || !taskID.Valid {
		return
	}
	// snapshot_prompt already removed these reads this run — defer so the two
	// savings never both claim the same calls and the prompt stays coherent.
	if resp.IssueSnapshot != "" {
		return
	}

	mode := h.bundledReadMode(ctx, issue.WorkspaceID)
	switch mode {
	case costSavingModeOn:
		// Flip the start prompt to the single bundled call. The real save is
		// recorded at the endpoint (recordBundledReadUsage) when the agent uses
		// it — we do not assume compliance here.
		resp.BundleContextHint = true
	case costSavingModeShadow:
		// Behaviour unchanged; record the hypothetical would-save for this run.
		if err := h.CerebroQueries.RecordCerebroCostOptimizationMeasurement(ctx, bundledReadMeasurementParams(issue.WorkspaceID, taskID, costSavingModeShadow)); err != nil {
			slog.Warn("bundled-read cost-saving: record would-save failed", "error", err)
		}
	}
}

// bundledReadMeasurementParams builds the measurement row for one run. Baseline
// is the separate reads the single bundled call replaces; effective is 1 (that
// one call). "on" is an applied (real) save, "shadow" a would-save. The metric
// is platform_calls, not dollars: daemon agents bill on a flat subscription, so
// the honest unit is calls saved. Used by both the claim-time shadow path and
// the endpoint's real-usage path so the two arms stay identically shaped.
func bundledReadMeasurementParams(workspaceID, taskID pgtype.UUID, mode string) cerebrodb.RecordCerebroCostOptimizationMeasurementParams {
	return cerebrodb.RecordCerebroCostOptimizationMeasurementParams{
		WorkspaceID:     workspaceID,
		TaskID:          taskID,
		SavingKey:       costSavingBundledKey,
		Mode:            mode,
		Applied:         mode == costSavingModeOn,
		HeldOut:         false,
		Metric:          costMetricPlatformCalls,
		BaselineValue:   bundledReadBaseline,
		EffectiveValue:  1,
		SavedCents:      0,
		ActualCostCents: 0,
	}
}

// shouldCreditBundledReadUsage decides whether a real `multica issue context`
// call this run earns a bundled_read measurement row. It credits only when
// bundled_read is committed "on" AND snapshot_prompt is not "on": snapshot has
// precedence — when it is on the claim handler already inlined and credited the
// issue+thread reads, and bundled_read deferred — so crediting bundled_read too
// would double-count the overlapping reads. This mirrors applyBundledReadSaving's
// claim-time precedence (it returns early when a snapshot was inlined) at the
// measurement layer, so the no-double-count guarantee holds regardless of whether
// the agent ignores the prompt and calls `issue context` during a snapshot run.
func shouldCreditBundledReadUsage(bundledMode, snapshotMode string) bool {
	return bundledMode == costSavingModeOn && snapshotMode != costSavingModeOn
}

// bundledReadMode returns the workspace's mode for the bundled_read saving
// ("on"/"shadow"), or "" when off/absent or on lookup error.
func (h *Handler) bundledReadMode(ctx context.Context, workspaceID pgtype.UUID) string {
	rows, err := h.CerebroQueries.ListCerebroCostOptimization(ctx, workspaceID)
	if err != nil {
		slog.Warn("bundled-read cost-saving: list modes failed", "error", err)
		return ""
	}
	for _, row := range rows {
		if row.SavingKey == costSavingBundledKey {
			return row.Mode
		}
	}
	return ""
}
