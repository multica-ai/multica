"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import {
  NOTIFICATION_ROUTE_KINDS,
  notificationRoutesAreDefault,
  resolveNotificationRoute,
  withDefaultNotificationRoutes,
  withNotificationRoute,
  type NotificationRouteKind,
} from "@multica/core/notification-preferences/routes";
import type { NotificationPreferences, NotificationRoute } from "@multica/core/types";
import { BellOff, Eye, RotateCcw } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
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

const ROUTES: readonly NotificationRoute[] = ["action", "activity", "muted"];

function RouteMark({ route }: { route: NotificationRoute }) {
  if (route === "action") {
    return (
      <span className="flex size-4 items-center justify-center" aria-hidden>
        <span className="size-1.5 rounded-full bg-brand" />
      </span>
    );
  }
  const Icon = route === "activity" ? Eye : BellOff;
  return <Icon aria-hidden className="text-muted-foreground" />;
}

export function NotificationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const mutation = useUpdateNotificationPreferences();

  const preferences = data?.preferences ?? {};

  const save = (next: NotificationPreferences) => {
    mutation.mutate(next, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.notifications.toast_failed),
        ),
    });
  };

  const routeLabel = (route: NotificationRoute) => t(($) => $.notifications.routes[route].label);

  const routeSelect = (kind: NotificationRouteKind) => {
    const route = resolveNotificationRoute(preferences, kind);
    const label = t(($) => $.notifications.kinds[kind.key].label);
    return (
      <SettingsRow
        key={kind.key}
        anchor={kind.key}
        label={label}
        description={t(($) => $.notifications.kinds[kind.key].description)}
        size="select"
      >
        <Select
          items={ROUTES.map((value) => ({ value, label: routeLabel(value) }))}
          value={route}
          onValueChange={(value) => {
            if (value && value !== route) {
              save(withNotificationRoute(preferences, kind, value as NotificationRoute));
            }
          }}
        >
          <SelectTrigger size="sm" className="w-full" aria-label={label}>
            <SelectValue>
              <span className="flex items-center gap-2">
                <RouteMark route={route} />
                <span className={route === "muted" ? "text-muted-foreground" : undefined}>
                  {routeLabel(route)}
                </span>
              </span>
            </SelectValue>
          </SelectTrigger>
          <SelectContent align="end" className="min-w-64">
            {ROUTES.map((value) => (
              <SelectItem key={value} value={value} className="items-start">
                <span className="mt-0.5 flex">
                  <RouteMark route={value} />
                </span>
                <span className="flex flex-col">
                  <span>{routeLabel(value)}</span>
                  <span className="text-caption text-muted-foreground">
                    {t(($) => $.notifications.routes[value].description)}
                  </span>
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </SettingsRow>
    );
  };

  const systemEnabled = preferences.system_notifications !== "muted";

  return (
    <SettingsTab title={t(($) => $.page.tabs.notifications)} scope="workspace-only">
      <SettingsSection
        title={t(($) => $.notifications.title)}
        description={t(($) => $.notifications.description)}
        anchor="inbox"
        action={
          <Button
            variant="ghost"
            size="sm"
            disabled={notificationRoutesAreDefault(preferences)}
            onClick={() => save(withDefaultNotificationRoutes(preferences))}
          >
            <RotateCcw />
            {t(($) => $.notifications.reset)}
          </Button>
        }
      >
        <SettingsCard>{NOTIFICATION_ROUTE_KINDS.map(routeSelect)}</SettingsCard>
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
                onCheckedChange={(checked) => {
                  const next: NotificationPreferences = { ...preferences };
                  if (checked) delete next.system_notifications;
                  else next.system_notifications = "muted";
                  save(next);
                }}
              />
          </SettingsRow>
        </SettingsCard>

        {/* Web-only: the browser permission banners require. Renders nothing on
            desktop (OS-native delivery) or where the Notification API is absent. */}
        <BrowserNotificationSetting />
      </SettingsSection>
    </SettingsTab>
  );
}
