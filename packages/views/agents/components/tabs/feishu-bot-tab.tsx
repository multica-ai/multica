"use client";

import { useEffect, useState } from "react";
import { AlertCircle, Bot, Loader2, Save } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { api } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Switch } from "@multica/ui/components/ui/switch";
import { toast } from "sonner";
import { useT } from "../../../i18n";

export function FeishuBotTab({
  agent,
  onDirtyChange,
}: {
  agent: Agent;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ["agent", agent.id, "feishu-bot"],
    queryFn: () => api.getAgentFeishuBotConfig(agent.id),
  });
  const callbackURL =
    typeof window === "undefined" || !data?.callback_url_path
      ? data?.callback_url_path
      : new URL(data.callback_url_path, window.location.origin).toString();
  const [enabled, setEnabled] = useState(false);
  const [appId, setAppId] = useState("");
  const [appSecret, setAppSecret] = useState("");
  const [verificationToken, setVerificationToken] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!data) return;
    setEnabled(data.enabled);
    setAppId(data.app_id);
    setAppSecret("");
    setVerificationToken(data.verification_token ?? "");
  }, [data]);

  const dirty =
    !!data &&
    (enabled !== data.enabled ||
      appId !== data.app_id ||
      appSecret.trim() !== "" ||
      verificationToken !== (data.verification_token ?? ""));

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const handleSave = async () => {
    if (enabled && !appId.trim()) {
      toast.error(t(($) => $.tab_body.feishu_bot.app_id_required_toast));
      return;
    }
    if (enabled && !data?.has_app_secret && !appSecret.trim()) {
      toast.error(t(($) => $.tab_body.feishu_bot.app_secret_required_toast));
      return;
    }
    setSaving(true);
    try {
      await api.updateAgentFeishuBotConfig(agent.id, {
        enabled,
        app_id: appId.trim(),
        app_secret: appSecret.trim() || undefined,
        verification_token: verificationToken.trim() || null,
      });
      await refetch();
      toast.success(t(($) => $.tab_body.feishu_bot.saved_toast));
    } catch {
      toast.error(t(($) => $.tab_body.feishu_bot.save_failed_toast));
    } finally {
      setSaving(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        {t(($) => $.tab_body.feishu_bot.loading)}
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive">
        <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
        <div className="space-y-2">
          <p>{t(($) => $.tab_body.feishu_bot.load_failed)}</p>
          <Button variant="outline" size="sm" onClick={() => void refetch()}>
            {t(($) => $.tab_body.feishu_bot.retry)}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <div className="flex items-start gap-3 rounded-md border bg-muted/20 p-3">
        <Bot className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
        <div className="space-y-1 text-xs text-muted-foreground">
          <p>{t(($) => $.tab_body.feishu_bot.intro)}</p>
          <p>{t(($) => $.tab_body.feishu_bot.console_hint)}</p>
          <code className="block rounded bg-background px-2 py-1 font-mono text-[11px] text-foreground">
            {callbackURL}
          </code>
          <p>{t(($) => $.tab_body.feishu_bot.event_hint)}</p>
        </div>
      </div>

      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-sm font-medium">{t(($) => $.tab_body.feishu_bot.enable_label)}</p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {t(($) => $.tab_body.feishu_bot.enable_hint)}
          </p>
        </div>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </div>

      <div className="space-y-3">
        <div className="space-y-1.5">
          <label className="text-xs font-medium">{t(($) => $.tab_body.feishu_bot.app_id_label)}</label>
          <Input
            value={appId}
            onChange={(e) => setAppId(e.target.value)}
            placeholder="cli_xxx"
            className="font-mono text-xs"
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium">{t(($) => $.tab_body.feishu_bot.app_secret_label)}</label>
          <Input
            type="password"
            value={appSecret}
            onChange={(e) => setAppSecret(e.target.value)}
            placeholder={data.has_app_secret ? t(($) => $.tab_body.feishu_bot.secret_placeholder_set) : ""}
            className="font-mono text-xs"
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs font-medium">{t(($) => $.tab_body.feishu_bot.verification_token_label)}</label>
          <Input
            type="password"
            value={verificationToken}
            onChange={(e) => setVerificationToken(e.target.value)}
            className="font-mono text-xs"
          />
        </div>
      </div>

      <div className="flex items-center justify-end gap-3">
        {dirty && (
          <span className="text-xs text-muted-foreground">{t(($) => $.tab_body.common.unsaved_changes)}</span>
        )}
        <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {t(($) => $.tab_body.common.save)}
        </Button>
      </div>
    </div>
  );
}
