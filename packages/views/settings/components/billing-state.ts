import type {
  AutopilotQuotaUsage,
  WorkspaceSubscriptionEntitlements,
  WorkspaceSubscriptionPaidPlan,
  WorkspaceSubscriptionPrice,
  WorkspaceSubscriptionSummary,
} from "@multica/core/types";

export type AutopilotUsageView =
  | { kind: "unlimited" }
  | { kind: "unavailable" }
  | {
      kind: "metered";
      used: number;
      reserved: number;
      total: number;
      limit: number;
      progress: number;
      reached: boolean;
      resetAt: string;
    };

/**
 * Quota admission counts completed and reserved runs. Keep reserved work
 * visible so the progress bar matches the server's blocking decision for a
 * limited workspace. Limit mode and the reached decision are server facts;
 * this helper only prepares presentation values.
 */
export function resolveAutopilotUsage(
  entitlements: WorkspaceSubscriptionEntitlements,
  usage: AutopilotQuotaUsage | undefined,
  failed: boolean,
): AutopilotUsageView {
  if (entitlements.limits.autopilotRuns.mode === "unlimited") {
    return { kind: "unlimited" };
  }

  if (!failed && usage !== undefined && usage.action !== "off") {
    const { used, reserved, total, limit, reached, reset_at: resetAt } = usage;
    if (
      used !== null &&
      reserved !== null &&
      total !== null &&
      limit !== null &&
      reached !== null &&
      resetAt !== null &&
      used >= 0 &&
      reserved >= 0 &&
      limit >= 0 &&
      Number.isFinite(used) &&
      Number.isFinite(reserved) &&
      Number.isFinite(total) &&
      Number.isFinite(limit)
    ) {
      const progress =
        limit === 0
          ? 100
          : Math.min(100, Math.max(0, (total / limit) * 100));

      return {
        kind: "metered",
        used,
        reserved,
        total,
        limit,
        progress,
        reached,
        resetAt,
      };
    }
  }

  return { kind: "unavailable" };
}

export function hasActiveWorkspaceSeatCapacity(
  summary: WorkspaceSubscriptionSummary | null | undefined,
): boolean {
  return summary?.seatCapacity != null;
}

/**
 * The plan an active subscription can switch to, or null when Cloud does not
 * offer a switch. Cloud's `changePlan` action is the gate; the current plan
 * only decides the direction, so an unknown plan never offers a switch.
 */
export function resolvePlanChangeTarget(
  summary: WorkspaceSubscriptionSummary | null | undefined,
): WorkspaceSubscriptionPaidPlan | null {
  if (summary?.availableActions.changePlan !== true) return null;
  switch (summary.entitlement.plan) {
    case "pro":
      return "business";
    case "business":
      return "pro";
    default:
      return null;
  }
}

/**
 * A yearly per-seat price expressed per month, in minor units. Returns null
 * when the yearly amount does not divide evenly into twelve minor-unit
 * months, so the caller shows the yearly amount instead of a rounded one.
 */
export function yearlyPriceMonthlyEquivalent(
  price: WorkspaceSubscriptionPrice,
): number | null {
  if (price.interval !== "year" || price.intervalCount !== 1) return null;
  return price.unitAmount % 12 === 0 ? price.unitAmount / 12 : null;
}
