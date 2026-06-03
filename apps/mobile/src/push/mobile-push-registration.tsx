import { useEffect, useRef } from "react";
import Constants from "expo-constants";
import { Platform } from "react-native";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import type { MobilePushPlatform, MobilePushProvider } from "@multica/core/types";
import { createSafeId } from "@multica/core/utils";
import { mobileStorage } from "../platform/storage";
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

type MobilePushAdapter = {
  addTokenListener: (listener: (token: string) => void) => () => void;
  initialize: () => Promise<string | null>;
  isAvailable: () => boolean;
  platform: MobilePushPlatform;
  provider: MobilePushProvider;
};

export function MobilePushRegistrationSync() {
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const registeredKeyRef = useRef<string | null>(null);

  useEffect(() => {
    const adapter = getCurrentMobilePushAdapter();
    if (!userId || !adapter?.isAvailable()) {
      registeredKeyRef.current = null;
      return;
    }

    let cancelled = false;

    const registerProviderToken = async (providerToken: string | null) => {
      if (cancelled || !providerToken) return;
      const installationId = getOrCreateInstallationId();
      const registrationKey = `${userId}:${installationId}:${adapter.provider}:${providerToken}`;
      if (registeredKeyRef.current === registrationKey) return;

      await api.upsertMobilePushRegistration({
        installation_id: installationId,
        platform: adapter.platform,
        provider: adapter.provider,
        provider_client_id: providerToken,
        app_version: Constants.expoConfig?.version,
      });
      registeredKeyRef.current = registrationKey;
    };

    const removeListener = adapter.addTokenListener((providerToken) => {
      void registerProviderToken(providerToken);
    });

    adapter.initialize()
      .then(registerProviderToken)
      .catch(() => {
        // Push registration should never block the signed-in app.
      });

    return () => {
      cancelled = true;
      removeListener();
    };
  }, [userId]);

  return null;
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
