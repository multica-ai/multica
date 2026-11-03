import { useEffect } from "react";
import { AppState } from "react-native";
import { router } from "expo-router";
import * as Notifications from "expo-notifications";
import { registerMobilePush, pushWorkspace } from "./mobile-push";
import { api } from "./api";

export function useMobilePush(userId: string | null) {
  useEffect(() => {
    if (!userId) return;
    let active = true;
    let handled: string | null = null;
    const open = async (response: Notifications.NotificationResponse | null) => {
      if (!response || response.actionIdentifier !== Notifications.DEFAULT_ACTION_IDENTIFIER) return;
      const id = response.notification.request.identifier;
      if (handled === id) return;
      const workspace = pushWorkspace(response.notification.request.content.data);
      if (!workspace) return;
      try {
        const workspaces = await api.listWorkspaces();
        if (!active || handled === id || !workspaces.some((item) => item.slug === workspace)) return;
        handled = id;
        router.push({ pathname: "/[workspace]/(tabs)/inbox", params: { workspace } });
        await Notifications.clearLastNotificationResponseAsync();
      } catch {
        console.warn("[push] could not open inbox");
      }
    };
    void registerMobilePush();
    void Notifications.getLastNotificationResponseAsync().then(open).catch(() => undefined);
    const response = Notifications.addNotificationResponseReceivedListener((value) => { void open(value); });
    const tokens = Notifications.addPushTokenListener(() => { void registerMobilePush(); });
    const foreground = AppState.addEventListener("change", (state) => {
      if (state === "active") void registerMobilePush();
    });
    return () => {
      active = false;
      response.remove();
      tokens.remove();
      foreground.remove();
    };
  }, [userId]);
}
