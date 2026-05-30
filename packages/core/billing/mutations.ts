import { useCallback } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateBillingCheckoutSessionRequest } from "../types";
import { billingKeys } from "./queries";

// Both mutations here trigger a hop OUT of the SPA — Stripe Checkout
// and Stripe Billing Portal are hosted pages. The mutation completes
// once the URL is in our hands; the caller is responsible for the
// `window.location.href = url` redirect (or in newer flows,
// `window.open` + tab-aware polling).
//
// We invalidate the topup list on settle so when the user returns
// from Stripe the new `pending` order shows up immediately. The
// balance and transactions are NOT invalidated here — they only flip
// after Stripe + the cloud webhook actually credit, which is a
// post-redirect concern.

export function useCreateCloudBillingCheckoutSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateBillingCheckoutSessionRequest) =>
      api.createCloudBillingCheckoutSession(data),
    onSettled: () => {
      // The new pending topup row is visible to the topups list as
      // soon as Cloud writes it. Invalidate so the user sees the new
      // pending entry without a page refresh.
      qc.invalidateQueries({ queryKey: [...billingKeys.all(), "topups"] });
    },
  });
}

export function useCreateCloudBillingPortalSession() {
  return useMutation({
    mutationFn: () => api.createCloudBillingPortalSession(),
    // No cache invalidation — the portal opens, the user does whatever,
    // and any state changes Stripe-side propagate back via webhook.
    // The next React Query refetch picks them up at its own cadence.
  });
}

/**
 * useInvalidateBillingDataAfterCredit returns a callback that flushes
 * the cached balance / transactions / batches / topups so the page
 * re-fetches them. Used by the Stripe-success polling banner: once it
 * detects the topup status flipped to a terminal value (credited /
 * failed / canceled), the banner is the only query that's been
 * polling — every other card on the page is still showing its
 * pre-checkout snapshot. Without this invalidation the user sees
 * "Final status: credited" while the balance card still displays the
 * old number until they click refresh.
 *
 * Scope of the invalidation:
 *
 *   - balance + transactions + batches: only ever change at the
 *     `credited` transition (the cloud writes the credit ledger and
 *     batch row in the same DB transaction as the wallet update).
 *     For `failed` / `canceled` they do NOT change, so technically we
 *     over-fetch in those cases — three extra cheap round-trips that
 *     simplify the call site and are negligible on a test page.
 *
 *   - topups: changes on every terminal transition (the order row
 *     flips status), so it always needs invalidating.
 *
 *   - the checkout-session query itself is intentionally NOT in this
 *     sweep. Its `refetchInterval` already returned `false` when
 *     status went terminal; refetching would just confirm the same
 *     value we already hold and wake the polling cycle back up for
 *     no benefit.
 */
export function useInvalidateBillingDataAfterCredit() {
  const qc = useQueryClient();
  return useCallback(() => {
    qc.invalidateQueries({ queryKey: billingKeys.balance() });
    qc.invalidateQueries({ queryKey: [...billingKeys.all(), "transactions"] });
    qc.invalidateQueries({ queryKey: [...billingKeys.all(), "batches"] });
    qc.invalidateQueries({ queryKey: [...billingKeys.all(), "topups"] });
  }, [qc]);
}
