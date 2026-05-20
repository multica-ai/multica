"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { BellRing, Loader2, MessageCircle, Send, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type {
  ExternalAccountBinding,
  NotificationChannel,
  NotificationChannelPreference,
  NotificationEventType,
  NotificationRenderMode,
  NotificationWebhook,
} from "@multica/core/types";
import type { AgentRuntime } from "@multica/core/types";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";

const channelLabels: Record<NotificationChannel, string> = {
  notification_trigger: "Notification Triggers",
  inbox: "Inbox",
  dingtalk: "DingTalk",
  email: "Email",
  custom_webhook: "Custom Webhook",
  openclaw_weixin: "微信（OpenClaw）",
};

const channelDescriptions: Record<NotificationChannel, string> = {
  notification_trigger: "Notification scenes that can fan out to enabled delivery channels.",
  inbox: "In-app notification delivered through the existing Inbox and websocket flow.",
  dingtalk: "External notification sent to your linked DingTalk account once that channel is enabled.",
  email: "Email notification sent to your linked email address when you are mentioned.",
  custom_webhook: "POST @ mentions, assignments, and subscribed issue updates to your own webhook endpoint.",
  openclaw_weixin: "通过 OpenClaw 本地助手将通知发送到你的微信。需要在线的 OpenClaw Runtime。",
};

const triggerEvents: NotificationEventType[] = [
  "mentioned",
  "replied",
  "issue_assigned",
  "subscribed_issue_updated",
  "task_completed",
  "task_failed",
];

const triggerEventLabels: Record<NotificationEventType, string> = {
  channel_enabled: "渠道已开启",
  mentioned: "被 @提及时",
  replied: "被回复时",
  issue_assigned: "被分配 Issue 时",
  subscribed_issue_updated: "订阅的 Issue 更新时",
  task_completed: "Agent 任务完成时",
  task_failed: "Agent 任务失败时",
};

const deliveryChannels: Exclude<NotificationChannel, "notification_trigger">[] = [
  "inbox",
  "dingtalk",
  "email",
  "custom_webhook",
  "openclaw_weixin",
];

const channelEvents: Record<Exclude<NotificationChannel, "notification_trigger">, NotificationEventType[]> = {
  inbox: ["mentioned"],
  dingtalk: ["mentioned", "task_completed", "task_failed"],
  email: ["mentioned"],
  custom_webhook: ["mentioned", "issue_assigned", "subscribed_issue_updated"],
  openclaw_weixin: ["mentioned", "replied", "task_completed", "task_failed"],
};

const renderModeLabels: Record<NotificationRenderMode, string> = {
  auto: "智能模式",
  compact: "始终简洁",
  detail: "始终详细",
};

const renderModeDescriptions: Record<NotificationRenderMode, string> = {
  auto: "根据内容长度和结构自动选择简洁或详细",
  compact: "只发送一行摘要和链接",
  detail: "发送完整通知内容",
};

const webhookTemplatePlaceholder = `{
  "msgtype": "text",
  "text": {
    "content": "{{content}}"
  }
}`;

function preferenceKey(pref: NotificationChannelPreference) {
  return `${pref.channel}:${pref.event_type}`;
}

