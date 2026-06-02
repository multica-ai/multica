"use client";

import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  isPushSupported,
  registerServiceWorker,
  getExistingSubscription,
  subscriptionToJSON,
  getNotificationPermission,
} from "@multica/core/push-notifications/subscription";
import {
  pushNotificationKeys,
  pushSubscriptionOptions,
} from "@multica/core/push-notifications/queries";
import { api } from "@multica/core";

/**
 * Client component mounted once in the root layout via `<WebProviders>`.
 *
 * Responsibilities:
 *   1. Register the service worker on app load.
 *   2. Detect whether this device has an active push subscription and
 *      synchronise it with the server (idempotent upsert on each boot).
 *   3. Listen for `navigate` messages from the service worker and perform
 *      the client-side navigation.
 *
 * This component renders nothing; it is purely a side-effect host.
 */
export function PushNotificationRegistrar() {
  const qc = useQueryClient();
  const didSyncRef = useRef(false);

  // Step 1: Register service worker (once, on mount).
  useEffect(() => {
    if (!isPushSupported()) return;
    registerServiceWorker();
  }, []);

  // Step 2: Fetch the list of server-held subscriptions for this user.
  //          Used to decide whether this device's subscription is stale.
  const { data: serverSubs } = useQuery(pushSubscriptionOptions());

  // Step 3: Re-sync this device's subscription with the server when the
  //         user has previously opted in (permission already granted).
  useEffect(() => {
    if (didSyncRef.current) return;
    if (!isPushSupported()) return;
    if (getNotificationPermission() !== "granted") return;
    didSyncRef.current = true;

    (async () => {
      try {
        const existing = await getExistingSubscription();
        if (!existing) return;

        // Check whether this subscription's endpoint is already known to the
        // server. If it is, we're in sync and can skip the upsert.
        const subs = serverSubs?.subscriptions ?? [];
        if (subs.some((s) => s.endpoint === existing.endpoint)) return;

        // Device subscription exists but server doesn't have it — upsert.
        const payload = subscriptionToJSON(existing);
        await api.subscribePush({
          subscription: payload,
          device_name: detectDeviceName(),
        });
        qc.invalidateQueries({
          queryKey: pushNotificationKeys.subscriptions(),
        });
      } catch {
        // Silently ignore sync failures; the user can re-enable via Settings.
      }
    })();
  }, [serverSubs, qc]);

  // Step 4: Handle `navigate` messages from the service worker.
  //         When a push notification is clicked the SW posts a message
  //         to the focused client with the target URL.
  useEffect(() => {
    if (typeof window === "undefined") return;

    const handler = (event: MessageEvent) => {
      if (event.data?.type === "navigate" && typeof event.data.url === "string") {
        window.location.assign(event.data.url);
      }
    };

    navigator.serviceWorker?.addEventListener("message", handler);
    return () => {
      navigator.serviceWorker?.removeEventListener("message", handler);
    };
  }, []);

  return null;
}

// ---------------------------------------------------------------------------
// Device name helper (client-only)
// ---------------------------------------------------------------------------

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
