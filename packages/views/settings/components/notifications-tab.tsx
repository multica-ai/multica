"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@wallts/core/hooks";
import { notificationPreferenceOptions } from "@wallts/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@wallts/core/notification-preferences/mutations";
import type { NotificationGroupKey, NotificationPreferences } from "@wallts/core/types";
import {
  isPushSupported,
  getNotificationPermission,
  requestNotificationPermission,
  getExistingSubscription,
} from "@wallts/core/push-notifications/subscription";
import {
  vapidPublicKeyOptions,
  pushSubscriptionOptions,
} from "@wallts/core/push-notifications/queries";
import {
  useSubscribePushNotifications,
  useUnsubscribePushNotifications,
} from "@wallts/core/push-notifications/mutations";
import { Card, CardContent } from "@wallts/ui/components/ui/card";
import { Switch } from "@wallts/ui/components/ui/switch";
import { toast } from "sonner";
import { useT } from "../../i18n";
import { useState, useEffect, useCallback } from "react";

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
          <CardContent>
            <div className="flex items-center justify-between">
              <div className="space-y-0.5 pr-4">
                <p className="text-sm font-medium">{t(($) => $.notifications.system.label)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.notifications.system.hint)}
                </p>
              </div>
              <Switch
                checked={systemEnabled}
                onCheckedChange={(checked) => handleToggle("system_notifications", checked)}
              />
            </div>
          </CardContent>
        </Card>
      </section>

      <BrowserPushSection />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Browser Push Notifications section
// ---------------------------------------------------------------------------

/**
 * Sub-component rendered inside NotificationsTab.
 *
 * Toggles browser push notifications (Web Push API). When enabled, the
 * browser displays native OS notifications for mentions, assignments,
 * agent completions, and CI failures even when the Wallts tab is not
 * focused.
 *
 * Visibility:
 *   - Hidden entirely when the browser does not support Push API
 *     (Safari < 16, in-app webviews, etc.).
 *   - Shows a warning state when the OS-level permission is "denied"
 *     (user must fix it in browser settings).
 */
function BrowserPushSection() {
  const { t } = useT("settings");
  const { data: vapidData } = useQuery(vapidPublicKeyOptions());
  const { data: subsData } = useQuery(pushSubscriptionOptions());
  const subscribeMutation = useSubscribePushNotifications();
  const unsubscribeMutation = useUnsubscribePushNotifications();

  // The opt-in state is derived from three signals:
  //   1. Browser permission (the user may have revoked it in OS settings).
  //   2. Whether the current device has an active PushSubscription.
  //   3. Whether the server has this device's subscription on file.
  // We use a local `enabled` state as the optimistic UI toggle and
  // reconcile it with reality on every render.
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);

  // Derive the effective subscription state on mount and when the
  // server data changes.
  useEffect(() => {
    if (!isPushSupported()) {
      setLoading(false);
      return;
    }

    let cancelled = false;

    (async () => {
      try {
        const permission = getNotificationPermission();
        if (permission !== "granted") {
          if (!cancelled) {
            setEnabled(false);
            setLoading(false);
          }
          return;
        }

        const localSub = await getExistingSubscription();
        const serverSubs = subsData?.subscriptions ?? [];
        const isKnown =
          localSub &&
          serverSubs.some((s) => s.endpoint === localSub.endpoint);

        if (!cancelled) {
          setEnabled(Boolean(isKnown));
          setLoading(false);
        }
      } catch {
        if (!cancelled) {
          setEnabled(false);
          setLoading(false);
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [subsData]);

  const handleToggle = useCallback(
    async (checked: boolean) => {
      if (checked) {
        // Ask for OS permission first, then subscribe.
        const permission = await requestNotificationPermission();
        if (permission !== "granted") {
          toast.error(t(($) => $.notifications.push.permission_denied));
          return;
        }

        const vapidKey = vapidData?.public_key;
        if (!vapidKey) {
          toast.error(t(($) => $.notifications.push.vapid_not_configured));
          return;
        }

        setEnabled(true); // optimistic
        subscribeMutation.mutate(vapidKey, {
          onError: () => {
            setEnabled(false);
            toast.error(t(($) => $.notifications.push.subscribe_failed));
          },
          onSuccess: () => {
            toast.success(t(($) => $.notifications.push.enabled));
          },
        });
      } else {
        setEnabled(false); // optimistic
        unsubscribeMutation.mutate(undefined, {
          onError: () => {
            setEnabled(true);
            toast.error(t(($) => $.notifications.push.unsubscribe_failed));
          },
          onSuccess: () => {
            toast.success(t(($) => $.notifications.push.disabled));
          },
        });
      }
    },
    [vapidData, subscribeMutation, unsubscribeMutation, t],
  );

  // Entire section hidden when push is unsupported.
  if (!isPushSupported()) return null;

  const deviceCount = subsData?.subscriptions.length ?? 0;
  const permissionDenied = getNotificationPermission() === "denied";

  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-sm font-semibold">
          {t(($) => $.notifications.push.title)}
        </h2>
        <p className="text-sm text-muted-foreground mt-1">
          {t(($) => $.notifications.push.description)}
        </p>
      </div>

      <Card>
        <CardContent>
          <div className="flex items-center justify-between">
            <div className="space-y-0.5 pr-4">
              <p className="text-sm font-medium">
                {t(($) => $.notifications.push.label)}
              </p>
              <p className="text-xs text-muted-foreground">
                {permissionDenied
                  ? t(($) => $.notifications.push.browser_blocked_hint)
                  : deviceCount > 0
                    ? t(($) => $.notifications.push.device_count, {
                        count: deviceCount,
                      })
                    : t(($) => $.notifications.push.hint)}
              </p>
            </div>
            <Switch
              checked={enabled}
              disabled={loading || permissionDenied}
              onCheckedChange={handleToggle}
            />
          </div>
        </CardContent>
      </Card>
    </section>
  );
}
