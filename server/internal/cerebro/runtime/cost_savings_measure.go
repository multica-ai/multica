package runtime

import (
	"context"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/pricing"

	"github.com/jackc/pgx/v5/pgtype"
)

// Cost-saving keys. These mirror CostSavingKey in
// packages/cerebro-cost-optimization/registry.ts, which is the single source of
// truth for the UI. The runtime only needs the keys (not their default modes):
// the default for every saving is "off", and "off" is represented by the
// ABSENCE of an override row, so a missing row already resolves to "off" without
// the runtime duplicating the defaults table.
const (
	savingSnapshotPrompt   = "snapshot_prompt"
	savingBundledRead      = "bundled_read"
	savingModelRouting     = "model_routing"
	savingPruneToolResults = "prune_tool_results"
)

// Saving modes (CostSavingMode in the registry). "off" is implicit (no row).
const (
	savingModeShadow = "shadow"
	savingModeOn     = "on"
)

// Metric units (CostSavingMetric in the registry).
const (
	metricPlatformCalls = "platform_calls"
	metricModelCost     = "model_cost"
	metricContextTokens = "context_tokens"
)

// approxTokensPerChar converts a character count to an approximate token count.
// ~4 characters per token is the standard rule-of-thumb for English/code; the
// dashboard treats context_tokens as an estimate, never a billing figure.
const approxCharsPerToken = 4

// CostSavingRunFacts is everything measured about a single completed run that a
// cost saving can be scored against. All fields are real values observed during
// the run — nothing here is a fixed guess.
type CostSavingRunFacts struct {
	// RequestedModel is the model the agent asked for (the expensive baseline
	// when model routing is in play). EffectiveModel is the model the run
	// actually used — equal to RequestedModel unless routing was applied.
	RequestedModel string
	EffectiveModel string
	Usage          pricing.Usage
	// ActualCostCents is the measured cost of the run as it actually executed
	// (with EffectiveModel).
	ActualCostCents int64
	// InlinedContextReads is how many platform reads (issue, comments, trigger
	// comment, chat history) the server inlined into the start prompt this run —
	// i.e. reads a daemon agent would otherwise have made itself.
	InlinedContextReads int
	// ToolResultChars is the total characters of tool-result content carried in
	// the run transcript — the surface that prune_tool_results targets.
	ToolResultChars int64
	// RoutingHeldOut is true when model_routing is "on" for the workspace but
	// this run was picked as the holdout control arm (FIR-2325 phase 5): the run
	// deliberately executed on the requested (expensive) model instead of the
	// cheap one, so the dashboard can compare its real cost against the treatment
	// runs that were routed. Only meaningful when model_routing is "on".
	RoutingHeldOut bool
}

// CostSavingMeasurement is one scored saving for one run, ready to persist.
type CostSavingMeasurement struct {
	SavingKey string
	Mode      string
	// Applied is true when the saving actually changed this run's behavior — i.e.
	// "on" and not held out. Shadow and held-out control runs are not applied.
	Applied bool
	// HeldOut is true when this run is the control arm of an "on" saving (the
	// saving was deliberately withheld so it can be compared against applied
	// runs). Shadow rows are never held out.
	HeldOut    bool
	Metric     string
	Baseline   int64
	Effective  int64
	SavedCents int64
	// ActualCostCents is the run's real billed model cost, stored on every row so
	// the dashboard can average treatment vs. control actual cost for the holdout
	// A/B comparison.
	ActualCostCents int64
}

// completionCostCents returns the measured cost of a completed run. It prefers
// the gateway-reported cents and falls back to pricing the usage — the same
// rule recordTaskUsage uses for budget rollups, so the measurement baseline
// matches what the run was actually billed.
func completionCostCents(c GatewayCompletion) int64 {
	if c.Usage.CostCents > 0 {
		return c.Usage.CostCents
	}
	return pricing.ComputeCents(c.Model, pricing.Usage{
		InputTokens:      c.Usage.InputTokens,
		OutputTokens:     c.Usage.OutputTokens,
		CacheReadTokens:  c.Usage.CacheReadTokens,
		CacheWriteTokens: c.Usage.CacheWriteTokens,
	})
}

// effectiveModelForRun returns the model a run should use given the workspace's
// saving modes. When model_routing is "on" and a cheap model is configured, the
// run is routed to the cheap model; otherwise the requested model is unchanged.
// Default (no override / no cheap model configured) is a no-op, so the fleet is
// unaffected unless an admin explicitly turns routing on.
func effectiveModelForRun(modes map[string]string, requestedModel, cheapModel string) string {
	if modes[savingModelRouting] == savingModeOn && cheapModel != "" {
		return cheapModel
	}
	return requestedModel
}

