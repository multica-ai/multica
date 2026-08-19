"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import {
  CONTENT_NOTIFICATION_GROUPS,
  notificationContentGroupState,
  notificationDeliveryEnabled,
  setNotificationContentGroup,
  setNotificationDelivery,
} from "@multica/core/notification-preferences";
import type { NotificationPreferences } from "@multica/core/types";
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

export function NotificationsTab() {
  const { t } = useT("settings");
  const { t: inboxT } = useT("inbox");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const mutation = useUpdateNotificationPreferences();

  const preferences = data?.preferences ?? {};

  const save = (updated: NotificationPreferences) => {
    mutation.mutate(updated, {
      onSuccess: () =>
        toast.success(
          t(($) => $.auto_save.toast_saved),
          {
            id: "settings-auto-save",
          },
        ),
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.notifications.toast_failed),
        ),
    });
  };

  const deliveryEnabled = notificationDeliveryEnabled(preferences);

  return (
    <SettingsTab title={t(($) => $.page.tabs.notifications)}>
      <SettingsSection
        title={t(($) => $.notifications.title)}
        description={t(($) => $.notifications.description)}
      >
        <SettingsCard>
          {CONTENT_NOTIFICATION_GROUPS.map((group) => {
            const state = notificationContentGroupState(preferences, group.key);
            return (
              <SettingsRow
                key={group.key}
                label={inboxT(($) => $.preferences.groups[group.key].label)}
                description={inboxT(
                  ($) => $.preferences.groups[group.key].description,
                )}
              >
                <Switch
                  checked={state !== "muted"}
                  aria-label={inboxT(
                    ($) => $.preferences.groups[group.key].label,
                  )}
                  onCheckedChange={(checked) =>
                    save(
                      setNotificationContentGroup(
                        preferences,
                        group.key,
                        checked,
                      ),
                    )
                  }
                />
              </SettingsRow>
            );
          })}
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={inboxT(($) => $.preferences.delivery.title)}
        description={inboxT(($) => $.preferences.delivery.description)}
      >
        <SettingsCard>
          <SettingsRow
            label={inboxT(($) => $.preferences.delivery.label)}
            description={inboxT(($) => $.preferences.delivery.hint)}
          >
            <Switch
              checked={deliveryEnabled}
              aria-label={inboxT(($) => $.preferences.delivery.label)}
              onCheckedChange={(checked) =>
                save(setNotificationDelivery(preferences, checked))
              }
            />
          </SettingsRow>
        </SettingsCard>

        {/* Web-only: permission and the durable background Push subscription.
            Renders nothing where service-worker Push is unavailable. */}
        <BrowserNotificationSetting />
      </SettingsSection>
    </SettingsTab>
  );
}
