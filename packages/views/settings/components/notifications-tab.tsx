"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useUpdateNotificationPreferences } from "@multica/core/notification-preferences/mutations";
import { channelConnectionListOptions } from "@multica/core/workspace/queries";
import type {
  ChannelEventKey,
  ChannelPreferences,
  NotificationGroupKey,
  NotificationPreferences,
} from "@multica/core/types";
import { isChannelEventEnabled } from "@multica/core/types";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Switch } from "@multica/ui/components/ui/switch";
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

const channelNotificationTypes: {
  key: ChannelEventKey;
}[] = [
  { key: "issues" },
  { key: "comments" },
  { key: "mentions" },
];

function providerLabel(value: string) {
  if (!value) return "Channel";
  return value
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function NotificationsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const { data: connectionsData } = useQuery(channelConnectionListOptions());
  const mutation = useUpdateNotificationPreferences();

  const preferences = data?.preferences ?? {};
  const configuredChannels = (connectionsData?.connections ?? [])
    .filter((connection) => connection.enabled)
    .map((connection) => ({
      id: connection.id,
      provider: connection.provider,
      label: connection.display_name || providerLabel(connection.provider),
    }));
  const preferenceChannels = Object.keys(preferences.channel ?? {}).map((id) => ({
    id,
    provider: id,
    label: providerLabel(id),
  }));
  const channelConnections = configuredChannels.length > 0 ? configuredChannels : preferenceChannels;

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

  const handleChannelToggle = (connectionId: string, key: ChannelEventKey, enabled: boolean) => {
    const currentConnection = preferences.channel?.[connectionId] ?? {};
    const updatedConnection = { ...currentConnection, [key]: enabled };

    // Remove keys set to true (default) to keep the object clean
    if (enabled) {
      delete updatedConnection[key];
    }

    const updatedChannel: ChannelPreferences = {
      ...preferences.channel,
    };
    if (Object.keys(updatedConnection).length > 0) {
      updatedChannel[connectionId] = updatedConnection;
    } else {
      delete updatedChannel[connectionId];
    }

    const updated: NotificationPreferences = { ...preferences };
    if (Object.keys(updatedChannel).length === 0) {
      delete updated.channel;
    } else {
      updated.channel = updatedChannel;
    }

    mutation.mutate(updated, {
      onError: () => toast.error(t(($) => $.notifications.toast_failed)),
    });
  };

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

      {channelConnections.length > 0 ? (
        <section className="space-y-4">
          <div>
            <h2 className="text-sm font-semibold">{t(($) => $.notifications.channel.title)}</h2>
            <p className="text-sm text-muted-foreground mt-1">
              {t(($) => $.notifications.channel.description)}
            </p>
          </div>

          <div className="space-y-3">
            {channelConnections.map((channelConnection) => (
              <Card key={channelConnection.id}>
                <CardContent className="divide-y">
                  <div className="pb-3">
                    <p className="text-sm font-medium">{channelConnection.label}</p>
                    <p className="text-xs text-muted-foreground">
                      {providerLabel(channelConnection.provider)}
                    </p>
                  </div>
                  {channelNotificationTypes.map((type) => {
                    // R4: route through the shared helper rather than inlining
                    // the "missing === enabled" rule, so the UI cannot drift
                    // from the backend default contract.
                    const enabled = isChannelEventEnabled(
                      preferences,
                      channelConnection.id,
                      type.key,
                    );
                    return (
                      <div
                        key={type.key}
                        className="flex items-center justify-between py-3 last:pb-0"
                      >
                        <div className="space-y-0.5 pr-4">
                          <p className="text-sm font-medium">{t(($) => $.notifications.channel.types[type.key].label)}</p>
                          <p className="text-xs text-muted-foreground">
                            {t(($) => $.notifications.channel.types[type.key].description)}
                          </p>
                        </div>
                        <Switch
                          checked={enabled}
                          onCheckedChange={(checked) =>
                            handleChannelToggle(channelConnection.id, type.key, checked)
                          }
                        />
                      </div>
                    );
                  })}
                </CardContent>
              </Card>
            ))}
          </div>
        </section>
      ) : null}
    </div>
  );
}
