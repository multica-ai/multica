"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import type { NotificationGroupKey, NotificationPreferences } from "@multica/core/types";
import {
  detectWebNotificationSupport,
  isDesktopApp,
  requestWebNotificationPermission,
  showSystemNotification,
  type WebNotificationSupport,
} from "@multica/core/notifications";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Switch } from "@multica/ui/components/ui/switch";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { useT } from "../../i18n";

// Inbox event groups rendered in the per-event toggle list. `system_notifications`
// is a sibling preference key but lives in its own section below.
const INBOX_GROUP_KEYS = [
  "assignments",
  "status_changes",
  "comments",
  "updates",
  "agent_activity",
] as const;
type InboxGroupKey = (typeof INBOX_GROUP_KEYS)[number];

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
      onError: () => toast.error(t(($) => $.notifications.toast_failed)),
    });
  };

  const systemEnabled = preferences.system_notifications !== "muted";

  // Browser permission state — desktop app handles notifications natively
  // through the main process, so this UI only shows for the web app.
  const desktop = isDesktopApp();
  const [support, setSupport] = useState<WebNotificationSupport>(() =>
    desktop ? "supported" : detectWebNotificationSupport(),
  );

  // Re-check permission on mount and when the page is re-shown (the user may
  // change browser-level permission in another tab, or grant via the URL bar).
  useEffect(() => {
    if (desktop) return;
    const refresh = () => setSupport(detectWebNotificationSupport());
    refresh();
    document.addEventListener("visibilitychange", refresh);
    return () => document.removeEventListener("visibilitychange", refresh);
  }, [desktop]);

  const handleSystemToggle = async (enabled: boolean) => {
    if (enabled && !desktop) {
      // Permission requests must originate from a user gesture; doing it
      // here (synchronously inside the click handler) keeps that contract.
      const result = await requestWebNotificationPermission();
      setSupport(detectWebNotificationSupport());
      if (result === "denied") {
        toast.error(t(($) => $.notifications.system.permission_denied_toast));
      } else if (result === "unsupported") {
        toast.error(t(($) => $.notifications.system.unsupported_toast));
      }
    }
    handleToggle("system_notifications", enabled);
  };

  const handleTest = () => {
    showSystemNotification({
      slug: "",
      itemId: "test",
      issueKey: "test",
      title: t(($) => $.notifications.system.test_title),
      body: t(($) => $.notifications.system.test_body),
      inboxPath: "/",
    });
  };

  const showPermissionHint = !desktop && systemEnabled && support !== "supported";

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">{t(($) => $.notifications.title)}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t(($) => $.notifications.description)}
          </p>
        </div>

        <Card>
          <CardContent className="divide-y">
            {INBOX_GROUP_KEYS.map((key: InboxGroupKey) => {
              const enabled = preferences[key] !== "muted";
              return (
                <div
                  key={key}
                  className="flex items-center justify-between py-3 first:pt-0 last:pb-0"
                >
                  <div className="space-y-0.5 pr-4">
                    <p className="text-sm font-medium">{t(($) => $.notifications.groups[key].label)}</p>
                    <p className="text-xs text-muted-foreground">
                      {t(($) => $.notifications.groups[key].description)}
                    </p>
                  </div>
                  <Switch
                    checked={enabled}
                    onCheckedChange={(checked) => handleToggle(key, checked)}
                  />
                </div>
              );
            })}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">{t(($) => $.notifications.system.title)}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t(($) => $.notifications.system.description)}
          </p>
        </div>

        <Card>
          <CardContent className="space-y-3">
            <div className="flex items-center justify-between">
              <div className="space-y-0.5 pr-4">
                <p className="text-sm font-medium">{t(($) => $.notifications.system.label)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.notifications.system.hint)}
                </p>
              </div>
              <Switch
                checked={systemEnabled}
                onCheckedChange={handleSystemToggle}
              />
            </div>

            {showPermissionHint && (
              <div className="rounded-md border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-900/40 dark:bg-amber-950/30 dark:text-amber-200">
                {support === "permission_denied" && (
                  <p>{t(($) => $.notifications.system.permission_denied_hint)}</p>
                )}
                {support === "permission_default" && (
                  <p>{t(($) => $.notifications.system.permission_default_hint)}</p>
                )}
                {support === "api_unavailable" && (
                  <p>{t(($) => $.notifications.system.api_unavailable_hint)}</p>
                )}
              </div>
            )}

            {systemEnabled && support === "supported" && (
              <div className="flex justify-end">
                <Button variant="outline" size="sm" onClick={handleTest}>
                  {t(($) => $.notifications.system.test_button)}
                </Button>
              </div>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
