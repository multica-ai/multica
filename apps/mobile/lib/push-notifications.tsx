import { useEffect, useRef } from "react";
import { AppState, Linking, Platform } from "react-native";
import Constants from "expo-constants";
import * as Device from "expo-device";
import * as Notifications from "expo-notifications";

import { savePushDevice } from "@/data/push-device";
import { useAuthStore } from "@/data/auth-store";

Notifications.setNotificationHandler({
  handleNotification: async () => {
    const show = AppState.currentState !== "active";
    return {
      shouldShowBanner: show,
      shouldShowList: show,
      shouldPlaySound: show,
      shouldSetBadge: true,
    };
  },
});

function projectId(): string | null {
  return (
    Constants.easConfig?.projectId ??
    (Constants.expoConfig?.extra?.eas?.projectId as string | undefined) ??
    process.env.EXPO_PUBLIC_EAS_PROJECT_ID ??
    null
  );
}

async function registerDevice(): Promise<void> {
  if (!Device.isDevice || Platform.OS !== "ios") return;

  const current = await Notifications.getPermissionsAsync();
  const permission = current.granted
    ? current
    : await Notifications.requestPermissionsAsync();
  if (!permission.granted) return;

  const easProjectId = projectId();
  if (!easProjectId) {
    console.warn(
      "[push] EXPO_PUBLIC_EAS_PROJECT_ID is missing; push registration skipped",
    );
    return;
  }

  const token = (
    await Notifications.getExpoPushTokenAsync({ projectId: easProjectId })
  ).data;
  await savePushDevice(token);
}

function openNotification(response: Notifications.NotificationResponse) {
  const url = response.notification.request.content.data?.url;
  if (typeof url === "string" && url.startsWith("multica:///")) {
    void Linking.openURL(url);
  }
  void Notifications.clearLastNotificationResponseAsync();
}

export function PushNotifications() {
  const userID = useAuthStore((state) => state.user?.id ?? null);
  const lastResponseID = useRef<string | null>(null);

  useEffect(() => {
    const subscription = Notifications.addNotificationResponseReceivedListener(
      (response) => {
        lastResponseID.current = response.notification.request.identifier;
        openNotification(response);
      },
    );
    void Notifications.getLastNotificationResponseAsync().then((response) => {
      if (
        response &&
        response.notification.request.identifier !== lastResponseID.current
      ) {
        lastResponseID.current = response.notification.request.identifier;
        openNotification(response);
      }
    });
    return () => subscription.remove();
  }, []);

  useEffect(() => {
    if (!userID) return;
    let cancelled = false;
    let tokenSubscription: Notifications.EventSubscription | null = null;

    void registerDevice()
      .then(() => {
        if (cancelled) return;
        tokenSubscription = Notifications.addPushTokenListener(() => {
          // The listener reports the rotated native APNs token. Ask Expo for
          // its corresponding Expo token again before updating the server.
          void registerDevice().catch((error) =>
            console.warn("[push] token refresh failed", error),
          );
        });
      })
      .catch((error) => console.warn("[push] registration failed", error));

    return () => {
      cancelled = true;
      tokenSubscription?.remove();
    };
  }, [userID]);

  return null;
}
