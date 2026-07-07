/**
 * Notification preferences subscreen. 5 inbox groups + system_notifications
 * toggle, each backed by an optimistic PUT /api/notification-preferences.
 *
 * Copy mirrors packages/views/settings/components/notifications-tab.tsx in
 * meaning (translated via apps/mobile/locales/*.json, not shared code). The
 * group labels MUST stay in sync with web — they describe the same
 * server-side semantics, and divergent labels would violate behavioral
 * parity (apps/mobile/CLAUDE.md).
 */
import { ActivityIndicator, ScrollView, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type {
  NotificationGroupKey,
  NotificationPreferences,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Switch } from "@/components/ui/switch";
import { Separator } from "@/components/ui/separator";
import { useWorkspaceStore } from "@/data/workspace-store";
import { notificationPreferenceOptions } from "@/data/queries/notification-preferences";
import { useUpdateNotificationPreferences } from "@/data/mutations/notification-preferences";

export default function NotificationsSettingsScreen() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data, isLoading, error } = useQuery(
    notificationPreferenceOptions(wsId),
  );
  const mutation = useUpdateNotificationPreferences();

  const { t } = useTranslation("settings");

  const inboxGroups: Array<{
    key: Exclude<NotificationGroupKey, "system_notifications">;
    label: string;
    description: string;
  }> = [
    {
      key: "assignments",
      label: t("notifications.groups.assignments.label"),
      description: t("notifications.groups.assignments.description"),
    },
    {
      key: "status_changes",
      label: t("notifications.groups.status_changes.label"),
      description: t("notifications.groups.status_changes.description"),
    },
    {
      key: "comments",
      label: t("notifications.groups.comments.label"),
      description: t("notifications.groups.comments.description"),
    },
    {
      key: "updates",
      label: t("notifications.groups.updates.label"),
      description: t("notifications.groups.updates.description"),
    },
    {
      key: "agent_activity",
      label: t("notifications.groups.agent_activity.label"),
      description: t("notifications.groups.agent_activity.description"),
    },
  ];

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
          {t("notifications.error")}
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
        title={t("notifications.inbox_section.title")}
        description={t("notifications.inbox_section.description")}
      >
        {inboxGroups.map((group, idx) => {
          const enabled = preferences[group.key] !== "muted";
          const isLast = idx === inboxGroups.length - 1;
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
        title={t("notifications.system_section.title")}
        description={t("notifications.system_section.description")}
      >
        <View className="flex-row items-center px-4 py-3 gap-3">
          <View className="flex-1">
            <Text className="text-base font-medium text-foreground">
              {t("notifications.system_notifications.label")}
            </Text>
            <Text className="text-xs text-muted-foreground mt-0.5">
              {t("notifications.system_notifications.description")}
            </Text>
          </View>
          <Switch
            checked={systemEnabled}
            onCheckedChange={(checked) =>
              onToggle("system_notifications", checked)
            }
          />
        </View>
      </Section>
    </ScrollView>
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
