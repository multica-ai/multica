"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { BellRing, Loader2, Send, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type {
  ExternalAccountBinding,
  NotificationChannel,
  NotificationChannelPreference,
  NotificationEventType,
  NotificationWebhook,
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
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";

const channelLabels: Record<NotificationChannel, string> = {
  inbox: "Inbox",
  dingtalk: "DingTalk",
  email: "Email",
  custom_webhook: "Custom Webhook",
};

const channelDescriptions: Record<NotificationChannel, string> = {
  inbox: "In-app notification delivered through the existing Inbox and websocket flow.",
  dingtalk: "External notification sent to your linked DingTalk account once that channel is enabled.",
  email: "Email notification sent to your linked email address when you are mentioned.",
  custom_webhook: "POST @ mentions, assignments, and subscribed issue updates to your own webhook endpoint.",
};

const customWebhookEvents: NotificationEventType[] = [
  "mentioned",
  "issue_assigned",
  "subscribed_issue_updated",
];

const webhookTemplatePlaceholder = `{
  "msgtype": "text",
  "text": {
    "content": "{{content}}"
  }
}`;

function preferenceKey(pref: NotificationChannelPreference) {
  return `${pref.channel}:${pref.event_type}`;
}

export function NotificationsTab() {
  const [bindings, setBindings] = useState<ExternalAccountBinding[]>([]);
  const [webhooks, setWebhooks] = useState<NotificationWebhook[]>([]);
  const [preferences, setPreferences] = useState<NotificationChannelPreference[]>([]);
  const [loading, setLoading] = useState(true);
  const [savingKey, setSavingKey] = useState<string | null>(null);
  const [busyWebhookId, setBusyWebhookId] = useState<string | null>(null);
  const [webhookName, setWebhookName] = useState("");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [webhookContentPrefix, setWebhookContentPrefix] = useState("");
  const [webhookPayloadTemplate, setWebhookPayloadTemplate] = useState("");
  const [creatingWebhook, setCreatingWebhook] = useState(false);

  const loadSettings = useCallback(async () => {
    setLoading(true);
    try {
      const [bindingsResp, preferencesResp, webhooksResp] = await Promise.all([
        api.listNotificationBindings(),
        api.listNotificationPreferences(),
        api.listNotificationWebhooks(),
      ]);
      setBindings(bindingsResp.bindings);
      setPreferences(preferencesResp.preferences);
      setWebhooks(webhooksResp.webhooks);
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

  const handleToggleCustomWebhook = async (enabled: boolean) => {
    const key = "custom_webhook";
    const previous = preferences;
    const targets = customWebhookEvents.map((eventType) => {
      const existing = preferences.find(
        (pref) => pref.channel === "custom_webhook" && pref.event_type === eventType,
      );
      return (
        existing ?? {
          channel: "custom_webhook" as const,
          event_type: eventType,
          enabled: false,
          binding_id: null,
          requires_binding: false,
        }
      );
    });

    setSavingKey(key);
    setPreferences((current) =>
      current.map((candidate) =>
        candidate.channel === "custom_webhook" ? { ...candidate, enabled } : candidate,
      ),
    );

    try {
      const updated = await Promise.all(
        targets.map((pref) =>
          api.updateNotificationPreference({
            channel: pref.channel,
            event_type: pref.event_type,
            enabled,
          }),
        ),
      );
      const updatedByKey = new Map(updated.map((pref) => [preferenceKey(pref), pref]));
      setPreferences((current) =>
        current.map((candidate) => updatedByKey.get(preferenceKey(candidate)) ?? candidate),
      );
    } catch (err) {
      setPreferences(previous);
      toast.error(err instanceof Error ? err.message : "Failed to update notification preference");
    } finally {
      setSavingKey(null);
    }
  };

  const handleCreateWebhook = async () => {
    setCreatingWebhook(true);
    try {
      const created = await api.createNotificationWebhook({
        name: webhookName.trim() || "Custom webhook",
        url: webhookUrl.trim(),
        content_prefix: webhookContentPrefix,
        payload_template: webhookPayloadTemplate.trim() || undefined,
      });
      setWebhooks((current) => [...current, created]);
      setWebhookName("");
      setWebhookUrl("");
      setWebhookContentPrefix("");
      setWebhookPayloadTemplate("");
      toast.success("Webhook saved");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save webhook");
    } finally {
      setCreatingWebhook(false);
    }
  };

  const handleTestWebhook = async (id: string) => {
    setBusyWebhookId(id);
    try {
      await api.testNotificationWebhook(id);
      toast.success("Test sent");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to send test");
    } finally {
      setBusyWebhookId(null);
    }
  };

  const handleDeleteWebhook = async (id: string) => {
    setBusyWebhookId(id);
    try {
      await api.deleteNotificationWebhook(id);
      setWebhooks((current) => current.filter((webhook) => webhook.id !== id));
      toast.success("Webhook deleted");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete webhook");
    } finally {
      setBusyWebhookId(null);
    }
  };

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
            Choose delivery channels for issue events that need your attention.
          </p>
        </div>

        <Alert>
          <BellRing className="h-4 w-4" />
          <AlertTitle>External delivery</AlertTitle>
          <AlertDescription>
            DingTalk remains available. Custom webhooks add a second delivery target for your own tools.
          </AlertDescription>
        </Alert>
      </section>

      <section className="space-y-4">
        <h3 className="text-sm font-semibold">Custom Webhooks</h3>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Webhook endpoints</CardTitle>
            <CardDescription>
              Multica can send its default JSON or render notification text into your JSON template.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 md:grid-cols-[minmax(120px,180px)_1fr_auto]">
              <div className="space-y-2">
                <Label htmlFor="webhook-name">Name</Label>
                <Input
                  id="webhook-name"
                  value={webhookName}
                  onChange={(event) => setWebhookName(event.target.value)}
                  placeholder="GTD"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="webhook-url">URL</Label>
                <Input
                  id="webhook-url"
                  value={webhookUrl}
                  onChange={(event) => setWebhookUrl(event.target.value)}
                  placeholder="https://example.com/multica/webhook"
                />
              </div>
              <Button
                className="self-end"
                disabled={!webhookUrl.trim() || creatingWebhook}
                onClick={() => void handleCreateWebhook()}
              >
                {creatingWebhook ? <Loader2 className="h-4 w-4 animate-spin" /> : "Add"}
              </Button>
            </div>
            <div className="grid gap-3 md:grid-cols-[minmax(160px,240px)_1fr]">
              <div className="space-y-2">
                <Label htmlFor="webhook-content-prefix">Content prefix</Label>
                <Input
                  id="webhook-content-prefix"
                  value={webhookContentPrefix}
                  onChange={(event) => setWebhookContentPrefix(event.target.value)}
                  placeholder="[Multica] "
                  maxLength={512}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="webhook-payload-template">Payload JSON template</Label>
                <Textarea
                  id="webhook-payload-template"
                  value={webhookPayloadTemplate}
                  onChange={(event) => setWebhookPayloadTemplate(event.target.value)}
                  placeholder={webhookTemplatePlaceholder}
                  className="min-h-28 font-mono text-xs"
                />
                <p className="text-xs text-muted-foreground">
                  Optional. Include <span className="font-mono">{"{{content}}"}</span> where Multica should place the formatted notification text.
                </p>
              </div>
            </div>

            {webhooks.length === 0 ? (
              <div className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
                No custom webhook configured.
              </div>
            ) : (
              <div className="space-y-3">
                {webhooks.map((webhook) => (
                  <div key={webhook.id} className="flex items-center justify-between gap-4 rounded-lg border p-4">
                    <div className="min-w-0 space-y-1">
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{webhook.name}</span>
                        <Badge variant={webhook.enabled ? "secondary" : "outline"}>
                          {webhook.enabled ? "active" : "disabled"}
                        </Badge>
                      </div>
                      <p className="truncate text-sm text-muted-foreground">{webhook.masked_url}</p>
                      {webhook.content_prefix ? (
                        <p className="truncate text-xs text-muted-foreground">
                          Prefix: {webhook.content_prefix}
                        </p>
                      ) : null}
                    </div>
                    <div className="flex items-center gap-2">
                      {busyWebhookId === webhook.id ? (
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                      ) : null}
                      <Button
                        variant="outline"
                        size="icon-sm"
                        disabled={busyWebhookId !== null}
                        onClick={() => void handleTestWebhook(webhook.id)}
                        aria-label={`Test ${webhook.name}`}
                      >
                        <Send className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        disabled={busyWebhookId !== null}
                        onClick={() => void handleDeleteWebhook(webhook.id)}
                        aria-label={`Delete ${webhook.name}`}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-4">
        <h3 className="text-sm font-semibold">Channels</h3>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Delivery channels</CardTitle>
            <CardDescription>
              Enable notification delivery targets.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {preferences
              .filter((pref) => pref.channel !== "custom_webhook")
              .map((pref) => {
                const binding = bindingByProvider.get(pref.channel);
                const needsBinding = pref.requires_binding && !binding;
                const key = preferenceKey(pref);

                return (
                  <div key={key} className="flex items-start justify-between gap-4 rounded-lg border p-4">
                    <div className="space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
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
                          Link your account from Profile → Linked Accounts before enabling this channel.
                        </p>
                      ) : null}
                    </div>
                    <div className="flex items-center gap-3 pt-1">
                      {savingKey === key ? (
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                      ) : null}
                      <Switch
                        checked={pref.enabled}
                        disabled={savingKey !== null || needsBinding}
                        onCheckedChange={(checked) => {
                          void handleToggle(pref, checked);
                        }}
                        aria-label={`Toggle ${channelLabels[pref.channel]}`}
                      />
                    </div>
                  </div>
                );
              })}
            {(() => {
              const customWebhookPrefs = preferences.filter(
                (pref) => pref.channel === "custom_webhook",
              );
              const needsWebhook = webhooks.length === 0;
              const enabled =
                customWebhookPrefs.length > 0 && customWebhookPrefs.every((pref) => pref.enabled);

              return (
                <div className="flex items-start justify-between gap-4 rounded-lg border p-4">
                  <div className="space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-medium">{channelLabels.custom_webhook}</span>
                    </div>
                    <p className="text-sm text-muted-foreground">
                      {channelDescriptions.custom_webhook}
                    </p>
                    {needsWebhook ? (
                      <p className="text-xs text-muted-foreground">
                        Add a webhook endpoint before enabling this channel.
                      </p>
                    ) : null}
                  </div>
                  <div className="flex items-center gap-3 pt-1">
                    {savingKey === "custom_webhook" ? (
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                    ) : null}
                    <Switch
                      checked={enabled}
                      disabled={savingKey !== null || needsWebhook}
                      onCheckedChange={(checked) => {
                        void handleToggleCustomWebhook(checked);
                      }}
                      aria-label="Toggle Custom Webhook"
                    />
                  </div>
                </div>
              );
            })()}
          </CardContent>
        </Card>
        <p className="text-sm text-muted-foreground">
          Manage DingTalk and email account linking from <span className="font-medium">Profile → Linked Accounts</span>.
        </p>
      </section>
    </div>
  );
}
