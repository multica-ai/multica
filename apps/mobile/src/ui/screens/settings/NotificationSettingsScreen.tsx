import { useMemo } from "react";
import { Alert } from "react-native";
import { useNavigation } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import type { NotificationGroupKey, NotificationPreferences } from "@multica/core/types";
import { useWorkspace } from "../../model/workspace-context";
import { useStrings } from "../../strings";
import { NotificationSettingsView } from "./NotificationSettingsView";

export function NotificationSettingsScreen() {
  const navigation = useNavigation();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const query = useQuery(notificationPreferenceOptions(workspace.id));
  const update = useUpdateNotificationPreferences();
  const preferences = query.data?.preferences;
  const muted = useMemo(
    () => new Set(Object.entries(preferences ?? {}).flatMap(([group, value]) => (value === "muted" ? [group as NotificationGroupKey] : []))),
    [preferences],
  );

  return (
    <NotificationSettingsView
      state={query.data ? "ready" : query.isError ? "error" : "loading"}
      muted={muted}
      onBack={() => navigation.goBack()}
      onRetry={() => void query.refetch()}
      onToggle={(group, enabled) => {
        // "all" is the default, so an enabled group is left out of the stored preferences.
        const next: NotificationPreferences = { ...preferences };
        if (enabled) delete next[group];
        else next[group] = "muted";
        update.mutate(next, { onError: () => Alert.alert(strings.notifications.failed) });
      }}
    />
  );
}
