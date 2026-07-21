/**
 * Public types for the @multica/core/feature-flags module.
 *
 * The shape mirrors the Go-side server/pkg/featureflag package on purpose so
 * a Decision returned by the backend can be marshalled directly into the
 * frontend Service without translation. Keep them in sync when extending
 * either side.
 */

/**
 * Reason explains why a Decision returned the value it did. Exposed in
 * diagnostics endpoints and in development overlays so engineers can tell
 * "this flag is on because the user is in the allowlist" apart from "this
 * flag is on because the default kicked in".
 */
export type Reason =
  | "static"
  | "percent"
  | "override"
  | "default"
  | "error";

/**
 * Structured outcome of a single flag evaluation. Most callers only need
 * the {@link FeatureFlagService.isEnabled} convenience, but tests and
 * dev tools want the full record.
 */
export interface Decision {
  /** The flag identifier that was evaluated. */
  key: string;
  /** Boolean projection. True for any variant except "off" / "" / "false" / "0". */
  enabled: boolean;
  /** Raw variant value. Boolean flags use "on" / "off"; variant flags use arbitrary identifiers. */
  variant: string;
  /** Why this decision was made. */
  reason: Reason;
  /** Name of the provider that produced the decision, or "default" when nothing matched. */
  source: string;
}

/**
 * Per-evaluation context for dynamic targeting (allow/deny lists, percent
 * rollouts). All fields are optional; a missing field never crashes the
 * evaluation, it simply skips the rules that depend on it.
 */
export interface EvalContext {
  userId?: string;
  workspaceId?: string;
  /** Free-form attributes (plan, country, client, ...). Keys are case-sensitive. */
  attributes?: Readonly<Record<string, string>>;
}

/**
 * Percent rollout descriptor. The bucket for (key, identifier) is computed
 * with FNV-1a so the same identifier always falls into the same bucket
 * across processes and tabs.
 */
export interface PercentRollout {
  /** Rollout size in [0, 100]. Out-of-range values are clamped. */
  percent: number;
  /**
   * Attribute name used as the bucketing identifier. Defaults to "user_id".
   * Use "workspace_id" for workspace-scoped rollouts.
   */
  by?: string;
}

/**
 * Rule describes how the {@link StaticProvider} evaluates a single flag.
 *
 * Evaluation order (first match wins):
 *   1. Deny:    if the EvalContext attribute matches an entry in deny, return OFF.
 *   2. Allow:   if it matches an entry in allow, return ON.
 *   3. Percent: if the bucket falls inside percent.percent, return ON; else OFF.
 *   4. Default: return defaultValue.
 */
export interface Rule {
  /** Value returned when no targeting rule matches. Defaults to false. */
  default?: boolean;
  /**
   * Variant identifier returned WHEN the rule evaluates to enabled=true.
   * Use for multi-arm experiments (e.g. "experiment-v2"). When the rule
   * evaluates to enabled=false the Decision's variant is always "off",
   * so callers branching on `Variant()` cannot accidentally enter the
   * experiment arm for users that did not roll in.
   */
  variant?: string;
  /** Identifier values that force the flag ON. */
  allow?: ReadonlyArray<string>;
  /** EvalContext attribute used for allow lookups. Defaults to "user_id". */
  allowBy?: string;
  /** Identifier values that force the flag OFF. Deny wins over allow. */
  deny?: ReadonlyArray<string>;
  /** EvalContext attribute used for deny lookups. Defaults to "user_id". */
  denyBy?: string;
  /** Deterministic percent rollout. */
  percent?: PercentRollout;
}

/**
 * Provider is the configuration backend for the Service. Implementations
 * MUST be safe for concurrent use; the Service reads providers from many
 * components without additional synchronization.
 *
 * Returning `undefined` (instead of a Decision) tells the Service to fall
 * through to the next provider in a ChainProvider, or to the caller's
 * default if there is no next provider.
 */
export interface Provider {
  /** Stable, human-readable identifier surfaced in Decision.source. */
  readonly name: string;
  /** Evaluate the flag, or return undefined if this provider does not know it. */
  lookup(key: string, ctx: EvalContext): Decision | undefined;
}
