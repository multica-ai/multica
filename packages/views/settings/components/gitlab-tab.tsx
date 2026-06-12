"use client";

import { useState, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Copy, Eye, EyeOff, GitBranch, PanelRight, Link2, RefreshCw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import {
  deriveGitlabSettings,
  gitlabSettingsOptions,
} from "@multica/core/gitlab";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { useT } from "../../i18n";
import { FeatureRow } from "./feature-row";

type SettingsKey =
  | "gitlab_enabled"
  | "gitlab_mr_sidebar_enabled"
  | "gitlab_auto_link_enabled";

export function GitlabTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const hasAdminRole = currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data: gitlabSettings } = useQuery({
    ...gitlabSettingsOptions(wsId),
    enabled: !!wsId,
  });
  // Fallback to workspace role when backend response lacks can_manage
  const canManage = gitlabSettings?.canManage === true || hasAdminRole;

  const flags = deriveGitlabSettings(workspace);
  const [savingKey, setSavingKey] = useState<SettingsKey | null>(null);

  // Webhook token from workspace settings
  const webhookToken =
    ((workspace?.settings as Record<string, unknown>)?.gitlab_webhook_token as string) ?? null;

  // Access token state
  const [accessTokenValue, setAccessTokenValue] = useState("");
  const [isEditingToken, setIsEditingToken] = useState(false);
  const [savingToken, setSavingToken] = useState(false);
  const [showToken, setShowToken] = useState(false);
  const tokenInputRef = useRef<HTMLInputElement>(null);

  // Regenerating webhook token
  const [regenerating, setRegenerating] = useState(false);

  const webhookUrl = (() => {
    const base = api.getBaseUrl();
    // If baseUrl is relative/empty (local dev), prefix with origin
    if (!base || !base.startsWith("http")) {
      const prefix = base ? (base.startsWith("/") ? base : `/${base}`) : "";
      return `${window.location.origin}${prefix}/api/webhooks/gitlab`;
    }
    return `${base}/api/webhooks/gitlab`;
  })();

  const maskedToken = webhookToken
    ? `${webhookToken.slice(0, 4)}${"*".repeat(Math.max(0, webhookToken.length - 8))}${webhookToken.slice(-4)}`
    : null;

  const hasAccessToken =
    ((workspace?.settings as Record<string, unknown>)?.gitlab_access_token as string)?.length > 0;

  const configured =
    gitlabSettings?.configured === true ||
    (flags.enabled && hasAccessToken);

  async function persistSetting(key: SettingsKey, next: boolean) {
    if (!workspace || savingKey) return;
    setSavingKey(key);
    try {
      const merged = {
        ...((workspace.settings as Record<string, unknown>) ?? {}),
        [key]: next,
      };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.gitlab.toast_failed));
    } finally {
      setSavingKey(null);
    }
  }

  async function handleRegenerateToken() {
    if (regenerating || !workspace) return;
    setRegenerating(true);
    try {
      const resp = await api.regenerateGitlabWebhookToken(wsId);
      const merged = {
        ...((workspace.settings as Record<string, unknown>) ?? {}),
        gitlab_webhook_token: resp.token,
      };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.gitlab.toast_token_regenerated));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.gitlab.toast_token_regenerate_failed));
    } finally {
      setRegenerating(false);
    }
  }

  async function handleCopy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(t(($) => $.gitlab.toast_copied, { label }));
    } catch {
      toast.error(t(($) => $.gitlab.toast_copy_failed));
    }
  }

  async function handleSaveAccessToken() {
    if (!workspace || !accessTokenValue || savingToken) return;
    setSavingToken(true);
    try {
      const merged = {
        ...((workspace.settings as Record<string, unknown>) ?? {}),
        gitlab_access_token: accessTokenValue,
      };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.gitlab.toast_access_token_saved));
      setIsEditingToken(false);
      setAccessTokenValue("");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.gitlab.toast_access_token_save_failed));
    } finally {
      setSavingToken(false);
    }
  }

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-1">
        <p className="text-sm text-muted-foreground">
          {t(($) => $.gitlab.page_description)}
        </p>
      </section>

      <section className="space-y-3">
        <Card>
          <CardContent>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                  <GitBranch className="h-4 w-4" />
                </div>
                <div className="space-y-1">
                  <Label htmlFor="gitlab-master" className="text-sm font-medium">
                    {t(($) => $.gitlab.section_master)}
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    {flags.enabled
                      ? t(($) => $.gitlab.master_description_on)
                      : t(($) => $.gitlab.master_description_off)}
                  </p>
                </div>
              </div>
              <Switch
                id="gitlab-master"
                checked={flags.enabled}
                onCheckedChange={(v) => persistSetting("gitlab_enabled", v)}
                disabled={!canManage || savingKey === "gitlab_enabled"}
              />
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">{t(($) => $.gitlab.section_connection)}</h2>
        <Card>
          <CardContent className="space-y-4">
            {/* Webhook URL */}
            <div className="space-y-1.5">
              <Label className="text-xs font-medium">{t(($) => $.gitlab.webhook_url_label)}</Label>
              <p className="text-[11px] text-muted-foreground">{t(($) => $.gitlab.webhook_url_hint)}</p>
              <div className="flex items-center gap-2">
                <Input
                  readOnly
                  value={webhookUrl}
                  className="text-xs font-mono"
                />
                <Button
                  variant="outline"
                  size="icon"
                  className="h-9 w-9 shrink-0"
                  aria-label={t(($) => $.gitlab.webhook_url_label)}
                  onClick={() => handleCopy(webhookUrl, t(($) => $.gitlab.webhook_url_label))}
                >
                  <Copy className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>

            {/* Webhook Secret Token */}
            <div className="space-y-1.5">
              <Label className="text-xs font-medium">{t(($) => $.gitlab.webhook_token_label)}</Label>
              <p className="text-[11px] text-muted-foreground">{t(($) => $.gitlab.webhook_token_hint)}</p>
              <div className="flex items-center gap-2">
                <Input
                  readOnly
                  value={maskedToken ?? "—"}
                  className="text-xs font-mono"
                />
                {webhookToken && (
                  <Button
                    variant="outline"
                    size="icon"
                    className="h-9 w-9 shrink-0"
                    aria-label={t(($) => $.gitlab.copy_webhook_token_aria)}
                    onClick={() => handleCopy(webhookToken, t(($) => $.gitlab.webhook_token_label))}
                  >
                    <Copy className="h-3.5 w-3.5" />
                  </Button>
                )}
                {canManage && (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={regenerating}
                    onClick={handleRegenerateToken}
                  >
                    <RefreshCw className="h-3 w-3" />
                    {regenerating
                      ? t(($) => $.gitlab.regenerating)
                      : t(($) => $.gitlab.regenerate_token)}
                  </Button>
                )}
              </div>
            </div>

            {/* Access Token */}
            <div className="space-y-1.5">
              <Label className="text-xs font-medium">{t(($) => $.gitlab.access_token_label)}</Label>
              <p className="text-[11px] text-muted-foreground">{t(($) => $.gitlab.access_token_hint)}</p>
              {isEditingToken ? (
                <div className="flex items-center gap-2">
                  <div className="relative flex-1">
                    <Input
                      ref={tokenInputRef}
                      type={showToken ? "text" : "password"}
                      value={accessTokenValue}
                      onChange={(e) => setAccessTokenValue(e.target.value)}
                      placeholder="glpat-..."
                      className="text-xs pr-9"
                      autoFocus
                    />
                    <Button
                      variant="ghost"
                      size="icon"
                      className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7"
                      onClick={() => setShowToken((v) => !v)}
                      aria-label={showToken ? t(($) => $.gitlab.hide_token) : t(($) => $.gitlab.show_token)}
                    >
                      {showToken ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                    </Button>
                  </div>
                  <Button
                    size="sm"
                    disabled={!accessTokenValue || savingToken}
                    onClick={handleSaveAccessToken}
                  >
                    {savingToken ? t(($) => $.gitlab.saving) : t(($) => $.gitlab.save_token)}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setIsEditingToken(false);
                      setAccessTokenValue("");
                    }}
                  >
                    {t(($) => $.gitlab.cancel)}
                  </Button>
                </div>
              ) : (
                <div className="flex items-center gap-2">
                  <Input
                    readOnly
                    type="password"
                    value={hasAccessToken ? "••••••••" : ""}
                    placeholder="—"
                    className="text-xs"
                  />
                  {canManage && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setIsEditingToken(true)}
                    >
                      {hasAccessToken
                        ? t(($) => $.gitlab.change_token)
                        : t(($) => $.gitlab.add_token)}
                    </Button>
                  )}
                </div>
              )}
            </div>

            {!configured && canManage && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.gitlab.not_configured)}
              </p>
            )}

            {!canManage && !configured && (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.gitlab.contact_admin_to_connect)}
              </p>
            )}
          </CardContent>
        </Card>
      </section>

      <section className="space-y-3">
        <h2 className="text-sm font-semibold">{t(($) => $.gitlab.section_features)}</h2>
        <Card>
          <CardContent className="space-y-4">
            <FeatureRow
              id="gitlab-mr-sidebar"
              icon={<PanelRight className="h-4 w-4" />}
              label={t(($) => $.gitlab.feature_mr_sidebar_label)}
              description={
                <p className="text-sm text-muted-foreground">
                  {t(($) => $.gitlab.feature_mr_sidebar_description)}
                </p>
              }
              checked={flags.mrSidebar}
              disabled={!canManage || !flags.enabled || savingKey === "gitlab_mr_sidebar_enabled"}
              onCheckedChange={(v) => persistSetting("gitlab_mr_sidebar_enabled", v)}
            />

            <FeatureRow
              id="gitlab-auto-link"
              icon={<Link2 className="h-4 w-4" />}
              label={t(($) => $.gitlab.feature_auto_link_label)}
              description={
                <p className="text-sm text-muted-foreground">
                  {t(($) => $.gitlab.feature_auto_link_description)}
                </p>
              }
              checked={flags.autoLinkMRs}
              disabled={!canManage || !flags.enabled || savingKey === "gitlab_auto_link_enabled"}
              onCheckedChange={(v) => persistSetting("gitlab_auto_link_enabled", v)}
            />
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
