"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import type {
  FocusedNotificationKey,
  InboxMode,
  NotificationGroupKey,
  NotificationPreferences,
} from "@multica/core/types/notification-preference";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";
import { useT } from "../../i18n";
import { SegmentedToggle } from "../../common/segmented-toggle";
import { BrowserNotificationSetting } from "./browser-notification-setting";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

// Inbox event groups rendered in the per-event toggle list. `system_notifications`
// is a sibling preference key but lives in its own section below.
const INBOX_GROUP_KEYS = [
  "assignments",
  "status_changes",
  "comments",
  "mentions",
  "updates",
  "new_issues",
  "agent_activity",
] as const;
type InboxGroupKey = (typeof INBOX_GROUP_KEYS)[number];

const INBOX_MODES = ["all", "focused"] as const satisfies readonly InboxMode[];

// Focused-mode filters, grouped as rendered.
const FOCUSED_REPLY_KEYS = [
  "focused_assigned",
  "focused_blocked",
  "focused_mentioned",
  "focused_thread_reply",
] as const satisfies readonly FocusedNotificationKey[];
const FOCUSED_AGENT_KEYS = [
  "focused_agent_activity",
] as const satisfies readonly FocusedNotificationKey[];

export function NotificationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const mutation = useUpdateNotificationPreferences();

  const preferences = data?.preferences ?? {};

  const save = (updated: NotificationPreferences) => {
    mutation.mutate(updated, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.notifications.toast_failed),
        ),
    });
  };

  const handleToggle = (key: NotificationGroupKey, enabled: boolean) => {
    const updated: NotificationPreferences = {
      ...preferences,
      [key]: enabled ? "all" : "muted",
    };
    // Remove keys set to "all" (default) to keep the object clean
    if (enabled) {
      delete updated[key];
    }
    save(updated);
  };

  const handleModeChange = (mode: InboxMode) => {
    const updated: NotificationPreferences = { ...preferences, inbox_mode: mode };
    if (mode === "all") delete updated.inbox_mode;
    save(updated);
  };

  const handleFocusedToggle = (key: FocusedNotificationKey, enabled: boolean) => {
    const updated: NotificationPreferences = {
      ...preferences,
      [key]: enabled ? "on" : "off",
    };
    if (enabled) delete updated[key];
    save(updated);
  };

  const inboxMode: InboxMode =
    preferences.inbox_mode === "focused" ? "focused" : "all";
  const systemEnabled = preferences.system_notifications !== "muted";
  const modeTitle = t(($) => $.notifications.inbox_mode.title);

  const focusedRow = (key: FocusedNotificationKey) => {
    const label = t(($) => $.notifications.focused[key]);
    return (
      <SettingsRow key={key} anchor={key} label={label}>
        <Checkbox
          checked={preferences[key] !== "off"}
          aria-label={label}
          onCheckedChange={(checked) => handleFocusedToggle(key, checked)}
        />
      </SettingsRow>
    );
  };

  return (
    <SettingsTab title={t(($) => $.page.tabs.notifications)} scope="workspace-only">
      <SettingsSection>
        <SettingsCard>
          <SettingsRow
            anchor="inbox-mode"
            label={modeTitle}
            description={t(($) => $.notifications.inbox_mode.note)}
          >
            <div role="group" aria-label={modeTitle}>
              <SegmentedToggle<InboxMode>
                value={inboxMode}
                onChange={handleModeChange}
                buttonClassName="px-3 py-1 text-label"
                options={INBOX_MODES.map(
                  (mode) => [mode, t(($) => $.notifications.inbox_mode[mode])] as const,
                )}
              />
            </div>
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      {inboxMode === "focused" ? (
        <>
          <SettingsSection title={t(($) => $.notifications.focused.reply_group)}>
            <SettingsCard>{FOCUSED_REPLY_KEYS.map(focusedRow)}</SettingsCard>
          </SettingsSection>
          <SettingsSection title={t(($) => $.notifications.focused.agent_group)}>
            <SettingsCard>{FOCUSED_AGENT_KEYS.map(focusedRow)}</SettingsCard>
          </SettingsSection>
        </>
      ) : (
        <SettingsSection
          title={t(($) => $.notifications.title)}
          description={t(($) => $.notifications.description)}
          anchor="inbox"
        >
          <SettingsCard>
            {INBOX_GROUP_KEYS.map((key: InboxGroupKey) => {
              const enabled = preferences[key] !== "muted";
              return (
                <SettingsRow
                  key={key}
                  anchor={key}
                  label={t(($) => $.notifications.groups[key].label)}
                  description={t(($) => $.notifications.groups[key].description)}
                >
                  <Switch
                    checked={enabled}
                    aria-label={t(($) => $.notifications.groups[key].label)}
                    onCheckedChange={(checked) => handleToggle(key, checked)}
                  />
                </SettingsRow>
              );
            })}
          </SettingsCard>
        </SettingsSection>
      )}

      <SettingsSection
        title={t(($) => $.notifications.system.title)}
        anchor={systemEnabled ? undefined : "chat_replies"}
      >
        <SettingsCard>
          <SettingsRow
            anchor="system"
            label={t(($) => $.notifications.system.label)}
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
