import { useEffect } from "react";
import Constants from "expo-constants";
import { AppState, Platform } from "react-native";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import type { MobilePushPlatform, MobilePushProvider } from "@multica/core/types";
import { createSafeId } from "@multica/core/utils";
import { mobileStorage } from "../platform/storage";
import { isMobileNotificationPermissionGranted } from "./mobile-notification-permissions";
import {
  addApnsDeviceTokenListener,
  initializeApnsPush,
  isApnsPushAvailable,
} from "./apns-push";
import {
  addGetuiClientIdListener,
  initializeGetuiPush,
  isGetuiPushAvailable,
} from "./getui-push";

const INSTALLATION_ID_KEY = "multica_mobile_push_installation_id";
let registeredKey: string | null = null;

type MobilePushAdapter = {
  addTokenListener: (listener: (token: string) => void) => () => void;
  initialize: () => Promise<string | null>;
  isAvailable: () => boolean;
  platform: MobilePushPlatform;
  provider: MobilePushProvider;
};

export function MobilePushRegistrationSync() {
  const userId = useAuthStore((state) => state.user?.id ?? null);

  useEffect(() => {
    const adapter = getCurrentMobilePushAdapter();
    if (!userId || !adapter?.isAvailable()) {
      registeredKey = null;
      return;
    }

    let cancelled = false;

    const registerProviderToken = async (providerToken: string | null) => {
      if (cancelled || !providerToken) return;
      if (!(await isMobileNotificationPermissionGranted())) return;
      await registerMobilePushToken(userId, adapter, providerToken);
    };

    const syncRegistration = async () => {
      if (cancelled) return;
      await syncCurrentMobilePushRegistration(userId);
    };

    const removeListener = adapter.addTokenListener((providerToken) => {
      void registerProviderToken(providerToken);
    });

    void syncRegistration().catch(() => {
      // Push registration should never block the signed-in app.
    });

    const appStateSubscription = AppState.addEventListener("change", (state) => {
      if (state !== "active") return;
      void syncRegistration().catch(() => {
        // Push registration should never block the signed-in app.
      });
    });

    return () => {
      cancelled = true;
      appStateSubscription.remove();
      removeListener();
    };
  }, [userId]);

  return null;
}

export async function syncCurrentMobilePushRegistration(userIdOverride?: string | null) {
  const userId = userIdOverride ?? useAuthStore.getState().user?.id ?? null;
  const adapter = getCurrentMobilePushAdapter();
  if (!userId || !adapter?.isAvailable()) return;
  if (!(await isMobileNotificationPermissionGranted())) return;

  const providerToken = await adapter.initialize();
  await registerMobilePushToken(userId, adapter, providerToken);
}

export async function disableCurrentMobilePushRegistration() {
  const adapter = getCurrentMobilePushAdapter();
  if (!adapter?.isAvailable()) return;
  const installationId = mobileStorage.getItem(INSTALLATION_ID_KEY);
  if (!installationId) return;
  await api.disableMobilePushRegistration(installationId, adapter.provider);
}

function getOrCreateInstallationId() {
  const existing = mobileStorage.getItem(INSTALLATION_ID_KEY);
  if (existing) return existing;

  const next = createSafeId();
  mobileStorage.setItem(INSTALLATION_ID_KEY, next);
  return next;
}

async function registerMobilePushToken(
  userId: string,
  adapter: MobilePushAdapter,
  providerToken: string | null,
) {
  if (!providerToken) return;
  const installationId = getOrCreateInstallationId();
  const nextRegisteredKey = `${userId}:${installationId}:${adapter.provider}:${providerToken}`;
  if (registeredKey === nextRegisteredKey) return;

  await api.upsertMobilePushRegistration({
    installation_id: installationId,
    platform: adapter.platform,
    provider: adapter.provider,
    provider_client_id: providerToken,
    app_version: Constants.expoConfig?.version,
  });
  registeredKey = nextRegisteredKey;
}

function getCurrentMobilePushAdapter(): MobilePushAdapter | null {
  if (Platform.OS === "android") {
    return {
      addTokenListener: addGetuiClientIdListener,
      initialize: initializeGetuiPush,
      isAvailable: isGetuiPushAvailable,
      platform: "android",
      provider: "getui",
    };
  }
  if (Platform.OS === "ios") {
    return {
      addTokenListener: addApnsDeviceTokenListener,
      initialize: initializeApnsPush,
      isAvailable: isApnsPushAvailable,
      platform: "ios",
      provider: "apns",
    };
  }
  return null;
}
