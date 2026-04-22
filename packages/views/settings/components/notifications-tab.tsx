"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { BellRing, Link2Off, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type {
  ExternalAccountBinding,
  NotificationChannel,
  NotificationChannelPreference,
} from "@multica/core/types";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Switch } from "@multica/ui/components/ui/switch";

const channelLabels: Record<NotificationChannel, string> = {
  inbox: "Inbox",
  dingtalk: "DingTalk",
};

const channelDescriptions: Record<NotificationChannel, string> = {
  inbox: "In-app notification delivered through the existing Inbox and websocket flow.",
  dingtalk: "External notification sent to your linked DingTalk account once that channel is enabled.",
};

function preferenceKey(pref: NotificationChannelPreference) {
  return `${pref.channel}:${pref.event_type}`;
}

export function NotificationsTab() {
  const [bindings, setBindings] = useState<ExternalAccountBinding[]>([]);
  const [preferences, setPreferences] = useState<NotificationChannelPreference[]>([]);
  const [loading, setLoading] = useState(true);
  const [savingKey, setSavingKey] = useState<string | null>(null);
  const [removingBindingId, setRemovingBindingId] = useState<string | null>(null);

  const loadSettings = useCallback(async () => {
    setLoading(true);
    try {
      const [bindingsResp, preferencesResp] = await Promise.all([
        api.listNotificationBindings(),
        api.listNotificationPreferences(),
      ]);
      setBindings(bindingsResp.bindings);
      setPreferences(preferencesResp.preferences);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load notification settings");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadSettings();
  }, [loadSettings]);

  const bindingByProvider = useMemo(() => {
    const next = new Map<string, ExternalAccountBinding>();
    for (const binding of bindings) next.set(binding.provider, binding);
    return next;
  }, [bindings]);

  const handleToggle = async (pref: NotificationChannelPreference, enabled: boolean) => {
    const key = preferenceKey(pref);
    const previous = preferences;
    setSavingKey(key);
    setPreferences((current) =>
      current.map((candidate) =>
        preferenceKey(candidate) === key ? { ...candidate, enabled } : candidate,
      ),
    );

    try {
      const updated = await api.updateNotificationPreference({
        channel: pref.channel,
        event_type: pref.event_type,
        enabled,
      });
      setPreferences((current) =>
        current.map((candidate) =>
          preferenceKey(candidate) === key ? updated : candidate,
        ),
      );
    } catch (err) {
      setPreferences(previous);
      toast.error(err instanceof Error ? err.message : "Failed to update notification preference");
    } finally {
      setSavingKey(null);
    }
  };

  const handleDisconnect = async (binding: ExternalAccountBinding) => {
    setRemovingBindingId(binding.id);
    try {
      await api.deleteNotificationBinding(binding.id);
      toast.success(`${channelLabels[binding.provider as NotificationChannel] ?? binding.provider} disconnected`);
      await loadSettings();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to disconnect account");
    } finally {
      setRemovingBindingId(null);
    }
  };

  const dingTalkBinding = bindingByProvider.get("dingtalk");

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="space-y-1">
          <h2 className="text-sm font-semibold">Notifications</h2>
          <p className="text-sm text-muted-foreground">
            Phase 1 only wires <code>mentioned</code> events. Inbox stays first-class; external channels layer on top.
          </p>
        </div>

        <Alert>
          <BellRing className="h-4 w-4" />
          <AlertTitle>Current scope</AlertTitle>
          <AlertDescription>
            DingTalk preference persistence is live here. The account linking callback flow is the next implementation step.
          </AlertDescription>
        </Alert>
      </section>

      <section className="space-y-4">
        <h3 className="text-sm font-semibold">Channels</h3>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">When you are mentioned</CardTitle>
            <CardDescription>
              Choose which channels should receive issue and comment mentions.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {preferences.map((pref) => {
              const binding = bindingByProvider.get(pref.channel);
              const needsBinding = pref.requires_binding && !binding;
              const key = preferenceKey(pref);

              return (
                <div key={key} className="flex items-start justify-between gap-4 rounded-lg border p-4">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{channelLabels[pref.channel]}</span>
                      {binding ? (
                        <Badge variant="secondary">{binding.status}</Badge>
                      ) : pref.requires_binding ? (
                        <Badge variant="outline">not connected</Badge>
                      ) : null}
                    </div>
                    <p className="text-sm text-muted-foreground">
                      {channelDescriptions[pref.channel]}
                    </p>
                    {needsBinding ? (
                      <p className="text-xs text-muted-foreground">
                        Link a DingTalk account before enabling this channel.
                      </p>
                    ) : null}
                  </div>
                  <div className="flex items-center gap-3 pt-1">
                    {savingKey === key ? (
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                    ) : null}
                    <Switch
                      checked={pref.enabled}
                      disabled={savingKey !== null || removingBindingId !== null || needsBinding}
                      onCheckedChange={(checked) => {
                        void handleToggle(pref, checked);
                      }}
                      aria-label={`Toggle ${channelLabels[pref.channel]} mentions`}
                    />
                  </div>
                </div>
              );
            })}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-4">
        <h3 className="text-sm font-semibold">Linked Accounts</h3>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">DingTalk</CardTitle>
            <CardDescription>
              The linking entrypoint is the next phase. This tab already reflects binding state and disconnect behavior.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-4">
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <span className="font-medium">
                  {dingTalkBinding?.display_name ?? "No DingTalk account connected"}
                </span>
                <Badge variant={dingTalkBinding ? "secondary" : "outline"}>
                  {dingTalkBinding?.status ?? "not connected"}
                </Badge>
              </div>
              <p className="text-sm text-muted-foreground">
                {dingTalkBinding
                  ? `External user: ${dingTalkBinding.external_user_id}`
                  : "Once the OAuth callback flow lands, this section will show the linked DingTalk identity."}
              </p>
            </div>

            {dingTalkBinding ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  void handleDisconnect(dingTalkBinding);
                }}
                disabled={savingKey !== null || removingBindingId !== null}
              >
                {removingBindingId === dingTalkBinding.id ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Link2Off className="h-4 w-4" />
                )}
                Disconnect
              </Button>
            ) : null}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
