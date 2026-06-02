import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { pushNotificationKeys } from "./queries";
import {
  isPushSupported,
  subscribeDevice,
  unsubscribeDevice,
  subscriptionToJSON,
  getExistingSubscription,
} from "./subscription";
import { PushSubscriptionError } from "./types";

/**
 * Hook: opt the current device in to browser push notifications.
 *
 * Flow:
 *   1. Request the browser's notification permission (OS-level prompt).
 *   2. Create a `PushSubscription` via the service worker.
 *   3. POST the subscription JSON to the server so it can deliver pushes
 *      to this device in future.
 *
 * On success the push subscription query is invalidated so the UI reflects
 * the new subscription count.
 */
export function useSubscribePushNotifications() {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (vapidPublicKey: string): Promise<void> => {
      if (!isPushSupported()) {
        throw new PushSubscriptionError(
          "unsupported",
          "Push notifications are not supported in this browser",
        );
      }

      const subscription = await subscribeDevice(vapidPublicKey);
      if (!subscription) {
        throw new PushSubscriptionError(
          "permission_denied",
          "Notification permission was denied. Enable it in browser settings.",
        );
      }

      const payload = subscriptionToJSON(subscription);
      const deviceName = detectDeviceName();

      try {
        await api.subscribePush({
          subscription: payload,
          device_name: deviceName,
        });
      } catch {
        throw new PushSubscriptionError(
          "server_error",
          "Failed to save subscription to the server.",
        );
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: pushNotificationKeys.subscriptions(),
      });
    },
  });
}

/**
 * Hook: opt the current device out of browser push notifications.
 *
 * Flow:
 *   1. Locate the current device's push subscription.
 *   2. DELETE it on the server so the backend stops routing pushes here.
 *   3. Unsubscribe locally so the browser stops accepting push events.
 */
export function useUnsubscribePushNotifications() {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (): Promise<void> => {
      const existing = await getExistingSubscription();
      if (existing) {
        // Remove on the server first — if the server call fails we want the
        // subscription to remain active so the user can retry.
        try {
          await api.unsubscribePush(existing.endpoint);
        } catch {
          throw new PushSubscriptionError(
            "server_error",
            "Failed to remove subscription from the server.",
          );
        }
      }

      // Unsubscribe locally even if no subscription was found; this is
      // a no-op if nothing is active.
      await unsubscribeDevice();
    },
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: pushNotificationKeys.subscriptions(),
      });
    },
  });
}

// ---------------------------------------------------------------------------
// Device name helper
// ---------------------------------------------------------------------------

/**
 * Infer a human-readable device label from the user-agent string.
 * This is a best-effort heuristic — it doesn't need to be exhaustive, just
 * recognisable enough for the settings UI ("Chrome on macOS", "Firefox on
 * Windows").
 */
function detectDeviceName(): string {
  if (typeof navigator === "undefined") return "unknown";

  const ua = navigator.userAgent;

  let browser = "Browser";
  if (ua.includes("Edg/")) browser = "Edge";
  else if (ua.includes("Chrome/") && !ua.includes("Chromium/")) browser = "Chrome";
  else if (ua.includes("Firefox/")) browser = "Firefox";
  else if (ua.includes("Safari/") && !ua.includes("Chrome/")) browser = "Safari";

  let os = "";
  if (ua.includes("Mac OS X")) os = "macOS";
  else if (ua.includes("Windows")) os = "Windows";
  else if (ua.includes("Linux")) os = "Linux";

  return os ? `${browser} on ${os}` : browser;
}
