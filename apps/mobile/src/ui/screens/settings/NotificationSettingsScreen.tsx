import { useMemo } from "react";
import { Alert } from "react-native";
import { useNavigation } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import { notificationPreferenceValue } from "@multica/core/notification-preferences/patch";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import type { NotificationPreferences } from "@multica/core/types";
import { useWorkspace } from "../../model/workspace-context";
import { useStrings } from "../../strings";
import { NOTIFICATION_GROUPS as GROUPS, NotificationSettingsView } from "./NotificationSettingsView";

export function NotificationSettingsScreen() {
  const navigation = useNavigation();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const query = useQuery(notificationPreferenceOptions(workspace.id));
  const update = useUpdateNotificationPreferences();
  const preferences = query.data?.preferences;
  const muted = useMemo(
    () => new Set(GROUPS.filter((group) => notificationPreferenceValue(preferences ?? {}, group) === "muted")),
    [preferences],
  );

  return (
    <NotificationSettingsView
      state={query.data ? "ready" : query.isError ? "error" : "loading"}
      muted={muted}
      onBack={() => navigation.goBack()}
      onRetry={() => void query.refetch()}
      onToggle={(group, enabled) => {
        const next: NotificationPreferences = { ...preferences, [group]: enabled ? "all" : "muted" };
        update.mutate(next, { onError: () => Alert.alert(strings.notifications.failed) });
      }}
    />
  );
}
