"use client";

import { Fragment, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import {
  useResolveAutoArchivePrompt,
  useUpdateNotificationPreferences,
} from "@multica/core/notification-preferences/mutations";
import { notificationPreferenceValue } from "@multica/core/notification-preferences/patch";
import {
  AUTO_ARCHIVE_PRESETS,
  autoArchiveActionsOn,
  autoArchivePreferences,
  autoArchivePreset,
  type AutoArchivePreset,
} from "@multica/core/notification-preferences/auto-archive-presets";
import type {
  AutoArchiveKey,
  NotificationGroupKey,
  NotificationPreferences,
  OthersIssuesValue,
} from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { buttonVariants } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
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
// each, as the custom choice lists them: those that settle the issue, then
// those that only tidy it.
const AUTO_ARCHIVE_GROUPS = [
  {
    key: "settle",
    actions: [
      { key: "auto_archive_completed", label: "completed" },
      { key: "auto_archive_on_agent_trigger", label: "agent_trigger" },
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

const AUTO_ARCHIVE_PRESET_ORDER: readonly AutoArchivePreset[] = ["never", "completed", "handled"];

export function NotificationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const mutation = useUpdateNotificationPreferences();
  const resolvePrompt = useResolveAutoArchivePrompt();
  const [autoArchiveCustom, setAutoArchiveCustom] = useState(false);

  const preferences = data?.preferences ?? {};

  const onSaveError = (err: unknown) =>
    toast.error(
      err instanceof Error && err.message
        ? err.message
        : t(($) => $.notifications.toast_failed),
    );
  const save = (updated: NotificationPreferences) => {
    mutation.mutate(updated, { onError: onSaveError });
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

  const autoArchiveOn = autoArchiveActionsOn(preferences);
  const autoArchiveChoice: AutoArchivePreset | "custom" = autoArchiveCustom
    ? "custom"
    : autoArchivePreset(preferences);
  const autoArchiveItems: { value: AutoArchivePreset | "custom"; label: string }[] = [
    ...AUTO_ARCHIVE_PRESET_ORDER.map((preset) => ({
      value: preset,
      label: t(($) => $.notifications.leaving.auto_presets[preset]),
    })),
    { value: "custom", label: t(($) => $.notifications.leaving.auto_presets.custom) },
  ];
  // Until the member answers the one-time question, a choice made here is
  // their answer, for every workspace where it is pending.
  const saveAutoArchive = (on: readonly AutoArchiveKey[]) => {
    const values = autoArchivePreferences(on);
    if (data?.auto_archive_prompt) {
      resolvePrompt.mutate(values, { onError: onSaveError });
    } else {
      save({ ...preferences, ...values });
    }
  };
  const handleAutoArchivePreset = (next: AutoArchivePreset | "custom") => {
    setAutoArchiveCustom(next === "custom");
    if (next !== "custom") saveAutoArchive(AUTO_ARCHIVE_PRESETS[next]);
  };
  const handleAutoArchiveAction = (key: AutoArchiveKey, on: boolean) => {
    setAutoArchiveCustom(true);
    saveAutoArchive(on ? [...autoArchiveOn, key] : autoArchiveOn.filter((k) => k !== key));
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
              {category.groups.map((key) => {
                const enabled = notificationPreferenceValue(preferences, key) !== "muted";
                return (
                  <Fragment key={key}>
                    <SettingsRow
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
                    {key === "comments" && enabled && (
                      <SettingsRow
                        anchor="squad_handoffs"
                        className="pl-8"
                        label={t(($) => $.notifications.groups.squad_handoffs.label)}
                        description={t(($) => $.notifications.groups.squad_handoffs.description)}
                      >
                        <Switch
                          checked={notificationPreferenceValue(preferences, "squad_handoffs") !== "muted"}
                          aria-label={t(($) => $.notifications.groups.squad_handoffs.label)}
                          onCheckedChange={(checked) => handleToggle("squad_handoffs", checked)}
                        />
                      </SettingsRow>
                    )}
                  </Fragment>
                );
              })}
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
          <div>
            <SettingsRow
              anchor="auto_archive_completed"
              className={cn(autoArchiveChoice === "custom" && "pb-2")}
              label={t(($) => $.notifications.leaving.auto_label)}
              description={t(($) => $.notifications.leaving.auto_description)}
            >
              <Select
                items={autoArchiveItems}
                value={autoArchiveChoice}
                onValueChange={(next) => next && handleAutoArchivePreset(next as AutoArchivePreset | "custom")}
              >
                <SelectTrigger
                  size="sm"
                  className="w-auto min-w-56"
                  aria-label={t(($) => $.notifications.leaving.auto_label)}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent align="end" alignItemWithTrigger={false} className="w-auto min-w-(--anchor-width)">
                  {AUTO_ARCHIVE_PRESET_ORDER.map((preset) => (
                    <SelectItem key={preset} value={preset}>
                      {t(($) => $.notifications.leaving.auto_presets[preset])}
                      {preset === "handled" && (
                        <Badge variant="secondary">
                          {t(($) => $.notifications.leaving.auto_recommended)}
                        </Badge>
                      )}
                    </SelectItem>
                  ))}
                  <SelectSeparator />
                  <SelectItem value="custom">
                    {t(($) => $.notifications.leaving.auto_presets.custom_item)}
                  </SelectItem>
                </SelectContent>
              </Select>
            </SettingsRow>
            {autoArchiveChoice === "custom" && (
              <div className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-2.5 px-4 pb-3.5">
                {AUTO_ARCHIVE_GROUPS.map((group) => (
                  <Fragment key={group.key}>
                    <div className="text-caption text-muted-foreground">
                      {t(($) => $.notifications.leaving.auto_groups[group.key])}
                    </div>
                    <div className="flex flex-wrap gap-2">
                      {group.actions.map((action) => {
                        const on = autoArchiveOn.includes(action.key);
                        return (
                          <label
                            key={action.key}
                            className={cn(
                              "inline-flex h-7 cursor-pointer items-center gap-2 rounded-md border border-border pr-2.5 pl-2 text-label transition-colors hover:bg-surface-hover",
                              !on && "text-muted-foreground",
                            )}
                          >
                            <Checkbox
                              checked={on}
                              onCheckedChange={(checked) => handleAutoArchiveAction(action.key, checked)}
                            />
                            {t(($) => $.notifications.leaving.auto_actions[action.label])}
                          </label>
                        );
                      })}
                    </div>
                  </Fragment>
                ))}
              </div>
            )}
          </div>
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
