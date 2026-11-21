"use client";

import { Fragment } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import { notificationPreferenceValue } from "@multica/core/notification-preferences/patch";
import type {
  AfterAutoArchiveValue,
  AutoArchiveKey,
  NotificationGroupKey,
  NotificationPreferences,
  OthersIssuesValue,
} from "@multica/core/types";
import { buttonVariants } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
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
  { key: "direct", groups: ["assignments", "mentions", "reactions"] },
  { key: "issues", groups: ["status_changes", "comments", "updates"] },
  { key: "projects", groups: ["new_issues"] },
  { key: "agents", groups: ["agent_activity"] },
] as const;

// The actions of yours that can archive an issue's notifications, one key
// each: those that settle the issue, on by default, then those that only tidy
// it.
const AUTO_ARCHIVE_GROUPS = [
  {
    key: "settle",
    actions: [
      { key: "auto_archive_completed", label: "completed" },
      { key: "auto_archive_on_comment", label: "comment" },
      { key: "auto_archive_on_status", label: "status" },
      { key: "auto_archive_on_assignee", label: "assignee" },
    ],
  },
  {
    key: "tidy",
    actions: [
      { key: "auto_archive_on_priority", label: "priority" },
      { key: "auto_archive_on_project", label: "project" },
      { key: "auto_archive_on_dates", label: "dates" },
      { key: "auto_archive_on_labels", label: "labels" },
    ],
  },
] as const satisfies readonly { key: string; actions: readonly { key: AutoArchiveKey; label: string }[] }[];

type AutoArchiveAction = (typeof AUTO_ARCHIVE_GROUPS)[number]["actions"][number];

const AUTO_ARCHIVE_ACTIONS: readonly AutoArchiveAction[] = AUTO_ARCHIVE_GROUPS.flatMap(
  (group): readonly AutoArchiveAction[] => group.actions,
);

export function NotificationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
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
    save({ ...preferences, [key]: enabled ? "all" : "muted" });
  };

  const handleOthersIssues = (value: OthersIssuesValue) => {
    const updated: NotificationPreferences = { ...preferences, others_issues: value };
    if (value === "key_updates") delete updated.others_issues;
    save(updated);
  };

  const othersIssuesItems: { value: OthersIssuesValue; label: string }[] = [
    { value: "key_updates", label: t(($) => $.notifications.others_issues.key_updates) },
    { value: "all", label: t(($) => $.notifications.others_issues.all) },
  ];

  const autoArchiveItems: { value: AutoArchiveKey; label: string }[] = AUTO_ARCHIVE_ACTIONS.map((action) => ({
    value: action.key,
    label: t(($) => $.notifications.leaving.auto_actions[action.label]),
  }));
  const autoArchiveOn = AUTO_ARCHIVE_ACTIONS.filter(
    (action) => notificationPreferenceValue(preferences, action.key) === "all",
  ).map((action) => action.key);
  const autoArchiveSummary =
    autoArchiveOn.length === 0
      ? t(($) => $.notifications.leaving.auto_never)
      : autoArchiveItems
          .filter((item) => autoArchiveOn.includes(item.value))
          .map((item) => item.label)
          .join(t(($) => $.notifications.leaving.auto_separator));
  const handleAutoArchive = (on: AutoArchiveKey[]) => {
    const updated = { ...preferences };
    for (const action of AUTO_ARCHIVE_ACTIONS) {
      updated[action.key] = on.includes(action.key) ? "all" : "muted";
    }
    save(updated);
  };
  const afterAutoArchiveItems: { value: AfterAutoArchiveValue; label: string }[] = [
    { value: "stay", label: t(($) => $.notifications.leaving.after_stay) },
    { value: "next", label: t(($) => $.notifications.leaving.after_next) },
  ];

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
              {category.key === "issues" && (
                <SettingsRow
                  anchor="others_issues"
                  label={t(($) => $.notifications.others_issues.label)}
                  description={t(($) => $.notifications.others_issues.description)}
                >
                  <Select
                    items={othersIssuesItems}
                    value={notificationPreferenceValue(preferences, "others_issues")}
                    onValueChange={(next) => next && handleOthersIssues(next as OthersIssuesValue)}
                  >
                    <SelectTrigger
                      size="sm"
                      className="w-40"
                      aria-label={t(($) => $.notifications.others_issues.label)}
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent align="end">
                      {othersIssuesItems.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </SettingsRow>
              )}
              {category.groups.map((key) => (
                <SettingsRow
                  key={key}
                  anchor={key}
                  label={t(($) => $.notifications.groups[key].label)}
                  description={t(($) => $.notifications.groups[key].description)}
                >
                  <Switch
                    checked={notificationPreferenceValue(preferences, key) !== "muted"}
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
            anchor="auto_archive_completed"
            label={t(($) => $.notifications.leaving.auto_label)}
            description={t(($) => $.notifications.leaving.auto_description)}
          >
            <Select multiple items={autoArchiveItems} value={autoArchiveOn} onValueChange={handleAutoArchive}>
              <SelectTrigger
                size="sm"
                className="w-56"
                aria-label={t(($) => $.notifications.leaving.auto_label)}
              >
                <SelectValue className="truncate">{() => autoArchiveSummary}</SelectValue>
              </SelectTrigger>
              <SelectContent align="end" alignItemWithTrigger={false}>
                {AUTO_ARCHIVE_GROUPS.map((group, index) => (
                  <Fragment key={group.key}>
                    {index > 0 && <SelectSeparator />}
                    <SelectGroup className="p-0">
                      <SelectLabel>{t(($) => $.notifications.leaving.auto_groups[group.key])}</SelectLabel>
                      {group.actions.map((action) => (
                        <SelectItem key={action.key} value={action.key}>
                          {t(($) => $.notifications.leaving.auto_actions[action.label])}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </Fragment>
                ))}
              </SelectContent>
            </Select>
          </SettingsRow>
          {autoArchiveOn.length > 0 && (
            <SettingsRow
              anchor="after_auto_archive"
              className="pl-8"
              label={t(($) => $.notifications.leaving.after_label)}
              description={t(($) => $.notifications.leaving.after_description)}
            >
              <Select
                items={afterAutoArchiveItems}
                value={notificationPreferenceValue(preferences, "after_auto_archive")}
                onValueChange={(next) =>
                  next && save({ ...preferences, after_auto_archive: next as AfterAutoArchiveValue })
                }
              >
                <SelectTrigger
                  size="sm"
                  className="w-56"
                  aria-label={t(($) => $.notifications.leaving.after_label)}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent align="end">
                  {afterAutoArchiveItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </SettingsRow>
          )}
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
