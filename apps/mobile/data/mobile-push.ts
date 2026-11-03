import { Platform } from "react-native";
import Constants from "expo-constants";
import * as Notifications from "expo-notifications";
import { currentSessionEpoch, sessionEpochChanged } from "./session-epoch";
import { registerPushToken, unregisterPushToken } from "./push-registration";

// Foreground activity is already visible in the app's Inbox. Only APNs
// banners delivered while inactive/backgrounded should interrupt the user.
Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldShowBanner: false,
    shouldShowList: false,
    shouldPlaySound: false,
    shouldSetBadge: false,
  }),
});

export async function mobilePushAllowed(): Promise<boolean> {
  if (Platform.OS !== "ios") return false;
  const permission = await Notifications.getPermissionsAsync();
  return permission.granted || permission.ios?.status === Notifications.IosAuthorizationStatus.PROVISIONAL;
}

export async function registerMobilePush(requestPermission = false): Promise<boolean> {
  const epoch = currentSessionEpoch();
  const projectId = Constants.expoConfig?.extra?.eas?.projectId;
  if (Platform.OS !== "ios" || typeof projectId !== "string" || !projectId) return false;
  try {
    let permission = await Notifications.getPermissionsAsync();
    if (requestPermission && !permission.granted) {
      permission = await Notifications.requestPermissionsAsync();
    }
    if (sessionEpochChanged(epoch)) return false;
    const allowed = permission.granted || permission.ios?.status === Notifications.IosAuthorizationStatus.PROVISIONAL;
    if (!allowed) {
      await unregisterPushToken();
      return false;
    }
    const { data } = await Notifications.getExpoPushTokenAsync({ projectId });
    if (sessionEpochChanged(epoch)) return false;
    await registerPushToken(data, epoch);
    return !sessionEpochChanged(epoch);
  } catch {
    console.warn("[push] registration unavailable; retrying on next foreground");
    return false;
  }
}

export function pushWorkspace(data: unknown): string | null {
  if (!data || typeof data !== "object" || !("workspace" in data)) return null;
  const workspace = data.workspace;
  // The destination is always the local Inbox, never an arbitrary payload URL.
  return typeof workspace === "string" && /^[a-zA-Z0-9_-]+$/.test(workspace)
    ? workspace
    : null;
}
