import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Workspace subscription reads.
 *
 * Unlike the account-level wallet keys in `./queries`, these ARE scoped by
 * workspace ID: the server resolves the workspace from the request context, so
 * two workspaces produce different data from the same URL. Keying on the
 * workspace is what stops a cached snapshot from one workspace being shown for
 * another after a switch.
 */
export const workspaceSubscriptionKeys = {
  all: (wsId: string) => ["workspace-subscriptions", wsId] as const,
  entitlements: (wsId: string) =>
    [...workspaceSubscriptionKeys.all(wsId), "entitlements"] as const,
  summary: (wsId: string) =>
    [...workspaceSubscriptionKeys.all(wsId), "summary"] as const,
  prices: (wsId: string) =>
    [...workspaceSubscriptionKeys.all(wsId), "prices"] as const,
};

export function workspaceSubscriptionEntitlementsOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceSubscriptionKeys.entitlements(wsId),
    queryFn: () => api.getWorkspaceSubscriptionEntitlements(),
    enabled: wsId.length > 0,
    staleTime: 60 * 1000,
    refetchOnWindowFocus: true,
  });
}

/**
 * The Billing page's primary read. Short stale time plus refetch on focus: a
 * user returning from Stripe in another tab or the system browser should see the
 * new state without a manual reload, and cloud serves this without calling
 * Stripe so refetching is cheap.
 */
export function workspaceSubscriptionSummaryOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceSubscriptionKeys.summary(wsId),
    queryFn: () => api.getWorkspaceSubscriptionSummary(),
    enabled: wsId.length > 0,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

/**
 * Prices are deployment configuration, not workspace state: they change only
 * when an operator repoints a Stripe Price. Cache them for much longer than the
 * summary and do not refetch on focus — cloud already caches them and each miss
 * costs a Stripe round trip.
 *
 * Retry is disabled because the failure mode here is a misconfigured or
 * unvalidatable Price, which retrying cannot fix; the caller shows "price
 * confirmed at Checkout" instead.
 */
export function workspaceSubscriptionPricesOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceSubscriptionKeys.prices(wsId),
    queryFn: () => api.getWorkspaceSubscriptionPrices(),
    enabled: wsId.length > 0,
    staleTime: 10 * 60 * 1000,
    refetchOnWindowFocus: false,
    retry: false,
  });
}
