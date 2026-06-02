import { useEffect, useRef } from "react";
import Constants from "expo-constants";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { createSafeId } from "@multica/core/utils";
import { mobileStorage } from "../platform/storage";
import {
  addGetuiClientIdListener,
  initializeGetuiPush,
  isGetuiPushAvailable,
} from "./getui-push";

const INSTALLATION_ID_KEY = "multica_mobile_push_installation_id";
const PROVIDER = "getui";
const PLATFORM = "android";

export function MobilePushRegistrationSync() {
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const registeredKeyRef = useRef<string | null>(null);

  useEffect(() => {
    if (!userId || !isGetuiPushAvailable()) {
      registeredKeyRef.current = null;
      return;
    }

    let cancelled = false;

    const registerClientId = async (clientId: string | null) => {
      if (cancelled || !clientId) return;
      const installationId = getOrCreateInstallationId();
      const registrationKey = `${userId}:${installationId}:${clientId}`;
      if (registeredKeyRef.current === registrationKey) return;

      await api.upsertMobilePushRegistration({
        installation_id: installationId,
        platform: PLATFORM,
        provider: PROVIDER,
        provider_client_id: clientId,
        app_version: Constants.expoConfig?.version,
      });
      registeredKeyRef.current = registrationKey;
    };

    const removeListener = addGetuiClientIdListener((clientId) => {
      void registerClientId(clientId);
    });

    initializeGetuiPush()
      .then(registerClientId)
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
  if (!isGetuiPushAvailable()) return;
  const installationId = mobileStorage.getItem(INSTALLATION_ID_KEY);
  if (!installationId) return;
  await api.disableMobilePushRegistration(installationId, PROVIDER);
}

function getOrCreateInstallationId() {
  const existing = mobileStorage.getItem(INSTALLATION_ID_KEY);
  if (existing) return existing;

  const next = createSafeId();
  mobileStorage.setItem(INSTALLATION_ID_KEY, next);
  return next;
}
