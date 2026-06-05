/**
 * Notification preferences subscreen. 5 inbox groups + system_notifications
 * toggle, each backed by an optimistic PUT /api/notification-preferences.
 *
 * Copy mirrors packages/views/settings/components/notifications-tab.tsx but
 * hardcoded English (mobile has no i18n infra yet). The group labels MUST
 * stay in sync with web — they describe the same server-side semantics,
 * and divergent labels would violate behavioral parity (apps/mobile/CLAUDE.md).
 */
import { ActivityIndicator, ScrollView, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type {
  NotificationGroupKey,
  NotificationPreferences,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { Button } from "@/components/ui/button";
import { useWorkspaceStore } from "@/data/workspace-store";
import { notificationPreferenceOptions } from "@/data/queries/notification-preferences";
import { useUpdateNotificationPreferences } from "@/data/mutations/notification-preferences";
import {
  useMobilePushActions,
  useMobilePushStatus,
} from "@/lib/mobile-push";

const INBOX_GROUPS: Array<{
  key: Exclude<NotificationGroupKey, "system_notifications">;
  label: string;
  description: string;
}> = [
  {
    key: "assignments",
    label: "Assignments",
    description: "When you're assigned an issue or removed as assignee.",
  },
  {
    key: "status_changes",
    label: "Status changes",
    description: "When an issue's status changes.",
  },
  {
    key: "comments",
    label: "Comments",
    description: "New comments on issues you're subscribed to.",
  },
  {
    key: "updates",
    label: "Issue updates",
    description: "Edits to title, description, labels, priority, or due date.",
  },
  {
    key: "agent_activity",
    label: "Agent activity",
    description: "When an agent picks up, runs, or completes a task.",
  },
];

export default function NotificationsSettingsScreen() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data, isLoading, error } = useQuery(
    notificationPreferenceOptions(wsId),
  );
  const mutation = useUpdateNotificationPreferences();
  const pushStatus = useMobilePushStatus();
  const pushActions = useMobilePushActions();

  const preferences: NotificationPreferences = data?.preferences ?? {};

  const onToggle = (key: NotificationGroupKey, enabled: boolean) => {
    const next: NotificationPreferences = { ...preferences };
    if (enabled) {
      // Default is "all" — omitting the key keeps the object clean.
      delete next[key];
    } else {
      next[key] = "muted";
    }
    mutation.mutate(next);
  };

  const systemEnabled = preferences.system_notifications !== "muted";
  const canUseSystemNotifications =
    pushStatus.permission === "granted" && systemEnabled;

  if (isLoading) {
    return (
      <View className="flex-1 items-center justify-center bg-background">
        <ActivityIndicator />
      </View>
    );
  }

  if (error) {
    return (
      <View className="flex-1 items-center justify-center bg-background px-6">
        <Text className="text-sm text-destructive text-center">
          Failed to load notification preferences.
        </Text>
      </View>
    );
  }

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="px-4 py-4 gap-6"
    >
      <Section
        title="Inbox notifications"
        description="Which events show up in your inbox."
      >
        {INBOX_GROUPS.map((group, idx) => {
          const enabled = preferences[group.key] !== "muted";
          const isLast = idx === INBOX_GROUPS.length - 1;
          return (
            <View key={group.key}>
              <View className="flex-row items-center px-4 py-3 gap-3">
                <View className="flex-1">
                  <Text className="text-base font-medium text-foreground">
                    {group.label}
                  </Text>
                  <Text className="text-xs text-muted-foreground mt-0.5">
                    {group.description}
                  </Text>
                </View>
                <Switch
                  checked={enabled}
                  onCheckedChange={(checked) => onToggle(group.key, checked)}
                />
              </View>
              {!isLast ? <Separator /> : null}
            </View>
          );
        })}
      </Section>

      <Section
        title="System push"
        description="Lock screen and background notifications for important inbox items."
      >
        <View className="px-4 py-3 gap-3">
          <View className="flex-row items-center gap-3">
            <View className="flex-1">
              <Text className="text-base font-medium text-foreground">
                Push notifications
              </Text>
              <Text className="text-xs text-muted-foreground mt-0.5">
                Assignments, mentions, comments, and agent status updates.
              </Text>
            </View>
            <Switch
              checked={systemEnabled}
              onCheckedChange={(checked) =>
                onToggle("system_notifications", checked)
              }
            />
          </View>
          <PermissionStatus
            enabled={canUseSystemNotifications}
            permission={pushStatus.permission}
            error={pushStatus.error}
            registering={pushStatus.registering}
            onRequest={pushActions.request}
            onOpenSettings={pushActions.openSettings}
          />
        </View>
      </Section>
    </ScrollView>
  );
}

function PermissionStatus({
  enabled,
  permission,
  error,
  registering,
  onRequest,
  onOpenSettings,
}: {
  enabled: boolean;
  permission: string;
  error: string | null;
  registering: boolean;
  onRequest: () => void;
  onOpenSettings: () => void;
}) {
  const message =
    permission === "granted"
      ? enabled
        ? "iOS push is enabled for this workspace."
        : "iOS permission is enabled. Turn on the switch to receive push."
      : permission === "denied"
        ? "iOS notification permission is off. Re-enable it in Settings."
        : permission === "undetermined"
          ? "Allow notifications to receive lock screen updates."
          : permission === "unsupported"
            ? "Push notifications are available on iOS builds."
            : "Checking notification permission.";

  return (
    <View className="gap-2">
      <Text className="text-xs text-muted-foreground">
        {error ?? message}
      </Text>
      {permission === "undetermined" || permission === "error" ? (
        <Button variant="outline" onPress={onRequest} disabled={registering}>
          <Text>{registering ? "Enabling..." : "Enable notifications"}</Text>
        </Button>
      ) : null}
      {permission === "denied" ? (
        <Button variant="outline" onPress={onOpenSettings}>
          <Text>Open iOS Settings</Text>
        </Button>
      ) : null}
    </View>
  );
}

function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <View className="gap-2">
      <View className="px-1">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground">
          {title}
        </Text>
        {description ? (
          <Text className="text-xs text-muted-foreground mt-1">
            {description}
          </Text>
        ) : null}
      </View>
      <View className="rounded-md border border-border bg-card overflow-hidden">
        {children}
      </View>
    </View>
  );
}
