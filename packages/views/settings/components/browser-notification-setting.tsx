"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ensureWebPushSubscription,
  getWebPushCapability,
  revokeWebPushSubscription,
  type WebPushCapability,
} from "@multica/core/platform";
import {
  useUpsertWebPushSubscription,
  useDeleteWebPushSubscription,
  webPushConfigOptions,
} from "@multica/core/notification-preferences/web-push";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow } from "./settings-layout";

/**
 * Web-only control for the permission + durable PushManager subscription used
 * by the root service worker. Unsupported browsers render no
 * control; foreground window.Notification is intentionally not a fallback.
 *
 * Capability and permission are read from `window`, so the first paint defers
 * to a post-mount effect to keep SSR and client markup identical (no hydration
 * mismatch).
 */
export function BrowserNotificationSetting() {
  const { t } = useT("settings");
  const [mounted, setMounted] = useState(false);
  const [capability, setCapability] =
    useState<WebPushCapability>("unsupported");
  const { data: config } = useQuery(webPushConfigOptions());
  const { mutate: upsertSubscription, mutateAsync: upsertSubscriptionAsync } =
    useUpsertWebPushSubscription();
  const { mutate: deleteSubscription } = useDeleteWebPushSubscription();

  useEffect(() => {
    setMounted(true);
    setCapability(getWebPushCapability());
  }, []);

  useEffect(() => {
    if (!mounted || capability !== "denied") return;
    void revokeWebPushSubscription()
      .then((endpoint) => {
        if (endpoint) deleteSubscription(endpoint);
      })
      .catch(() => undefined);
  }, [capability, deleteSubscription, mounted]);

  useEffect(() => {
    if (!config?.enabled || !config.public_key) return;
    if (getWebPushCapability() !== "available") return;
    void ensureWebPushSubscription(config.public_key, false)
      .then((subscription) => {
        if (subscription) upsertSubscription(subscription);
      })
      .catch(() => undefined);
  }, [config?.enabled, config?.public_key, upsertSubscription]);

  if (!mounted || capability === "unsupported" || !config?.enabled) return null;

  const handleEnable = async () => {
    if (!config.public_key) return;
    try {
      const subscription = await ensureWebPushSubscription(
        config.public_key,
        true,
      );
      setCapability(getWebPushCapability());
      if (subscription) await upsertSubscriptionAsync(subscription);
    } catch {
      // Keep the explicit action available for retry; delivery fails closed.
    }
  };

  const statusHint =
    capability === "available"
      ? t(($) => $.notifications.browser.granted)
      : capability === "denied"
        ? t(($) => $.notifications.browser.denied)
        : t(($) => $.notifications.browser.hint);

  return (
    <SettingsCard>
      <SettingsRow
        label={t(($) => $.notifications.browser.label)}
        description={statusHint}
      >
        {capability === "prompt" && (
          <Button size="sm" variant="outline" onClick={handleEnable}>
            {t(($) => $.notifications.browser.enable)}
          </Button>
        )}
        {capability === "available" && (
          <span className="shrink-0 text-caption font-medium text-muted-foreground">
            {t(($) => $.notifications.browser.enabled_badge)}
          </span>
        )}
      </SettingsRow>
    </SettingsCard>
  );
}
