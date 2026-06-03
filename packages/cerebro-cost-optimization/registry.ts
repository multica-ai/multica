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
  | "prune_tool_results"
  | "context_duplication";

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
  | "context_tokens"
  | "tokens";

/**
 * Which agent runtime a saving actually affects. Savings live in two runtimes:
 *
 *   - "daemon"  — local agents that claim tasks from the server (the team's own
 *                 agents on a fixed plan). Server-side affordances apply here.
 *   - "gateway" — cloud agents the server runs and bills per token; the server
 *                 controls the model call and the context window.
 *   - "both"    — the saving applies in either runtime.
 *
 * model_routing and prune_tool_results only change cost in the gateway, where
 * the server owns the model choice and the context window. The settings UI
 * marks those so a daemon-only workspace sees the button is scoped to the cloud
 * runtime — not a dead toggle, and no false promise that it touches local
 * agents.
 */
export type CostSavingRuntimeScope = "daemon" | "gateway" | "both";

/**
 * Short badge text for a runtime-scoped saving, or null when the saving applies
 * in every runtime (no badge needed). A default branch keeps an unknown scope
 * from throwing — it just renders no badge.
 */
export function runtimeScopeBadge(scope: CostSavingRuntimeScope): string | null {
  switch (scope) {
    case "gateway":
      return "Cloud runtime only";
    case "daemon":
      return "Local runtime only";
    default:
      return null;
  }
}

/**
 * One-line explanation shown under a runtime-scoped saving so the scope is
 * honest about what it does and does not affect, or null when it applies
 * everywhere.
 */
export function runtimeScopeNote(scope: CostSavingRuntimeScope): string | null {
  switch (scope) {
    case "gateway":
      return "Only affects cloud (gateway) agents the server runs and bills per token. Local daemon agents run on a fixed plan, so this saving does not change their cost.";
    case "daemon":
      return "Only affects local daemon agents that claim tasks from the server.";
    default:
      return null;
  }
}

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
  context_duplication: "off",
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

/**
 * Default holdout share (percent of "on" runs deliberately withheld as the A/B
 * control arm) shown when a workspace has no override. MUST mirror the server's
 * `defaultCostSavingHoldoutPct` (server/internal/cerebro/runtime/cost_savings_holdout.go):
 * the server stores only overrides, so a missing row resolves to this default
 * for both display and the actual run. (FIR-2640)
 */
export const DEFAULT_COST_SAVING_HOLDOUT_PCT = 20;

/**
 * Clamp a holdout share to the valid 0-100 integer range. Non-finite input
 * falls back to the registry default. Used to sanitize both server responses
 * and user input before they reach the API.
 */
export function clampHoldoutPct(pct: number): number {
  if (!Number.isFinite(pct)) return DEFAULT_COST_SAVING_HOLDOUT_PCT;
  if (pct < 0) return 0;
  if (pct > 100) return 100;
  return Math.round(pct);
}

/**
 * Whether a holdout share represents full rollout — every "on" run gets the
 * saving, with no control arm held back. That is exactly holdout = 0%.
 */
export function isFullRollout(pct: number): boolean {
  return pct <= 0;
}

/**
 * Savings that actually have a runtime holdout arm, so a rollout/holdout control
 * does something for them. Only the gateway savings the server can withhold per
 * run qualify: model_routing (keep the run on the expensive model) and
 * prune_tool_results (skip pruning). The others have no behavioral control arm,
 * so showing them a holdout knob would be a dead control — we don't. MUST stay
 * in sync with the runtime's heldOut logic in cost_savings_measure.go.
 */
const HOLDOUT_SAVINGS: ReadonlySet<CostSavingKey> = new Set<CostSavingKey>([
  "model_routing",
  "prune_tool_results",
]);

/** Whether a saving supports a per-saving rollout/holdout control. */
export function savingSupportsHoldout(key: CostSavingKey): boolean {
  return HOLDOUT_SAVINGS.has(key);
}

export interface CostSavingDefinition {
  key: CostSavingKey;
  label: string;
  /** What the saving does and what it reduces. */
  description: string;
  /** The unit this saving reduces — see {@link CostSavingMetric}. */
  metric: CostSavingMetric;
  /** Which runtime this saving actually affects — see {@link CostSavingRuntimeScope}. */
  runtimeScope: CostSavingRuntimeScope;
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
    metric: "input_tokens",
    runtimeScope: "both",
    estimateNote: "Removes ~40% of platform calls per run.",
  },
  {
    key: "bundled_read",
    label: "Bundled context read",
    description:
      "Serve a single combined \"issue context\" call (issue + comments + members + labels) instead of 4-5 separate calls.",
    metric: "input_tokens",
    runtimeScope: "both",
    estimateNote: "Collapses 4-5 calls into 1 at run start.",
  },
  {
    key: "model_routing",
    label: "Model routing",
    description:
      "Use a cheaper model as the default and only escalate to an expensive model when the task requires it.",
    metric: "model_cost",
    runtimeScope: "gateway",
    estimateNote: "Cheaper model handles the majority of runs.",
  },
  {
    key: "prune_tool_results",
    label: "Prune stale tool results",
    description:
      "Drop outdated tool-call results from the context window mid-run so the agent does not keep paying for superseded output.",
    metric: "context_tokens",
    runtimeScope: "gateway",
    estimateNote: "Trims superseded tool output from later turns.",
  },
  {
    key: "context_duplication",
    label: "Context duplication",
    description:
      "Measure how much of the composed agent brief repeats the same sentences across sections (workspace context, skills list, agent instructions, and similar).",
    metric: "tokens",
    runtimeScope: "both",
    estimateNote: "Typical workspaces show 20–40% duplicate sentences before cleanup.",
  },
];
