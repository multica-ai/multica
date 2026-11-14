"use client";

import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { errorCode } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import type { NotificationGroupKey, NotificationPreferences } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
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
import { toast } from "sonner";
import { useT } from "../../i18n";
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

export function NotificationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
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
