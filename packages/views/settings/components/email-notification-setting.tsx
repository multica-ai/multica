"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useConfigStore } from "@multica/core/config";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow } from "./settings-layout";

/**
 * Channel-level toggle for email notifications. Mirrors the
 * BrowserNotificationSetting shape: single row, status hint, and either a
 * toggle (when the server has SMTP/Resend configured) or an Unavailable
 * badge.
 *
 * Email is strictly downstream of inbox notifications — muting "Comments &
 * Mentions" in the Inbox section also suppresses the corresponding emails,
 * because no inbox row → no EventInboxNew → no email.
 */
export function EmailNotificationSetting() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();

  const emailConfigured = useConfigStore((s) => s.emailConfigured);

  const { data: prefsData } = useQuery(notificationPreferenceOptions(wsId));
  const mutation = useUpdateNotificationPreferences();

  const preferences = prefsData?.preferences ?? {};
  const enabled = preferences.email_notifications !== "muted";

  const handleToggle = (checked: boolean) => {
    const updated = { ...preferences };
    if (checked) {
      delete updated.email_notifications;
    } else {
      updated.email_notifications = "muted";
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

  const hint = emailConfigured
    ? t(($) => $.notifications.email.hint_enabled)
    : t(($) => $.notifications.email.hint_unavailable);

  return (
    <SettingsCard>
      <SettingsRow
        label={t(($) => $.notifications.email.label)}
        description={hint}
      >
        {emailConfigured ? (
          <Switch
            checked={enabled}
            aria-label={t(($) => $.notifications.email.label)}
            onCheckedChange={handleToggle}
          />
        ) : (
          <span className="shrink-0 text-xs font-medium text-muted-foreground">
            {t(($) => $.notifications.email.unavailable_badge)}
          </span>
        )}
      </SettingsRow>
    </SettingsCard>
  );
}
