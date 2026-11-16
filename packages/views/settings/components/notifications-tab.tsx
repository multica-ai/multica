"use client";

import { Fragment, useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { errorCode } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import { notificationPreferenceValue } from "@multica/core/notification-preferences/patch";
import type {
  NotificationGroupKey,
  NotificationPreferences,
  OthersIssuesValue,
} from "@multica/core/types";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@multica/ui/components/ui/alert-dialog";
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

export function NotificationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const mutation = useUpdateNotificationPreferences();
  const [webhookInput, setWebhookInput] = useState("");
  const [replacingWebhook, setReplacingWebhook] = useState(false);
  const [webhookError, setWebhookError] = useState("");
  const [removeOpen, setRemoveOpen] = useState(false);
  const webhookHelpId = useId();
  const webhookErrorId = useId();

  const preferences = data?.preferences ?? {};
  const configuredWebhook = preferences.google_chat_webhook?.trim() ?? "";

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

  const handleWebhookUpdate = async (webhook: string) => {
    if (mutation.isPending) return;
    setWebhookError("");
    const updated: NotificationPreferences = {
      ...preferences,
      google_chat_webhook: webhook,
    };
    try {
      await mutation.mutateAsync(updated);
      setWebhookInput("");
      setReplacingWebhook(false);
      setRemoveOpen(false);
    } catch (error) {
      switch (errorCode(error)) {
        case "google_chat_invalid_url":
          setWebhookError(t(($) => $.notifications.delivery.google_chat.invalid_url));
          break;
        case "google_chat_missing_credentials":
          setWebhookError(t(($) => $.notifications.delivery.google_chat.missing_credentials));
          break;
        case "google_chat_connection_failed":
          setWebhookError(t(($) => $.notifications.delivery.google_chat.connection_failed));
          break;
        default:
          setWebhookError(t(($) => $.notifications.toast_failed));
      }
    }
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

  const systemEnabled = preferences.system_notifications !== "muted";
  const editingWebhook = configuredWebhook === "" || replacingWebhook;

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
            anchor="auto_archive_completed"
            label={t(($) => $.notifications.leaving.done_label)}
            description={t(($) => $.notifications.leaving.done_description)}
          >
            <Switch
              checked={preferences.auto_archive_completed !== "muted"}
              aria-label={t(($) => $.notifications.leaving.done_label)}
              onCheckedChange={(checked) => handleToggle("auto_archive_completed", checked)}
            />
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

      <SettingsSection
        title={t(($) => $.notifications.delivery.title)}
        anchor="delivery"
      >
        <SettingsCard>
          <SettingsRow
            anchor="google-chat"
            label={t(($) => $.notifications.delivery.google_chat.label)}
            description={t(
              ($) => $.notifications.delivery.google_chat.description,
            )}
            size="text"
            align="start"
          >
            {editingWebhook ? (
              <form
                className="space-y-2"
                noValidate
                onSubmit={(event) => {
                  event.preventDefault();
                  if (webhookInput.trim()) void handleWebhookUpdate(webhookInput.trim());
                }}
              >
                <Input
                  type="url"
                  value={webhookInput}
                  aria-label={t(
                    ($) => $.notifications.delivery.google_chat.input_label,
                  )}
                  placeholder={t(
                    ($) => $.notifications.delivery.google_chat.placeholder,
                  )}
                  aria-invalid={webhookError !== ""}
                  aria-describedby={`${webhookHelpId}${webhookError ? ` ${webhookErrorId}` : ""}`}
                  onChange={(event) => {
                    setWebhookInput(event.target.value);
                    setWebhookError("");
                  }}
                  disabled={mutation.isPending}
                />
                {webhookError && (
                  <p id={webhookErrorId} role="alert" className="text-caption text-destructive">
                    {webhookError}
                  </p>
                )}
                <p id={webhookHelpId} className="text-caption leading-5 text-muted-foreground">
                  {t(($) => $.notifications.delivery.google_chat.how_to)}
                </p>
                <div className="flex flex-wrap justify-end gap-2">
                  {configuredWebhook !== "" ? (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setWebhookInput("");
                        setReplacingWebhook(false);
                        setWebhookError("");
                      }}
                      disabled={mutation.isPending}
                    >
                      {t(($) => $.notifications.delivery.google_chat.cancel)}
                    </Button>
                  ) : null}
                  <Button
                    type="submit"
                    size="sm"
                    aria-busy={mutation.isPending}
                    disabled={webhookInput.trim() === "" || mutation.isPending}
                  >
                    {mutation.isPending && <Spinner aria-hidden="true" />}
                    {mutation.isPending
                      ? t(($) => $.notifications.delivery.google_chat.connecting)
                      : t(($) => $.notifications.delivery.google_chat.save)}
                  </Button>
                </div>
              </form>
            ) : (
              <div className="space-y-2">
                <p className="break-all text-caption text-muted-foreground">
                  {t(($) => $.notifications.delivery.google_chat.connected, {
                    space: configuredWebhook.split("/spaces/")[1]?.split("/")[0] ?? "…",
                  })}
                </p>
                <div className="flex flex-wrap justify-end gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      setWebhookError("");
                      setReplacingWebhook(true);
                    }}
                    disabled={mutation.isPending}
                  >
                    {t(($) => $.notifications.delivery.google_chat.replace)}
                  </Button>
                  <AlertDialog
                    open={removeOpen}
                    onOpenChange={(open) => {
                      if (!mutation.isPending) {
                        setRemoveOpen(open);
                        setWebhookError("");
                      }
                    }}
                  >
                    <AlertDialogTrigger render={<Button type="button" size="sm" variant="ghost" disabled={mutation.isPending} />}>
                      {t(($) => $.notifications.delivery.google_chat.remove)}
                    </AlertDialogTrigger>
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>{t(($) => $.notifications.delivery.google_chat.remove_title)}</AlertDialogTitle>
                        <AlertDialogDescription>{t(($) => $.notifications.delivery.google_chat.remove_description)}</AlertDialogDescription>
                      </AlertDialogHeader>
                      {webhookError && <p role="alert" className="text-caption text-destructive">{webhookError}</p>}
                      <AlertDialogFooter>
                        <AlertDialogCancel disabled={mutation.isPending}>{t(($) => $.notifications.delivery.google_chat.cancel)}</AlertDialogCancel>
                        <Button variant="destructive" disabled={mutation.isPending} aria-busy={mutation.isPending} onClick={() => handleWebhookUpdate("")}>
                          {t(($) => $.notifications.delivery.google_chat.remove)}
                        </Button>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                </div>
              </div>
            )}
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}
