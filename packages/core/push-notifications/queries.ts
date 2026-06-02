import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListPushSubscriptionsResponse, VapidPublicKeyResponse } from "../types";

/**
 * React Query keys for push notification data.
 * Centralized so mutations can invalidate them by reference.
 */
export const pushNotificationKeys = {
  all: ["push-notifications"] as const,
  vapidKey: () => [...pushNotificationKeys.all, "vapid-key"] as const,
  subscriptions: () => [...pushNotificationKeys.all, "subscriptions"] as const,
};

/**
 * Fetch the server's VAPID public key.
 *
 * The key is required by `PushManager.subscribe({ applicationServerKey })`.
 * The web app fetches it once at boot and caches it in React Query.
 */
export function vapidPublicKeyOptions() {
  return queryOptions({
    queryKey: pushNotificationKeys.vapidKey(),
    queryFn: () => api.getVapidPublicKey(),
    // The VAPID key is stable for the lifetime of the deployment; cache it
    // for the duration of the session and only refetch on explicit invalidation.
    staleTime: Infinity,
    retry: false,
  });
}

/**
 * List all push subscriptions owned by the current user across devices.
 * The settings UI uses this to show "Browser Push is enabled on this device
 * (1 of 2)" and to surface per-device management controls.
 */
export function pushSubscriptionOptions() {
  return queryOptions({
    queryKey: pushNotificationKeys.subscriptions(),
    queryFn: () => api.getPushSubscriptions(),
  });
}

export type { ListPushSubscriptionsResponse, VapidPublicKeyResponse };
