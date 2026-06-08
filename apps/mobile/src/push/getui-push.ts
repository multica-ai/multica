import {
  NativeEventEmitter,
  NativeModules,
  Platform,
  type EmitterSubscription,
} from "react-native";

type GetuiPushNativeModule = {
  initialize: () => Promise<string | null>;
  getClientId: () => Promise<string | null>;
  getPendingNotificationUrl: () => Promise<string | null>;
  consumePendingNotificationUrl: () => Promise<string | null>;
};

const EVENT_CLIENT_ID = "GetuiPushClientId";
const EVENT_NOTIFICATION_URL = "GetuiPushNotificationUrl";

const nativeModule =
  Platform.OS === "android"
    ? (NativeModules.GetuiPush as GetuiPushNativeModule | undefined)
    : undefined;

const nativeEvents = nativeModule ? new NativeEventEmitter(nativeModule as never) : null;

export function isGetuiPushAvailable() {
  return Platform.OS === "android" && !!nativeModule;
}

export async function initializeGetuiPush(): Promise<string | null> {
  if (!nativeModule) return null;
  const clientId = await nativeModule.initialize();
  return normalizeClientId(clientId);
}

export async function getGetuiClientId(): Promise<string | null> {
  if (!nativeModule) return null;
  const clientId = await nativeModule.getClientId();
  return normalizeClientId(clientId);
}

export function addGetuiClientIdListener(
  listener: (clientId: string) => void,
): () => void {
  if (!nativeEvents) return () => {};
  const subscription: EmitterSubscription = nativeEvents.addListener(
    EVENT_CLIENT_ID,
    (clientId: unknown) => {
      const normalized = normalizeClientId(clientId);
      if (normalized) listener(normalized);
    },
  );
  return () => {
    subscription.remove();
  };
}

export async function consumeGetuiPendingNotificationUrl(): Promise<string | null> {
  if (!nativeModule) return null;
  const url = await nativeModule.consumePendingNotificationUrl();
  return normalizeNotificationUrl(url);
}

export function addGetuiNotificationUrlListener(
  listener: (url: string) => void,
): () => void {
  if (!nativeEvents) return () => {};
  const subscription: EmitterSubscription = nativeEvents.addListener(
    EVENT_NOTIFICATION_URL,
    (url: unknown) => {
      const normalized = normalizeNotificationUrl(url);
      if (normalized) listener(normalized);
    },
  );
  return () => {
    subscription.remove();
  };
}

function normalizeClientId(clientId: unknown): string | null {
  return typeof clientId === "string" && clientId.trim() ? clientId.trim() : null;
}

function normalizeNotificationUrl(url: unknown): string | null {
  if (typeof url !== "string") return null;
  const cleanUrl = url.trim();
  return cleanUrl.startsWith("wujieai-multicam://") ? cleanUrl : null;
}