function preferenceTupleKey(channel: NotificationChannel, eventType: NotificationEventType) {
  return `${channel}:${eventType}`;
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

  // OpenClaw WeChat state
  const [runtimes, setRuntimes] = useState<AgentRuntime[]>([]);
  const [weixinId, setWeixinId] = useState("");
  const [bindingWeixin, setBindingWeixin] = useState(false);

  const hasOpenclawRuntime = useMemo(() => {
    return runtimes.some(
      (rt) => rt.provider === "openclaw" && rt.status === "online",
    );
  }, [runtimes]);

  const weixinBinding = useMemo(() => {
    return bindings.find((b) => b.provider === "openclaw_weixin" && b.status === "active");
  }, [bindings]);

  const loadSettings = useCallback(async () => {
    setLoading(true);
    try {
      const [bindingsResp, preferencesResp, webhooksResp, runtimesList] = await Promise.all([
        api.listNotificationBindings(),
        api.listNotificationPreferences(),
        api.listNotificationWebhooks(),
        api.listRuntimes({ owner: "me" }),
      ]);
      setBindings(bindingsResp.bindings);
      setPreferences(preferencesResp.preferences);
      setWebhooks(webhooksResp.webhooks);
      setRuntimes(runtimesList);
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

  const preferenceByKey = useMemo(() => {
    const next = new Map<string, NotificationChannelPreference>();
    for (const pref of preferences) {
      next.set(preferenceKey(pref), pref);
    }
    return next;
  }, [preferences]);

  const updatePreferencesByKey = useCallback((updated: NotificationChannelPreference[]) => {
    const updatedByKey = new Map(updated.map((pref) => [preferenceKey(pref), pref]));
    setPreferences((current) =>
      current.map((candidate) => updatedByKey.get(preferenceKey(candidate)) ?? candidate),
    );
  }, []);

  const getPreference = useCallback(
    (channel: NotificationChannel, eventType: NotificationEventType) => {
      return preferenceByKey.get(preferenceTupleKey(channel, eventType));
    },
    [preferenceByKey],
  );

  const isTriggerEnabled = useCallback(
    (eventType: NotificationEventType) => {
      const pref = getPreference("notification_trigger", eventType);
      return Boolean(pref?.enabled || deliveryChannels.some((channel) =>
        channelEvents[channel].includes(eventType) && getPreference(channel, eventType)?.enabled,
      ));
    },
    [getPreference],
  );

  const isChannelEnabled = useCallback(
    (channel: Exclude<NotificationChannel, "notification_trigger">) => {
      const pref = getPreference(channel, "channel_enabled");
      return Boolean(pref?.enabled || channelEvents[channel].some((eventType) => getPreference(channel, eventType)?.enabled));
    },
    [getPreference],
  );

  const channelNeedsSetup = useCallback(
    (channel: Exclude<NotificationChannel, "notification_trigger">) => {
      if (channel === "custom_webhook") return webhooks.length === 0;

      const channelPref = getPreference(channel, "channel_enabled");
      if (channelPref?.requires_binding && !bindingByProvider.get(channel)) return true;

      return channelEvents[channel].some((eventType) => {
        const pref = getPreference(channel, eventType);
        return Boolean(pref?.requires_binding && !bindingByProvider.get(channel));
      });
    },
    [bindingByProvider, getPreference, webhooks.length],
  );

  const supportedActiveTargets = useCallback(
    (eventType: NotificationEventType) => {
      return deliveryChannels.filter(
        (channel) =>
          channelEvents[channel].includes(eventType) &&
          isChannelEnabled(channel) &&
          !channelNeedsSetup(channel) &&
          Boolean(getPreference(channel, eventType)),
      );
    },
    [channelNeedsSetup, getPreference, isChannelEnabled],
  );

  const channelBadgeText = useCallback(
    (channel: Exclude<NotificationChannel, "notification_trigger">) => {
      if (channel === "custom_webhook") {
        return webhooks.length > 0 ? `${webhooks.length} endpoint${webhooks.length > 1 ? "s" : ""}` : "not configured";
      }
      return bindingByProvider.get(channel)?.status ?? (channelNeedsSetup(channel) ? "not connected" : null);
    },
    [bindingByProvider, channelNeedsSetup, webhooks.length],
  );

  const syncNotificationPreferenceMatrix = async (
    triggerState: Map<NotificationEventType, boolean>,
    channelState: Map<Exclude<NotificationChannel, "notification_trigger">, boolean>,
  ) => {
    const updates: Promise<NotificationChannelPreference>[] = [];

    for (const eventType of triggerEvents) {
      const triggerPref = getPreference("notification_trigger", eventType);
      if (triggerPref && triggerPref.enabled !== triggerState.get(eventType)) {
        updates.push(
          api.updateNotificationPreference({
            channel: "notification_trigger",
            event_type: eventType,
            enabled: triggerState.get(eventType) ?? false,
          }),
        );
      }
    }

    for (const channel of deliveryChannels) {
      const channelPref = getPreference(channel, "channel_enabled");
      if (channelPref && channelPref.enabled !== channelState.get(channel)) {
        updates.push(
          api.updateNotificationPreference({
            channel,
            event_type: "channel_enabled",
            enabled: channelState.get(channel) ?? false,
          }),
        );
      }

      const activeChannel = channelState.get(channel) ?? false;
      const ready = !channelNeedsSetup(channel);
      for (const eventType of channelEvents[channel]) {
        const pref = getPreference(channel, eventType);
        if (!pref) continue;

        const nextEnabled = activeChannel && ready && (triggerState.get(eventType) ?? false);
        if (pref.enabled !== nextEnabled) {
          updates.push(
            api.updateNotificationPreference({
              channel,
              event_type: eventType,
              enabled: nextEnabled,
            }),
          );
        }
      }
    }

    if (updates.length === 0) return [];
    return Promise.all(updates);
  };

  const handleTriggerToggle = async (eventType: NotificationEventType, enabled: boolean) => {
    const key = `notification_trigger:${eventType}`;
    const previous = preferences;
    const triggerState = new Map(
      triggerEvents.map((triggerEvent) => [
        triggerEvent,
        triggerEvent === eventType ? enabled : isTriggerEnabled(triggerEvent),
      ]),
    );
    const channelState = new Map(
      deliveryChannels.map((channel) => [channel, isChannelEnabled(channel)]),
    );

    setSavingKey(key);
    setPreferences((current) =>
      current.map((candidate) => {
        if (candidate.channel === "notification_trigger" && candidate.event_type === eventType) {
          return { ...candidate, enabled };
        }
        if (
          candidate.channel !== "notification_trigger" &&
          candidate.event_type !== "channel_enabled" &&
          candidate.event_type === eventType
        ) {
          const channel = candidate.channel as Exclude<NotificationChannel, "notification_trigger">;
          return {
            ...candidate,
            enabled: enabled && isChannelEnabled(channel) && !channelNeedsSetup(channel),
          };
        }
        return candidate;
      }),
    );

    try {
      const updated = await syncNotificationPreferenceMatrix(triggerState, channelState);
      updatePreferencesByKey(updated);
    } catch (err) {
      setPreferences(previous);
      toast.error(err instanceof Error ? err.message : "Failed to update notification triggers");
    } finally {
      setSavingKey(null);
    }
  };

  const handleChannelToggle = async (
    channel: Exclude<NotificationChannel, "notification_trigger">,
    enabled: boolean,
  ) => {
    const key = `${channel}:channel_enabled`;
    const previous = preferences;
    const triggerState = new Map(
      triggerEvents.map((eventType) => [eventType, isTriggerEnabled(eventType)]),
    );
    const channelState = new Map(
      deliveryChannels.map((candidate) => [
        candidate,
        candidate === channel ? enabled : isChannelEnabled(candidate),
      ]),
    );

    setSavingKey(key);
    setPreferences((current) =>
      current.map((candidate) => {
        if (candidate.channel !== channel) return candidate;
        if (candidate.event_type === "channel_enabled") return { ...candidate, enabled };
        if (!channelEvents[channel].includes(candidate.event_type)) {
          return candidate;
        }
        return {
          ...candidate,
          enabled: enabled && !channelNeedsSetup(channel) && isTriggerEnabled(candidate.event_type),
        };
      }),
    );

    try {
      const updated = await syncNotificationPreferenceMatrix(triggerState, channelState);
      updatePreferencesByKey(updated);
    } catch (err) {
      setPreferences(previous);
      toast.error(err instanceof Error ? err.message : "Failed to update notification channel");
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

  const handleBindWeixin = async () => {
    const id = weixinId.trim();
    if (!id) return;
    setBindingWeixin(true);
    try {
      const resp = await api.bindOpenclawWeixin({ wechat_id: id });
      setBindings((current) => {
        const filtered = current.filter((b) => b.provider !== "openclaw_weixin");
        return [...filtered, {
          id: resp.id,
          provider: resp.provider,
          external_user_id: resp.external_user_id,
          display_name: resp.display_name,
          status: resp.status as "active",
          metadata: resp.metadata,
          created_at: resp.created_at,
          updated_at: resp.updated_at,
        }];
      });
      setWeixinId("");
      toast.success("微信绑定成功");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "绑定失败");
    } finally {
      setBindingWeixin(false);
    }
  };

  const handleRenderModeChange = async (channel: NotificationChannel, mode: NotificationRenderMode) => {
    // Update render_mode for all event types under this channel
    const channelPrefs = preferences.filter((p) => p.channel === channel);
    if (channelPrefs.length === 0) return;

    const saveKey = `${channel}:render_mode`;
    const previous = preferences;
    setSavingKey(saveKey);

    // Optimistic update
    setPreferences((current) =>
      current.map((p) =>
        p.channel === channel ? { ...p, render_mode: mode } : p,
      ),
    );

    try {
      // Update each event type's render_mode for this channel
      const updates = await Promise.all(
        channelPrefs.map((pref) =>
          api.updateNotificationPreference({
            channel: pref.channel,
            event_type: pref.event_type,
            render_mode: mode,
          }),
        ),
      );
      const updatedByKey = new Map(updates.map((p) => [preferenceKey(p), p]));
      setPreferences((current) =>
        current.map((p) => updatedByKey.get(preferenceKey(p)) ?? p),
      );
    } catch (err) {
      setPreferences(previous);
      toast.error(err instanceof Error ? err.message : "Failed to update render mode");
    } finally {
      setSavingKey(null);
    }
  };

  // Get current render_mode for a channel (uses first preference's value since they're synchronized)
  const getChannelRenderMode = (channel: NotificationChannel): NotificationRenderMode => {
    const pref = preferences.find((p) => p.channel === channel);
    return pref?.render_mode ?? "auto";
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
        <h3 className="text-sm font-semibold">触发场景</h3>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">何时通知我</CardTitle>
            <CardDescription>
              这些场景独立于具体通知渠道；开启或关闭渠道不会改变这里的勾选。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {triggerEvents.map((eventType) => {
              const key = `notification_trigger:${eventType}`;
              const enabled = isTriggerEnabled(eventType);
              const targets = supportedActiveTargets(eventType);

              return (
                <div key={eventType} className="flex items-center justify-between gap-4 rounded-lg border p-4">
                  <div className="min-w-0 space-y-1">
                    <span className="text-sm font-medium">
                      {triggerEventLabels[eventType]}
                    </span>
                    <p className="text-xs text-muted-foreground">
                      {targets.length > 0
                        ? `当前会发送到：${targets.map((channel) => channelLabels[channel]).join("、")}`
                        : "可先保留勾选，待任意渠道开启并配置后自动生效。"}
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    {savingKey === key ? (
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                    ) : null}
                    <Switch
                      checked={enabled}
                      disabled={savingKey !== null}
                      onCheckedChange={(checked) => {
                        void handleTriggerToggle(eventType, checked);
                      }}
                      aria-label={`Toggle trigger ${triggerEventLabels[eventType]}`}
                    />
                  </div>
                </div>
              );
            })}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-4">
        <h3 className="text-sm font-semibold">通知渠道</h3>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">发送到哪里</CardTitle>
            <CardDescription>
              渠道开关只决定投递目标；它不会反向修改触发场景。
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {deliveryChannels
              .filter((channel) => channel !== "openclaw_weixin" || hasOpenclawRuntime)
              .map((channel) => {
                const key = `${channel}:channel_enabled`;
                const needsSetup = channelNeedsSetup(channel);
                const enabled = isChannelEnabled(channel);
                const badge = channelBadgeText(channel);

                return (
                  <div key={channel} className="rounded-lg border p-4">
                    <div className="flex items-start justify-between gap-4">
                      <div className="min-w-0 space-y-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-medium">{channelLabels[channel]}</span>
                          {badge ? (
                            <Badge variant={needsSetup ? "outline" : "secondary"}>{badge}</Badge>
                          ) : null}
                        </div>
                        <p className="text-sm text-muted-foreground">
                          {channelDescriptions[channel]}
                        </p>
                        {channel === "dingtalk" && needsSetup ? (
                          <p className="text-xs text-muted-foreground">
                            Link your account from Profile → Linked Accounts before enabling this channel.
                          </p>
                        ) : null}
                        {channel === "email" && needsSetup ? (
                          <p className="text-xs text-muted-foreground">
                            Link your email from Profile → Linked Accounts before enabling this channel.
                          </p>
                        ) : null}
                        {channel === "custom_webhook" && needsSetup ? (
                          <p className="text-xs text-muted-foreground">
                            Add a webhook endpoint before enabling this channel.
                          </p>
                        ) : null}
                      </div>
                      <div className="flex items-center gap-3 pt-1">
                        {savingKey === key ? (
                          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                        ) : null}
                        <Switch
                          checked={enabled}
                          disabled={savingKey !== null || needsSetup}
                          onCheckedChange={(checked) => {
                            void handleChannelToggle(channel, checked);
                          }}
                          aria-label={`Toggle channel ${channelLabels[channel]}`}
                        />
                      </div>
                    </div>

                    {channel === "openclaw_weixin" && !weixinBinding ? (
                      <div className="mt-4 space-y-4 border-t pt-4">
                        <Alert>
                          <MessageCircle className="h-4 w-4" />
                          <AlertTitle>绑定微信通知</AlertTitle>
                          <AlertDescription className="space-y-3">
                            <p className="text-sm">方式一：复制以下内容发送给你的 OpenClaw 助手自动绑定：</p>
                            <code className="block rounded bg-muted p-2 text-xs">
                              帮我配置 Multica 微信通知：通过 Multica API 将我的微信 ID 绑定到通知系统。
                            </code>
                            <p className="text-sm">方式二：手动获取微信 ID 后填写：</p>
                            <code className="block rounded bg-muted p-2 text-xs">
                              openclaw whoami --channel weixin
                            </code>
                          </AlertDescription>
                        </Alert>
                        <div className="flex items-end gap-3">
                          <div className="flex-1 space-y-2">
                            <Label htmlFor="weixin-id">微信 ID</Label>
                            <Input
                              id="weixin-id"
                              value={weixinId}
                              onChange={(e) => setWeixinId(e.target.value)}
                              placeholder="o9cq809u...@im.wechat"
                            />
                          </div>
                          <Button
                            disabled={!weixinId.trim() || bindingWeixin}
                            onClick={() => void handleBindWeixin()}
                          >
                            {bindingWeixin ? <Loader2 className="h-4 w-4 animate-spin" /> : "绑定"}
                          </Button>
                        </div>
                      </div>
                    ) : null}

                    {(channel === "dingtalk" || channel === "openclaw_weixin") && !needsSetup ? (
                      <div className="mt-4 flex items-center justify-between gap-4 border-t pt-4">
                        <div className="space-y-1">
                          <span className="text-sm font-medium">通知样式</span>
                          <p className="text-xs text-muted-foreground">
                            {renderModeDescriptions[getChannelRenderMode(channel)]}
                          </p>
                        </div>
                        <div className="flex items-center gap-2">
                          {savingKey === `${channel}:render_mode` ? (
                            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                          ) : null}
                          <Select
                            value={getChannelRenderMode(channel)}
                            onValueChange={(value) => {
                              void handleRenderModeChange(channel, value as NotificationRenderMode);
                            }}
                          >
                            <SelectTrigger size="sm">
                              <SelectValue>{() => renderModeLabels[getChannelRenderMode(channel)]}</SelectValue>
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="auto">{renderModeLabels.auto}</SelectItem>
                              <SelectItem value="compact">{renderModeLabels.compact}</SelectItem>
                              <SelectItem value="detail">{renderModeLabels.detail}</SelectItem>
                            </SelectContent>
                          </Select>
                        </div>
                      </div>
                    ) : null}
                  </div>
                );
              })}
          </CardContent>
        </Card>
        <p className="text-sm text-muted-foreground">
          Manage DingTalk and email account linking from <span className="font-medium">Profile → Linked Accounts</span>.
        </p>
      </section>
    </div>
  );
}
