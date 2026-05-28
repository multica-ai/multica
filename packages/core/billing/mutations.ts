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
