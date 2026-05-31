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

// approxCharsPerToken is the FALLBACK chars→tokens divisor when the run gave us
// no usable calibration data. ~4 characters per token is the standard
// rule-of-thumb for English/code; the dashboard treats an uncalibrated
// context_tokens as an estimate, never a billing figure.
const approxCharsPerToken = 4

// promptTokensForRun is the denominator used to calibrate chars→tokens for a
// single run: the provider's reported prompt tokens. Cache-read and cache-write
// tokens are prompt tokens too (they correspond to characters that WERE sent in
// the prompt, just served from / written to the prompt cache), so they belong
// in the denominator alongside the fresh input tokens — otherwise a cache-heavy
// run would report far too few tokens per character. FIR-2572.
func promptTokensForRun(f CostSavingRunFacts) int64 {
	return f.Usage.InputTokens + f.Usage.CacheReadTokens + f.Usage.CacheWriteTokens
}

// savedContextTokens converts a number of pruned characters into a saved-token
// count. This is the per-run REAL measurement that works at 100% rollout
// (holdout 0, no control group): when the run reported both its total prompt
// characters AND the provider's own prompt-token count, we derive this run's
// actual chars-per-token ratio for its specific model/tokenizer and apply it to
// the pruned characters. That is more accurate than the chars/4 rule-of-thumb
// and more honest than a generic vocabulary, with zero new dependency. When no
// calibration data is available we fall back to approxCharsPerToken. It
// complements the holdout A/B, which remains available for partial rollouts.
func savedContextTokens(prunedChars int64, f CostSavingRunFacts) int64 {
	promptTokens := promptTokensForRun(f)
	if prunedChars > 0 && f.PromptInputChars > 0 && promptTokens > 0 {
		// savedTokens = prunedChars / (promptChars / promptTokens)
		//            = prunedChars * promptTokens / promptChars
		// rounded to nearest, computed as int64 to avoid float drift.
		return (prunedChars*promptTokens + f.PromptInputChars/2) / f.PromptInputChars
	}
	return prunedChars / approxCharsPerToken
}

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
	// the run transcript — the surface that prune_tool_results targets. Used as
	// the would-save estimate when the saving is in shadow mode.
	ToolResultChars int64
	// PrunedToolResultChars is the characters prune_tool_results ACTUALLY dropped
	// from the transcript this run (0 unless the saving was "on" and a round was
	// superseded). Used as the measured saving when the saving is applied.
	PrunedToolResultChars int64
	// PromptInputChars is the total prompt characters the run actually sent to the
	// model (summed across every request). Together with the provider-reported
	// prompt tokens (Usage input + cache tokens) it gives a REAL chars-per-token
	// ratio for this run's model, used to convert pruned characters into an
	// accurate saved-token count. 0 when not instrumented (then we fall back to
	// the approxCharsPerToken constant). FIR-2572.
	PromptInputChars int64
	// RoutingHeldOut is true when model_routing is "on" for the workspace but
	// this run was picked as the holdout control arm (FIR-2325 phase 5): the run
	// deliberately executed on the requested (expensive) model instead of the
	// cheap one, so the dashboard can compare its real cost against the treatment
	// runs that were routed. Only meaningful when model_routing is "on".
	RoutingHeldOut bool
	// PruneHeldOut is true when prune_tool_results is "on" for the workspace but
	// this run was deterministically picked as the holdout control arm: pruning
	// was deliberately NOT applied so its real cost can be compared against the
	// treatment runs that were pruned. Only meaningful when prune_tool_results
	// is "on".
	PruneHeldOut bool
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
		// Holdout governs the savings with a real runtime toggle and a control
		// arm: model_routing (the run stayed on the expensive model) and
		// prune_tool_results (pruning was withheld). The others have no
		// behavioral A/B arm yet, so an "on" run is always treatment for them.
		heldOut := mode == savingModeOn && ((key == savingModelRouting && f.RoutingHeldOut) || (key == savingPruneToolResults && f.PruneHeldOut))
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
			// When applied ("on"), the saving counts the chars we ACTUALLY pruned
			// this run; in shadow it estimates the full tool-result surface as the
			// would-save. Baseline = approx tokens dropped; effective = 0 once
			// pruned. The saved tokens are priced as input tokens on the run's
			// model so the dashboard shows a real dollar figure — never a guess.
			if f.ToolResultChars <= 0 {
				continue
			}
			savedChars := f.ToolResultChars
			if applied {
				savedChars = f.PrunedToolResultChars
			}
			// savedContextTokens calibrates chars→tokens from this run's own
			// provider-reported usage when available, so the saved-token count is
			// REAL at 100% rollout (no holdout needed); it falls back to the
			// chars/4 estimate when the run lacked calibration data. FIR-2572.
			savedTokens := savedContextTokens(savedChars, f)
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
