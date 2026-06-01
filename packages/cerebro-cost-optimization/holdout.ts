/**
 * Types + a defensive parser for the per-saving holdout-share settings
 * (GET /api/workspaces/{id}/cost-optimization/holdout, FIR-2640).
 *
 * The holdout share is the percent of a saving's "on" runs deliberately withheld
 * as the A/B control arm. Each saving has its own share, so an admin can put one
 * saving at full rollout (holdout 0) and another at, say, 30% independently. The
 * server stores ONLY overrides and returns a flat map of saving_key -> percent;
 * a saving missing from the map has no override and uses the server default.
 *
 * The client returns the body as `unknown` (api.getCostOptimizationHoldout), so
 * the contract is validated here, at the boundary, per the repo's API Response
 * Compatibility rule: a drifted or malformed shape degrades to "no overrides"
 * instead of throwing into the settings page.
 */

import { clampHoldoutPct, type CostSavingKey, COST_SAVING_DEFAULTS } from "./registry";

/** Per-saving holdout overrides; a key absent here uses the server default. */
export type HoldoutOverrides = Partial<Record<CostSavingKey, number>>;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/**
 * Validate the holdout response. Only known saving keys with a finite numeric
 * percent survive (clamped to 0-100); unknown keys, null, wrong types and any
 * unexpected shape are dropped so a contract drift never white-screens the page.
 */
export function parseHoldoutResponse(raw: unknown): HoldoutOverrides {
  const out: HoldoutOverrides = {};
  if (!isRecord(raw) || !isRecord(raw.overrides)) return out;
  for (const key of Object.keys(COST_SAVING_DEFAULTS) as CostSavingKey[]) {
    const value = raw.overrides[key];
    if (typeof value === "number" && Number.isFinite(value)) {
      out[key] = clampHoldoutPct(value);
    }
  }
  return out;
}
