export const VIBES_PUSH_SERVICE_WORKER_URL = "/vibes-sw.js?v=vibes-feed-v7";

export type WebPushCapability =
  "unsupported" | "prompt" | "denied" | "available";

export interface WebPushSubscriptionJSON {
  endpoint: string;
  expirationTime: number | null;
  keys: { p256dh: string; auth: string };
}

export function getWebPushCapability(): WebPushCapability {
  if (
    typeof navigator === "undefined" ||
    !("serviceWorker" in navigator) ||
    typeof PushManager === "undefined" ||
    typeof Notification === "undefined"
  ) {
    return "unsupported";
  }
  if (Notification.permission === "denied") return "denied";
  if (Notification.permission === "default") return "prompt";
  return "available";
}

function applicationServerKey(value: string): Uint8Array<ArrayBuffer> {
  const padded = value
    .replace(/-/gu, "+")
    .replace(/_/gu, "/")
    .padEnd(Math.ceil(value.length / 4) * 4, "=");
  const binary = atob(padded);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

function subscriptionJSON(
  subscription: PushSubscription,
): WebPushSubscriptionJSON | null {
  const value = subscription.toJSON();
  if (
    typeof value.endpoint !== "string" ||
    !value.endpoint ||
    typeof value.keys?.p256dh !== "string" ||
    !value.keys.p256dh ||
    typeof value.keys.auth !== "string" ||
    !value.keys.auth
  ) {
    return null;
  }
  return {
    endpoint: value.endpoint,
    expirationTime: value.expirationTime ?? null,
    keys: { p256dh: value.keys.p256dh, auth: value.keys.auth },
  };
}

/**
 * Return the durable browser subscription used for background Web Push.
 * Permission is requested only when called from an explicit user action.
 */
export async function ensureWebPushSubscription(
  publicKey: string,
  requestPermission: boolean,
): Promise<WebPushSubscriptionJSON | null> {
  const capability = getWebPushCapability();
  if (capability === "unsupported" || capability === "denied") return null;
  if (capability === "prompt") {
    if (!requestPermission) return null;
    if ((await Notification.requestPermission()) !== "granted") return null;
  }

  const registration = await navigator.serviceWorker.register(
    VIBES_PUSH_SERVICE_WORKER_URL,
    { scope: "/", updateViaCache: "none" },
  );
  const existing = await registration.pushManager.getSubscription();
  if (existing) return subscriptionJSON(existing);

  const created = await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: applicationServerKey(publicKey),
  });
  return subscriptionJSON(created);
}

/**
 * Remove a durable browser subscription after permission is revoked. The
 * endpoint is returned so the authenticated backend can stop attempting it.
 */
export async function revokeWebPushSubscription(): Promise<string | null> {
  if (
    typeof navigator === "undefined" ||
    !("serviceWorker" in navigator) ||
    typeof navigator.serviceWorker.getRegistration !== "function"
  ) {
    return null;
  }
  const registration = await navigator.serviceWorker.getRegistration("/");
  const subscription = await registration?.pushManager.getSubscription();
  if (!subscription) return null;
  const endpoint = subscription.endpoint;
  try {
    await subscription.unsubscribe();
  } catch {
    // Server-side removal still prevents delivery attempts for this endpoint.
  }
  return endpoint || null;
}
