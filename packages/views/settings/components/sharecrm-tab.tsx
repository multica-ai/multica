"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, ExternalLink, Trash2 } from "lucide-react";
import { ShareCRMMark } from "./sharecrm-mark";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { sharecrmInstallationsOptions, sharecrmKeys } from "@multica/core/sharecrm";
import { api } from "@multica/core/api";
import type { ShareCRMInstallation } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { openExternal } from "../../platform";
import { useT } from "../../i18n";

// formatInstalledAt renders the install timestamp defensively: the schema
// defaults installed_at to "" and the backend can emit a zero-value timestamp
// (0001-01-01T…) for a never-set time, either of which would otherwise surface
// as "Invalid Date" or a year-1 date. Fall back to a neutral placeholder.
function formatInstalledAt(value: string): string {
  const t = Date.parse(value);
  if (!value || Number.isNaN(t) || t <= 0) return "—";
  return new Date(t).toLocaleString();
}

// ShareCRMTab is the workspace settings panel for ShareCRM robot installations.
// Listing is member-visible; the disconnect action is admin-only (the backend
// enforces it; the UI hides the button for non-admins to match).
//
// Adding a new installation flows through the Agent detail page: the install
// path is per-agent (each Multica agent gets exactly one robot — the
// (workspace_id, agent_id, channel_type) UNIQUE in channel_installation), so
// asking the user to pick an agent here would re-create that page's picker.
export function ShareCRMTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading } = useQuery({
    ...sharecrmInstallationsOptions(wsId),
  });
  const installations = data?.installations ?? [];
  const configured = data?.configured === true;

  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteShareCRMInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: sharecrmKeys.installations(wsId) });
      toast.success(t(($) => $.sharecrm.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.sharecrm.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="space-y-8">
      {!configured ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-body font-medium">{t(($) => $.sharecrm.not_enabled_title)}</p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.sharecrm.not_enabled_description_prefix)}{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-micro">
                MULTICA_SHARECRM_SECRET_KEY
              </code>{" "}
              {t(($) => $.sharecrm.not_enabled_description_suffix)}{" "}
              {t(($) => $.sharecrm.not_enabled_self_host_hint)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <h2 className="text-body font-semibold">{t(($) => $.sharecrm.connected_bots)}</h2>
          {isLoading ? (
            <Card>
              <CardContent>
                <p className="text-body text-muted-foreground">{t(($) => $.sharecrm.loading)}</p>
              </CardContent>
            </Card>
          ) : installations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-body font-medium">{t(($) => $.sharecrm.empty_title)}</p>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.sharecrm.empty_description_prefix)}{" "}
                  <strong>{t(($) => $.sharecrm.empty_description_cta)}</strong>{" "}
                  {t(($) => $.sharecrm.empty_description_suffix)}
                </p>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="divide-y">
                {installations.map((inst) => (
                  <InstallationRow
                    key={inst.id}
                    installation={inst}
                    canManage={canManage}
                    onDisconnect={() => setDisconnectTarget(inst.id)}
                  />
                ))}
              </CardContent>
            </Card>
          )}
        </section>
      )}

      <AlertDialog
        open={!!disconnectTarget}
        onOpenChange={(v) => {
          if (!v && !disconnecting) setDisconnectTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.sharecrm.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.sharecrm.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.sharecrm.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.sharecrm.disconnecting)
                : t(($) => $.sharecrm.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function InstallationRow({
  installation,
  canManage,
  onDisconnect,
}: {
  installation: ShareCRMInstallation;
  canManage: boolean;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  const { getAgentName } = useActorName();
  const isActive = installation.status === "active";
  const agentName = getAgentName(installation.agent_id);
  return (
    <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
      <div className="flex items-start gap-3">
        <ActorAvatar
          actorType="agent"
          actorId={installation.agent_id}
          size="lg"
          enableHoverCard
          profileLink
        />
        <div className="space-y-1">
          <p className="text-body font-medium">
            {agentName}
            {!isActive && (
              <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                {t(($) => $.sharecrm.revoked_badge)}
              </span>
            )}
          </p>
          <p className="text-micro text-muted-foreground">
            {t(($) => $.sharecrm.installed_at_label, {
              when: formatInstalledAt(installation.installed_at),
            })}
          </p>
        </div>
      </div>
      {canManage && isActive && (
        <Button variant="outline" size="sm" onClick={onDisconnect}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.sharecrm.disconnect)}
        </Button>
      )}
    </div>
  );
}

// sharecrmDocsUrl points at the ShareCRM integration guide on the docs site,
// localized to the viewer's language. The docs site uses /<lang>/ path
// prefixes (English has none), matching the convention used elsewhere in the
// app for doc links.
function sharecrmDocsUrl(lang: string | undefined): string {
  const prefix = lang?.startsWith("zh")
    ? "/zh"
    : lang?.startsWith("ja")
      ? "/ja"
      : lang?.startsWith("ko")
        ? "/ko"
        : "";
  return `https://multica.ai/docs${prefix}/sharecrm-bot-integration`;
}

// ShareCRMAgentBindButton is the per-agent CTA exposed from the agent detail
// page. ShareCRM uses the bring-your-own-app model: the button opens a dialog
// where the admin pastes the App ID + App Secret of the ShareCRM bot they
// created (the backend validates both, matching DingTalk/Slack BYO). Visibility:
//   1. Non-owner/admin viewers see nothing (the backend gates install/revoke).
//   2. If this agent already has an active installation, show the connected
//      badge (already-installed robots stay manageable).
//   3. Otherwise the Connect CTA shows whenever install is available.
export function ShareCRMAgentBindButton({
  agentId,
  agentName,
  className,
  onShowConnectedDetails,
}: {
  agentId: string;
  agentName?: string;
  className?: string;
  /**
   * When set, the connected state renders as a compact read-only status row
   * that invokes this callback on click instead of the full badge with inline
   * actions — the agent inspector passes a "jump to the Integrations tab"
   * handler so management actions live in one place.
   */
  onShowConnectedDetails?: () => void;
}) {
  const { t, i18n } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const [dialogOpen, setDialogOpen] = useState(false);
  const [appId, setAppId] = useState("");
  const [appSecret, setAppSecret] = useState("");
  const [gatewayBaseUrl, setGatewayBaseUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const { data: listing } = useQuery({
    ...sharecrmInstallationsOptions(wsId),
  });
  const installSupported = listing?.install_supported === true;

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  if (!canManage) return null;

  const existing = listing?.installations?.find(
    (inst) => inst.agent_id === agentId && inst.status === "active",
  );
  if (existing) {
    return onShowConnectedDetails ? (
      <ShareCRMAgentBotStatusRow
        onClick={onShowConnectedDetails}
        className={className}
      />
    ) : (
      <ShareCRMAgentBotConnectedBadge installation={existing} className={className} />
    );
  }

  if (!installSupported) return null;

  function closeDialog() {
    if (submitting) return;
    setDialogOpen(false);
    setAppId("");
    setAppSecret("");
    setGatewayBaseUrl("");
  }

  async function handleSubmit() {
    const app_id = appId.trim();
    const app_secret = appSecret.trim();
    const gateway_base_url = gatewayBaseUrl.trim();
    if (submitting || !agentId || !app_id || !app_secret) return;
    setSubmitting(true);
    try {
      await api.registerShareCRMBYO(wsId, agentId, {
        app_id,
        app_secret,
        ...(gateway_base_url ? { gateway_base_url } : {}),
      });
      // The sharecrm_installation realtime event also refreshes this list, but
      // invalidate explicitly so the connected badge appears immediately.
      await qc.invalidateQueries({ queryKey: sharecrmKeys.installations(wsId) });
      toast.success(t(($) => $.sharecrm.byo_success_toast));
      setDialogOpen(false);
      setAppId("");
      setAppSecret("");
      setGatewayBaseUrl("");
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.sharecrm.byo_failed_toast),
      );
    } finally {
      setSubmitting(false);
    }
  }

  const canSubmit =
    appId.trim() !== "" && appSecret.trim() !== "" && !submitting;

  return (
    <div
      className={cn("flex flex-wrap items-center gap-2", className)}
      data-testid="sharecrm-agent-bind-buttons"
    >
      <Button
        variant="outline"
        size="sm"
        onClick={() => setDialogOpen(true)}
        disabled={!agentId}
        title={
          agentName
            ? t(($) => $.sharecrm.bind_button_title, { agent: agentName })
            : undefined
        }
        data-testid="sharecrm-agent-connect"
      >
        <ShareCRMMark className="h-3 w-3" />
        {t(($) => $.sharecrm.bind_button)}
      </Button>

      <Dialog
        open={dialogOpen}
        onOpenChange={(v) => (v ? setDialogOpen(true) : closeDialog())}
      >
        <DialogContent
          className="gap-0 overflow-hidden p-0 sm:max-w-lg"
          data-testid="sharecrm-byo-dialog"
        >
          <DialogHeader className="gap-1 border-b px-5 py-3">
            <DialogTitle className="text-title-sm font-semibold">
              {t(($) => $.sharecrm.byo_dialog_title)}
            </DialogTitle>

            <button
              type="button"
              onClick={() => openExternal(sharecrmDocsUrl(i18n.language))}
              className="inline-flex w-fit items-center gap-1.5 text-caption text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline"
              data-testid="sharecrm-byo-docs-link"
            >
              <ExternalLink className="h-3.5 w-3.5" />
              {t(($) => $.sharecrm.byo_docs_link)}
            </button>
          </DialogHeader>

          <div className="space-y-4 p-5">
            <div className="space-y-1.5">
              <Label
                htmlFor="sharecrm-byo-app-id"
                className="text-caption text-muted-foreground"
              >
                {t(($) => $.sharecrm.byo_app_id_label)}
              </Label>
              <Input
                id="sharecrm-byo-app-id"
                data-testid="sharecrm-byo-app-id"
                type="password"
                value={appId}
                onChange={(e) => setAppId(e.target.value)}
                autoComplete="off"
                spellCheck={false}
                disabled={submitting}
              />
            </div>

            <div className="space-y-1.5">
              <Label
                htmlFor="sharecrm-byo-app-secret"
                className="text-caption text-muted-foreground"
              >
                {t(($) => $.sharecrm.byo_app_secret_label)}
              </Label>
              <Input
                id="sharecrm-byo-app-secret"
                data-testid="sharecrm-byo-app-secret"
                type="password"
                value={appSecret}
                onChange={(e) => setAppSecret(e.target.value)}
                autoComplete="off"
                spellCheck={false}
                disabled={submitting}
              />
            </div>

            <div className="space-y-1.5">
              <Label
                htmlFor="sharecrm-byo-gateway-url"
                className="text-caption text-muted-foreground"
              >
                {t(($) => $.sharecrm.byo_gateway_url_label)}
              </Label>
              <Input
                id="sharecrm-byo-gateway-url"
                data-testid="sharecrm-byo-gateway-url"
                type="url"
                value={gatewayBaseUrl}
                onChange={(e) => setGatewayBaseUrl(e.target.value)}
                placeholder={t(($) => $.sharecrm.byo_gateway_url_placeholder)}
                autoComplete="off"
                spellCheck={false}
                disabled={submitting}
              />
            </div>
          </div>

          {/* Inline footer instead of <DialogFooter>: its -mx-4/-mb-4 offsets
              assume the default p-4 DialogContent; with p-0 they push the bar
              outside the dialog (same workaround as CreateAgentDialog). */}
          <div className="flex items-center justify-end gap-2 border-t bg-background px-5 py-3">
            <Button variant="ghost" onClick={closeDialog} disabled={submitting}>
              {t(($) => $.sharecrm.byo_cancel)}
            </Button>
            <Button
              onClick={handleSubmit}
              disabled={!canSubmit}
              data-testid="sharecrm-byo-submit"
            >
              {submitting
                ? t(($) => $.sharecrm.byo_submitting)
                : t(($) => $.sharecrm.byo_submit)}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ShareCRMAgentBotStatusRow is the compact, read-only connected affordance the
// agent inspector renders instead of the full badge; it deep-links into the
// Integrations tab where Manage / Disconnect live.
function ShareCRMAgentBotStatusRow({
  onClick,
  className,
}: {
  onClick: () => void;
  className?: string;
}) {
  const { t } = useT("settings");
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-caption text-muted-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        className,
      )}
      data-testid="sharecrm-agent-bot-status"
    >
      <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
      <span className="truncate">{t(($) => $.sharecrm.agent_bot_connected_label)}</span>
      <ChevronRight className="ml-auto h-3.5 w-3.5 shrink-0" />
    </button>
  );
}

