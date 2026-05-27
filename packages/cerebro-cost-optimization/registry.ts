/**
 * Single source of truth for the cerebro fork's agent cost-optimization
 * savings (FIR-2325).
 *
 * Each saving is a behavior the agent runtime can apply to spend fewer tokens
 * or fewer platform calls per run. Unlike feature flags (boolean on/off), each
 * saving has THREE states so we can measure before we commit:
 *
 *   - "off"    — runs exactly as today. No measurement.
 *   - "shadow" — runs exactly as today, but additionally computes what the
 *                saving WOULD have saved. Zero behavior risk ("kun måling").
 *   - "on"     — the saving is active; we measure what it ACTUALLY saved
 *                against a baseline.
 *
 * Defaults are held in TypeScript (no migration when a new saving ships).
 * Server-side persistence stores only overrides — savings whose mode differs
 * from the default for a given workspace.
 */

export type CostSavingKey =
  | "snapshot_prompt"
  | "bundled_read"
  | "model_routing"
  | "prune_tool_results";

/** The three states every saving can be in. */
export type CostSavingMode = "off" | "shadow" | "on";

/**
 * The unit each saving reduces. Drives how the measurement layer attributes
 * a saving to a run and how the dashboard converts it to kroner.
 */
export type CostSavingMetric =
  | "platform_calls"
  | "input_tokens"
  | "model_cost"
  | "context_tokens";

/**
 * Default mode for each saving. Applied at read time when no override exists.
 *
 * Every saving defaults to "off" — opt-in, not opt-out. These savings change
 * agent behavior in production, so we never flip one on by default; a human
 * turns it to "shadow" to measure, then to "on" once the numbers justify it.
 */
export const COST_SAVING_DEFAULTS: Record<CostSavingKey, CostSavingMode> = {
  snapshot_prompt: "off",
  bundled_read: "off",
  model_routing: "off",
  prune_tool_results: "off",
};

/**
 * Whether a chosen mode equals the registry default for a saving. The settings
 * UI uses this to decide how to persist a change: when the chosen mode is the
 * default, the per-workspace override is cleared (DELETE) so only true
 * deviations are stored server-side; otherwise the mode is written (PUT).
 */
export function isDefaultMode(key: CostSavingKey, mode: CostSavingMode): boolean {
  return mode === COST_SAVING_DEFAULTS[key];
}

export interface CostSavingDefinition {
  key: CostSavingKey;
  label: string;
  /** What the saving does and what it reduces. */
  description: string;
  /** The unit this saving reduces — see {@link CostSavingMetric}. */
  metric: CostSavingMetric;
  /**
   * Rough estimate of the per-run reduction, used only as a sanity hint in the
   * UI before real measurement exists. Measurement always overrides this.
   */
  estimateNote: string;
}

/**
 * Display metadata for the settings UI. Order here is the order shown to
 * the user.
 */
export const COST_SAVINGS: CostSavingDefinition[] = [
  {
    key: "snapshot_prompt",
    label: "Snapshot in start prompt",
    description:
      "Put the issue and the latest comment thread directly into the run's start prompt, so the agent does not have to fetch the issue and its comments itself on every run.",
    metric: "platform_calls",
    estimateNote: "Removes ~40% of platform calls per run.",
  },
  {
    key: "bundled_read",
    label: "Bundled context read",
    description:
      "Serve a single combined \"issue context\" call (issue + comments + members + labels) instead of 4-5 separate calls.",
    metric: "platform_calls",
    estimateNote: "Collapses 4-5 calls into 1 at run start.",
  },
  {
    key: "model_routing",
    label: "Model routing",
    description:
      "Use a cheaper model as the default and only escalate to an expensive model when the task requires it.",
    metric: "model_cost",
    estimateNote: "Cheaper model handles the majority of runs.",
  },
  {
    key: "prune_tool_results",
    label: "Prune stale tool results",
    description:
      "Drop outdated tool-call results from the context window mid-run so the agent does not keep paying for superseded output.",
    metric: "context_tokens",
    estimateNote: "Trims superseded tool output from later turns.",
  },
];