// measureRun scores every non-off saving for one run. A saving in "off" mode
// (no entry in modes) produces no measurement. model_routing is skipped when no
// cheap model is configured, since there is then no real alternative cost to
// compare against — we never fabricate a saving.
func measureRun(modes map[string]string, f CostSavingRunFacts, cheapModel string) []CostSavingMeasurement {
	out := make([]CostSavingMeasurement, 0, len(modes))

	for _, key := range []string{savingSnapshotPrompt, savingBundledRead, savingModelRouting, savingPruneToolResults} {
		mode := modes[key]
		if mode != savingModeShadow && mode != savingModeOn {
			continue
		}
		// Holdout only governs model_routing — the one saving with a real runtime
		// toggle. The others have no behavioral A/B arm yet, so an "on" run is
		// always treatment for them.
		heldOut := key == savingModelRouting && mode == savingModeOn && f.RoutingHeldOut
		applied := mode == savingModeOn && !heldOut

		// base stamps the fields common to every measurement so each case below
		// only has to fill the metric and baseline/effective values.
		base := CostSavingMeasurement{
			SavingKey: key, Mode: mode, Applied: applied, HeldOut: heldOut,
			ActualCostCents: f.ActualCostCents,
		}

		switch key {
		case savingSnapshotPrompt:
			// Putting the issue + thread in the start prompt removes the per-run
			// reads the agent would otherwise make. Baseline = those reads;
			// effective = 0 (none left to make).
			if f.InlinedContextReads <= 0 {
				continue
			}
			m := base
			m.Metric, m.Baseline, m.Effective = metricPlatformCalls, int64(f.InlinedContextReads), 0
			out = append(out, m)

		case savingBundledRead:
			// Bundling collapses the separate context reads into a single call.
			// Baseline = those reads; effective = 1 (the one bundled call).
			if f.InlinedContextReads <= 1 {
				continue
			}
			m := base
			m.Metric, m.Baseline, m.Effective = metricPlatformCalls, int64(f.InlinedContextReads), 1
			out = append(out, m)

		case savingModelRouting:
			if cheapModel == "" {
				continue
			}
			expensiveCents := pricing.ComputeCents(f.RequestedModel, f.Usage)
			cheapCents := pricing.ComputeCents(cheapModel, f.Usage)
			m := base
			m.Metric = metricModelCost
			if applied {
				// Routing was applied: the run already ran on the cheap model.
				// Baseline is what the expensive model would have cost this usage;
				// effective is the measured actual cost.
				m.Baseline, m.Effective = expensiveCents, f.ActualCostCents
			} else {
				// Shadow or held-out control: the run ran on the expensive model;
				// effective is the hypothetical cheap-model cost on the same usage.
				m.Baseline, m.Effective = f.ActualCostCents, cheapCents
			}
			m.SavedCents = m.Baseline - m.Effective
			out = append(out, m)

		case savingPruneToolResults:
			// Pruning drops superseded tool-call output from the context window.
			// Baseline = approx tokens of the tool-result surface; effective = 0
			// once pruned. The saved tokens are priced as input tokens on the run's
			// model so the dashboard can show a real dollar figure — never a guess.
			if f.ToolResultChars <= 0 {
				continue
			}
			savedTokens := f.ToolResultChars / approxCharsPerToken
			m := base
			m.Metric, m.Baseline, m.Effective = metricContextTokens, savedTokens, 0
			m.SavedCents = tokenSavingCents(f.EffectiveModel, f.RequestedModel, savedTokens)
			out = append(out, m)
		}
	}
	return out
}

// tokenSavingCents prices a number of saved context tokens as input tokens on
// the run's model. Prefers the effective (actually-used) model, falls back to
// the requested model. Returns 0 when neither model is known to the pricing
// table, so an unknown model never produces a fabricated dollar figure.
func tokenSavingCents(effectiveModel, requestedModel string, savedTokens int64) int64 {
	if savedTokens <= 0 {
		return 0
	}
	model := effectiveModel
	if model == "" {
		model = requestedModel
	}
	if model == "" || !pricing.Known(model) {
		return 0
	}
	return pricing.ComputeCents(model, pricing.Usage{InputTokens: savedTokens})
}

// loadCostSavingModes reads the per-workspace saving-mode overrides. A saving
// with no row is "off" and simply absent from the returned map.
func (e *FirtalGatewayExecutor) loadCostSavingModes(ctx context.Context, workspaceID pgtype.UUID) map[string]string {
	if e.cerebro == nil || !workspaceID.Valid {
		return nil
	}
	rows, err := e.cerebro.ListCerebroCostOptimization(ctx, workspaceID)
	if err != nil {
		e.logger.Warn("firtal gateway cost-optimization mode load failed",
			"workspace_id", util.UUIDToString(workspaceID), "error", err)
		return nil
	}
	modes := make(map[string]string, len(rows))
	for _, row := range rows {
		modes[row.SavingKey] = row.Mode
	}
	return modes
}

// recordCostSavingMeasurements scores the run against the workspace's saving
// modes and persists one row per active saving. Best-effort: a failure here
// never affects the run's own completion.
func (e *FirtalGatewayExecutor) recordCostSavingMeasurements(ctx context.Context, workspaceID, taskID pgtype.UUID, modes map[string]string, facts CostSavingRunFacts) {
	if e.cerebro == nil || !workspaceID.Valid || !taskID.Valid || len(modes) == 0 {
		return
	}
	for _, m := range measureRun(modes, facts, e.cfg.CheapModel) {
		if err := e.cerebro.RecordCerebroCostOptimizationMeasurement(ctx, cerebrodb.RecordCerebroCostOptimizationMeasurementParams{
			WorkspaceID:     workspaceID,
			TaskID:          taskID,
			SavingKey:       m.SavingKey,
			Mode:            m.Mode,
			Applied:         m.Applied,
			HeldOut:         m.HeldOut,
			Metric:          m.Metric,
			BaselineValue:   m.Baseline,
			EffectiveValue:  m.Effective,
			SavedCents:      m.SavedCents,
			ActualCostCents: m.ActualCostCents,
		}); err != nil {
			e.logger.Warn("firtal gateway cost-optimization measurement record failed",
				"workspace_id", util.UUIDToString(workspaceID),
				"task_id", util.UUIDToString(taskID),
				"saving_key", m.SavingKey,
				"error", err)
		}
	}
}
