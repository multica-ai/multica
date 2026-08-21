import type {
  AutopilotQuotaUsage,
  WorkspaceSubscriptionEntitlements,
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
 * visible so the progress bar matches the server's blocking decision. A
 * complete metered response is authoritative independently of entitlement
 * state; only the unlimited fallback comes from the entitlement response.
 */
export function resolveAutopilotUsage(
  entitlements: WorkspaceSubscriptionEntitlements,
  usage: AutopilotQuotaUsage | undefined,
  failed: boolean,
  allowEntitlementUnlimited: boolean,
): AutopilotUsageView {
  if (!failed && usage !== undefined && usage.action !== "off") {
    const { used, reserved, limit, reset_at: resetAt } = usage;
    if (
      used !== null &&
      reserved !== null &&
      limit !== null &&
      resetAt !== null &&
      used >= 0 &&
      reserved >= 0 &&
      limit >= 0 &&
      Number.isFinite(used) &&
      Number.isFinite(reserved) &&
      Number.isFinite(limit)
    ) {
      const total = used + reserved;
      const reached = total >= limit;
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

  if (
    allowEntitlementUnlimited &&
    entitlements.plan === "pro" &&
    entitlements.autopilotRuns === null
  ) {
    return { kind: "unlimited" };
  }

  return { kind: "unavailable" };
}

const MANAGED_SUBSCRIPTION_STATUSES = new Set([
  "active",
  "trialing",
  "past_due",
  "canceled",
  "incomplete",
  "incomplete_expired",
  "paused",
  "unpaid",
]);

const PURCHASABLE_SUBSCRIPTION_STATUSES = new Set([
  "inactive",
  "canceled",
  "incomplete_expired",
]);

/**
 * Summary facts are primary. Plan and status are compatibility fallbacks so
 * an older or temporarily unavailable summary does not hide the recovery UI.
 */
export function hasManagedWorkspaceSubscription(
  entitlements: WorkspaceSubscriptionEntitlements,
  summary: WorkspaceSubscriptionSummary | null | undefined,
): boolean {
  return (
    summary?.hasStripeCustomer === true ||
    summary?.billedSeats !== null && summary?.billedSeats !== undefined ||
    entitlements.plan === "pro" ||
    MANAGED_SUBSCRIPTION_STATUSES.has(entitlements.status)
  );
}

/**
 * Checkout creates a new subscription. Cloud accepts an existing record only
 * after cancellation or incomplete setup expiry; every other known status may
 * still represent a live or recoverable subscription and stays in Portal.
 */
export function canPurchaseWorkspaceSubscription(
  entitlements: WorkspaceSubscriptionEntitlements,
): boolean {
  return (
    entitlements.plan === "free" &&
    PURCHASABLE_SUBSCRIPTION_STATUSES.has(entitlements.status)
  );
}