// ShareCRMAgentBotConnectedBadge is the full "already connected" affordance the
// Integrations tab renders in place of the Connect button: a status row plus a
// soft-destructive Disconnect. Only owners/admins ever reach this component.
function ShareCRMAgentBotConnectedBadge({
  installation,
  className,
}: {
  installation: ShareCRMInstallation;
  className?: string;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const [confirmOpen, setConfirmOpen] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);

  async function handleDisconnect() {
    if (disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteShareCRMInstallation(wsId, installation.id);
      await qc.invalidateQueries({ queryKey: sharecrmKeys.installations(wsId) });
      toast.success(t(($) => $.sharecrm.toast_disconnected));
      setConfirmOpen(false);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.sharecrm.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div
      className={cn("space-y-2", className)}
      data-testid="sharecrm-agent-bot-connected"
    >
      <div className="flex items-center justify-between gap-3">
        <span className="inline-flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
          <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
          <span className="truncate">{t(($) => $.sharecrm.agent_bot_connected_label)}</span>
        </span>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => setConfirmOpen(true)}
          disabled={disconnecting}
          title={t(($) => $.sharecrm.agent_bot_disconnect_tooltip)}
          aria-label={t(($) => $.sharecrm.disconnect)}
          data-testid="sharecrm-agent-bot-disconnect"
        >
          <Trash2 className="h-3 w-3" />
          {disconnecting
            ? t(($) => $.sharecrm.disconnecting)
            : t(($) => $.sharecrm.disconnect)}
        </Button>
      </div>

      <AlertDialog
        open={confirmOpen}
        onOpenChange={(v) => {
          if (!v && !disconnecting) setConfirmOpen(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.sharecrm.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.sharecrm.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.sharecrm.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.sharecrm.disconnecting)
                : t(($) => $.sharecrm.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
