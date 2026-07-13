import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Billing data is account-level (single owner per X-User-ID), so the
// React Query keys are NOT scoped to a workspace — keying on workspace
// would force a refetch every time the user navigates to a different
// workspace, even though the backing data is identical.
//
// Query-level staleness: the cloud's billing module is the source of
// truth. Topup status flips from `pending` → `paid` → `credited` after
// Stripe + the cloud-side webhook handler do their thing, so a returning-
// from-Stripe page needs prompt freshness. We rely on the page-level
// hooks (refetchInterval / invalidate-on-mount) rather than baking a
// stale-time here, so consumers can tune polling per surface.

export const billingKeys = {
  all: () => ["billing"] as const,
  balance: () => [...billingKeys.all(), "balance"] as const,
  transactions: (params?: { page?: number; page_size?: number }) =>
    [...billingKeys.all(), "transactions", params ?? {}] as const,
  batches: (params?: { page?: number; page_size?: number }) =>
    [...billingKeys.all(), "batches", params ?? {}] as const,
  topups: (params?: { page?: number; page_size?: number }) =>
    [...billingKeys.all(), "topups", params ?? {}] as const,
  priceTiers: () => [...billingKeys.all(), "price-tiers"] as const,
  checkoutSession: (sessionId: string) =>
    [...billingKeys.all(), "checkout-session", sessionId] as const,
};

export function billingBalanceOptions() {
  return queryOptions({
    queryKey: billingKeys.balance(),
    queryFn: () => api.getCloudBillingBalance(),
    // 30s stale-time: balance changes only when a topup credits or a
    // deduction happens. For the test page the user's main interest is
    // post-checkout state; we let the page invalidate explicitly.
    staleTime: 30 * 1000,
  });
}

export function billingTransactionsOptions(params?: {
  page?: number;
  page_size?: number;
}) {
  return queryOptions({
    queryKey: billingKeys.transactions(params),
    queryFn: () => api.listCloudBillingTransactions(params),
    staleTime: 30 * 1000,
  });
}

export function billingBatchesOptions(params?: {
  page?: number;
  page_size?: number;
}) {
  return queryOptions({
    queryKey: billingKeys.batches(params),
    queryFn: () => api.listCloudBillingBatches(params),
    staleTime: 30 * 1000,
  });
}

export function billingTopupsOptions(params?: {
  page?: number;
  page_size?: number;
}) {
  return queryOptions({
    queryKey: billingKeys.topups(params),
    queryFn: () => api.listCloudBillingTopups(params),
    staleTime: 30 * 1000,
  });
}

export function billingPriceTiersOptions() {
  return queryOptions({
    queryKey: billingKeys.priceTiers(),
    queryFn: () => api.listCloudBillingPriceTiers(),
    // Price tiers come from server config and basically never change at
    // runtime — once we've fetched once we can keep it for the whole
    // session. 5 minutes is more than enough for a test page.
    staleTime: 5 * 60 * 1000,
  });
}

// Stripe-success-redirect polling: when the page loads with
// `?session_id=...` in the URL, the user just came back from Stripe and
// the topup is racing through `pending → paid → credited`. Poll until
// it's terminal so the UI can show a final outcome before the user
// closes the tab.
//
// Caller is expected to short-circuit by passing `enabled: !!sessionId`
// — that's why we don't gate inside queryOptions itself.
export function billingCheckoutSessionOptions(sessionId: string) {
  return queryOptions({
    queryKey: billingKeys.checkoutSession(sessionId),
    queryFn: () => api.getCloudBillingCheckoutSession(sessionId),
    // Refetch every 2s while we're still in a non-terminal state,
    // stop once we land in `credited` / `failed` / `canceled`.
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      if (status === "credited" || status === "failed" || status === "canceled") {
        return false;
      }
      return 2000;
    },
    staleTime: 0,
  });
}
