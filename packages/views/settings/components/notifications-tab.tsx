"use client";

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import type { NotificationGroupKey, NotificationPreferences } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
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

// Inbox event groups rendered in the per-event toggle list. `system_notifications`
// is a sibling preference key but lives in its own section below.
const INBOX_GROUP_KEYS = [
  "assignments",
  "status_changes",
  "comments",
  "mentions",
  "updates",
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
    const updated: NotificationPreferences = {
      ...preferences,
      google_chat_webhook: webhook,
    };
    try {
      await mutation.mutateAsync(updated);
      setWebhookInput("");
      setReplacingWebhook(false);
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.notifications.toast_failed),
      );
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
              <div className="space-y-2">
                <Input
                  type="url"
                  value={webhookInput}
                  aria-label={t(
                    ($) => $.notifications.delivery.google_chat.input_label,
                  )}
                  placeholder={t(
                    ($) => $.notifications.delivery.google_chat.placeholder,
                  )}
                  onChange={(event) => setWebhookInput(event.target.value)}
                  disabled={mutation.isPending}
                />
                <p className="text-caption leading-5 text-muted-foreground">
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
                      }}
                      disabled={mutation.isPending}
                    >
                      {t(($) => $.notifications.delivery.google_chat.cancel)}
                    </Button>
                  ) : null}
                  <Button
                    type="button"
                    size="sm"
                    aria-busy={mutation.isPending}
                    onClick={() => handleWebhookUpdate(webhookInput.trim())}
                    disabled={webhookInput.trim() === "" || mutation.isPending}
                  >
                    {t(($) => $.notifications.delivery.google_chat.save)}
                  </Button>
                </div>
              </div>
            ) : (
              <div className="space-y-2">
                <p className="break-all font-mono text-caption text-muted-foreground">
                  {configuredWebhook}
                </p>
                <div className="flex flex-wrap justify-end gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => setReplacingWebhook(true)}
                    disabled={mutation.isPending}
                  >
                    {t(($) => $.notifications.delivery.google_chat.replace)}
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    aria-busy={mutation.isPending}
                    onClick={() => handleWebhookUpdate("")}
                    disabled={mutation.isPending}
                  >
                    {t(($) => $.notifications.delivery.google_chat.remove)}
                  </Button>
                </div>
              </div>
            )}
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}
