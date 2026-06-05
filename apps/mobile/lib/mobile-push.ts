import { useCallback, useEffect, useSyncExternalStore } from "react";
import { AppState, Linking, Platform } from "react-native";
import Constants from "expo-constants";
import * as Notifications from "expo-notifications";
import { router } from "expo-router";
import { api, type MobilePushTokenRequest } from "@/data/api";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";

type PushPermissionState =
  | "unknown"
  | "unsupported"
  | "granted"
  | "denied"
  | "undetermined"
  | "error";

interface PushState {
  permission: PushPermissionState;
  token: string | null;
  error: string | null;
  registering: boolean;
}

const state: PushState = {
  permission: "unknown",
  token: null,
  error: null,
  registering: false,
};

const listeners = new Set<() => void>();

function emit() {
  for (const listener of listeners) listener();
}

function patchState(next: Partial<PushState>) {
  Object.assign(state, next);
  emit();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function snapshot() {
  return state;
}

Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldPlaySound: false,
    shouldSetBadge: true,
    shouldShowBanner: true,
    shouldShowList: true,
  }),
});

export function useMobilePushStatus() {
  return useSyncExternalStore(subscribe, snapshot, snapshot);
}

export function useMobilePushActions() {
  const refresh = useCallback(() => refreshPushPermissionStatus(), []);
  const request = useCallback(() => registerForMobilePush({ requestPermission: true }), []);
  const openSettings = useCallback(() => Linking.openSettings(), []);
  return { refresh, request, openSettings };
}

export function useMobilePushLifecycle() {
  const user = useAuthStore((s) => s.user);
  const workspaceId = useWorkspaceStore((s) => s.currentWorkspaceId);

  useEffect(() => {
    void refreshPushPermissionStatus();
  }, []);

  useEffect(() => {
    if (!user || !workspaceId) return;
    void registerForMobilePush({ requestPermission: true });
  }, [user, workspaceId]);

  useEffect(() => {
    const subscription = AppState.addEventListener("change", (next) => {
      if (next === "active") {
        void refreshPushPermissionStatus();
        if (useAuthStore.getState().user && useWorkspaceStore.getState().currentWorkspaceId) {
          void registerForMobilePush({ requestPermission: false });
        }
      }
    });
    return () => subscription.remove();
  }, []);

  useEffect(() => {
    const responseSubscription = Notifications.addNotificationResponseReceivedListener((response) => {
      void routeFromNotificationData(response.notification.request.content.data);
    });
    void Notifications.getLastNotificationResponseAsync().then((response) => {
      if (response) {
        void routeFromNotificationData(response.notification.request.content.data);
      }
    });
    return () => responseSubscription.remove();
  }, []);
}

export async function unregisterCurrentMobilePushToken() {
  if (!state.token) return;
  if (!useWorkspaceStore.getState().currentWorkspaceId) return;
  try {
    await api.unregisterMobilePushToken(buildTokenRequest(state.token));
  } catch (err) {
    console.warn("[push] unregister failed", err);
  } finally {
    patchState({ token: null });
  }
}

async function refreshPushPermissionStatus() {
  if (Platform.OS !== "ios") {
    patchState({ permission: "unsupported" });
    return;
  }
  try {
    const permissions = await Notifications.getPermissionsAsync();
    patchState({ permission: permissionState(permissions), error: null });
  } catch (err) {
    patchState({
      permission: "error",
      error: err instanceof Error ? err.message : "Unable to read notification permission.",
    });
  }
}

async function registerForMobilePush(opts: { requestPermission: boolean }) {
  if (Platform.OS !== "ios" || state.registering) return;
  if (!useAuthStore.getState().user || !useWorkspaceStore.getState().currentWorkspaceId) return;

  patchState({ registering: true, error: null });
  try {
    let permissions = await Notifications.getPermissionsAsync();
    if (!permissionGranted(permissions) && opts.requestPermission && permissions.status === "undetermined") {
      permissions = await Notifications.requestPermissionsAsync();
    }
    const nextPermission = permissionState(permissions);
    if (!permissionGranted(permissions)) {
      patchState({ permission: nextPermission, registering: false });
      return;
    }

    const projectId = Constants.expoConfig?.extra?.eas?.projectId;
    const token = (await Notifications.getExpoPushTokenAsync(projectId ? { projectId } : undefined)).data;
    await api.registerMobilePushToken(buildTokenRequest(token));
    patchState({ permission: "granted", token, registering: false, error: null });
  } catch (err) {
    patchState({
      permission: "error",
      registering: false,
      error: err instanceof Error ? err.message : "Unable to register push notifications.",
    });
  }
}

function buildTokenRequest(token: string): MobilePushTokenRequest {
  return {
    provider: "expo",
    token,
    device_id: null,
    platform: "ios",
    app_version: Constants.expoConfig?.version ?? null,
    environment: String(Constants.expoConfig?.extra?.APP_ENV ?? "development"),
  };
}

function permissionGranted(permissions: Notifications.NotificationPermissionsStatus) {
  return (
    permissions.granted ||
    permissions.ios?.status === Notifications.IosAuthorizationStatus.PROVISIONAL ||
    permissions.ios?.status === Notifications.IosAuthorizationStatus.AUTHORIZED
  );
}

function permissionState(permissions: Notifications.NotificationPermissionsStatus): PushPermissionState {
  if (permissionGranted(permissions)) return "granted";
  if (permissions.status === "denied") return "denied";
  if (permissions.status === "undetermined") return "undetermined";
  return "unknown";
}

async function routeFromNotificationData(data: Notifications.NotificationContent["data"]) {
  if (!data) return;
  const issueId = typeof data.issue_id === "string" ? data.issue_id : "";
  const workspaceId = typeof data.workspace_id === "string" ? data.workspace_id : "";
  if (!issueId || !workspaceId) return;

  try {
    const workspaces = await api.listWorkspaces();
    const workspace = workspaces.find((w) => w.id === workspaceId);
    if (!workspace) {
      router.replace("/select-workspace");
      return;
    }
    await useWorkspaceStore.getState().setCurrentWorkspace(workspace.id, workspace.slug);
    const commentId = typeof data.comment_id === "string" ? data.comment_id : "";
    const suffix = commentId ? `?h=${encodeURIComponent(commentId)}` : "";
    router.push(`/${workspace.slug}/issue/${issueId}${suffix}`);
  } catch (err) {
    console.warn("[push] route failed", err);
    router.replace("/select-workspace");
  }
}
