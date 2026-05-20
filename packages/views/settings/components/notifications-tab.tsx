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
  inbox: "Inbox",
  dingtalk: "DingTalk",
  email: "Email",
  custom_webhook: "Custom Webhook",
  openclaw_weixin: "微信（OpenClaw）",
};

const channelDescriptions: Record<NotificationChannel, string> = {
  inbox: "In-app notification delivered through the existing Inbox and websocket flow.",
  dingtalk: "External notification sent to your linked DingTalk account once that channel is enabled.",
  email: "Email notification sent to your linked email address when you are mentioned.",
  custom_webhook: "POST @ mentions, assignments, and subscribed issue updates to your own webhook endpoint.",
  openclaw_weixin: "通过 OpenClaw 本地助手将通知发送到你的微信。需要在线的 OpenClaw Runtime。",
};

const customWebhookEvents: NotificationEventType[] = [
  "mentioned",
  "issue_assigned",
  "subscribed_issue_updated",
];

const openclawWeixinEvents: NotificationEventType[] = [
  "task_completed",
  "task_failed",
  "mentioned",
  "replied",
];

const openclawWeixinEventLabels: Record<string, string> = {
  task_completed: "Agent 任务完成时",
  task_failed: "Agent 任务失败时",
  mentioned: "被 @提及时",
  replied: "被回复时",
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

  const handleToggleOpenclawWeixin = async (eventType: NotificationEventType, enabled: boolean) => {
    const key = `openclaw_weixin:${eventType}`;
    const previous = preferences;
    setSavingKey(key);
    setPreferences((current) =>
      current.map((candidate) =>
        preferenceKey(candidate) === key ? { ...candidate, enabled } : candidate,
      ),
    );

    try {
      const updated = await api.updateNotificationPreference({
        channel: "openclaw_weixin",
        event_type: eventType,
        enabled,
      });
      setPreferences((current) =>
        current.map((candidate) =>
          preferenceKey(candidate) === key ? updated : candidate,
        ),
      );
    } catch (err) {
      setPreferences(previous);
      toast.error(err instanceof Error ? err.message : "Failed to update preference");
    } finally {
      setSavingKey(null);
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
              .filter((pref) => pref.channel !== "custom_webhook" && pref.channel !== "openclaw_weixin")
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

            {/* Render mode selector for DingTalk */}
            {preferences.some((p) => p.channel === "dingtalk") && (
              <div className="rounded-lg border p-4 space-y-2">
                <div className="flex items-center justify-between gap-4">
                  <div className="space-y-1">
                    <span className="text-sm font-medium">DingTalk 通知样式</span>
                    <p className="text-xs text-muted-foreground">
                      {renderModeDescriptions[getChannelRenderMode("dingtalk")]}
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    {savingKey === "dingtalk:render_mode" ? (
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                    ) : null}
                    <Select
                      value={getChannelRenderMode("dingtalk")}
                      onValueChange={(value) => {
                        void handleRenderModeChange("dingtalk", value as NotificationRenderMode);
                      }}
                    >
                      <SelectTrigger size="sm">
                        <SelectValue>{() => renderModeLabels[getChannelRenderMode("dingtalk")]}</SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="auto">{renderModeLabels.auto}</SelectItem>
                        <SelectItem value="compact">{renderModeLabels.compact}</SelectItem>
                        <SelectItem value="detail">{renderModeLabels.detail}</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
              </div>
            )}
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

      {hasOpenclawRuntime && (
        <section className="space-y-4">
          <h3 className="text-sm font-semibold">微信通知（OpenClaw）</h3>
          <Card>
            <CardHeader>
              <CardTitle className="text-base flex items-center gap-2">
                <MessageCircle className="h-4 w-4" />
                微信通知
              </CardTitle>
              <CardDescription>
                通过 OpenClaw 本地助手将通知发送到你的微信。
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {!weixinBinding ? (
                <div className="space-y-4">
                  <Alert>
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
              ) : (
                <div className="space-y-4">
                  <div className="flex items-center gap-2">
                    <Badge variant="secondary">已绑定</Badge>
                    <span className="text-sm text-muted-foreground">
                      {weixinBinding.external_user_id}
                    </span>
                  </div>
                  <div className="space-y-3">
                    {openclawWeixinEvents.map((eventType) => {
                      const pref = preferences.find(
                        (p) => p.channel === "openclaw_weixin" && p.event_type === eventType,
                      );
                      const key = `openclaw_weixin:${eventType}`;
                      const enabled = pref?.enabled ?? false;

                      return (
                        <div key={key} className="flex items-center justify-between gap-4 rounded-lg border p-3">
                          <span className="text-sm font-medium">
                            {openclawWeixinEventLabels[eventType] ?? eventType}
                          </span>
                          <div className="flex items-center gap-2">
                            {savingKey === key ? (
                              <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                            ) : null}
                            <Switch
                              checked={enabled}
                              disabled={savingKey !== null}
                              onCheckedChange={(checked) => {
                                void handleToggleOpenclawWeixin(eventType, checked);
                              }}
                              aria-label={`Toggle ${openclawWeixinEventLabels[eventType]}`}
                            />
                          </div>
                        </div>
                      );
                    })}
                  </div>
                  <div className="rounded-lg border p-4 space-y-2">
                    <div className="flex items-center justify-between gap-4">
                      <div className="space-y-1">
                        <span className="text-sm font-medium">通知样式</span>
                        <p className="text-xs text-muted-foreground">
                          {renderModeDescriptions[getChannelRenderMode("openclaw_weixin")]}
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        {savingKey === "openclaw_weixin:render_mode" ? (
                          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                        ) : null}
                        <Select
                          value={getChannelRenderMode("openclaw_weixin")}
                          onValueChange={(value) => {
                            void handleRenderModeChange("openclaw_weixin", value as NotificationRenderMode);
                          }}
                        >
                          <SelectTrigger size="sm">
                            <SelectValue>{() => renderModeLabels[getChannelRenderMode("openclaw_weixin")]}</SelectValue>
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="auto">{renderModeLabels.auto}</SelectItem>
                            <SelectItem value="compact">{renderModeLabels.compact}</SelectItem>
                            <SelectItem value="detail">{renderModeLabels.detail}</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                    </div>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </section>
      )}
    </div>
  );
}
