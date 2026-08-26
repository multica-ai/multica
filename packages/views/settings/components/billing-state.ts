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
 * visible so the progress bar matches the server's blocking decision for a
 * limited workspace. A trusted Pro entitlement is authoritative for plan
 * limits because the server enforcement policy may lag a subscription change.
 */
export function resolveAutopilotUsage(
  entitlements: WorkspaceSubscriptionEntitlements,
  usage: AutopilotQuotaUsage | undefined,
  failed: boolean,
  allowEntitlementUnlimited: boolean,
): AutopilotUsageView {
  if (
    allowEntitlementUnlimited &&
    entitlements.plan === "pro" &&
    entitlements.autopilotRuns === null
  ) {
    return { kind: "unlimited" };
  }

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

  return { kind: "unavailable" };
}

const PURCHASABLE_SUBSCRIPTION_STATUSES = new Set([
  "inactive",
  "canceled",
  "incomplete_expired",
]);

export function hasActiveWorkspaceSeatCapacity(
  summary: WorkspaceSubscriptionSummary | null | undefined,
): boolean {
  return summary?.seatCapacity != null;
}

export function hasWorkspaceBillingRelationship(
  summary: WorkspaceSubscriptionSummary | null | undefined,
): boolean {
  return summary?.hasStripeCustomer === true;
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
