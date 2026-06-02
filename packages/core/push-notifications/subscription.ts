/**
 * Low-level utilities for the Web Push API.
 *
 * All functions are safe to import in any module — browser-only work is
 * guarded behind runtime checks. Server-side imports are no-ops; they will
 * not throw but every function returns `null` or `false`.
 */

import type { PushSubscriptionJSON } from "../types";

const SERVICE_WORKER_PATH = "/sw.js";
const APPLICATION_SERVER_KEY_STORAGE_KEY = "wallts_vapid_public_key";

// ---------------------------------------------------------------------------
// Feature detection
// ---------------------------------------------------------------------------

/**
 * `true` when the current runtime supports the Notification and Push APIs.
 * Useful for feature-gating the entire Browser Push section in settings.
 */
export function isPushSupported(): boolean {
  if (typeof window === "undefined") return false;
  return (
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window
  );
}

// ---------------------------------------------------------------------------
// Service Worker
// ---------------------------------------------------------------------------

/**
 * Register the Wallts service worker. Returns the registration if
 * successful, `null` otherwise (e.g. unsupported browser or permission
 * denied at the OS level).
 */
export async function registerServiceWorker(): Promise<ServiceWorkerRegistration | null> {
  if (!isPushSupported()) return null;
  try {
    return await navigator.serviceWorker.register(SERVICE_WORKER_PATH, {
      scope: "/",
    });
  } catch {
    // Registration failures are non-fatal; log to console for debugging
    // but do not throw — the app should remain usable without push.
    if (typeof console !== "undefined") {
      console.warn("[push] Service worker registration failed");
    }
    return null;
  }
}

// ---------------------------------------------------------------------------
// Permission
// ---------------------------------------------------------------------------

/** The current browser notification permission state. */
export function getNotificationPermission(): NotificationPermission {
  if (typeof window === "undefined" || !("Notification" in window)) {
    return "denied";
  }
  return Notification.permission;
}

/**
 * Request notification permission from the user.
 * Returns the resulting permission state. Browsers that don't support the
 * API return `"denied"` immediately.
 */
export async function requestNotificationPermission(): Promise<NotificationPermission> {
  if (!isPushSupported()) return "denied";
  return Notification.requestPermission();
}

// ---------------------------------------------------------------------------
// Subscription
// ---------------------------------------------------------------------------

/**
 * Convert a `PushSubscription` to a plain JSON object that the server can
 * store and later use to send a push message.
 */
export function subscriptionToJSON(
  subscription: PushSubscription,
): PushSubscriptionJSON {
  const json = subscription.toJSON();
  return {
    endpoint: json.endpoint ?? "",
    expirationTime: json.expirationTime ?? null,
    keys: {
      p256dh: json.keys?.p256dh ?? "",
      auth: json.keys?.auth ?? "",
    },
  };
}

/**
 * Get the current push subscription for the active service worker
 * registration, or `null` if no subscription exists.
 */
export async function getExistingSubscription(): Promise<PushSubscription | null> {
  if (!isPushSupported()) return null;
  try {
    const reg = await navigator.serviceWorker.ready;
    return await reg.pushManager.getSubscription();
  } catch {
    return null;
  }
}

/**
 * Subscribe the current device for push messages.
 *
 * @param vapidPublicKey Base64url-encoded VAPID public key from the server.
 *   If omitted the function attempts to read the key from localStorage
 *   (cached from a previous call).
 */
export async function subscribeDevice(
  vapidPublicKey?: string,
): Promise<PushSubscription | null> {
  if (!isPushSupported()) return null;

  const key = vapidPublicKey ?? getStoredVapidKey();
  if (!key) return null;

  const reg = await navigator.serviceWorker.ready;
  const existing = await reg.pushManager.getSubscription();
  if (existing) return existing;

  try {
    const subscription = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(key) as BufferSource,
    });
    storeVapidKey(key);
    return subscription;
  } catch {
    return null;
  }
}

/**
 * Unsubscribe the current device from push messages.
 * Returns `true` if a subscription was removed, `false` otherwise.
 */
export async function unsubscribeDevice(): Promise<boolean> {
  if (!isPushSupported()) return false;
  try {
    const reg = await navigator.serviceWorker.ready;
    const subscription = await reg.pushManager.getSubscription();
    if (subscription) {
      await subscription.unsubscribe();
      return true;
    }
    return false;
  } catch {
    return false;
  }
}

// ---------------------------------------------------------------------------
// VAPID key caching (localStorage, scoped to this browser)
// ---------------------------------------------------------------------------

function storeVapidKey(key: string): void {
  try {
    localStorage.setItem(APPLICATION_SERVER_KEY_STORAGE_KEY, key);
  } catch {
    // localStorage unavailable (private browsing, storage quota, etc.)
  }
}

function getStoredVapidKey(): string | null {
  try {
    return localStorage.getItem(APPLICATION_SERVER_KEY_STORAGE_KEY);
  } catch {
    return null;
  }
}

/**
 * Return the cached VAPID public key, or `null` if nothing is cached yet.
 */
export function getCachedVapidKey(): string | null {
  return getStoredVapidKey();
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Convert a base64url-encoded string to a Uint8Array, as required by
 * `PushManager.subscribe({ applicationServerKey })`.
 */
function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = "=".repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, "+").replace(/_/g, "/");
  const rawData = atob(base64);
  const outputArray = new Uint8Array(rawData.length);
  for (let i = 0; i < rawData.length; i++) {
    outputArray[i] = rawData.charCodeAt(i);
  }
  return outputArray;
}
