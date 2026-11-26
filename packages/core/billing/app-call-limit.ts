import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { AppCallMemberUsage, AppCallUsage } from "../types";

/**
 * Monthly app call limit (MUL-10047).
 *
 * The calls agents send to connected apps each month are limited per
 * workspace. When the workspace runs out, the gateway refuses further calls
 * and the agent says so; this module reads the limit for Billing.
 */
export const appCallUsageKeys = {
  all: (wsId: string) => ["workspace-app-call-usage", wsId] as const,
};

/** Refetched on focus: an upgrade happens outside this tab, in Stripe. */
export function appCallUsageOptions(wsId: string) {
  return queryOptions({
    queryKey: appCallUsageKeys.all(wsId),
    queryFn: () => api.getAppCallUsage(wsId),
    enabled: wsId.length > 0,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

/** The Billing row for app calls. */
export type AppCallRow =
  | {
      kind: "metered";
      used: number;
      limit: number;
      reached: boolean;
      resetAt: string | null;
      members: AppCallMemberUsage[];
    }
  | { kind: "unavailable"; limit: number | null };

/**
 * What Billing shows for app calls, or null when no limit applies.
 *
 * The limit comes from the entitlement snapshot when it has one, as for
 * storage: a page that has just upgraded or added seats shows the new limit
 * at once, while the usage read can still carry a policy cached before it.
 */
export function resolveAppCallRow(
  usage: AppCallUsage | undefined,
  usageFailed: boolean,
  entitlementLimit: { mode: string; limit: number | null } | null | undefined,
): AppCallRow | null {
  const planLimit = entitlementLimit?.mode === "limited" ? entitlementLimit.limit : null;
  if (usageFailed || !usage) {
    return usageFailed && planLimit !== null ? { kind: "unavailable", limit: planLimit } : null;
  }
  if (usage.action === "off" || usage.used === null || usage.limit === null) return null;
  const limit = planLimit ?? usage.limit;
  return {
    kind: "metered",
    used: usage.used,
    limit,
    // Only an enforced limit is ever reached; observe never refuses.
    reached: usage.action === "enforce" && usage.used >= limit,
    resetAt: usage.resetAt,
    members: usage.members,
  };
}
