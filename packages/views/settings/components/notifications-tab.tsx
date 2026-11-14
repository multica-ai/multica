"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import type { NotificationGroupKey, NotificationPreferences } from "@multica/core/types";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";
import { useT } from "../../i18n";
import { BrowserNotificationSetting } from "./browser-notification-setting";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

// What is addressed to the user: always delivered, so these rows have no
// switch. The Inbox files them under "Needs you" (MUL-9215).
const ALWAYS_ON_KEYS = ["mentions", "assignments", "runs", "your_issues"] as const;

// Activity on what the user only follows, filed under "Updates". Each kind can
// be switched off. `system_notifications` is a sibling preference key but
// lives in its own section below.
const UPDATE_GROUP_KEYS = [
  "comments",
  "status_changes",
  "assignments",
  "updates",
  "new_issues",
  "agent_activity",
] as const;
type SwitchGroupKey = (typeof UPDATE_GROUP_KEYS)[number] | "mentions";

export function NotificationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const mutation = useUpdateNotificationPreferences();

  const preferences = data?.preferences ?? {};

  const handleToggle = (key: NotificationGroupKey, enabled: boolean) => {
    const updated: NotificationPreferences = {
      ...preferences,
      [key]: enabled ? "all" : "muted",
    };
    // Remove keys set to "all" (default) to keep the object clean
    if (enabled) {
      delete updated[key];
    }
    mutation.mutate(updated, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.notifications.toast_failed),
        ),
    });
  };

  const groupSwitch = (key: SwitchGroupKey) => (
    <SettingsRow
      key={key}
      anchor={key}
      label={t(($) => $.notifications.groups[key].label)}
      description={t(($) => $.notifications.groups[key].description)}
    >
      <Switch
        checked={preferences[key] !== "muted"}
        aria-label={t(($) => $.notifications.groups[key].label)}
        onCheckedChange={(checked) => handleToggle(key, checked)}
      />
    </SettingsRow>
  );

  const systemEnabled = preferences.system_notifications !== "muted";

  return (
    <SettingsTab title={t(($) => $.page.tabs.notifications)} scope="workspace-only">
      <SettingsSection
        title={t(($) => $.notifications.needs_you.title)}
        description={t(($) => $.notifications.needs_you.description)}
        anchor="inbox"
      >
        <SettingsCard>
          {ALWAYS_ON_KEYS.map((key) => (
            <SettingsRow
              key={key}
              label={t(($) => $.notifications.needs_you[key].label)}
              description={t(($) => $.notifications.needs_you[key].description)}
            >
              <span className="shrink-0 text-caption text-muted-foreground">
                {t(($) => $.notifications.needs_you.always_on)}
              </span>
            </SettingsRow>
          ))}
          {/* The one choice here: a mention of a group is addressed to everyone
              in it, so the user decides whether that counts as theirs. */}
          {groupSwitch("mentions")}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.notifications.updates.title)}
        description={t(($) => $.notifications.updates.description)}
      >
        <SettingsCard>{UPDATE_GROUP_KEYS.map(groupSwitch)}</SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.notifications.system.title)}
        anchor={systemEnabled ? undefined : "chat_replies"}
      >
        <SettingsCard>
          <SettingsRow
            anchor="system"
            label={t(($) => $.notifications.system.label)}
            description={t(($) => $.notifications.system.description)}
          >
            <Switch
              checked={systemEnabled}
              aria-label={t(($) => $.notifications.system.label)}
              onCheckedChange={(checked) => handleToggle("system_notifications", checked)}
            />
          </SettingsRow>
          {systemEnabled && (
            <SettingsRow
              anchor="chat_replies"
              className="pl-8"
              label={t(($) => $.notifications.groups.chat_replies.label)}
            >
              <Switch
                checked={preferences.chat_replies !== "muted"}
                aria-label={t(($) => $.notifications.groups.chat_replies.label)}
                onCheckedChange={(checked) => handleToggle("chat_replies", checked)}
              />
            </SettingsRow>
          )}
        </SettingsCard>

        {/* Web-only: the browser permission banners require. Renders nothing on
            desktop (OS-native delivery) or where the Notification API is absent. */}
        <BrowserNotificationSetting />
      </SettingsSection>
    </SettingsTab>
  );
}
