import * as Notifications from "expo-notifications";
import { Linking, Platform } from "react-native";

export type MobileNotificationPermissionPlatform = "android" | "ios" | "unsupported";
export type MobileNotificationPermissionStatus =
  | "granted"
  | "denied"
  | "undetermined"
  | "unavailable";

export type MobileNotificationPermissionState = {
  canRequest: boolean;
  granted: boolean;
  platform: MobileNotificationPermissionPlatform;
  status: MobileNotificationPermissionStatus;
};

type NotificationPermissionResponse = {
  canAskAgain?: boolean;
  granted?: boolean;
  status?: string;
};

export async function getMobileNotificationPermissionStatus(): Promise<MobileNotificationPermissionState> {
  const platform = getCurrentNotificationPermissionPlatform();
  if (platform === "unsupported") return createUnsupportedPermissionState();

  try {
    const permission = await Notifications.getPermissionsAsync();
    return normalizeMobileNotificationPermissionStatus(platform, permission);
  } catch {
    return {
      canRequest: false,
      granted: false,
      platform,
      status: "unavailable",
    };
  }
}

export async function requestMobileNotificationPermission(): Promise<MobileNotificationPermissionState> {
  const platform = getCurrentNotificationPermissionPlatform();
  if (platform === "unsupported") return createUnsupportedPermissionState();

  try {
    const permission = await Notifications.requestPermissionsAsync();
    return normalizeMobileNotificationPermissionStatus(platform, permission);
  } catch {
    return {
      canRequest: false,
      granted: false,
      platform,
      status: "unavailable",
    };
  }
}

export async function isMobileNotificationPermissionGranted(): Promise<boolean> {
  const permission = await getMobileNotificationPermissionStatus();
  return permission.granted;
}

export async function openMobileNotificationSettings(): Promise<void> {
  await Linking.openSettings();
}

export function normalizeMobileNotificationPermissionStatus(
  platform: MobileNotificationPermissionPlatform,
  permission: NotificationPermissionResponse,
): MobileNotificationPermissionState {
  const status = normalizePermissionStatus(permission.status);
  const granted = permission.granted === true || status === "granted";
  const canAskAgain = permission.canAskAgain !== false;

  return {
    canRequest: !granted && canAskAgain,
    granted,
    platform,
    status,
  };
}

function getCurrentNotificationPermissionPlatform(): MobileNotificationPermissionPlatform {
  if (Platform.OS === "android" || Platform.OS === "ios") return Platform.OS;
  return "unsupported";
}

function createUnsupportedPermissionState(): MobileNotificationPermissionState {
  return {
    canRequest: false,
    granted: false,
    platform: "unsupported",
    status: "unavailable",
  };
}

function normalizePermissionStatus(status: string | undefined): MobileNotificationPermissionStatus {
  switch (status) {
    case "granted":
      return "granted";
    case "denied":
      return "denied";
    case "undetermined":
      return "undetermined";
    default:
      return "unavailable";
  }
}
