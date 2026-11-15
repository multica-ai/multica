"use client";

import { Fragment } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import type { NotificationGroupKey, NotificationPreferences } from "@multica/core/types";
import { buttonVariants } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useT } from "../../i18n";
import { ARCHIVED_VIEW_PARAM } from "../../inbox/components/inbox-view";
import { AppLink } from "../../navigation";
import { BrowserNotificationSetting } from "./browser-notification-setting";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

// Inbox event groups, by how the event reaches you. `system_notifications`
// is a sibling preference key but lives in its own section below.
const INBOX_CATEGORIES = [
  { key: "direct", groups: ["assignments", "mentions"] },
  { key: "issues", groups: ["status_changes", "comments", "updates"] },
  { key: "projects", groups: ["new_issues"] },
  { key: "agents", groups: ["agent_activity"] },
] as const;

export function NotificationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
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

  const systemEnabled = preferences.system_notifications !== "muted";

  return (
    <SettingsTab title={t(($) => $.page.tabs.notifications)} scope="workspace-only">
      <SettingsSection
        title={t(($) => $.notifications.title)}
        description={t(($) => $.notifications.description)}
        anchor="inbox"
      >
        <SettingsCard>
          {INBOX_CATEGORIES.map((category, index) => (
            <Fragment key={category.key}>
              <div
                className={cn(
                  "border-b-0 px-4 pb-1 text-caption font-medium text-muted-foreground",
                  index === 0 ? "pt-3" : "pt-4",
                )}
              >
                {t(($) => $.notifications.categories[category.key])}
              </div>
              {category.groups.map((key) => (
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
              ))}
            </Fragment>
          ))}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.notifications.leaving.title)}
        description={t(($) => $.notifications.leaving.description)}
        anchor="leaving_inbox"
      >
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.notifications.leaving.manual_label)}
            description={t(($) => $.notifications.leaving.manual_description)}
          >
            <span className="text-caption text-muted-foreground">
              {t(($) => $.notifications.leaving.manual_value)}
            </span>
          </SettingsRow>
          <SettingsRow
            label={t(($) => $.notifications.leaving.done_label)}
            description={t(($) => $.notifications.leaving.done_description)}
          >
            <span className="text-caption text-muted-foreground">
              {t(($) => $.notifications.leaving.done_value)}
            </span>
          </SettingsRow>
          <SettingsRow
            anchor="archived"
            label={t(($) => $.notifications.leaving.archived_label)}
            description={t(($) => $.notifications.leaving.archived_description)}
          >
            <AppLink
              href={`${p.inbox()}?${new URLSearchParams({ view: ARCHIVED_VIEW_PARAM })}`}
              className={buttonVariants({ variant: "outline", size: "sm" })}
            >
              {t(($) => $.notifications.leaving.archived_action)}
            </AppLink>
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

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
