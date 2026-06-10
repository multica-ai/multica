import * as Notifications from "expo-notifications";
import { Platform } from "react-native";

type NotificationSubscription = {
  remove: () => void;
};

export function isApnsPushAvailable() {
  return Platform.OS === "ios";
}

export async function initializeApnsPush(): Promise<string | null> {
  if (!isApnsPushAvailable()) return null;

  const permission = await Notifications.getPermissionsAsync();
  if (!permission.granted && permission.status !== "granted") return null;

  const token = await Notifications.getDevicePushTokenAsync();
  return normalizeApnsDeviceToken(token);
}

export function addApnsDeviceTokenListener(
  listener: (deviceToken: string) => void,
): () => void {
  if (!isApnsPushAvailable()) return () => {};
  const subscription = Notifications.addPushTokenListener((token) => {
    const normalized = normalizeApnsDeviceToken(token);
    if (normalized) listener(normalized);
  }) as NotificationSubscription;
  return () => {
    subscription.remove();
  };
}

export async function consumeApnsPendingNotificationUrl(): Promise<string | null> {
  if (!isApnsPushAvailable()) return null;
  const notifications = Notifications as typeof Notifications & {
    clearLastNotificationResponseAsync?: () => Promise<void>;
    getLastNotificationResponse?: () => unknown;
    getLastNotificationResponseAsync?: () => Promise<unknown>;
  };
  const response = notifications.getLastNotificationResponseAsync
    ? await notifications.getLastNotificationResponseAsync()
    : notifications.getLastNotificationResponse?.();
  const url = extractNotificationResponseUrl(response);
  if (url) {
    await notifications.clearLastNotificationResponseAsync?.();
  }
  return url;
}

export function addApnsNotificationUrlListener(
  listener: (url: string) => void,
): () => void {
  if (!isApnsPushAvailable()) return () => {};
  const subscription = Notifications.addNotificationResponseReceivedListener((response) => {
    const url = extractNotificationResponseUrl(response);
    if (url) listener(url);
  }) as NotificationSubscription;
  return () => {
    subscription.remove();
  };
}

export function normalizeApnsDeviceToken(token: unknown): string | null {
  if (!token || typeof token !== "object") return null;
  const candidate = token as { data?: unknown; type?: unknown };
  if (candidate.type !== "ios") return null;
  return typeof candidate.data === "string" && candidate.data.trim()
    ? candidate.data.trim()
    : null;
}

export function extractNotificationResponseUrl(response: unknown): string | null {
  if (!response || typeof response !== "object") return null;
  const data = (response as {
    notification?: {
      request?: {
        content?: {
          data?: unknown;
        };
      };
    };
  }).notification?.request?.content?.data;
  if (!data || typeof data !== "object") return null;
  return normalizeNotificationUrl((data as { url?: unknown }).url);
}

function normalizeNotificationUrl(url: unknown): string | null {
  if (typeof url !== "string") return null;
  const cleanUrl = url.trim();
  return cleanUrl.startsWith("wujieai-multicam://") ? cleanUrl : null;
}
