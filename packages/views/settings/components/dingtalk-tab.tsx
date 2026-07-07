"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, ExternalLink, RefreshCw, Trash2 } from "lucide-react";
// Named import, NOT default — same electron-vite CJS interop constraint
// documented in lark-tab.tsx.
import { QRCode } from "react-qr-code";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { dingtalkInstallationsOptions, dingtalkKeys } from "@multica/core/dingtalk";
import { api, ApiError } from "@multica/core/api";
import type { DingTalkInstallation, DingTalkInstallStatusResponse } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

// The DingTalk developer console where installed apps are managed
// (credentials, permissions, release). The scan-to-create flow does not
// return a console deep link per app, so we link to the console home.
/** Deep link into the dev console's enterprise internal-app list — the
 * page the scan-created bot app lives on. A per-app detail link needs the
 * console's numeric appId, which the device flow does not return, so the
 * list page is the closest stable target. */
const DINGTALK_DEV_CONSOLE = "https://open-dev.dingtalk.com/fe/app#/corp/app";

// DingTalkTab is the workspace settings panel for DingTalk bot
// installations, created through the scan-to-create device flow
// ("一键创建钉钉应用"). Listing is member-visible; the disconnect action
// is admin-only (the backend enforces it; the UI hides the button for
// non-admins to match).
//
// Adding a new installation flows through the Agent detail page: the
// install path is per-agent (each Multica Agent gets exactly one app —
// see the (workspace_id, agent_id, channel_type) UNIQUE), so the
// "Bind your first agent" copy in the empty state hints users at the
// right entry point. Mirrors LarkTab.
export function DingTalkTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading } = useQuery({
    ...dingtalkInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const installations = data?.installations ?? [];
  const configured = data?.configured === true;
  const installSupported = data?.install_supported === true;

  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteDingTalkInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: dingtalkKeys.installations(wsId) });
      toast.success(t(($) => $.dingtalk.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.dingtalk.toast_disconnect_failed));
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="space-y-8">
      <section className="space-y-1">
        <p className="text-sm text-muted-foreground">
          {t(($) => $.dingtalk.page_description)}
        </p>
      </section>

      {!configured ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-sm font-medium">{t(($) => $.dingtalk.not_enabled_title)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.dingtalk.not_enabled_description_prefix)}{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                MULTICA_DINGTALK_SECRET_KEY
              </code>{" "}
              {t(($) => $.dingtalk.not_enabled_description_suffix)}{" "}
              {t(($) => $.dingtalk.not_enabled_self_host_hint)}
            </p>
          </CardContent>
        </Card>
      ) : !installSupported && installations.length === 0 ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-sm font-medium">{t(($) => $.dingtalk.preview_title)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.dingtalk.preview_description)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <h2 className="text-sm font-semibold">{t(($) => $.dingtalk.connected_bots)}</h2>
          {isLoading ? (
            <Card>
              <CardContent>
                <p className="text-sm text-muted-foreground">{t(($) => $.dingtalk.loading)}</p>
              </CardContent>
            </Card>
          ) : installations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-sm font-medium">{t(($) => $.dingtalk.empty_title)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.dingtalk.empty_description_prefix)}{" "}
                  <strong>{t(($) => $.dingtalk.empty_description_cta)}</strong>{" "}
                  {t(($) => $.dingtalk.empty_description_suffix)}
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
              {t(($) => $.dingtalk.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.dingtalk.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.dingtalk.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.dingtalk.disconnecting)
                : t(($) => $.dingtalk.disconnect)}
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
  installation: DingTalkInstallation;
  canManage: boolean;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  // The bot is bound 1:1 to a Multica Agent. Render the Multica agent's
  // identity here rather than the raw client_id — that means nothing to
  // product users.
  const { getAgentName } = useActorName();
  const isActive = installation.status === "active";
  const agentName = getAgentName(installation.agent_id);
  return (
    <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
      <div className="flex items-start gap-3">
        <ActorAvatar
          actorType="agent"
          actorId={installation.agent_id}
          size={32}
          enableHoverCard
          profileLink
        />
        <div className="space-y-1">
          <p className="text-sm font-medium">
            {agentName}
            {!isActive && (
              <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                {t(($) => $.dingtalk.revoked_badge)}
              </span>
            )}
          </p>
          <p className="text-[10px] text-muted-foreground">
            {t(($) => $.dingtalk.installed_at_label, {
              when: new Date(installation.installed_at).toLocaleString(),
            })}
          </p>
        </div>
      </div>
      {canManage && isActive && (
        <Button variant="outline" size="sm" onClick={onDisconnect}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.dingtalk.disconnect)}
        </Button>
      )}
    </div>
  );
}

// DingTalkAgentBindButton is the per-agent CTA we expose from the agent
// detail page. Visibility rules mirror LarkAgentBindButton:
//   1. Non-owner/admin viewers see nothing — the backend gates install /
//      status / disconnect on those roles.
//   2. If this agent ALREADY has an active installation, owner/admins see
//      the connected badge regardless of install_supported (which only
//      governs NEW scan-installs).
//   3. Otherwise the Bind CTA shows only when install_supported is true.
export function DingTalkAgentBindButton({
  agentId,
  agentName,
  className,
  onShowConnectedDetails,
}: {
  agentId: string;
  agentName?: string;
  className?: string;
  /** When set, the connected state renders as a compact read-only status
   * row that invokes this callback on click instead of the full badge —
   * same contract as LarkAgentBindButton. */
  onShowConnectedDetails?: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const [dialogOpen, setDialogOpen] = useState(false);

  const { data: listing } = useQuery({
    ...dingtalkInstallationsOptions(wsId),
    enabled: !!wsId,
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

  const existing = listing?.installations.find(
    (inst) => inst.agent_id === agentId && inst.status === "active",
  );
  if (existing) {
    return onShowConnectedDetails ? (
      <DingTalkAgentBotStatusRow
        onClick={onShowConnectedDetails}
        className={className}
      />
    ) : (
      <DingTalkAgentBotConnectedBadge installation={existing} className={className} />
    );
  }

  if (!installSupported) return null;

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className={className}
        onClick={() => setDialogOpen(true)}
        disabled={!agentId}
        title={
          agentName
            ? t(($) => $.dingtalk.bind_button_title, { agent: agentName })
            : undefined
        }
        data-testid="dingtalk-agent-bind"
      >
        <ExternalLink className="h-3 w-3" />
        {t(($) => $.dingtalk.bind_button)}
      </Button>
      {dialogOpen && (
        <DingTalkInstallDialog
          wsId={wsId}
          agentId={agentId}
          agentName={agentName}
          onClose={() => setDialogOpen(false)}
        />
      )}
    </>
  );
}

// DingTalkAgentBotStatusRow is the compact, read-only connected
// affordance the agent inspector renders instead of the full badge —
// a single full-width button deep-linking into the Integrations tab.
function DingTalkAgentBotStatusRow({
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
        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        className,
      )}
      data-testid="dingtalk-agent-bot-status"
    >
      <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
      <span className="truncate">{t(($) => $.dingtalk.agent_bot_connected_label)}</span>
      <ChevronRight className="ml-auto h-3.5 w-3.5 shrink-0" />
    </button>
  );
}

// DingTalkAgentBotConnectedBadge is the full "already connected"
// affordance the Integrations tab renders in place of the Bind button:
// green-dot status + Disconnect, and a secondary link to the DingTalk
// developer console (the scan-to-create flow yields no per-app deep
// link, so the console home is the closest management surface).
function DingTalkAgentBotConnectedBadge({
  installation,
  className,
}: {
  installation: DingTalkInstallation;
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
      await api.deleteDingTalkInstallation(wsId, installation.id);
      await qc.invalidateQueries({ queryKey: dingtalkKeys.installations(wsId) });
      toast.success(t(($) => $.dingtalk.toast_disconnected));
      setConfirmOpen(false);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.dingtalk.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div
      className={cn("space-y-2", className)}
      data-testid="dingtalk-agent-bot-connected"
    >
      <div className="flex items-center justify-between gap-3">
        <span className="inline-flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
          <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
          <span className="truncate">{t(($) => $.dingtalk.agent_bot_connected_label)}</span>
        </span>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => setConfirmOpen(true)}
          disabled={disconnecting}
          title={t(($) => $.dingtalk.agent_bot_disconnect_tooltip)}
          aria-label={t(($) => $.dingtalk.disconnect)}
          data-testid="dingtalk-agent-bot-disconnect"
        >
          <Trash2 className="h-3 w-3" />
          {disconnecting
            ? t(($) => $.dingtalk.disconnecting)
            : t(($) => $.dingtalk.disconnect)}
        </Button>
      </div>

      <a
        href={DINGTALK_DEV_CONSOLE}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline"
        title={t(($) => $.dingtalk.agent_bot_manage_tooltip)}
      >
        <ExternalLink className="h-3 w-3" />
        {t(($) => $.dingtalk.agent_bot_manage_link)}
      </a>

      <AlertDialog
        open={confirmOpen}
        onOpenChange={(v) => {
          if (!v && !disconnecting) setConfirmOpen(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.dingtalk.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.dingtalk.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.dingtalk.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDisconnect}
              disabled={disconnecting}
            >
              {disconnecting
                ? t(($) => $.dingtalk.disconnecting)
                : t(($) => $.dingtalk.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// DingTalkInstallDialog walks the user through the device-flow install:
// 1) POST /dingtalk/install/begin → render QR
// 2) poll /dingtalk/install/{sessionId}/status until success | error
// 3) on success: toast, close, invalidate installations cache
//
// The dialog re-fetches a fresh session on each "retry" rather than
// reusing a stale device_code — DingTalk's device_code is single-use
// and time-boxed. Session/polling state handling mirrors
// LarkInstallDialog (see that file for the StrictMode closedRef note).
function DingTalkInstallDialog({
  wsId,
  agentId,
  agentName,
  onClose,
}: {
  wsId: string;
  agentId: string;
  agentName?: string;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();

  const [session, setSession] = useState<null | {
    sessionId: string;
    qrCodeURL: string;
    expiresInSeconds: number;
    pollIntervalSeconds: number;
  }>(null);
  const [status, setStatus] = useState<DingTalkInstallStatusResponse["status"]>("pending");
  const [errorReason, setErrorReason] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [beginning, setBeginning] = useState(false);
  const closedRef = useRef(false);

  async function beginSession() {
    setBeginning(true);
    setStatus("pending");
    setErrorReason(null);
    setErrorMessage(null);
    setSession(null);
    try {
      const res = await api.beginDingTalkInstall(wsId, agentId);
      if (closedRef.current) return;
      setSession({
        sessionId: res.session_id,
        qrCodeURL: res.qr_code_url,
        expiresInSeconds: res.expires_in_seconds,
        pollIntervalSeconds: res.poll_interval_seconds,
      });
    } catch (e) {
      if (closedRef.current) return;
      setStatus("error");
      setErrorReason("internal_error");
      setErrorMessage(e instanceof Error ? e.message : String(e));
    } finally {
      setBeginning(false);
    }
  }

  useEffect(() => {
    closedRef.current = false;
    void beginSession();
    return () => {
      closedRef.current = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!session || status !== "pending") return;
    const intervalMs = Math.max(2000, session.pollIntervalSeconds * 1000);
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const poll = async () => {
      if (cancelled) return;
      try {
        const res = await api.getDingTalkInstallStatus(wsId, session.sessionId);
        if (cancelled) return;
        setStatus(res.status);
        if (res.status === "success") {
          await qc.invalidateQueries({ queryKey: dingtalkKeys.installations(wsId) });
          toast.success(t(($) => $.dingtalk.install_success_toast));
          setTimeout(() => {
            if (!cancelled) onClose();
          }, 800);
          return;
        }
        if (res.status === "error") {
          setErrorReason(res.error_reason ?? "internal_error");
          setErrorMessage(res.error_message ?? null);
          return;
        }
        timer = setTimeout(poll, intervalMs);
      } catch (e) {
        if (cancelled) return;
        // Terminal HTTP states must NOT be retried — same rationale as
        // LarkInstallDialog: 404 = session lost, 403/401 = permission
        // gone; anything else is transient and re-polls.
        if (e instanceof ApiError) {
          if (e.status === 404) {
            setStatus("error");
            setErrorReason("session_lost");
            setErrorMessage(e.message);
            return;
          }
          if (e.status === 403 || e.status === 401) {
            setStatus("error");
            setErrorReason("forbidden");
            setErrorMessage(e.message);
            return;
          }
        }
        timer = setTimeout(poll, intervalMs);
        toast.message(t(($) => $.dingtalk.install_poll_retry), {
          description: e instanceof Error ? e.message : String(e),
        });
      }
    };

    timer = setTimeout(poll, intervalMs);
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session?.sessionId, status]);

  return (
    <Dialog
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{t(($) => $.dingtalk.install_dialog_title)}</DialogTitle>
          <DialogDescription>
            {agentName
              ? t(($) => $.dingtalk.install_dialog_description_for_agent, { agent: agentName })
              : t(($) => $.dingtalk.install_dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-4 py-2">
          {beginning && !session && (
            <p className="text-sm text-muted-foreground">{t(($) => $.dingtalk.install_starting)}</p>
          )}

          {session && status === "pending" && (
            <>
              <div className="rounded-md border bg-white p-3">
                <QRCode value={session.qrCodeURL} size={192} />
              </div>
              <p className="text-center text-xs text-muted-foreground">
                {t(($) => $.dingtalk.install_scan_hint)}
              </p>
              <a
                href={session.qrCodeURL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs underline text-muted-foreground"
              >
                {t(($) => $.dingtalk.install_open_link_fallback)}
              </a>
            </>
          )}

          {status === "success" && (
            <p className="text-sm font-medium">{t(($) => $.dingtalk.install_success)}</p>
          )}

          {status === "error" && (
            <div className="space-y-2 text-center">
              <p className="text-sm font-medium text-destructive">
                {(() => {
                  switch (errorReason) {
                    case "expired":
                      return t(($) => $.dingtalk.install_error_expired);
                    case "install_failed":
                      return t(($) => $.dingtalk.install_error_install_failed);
                    case "dingtalk_protocol_error":
                      return t(($) => $.dingtalk.install_error_protocol);
                    case "credentials_check_failed":
                      return t(($) => $.dingtalk.install_error_credentials);
                    case "installation_conflict":
                      return t(($) => $.dingtalk.install_error_conflict);
                    case "session_lost":
                      return t(($) => $.dingtalk.install_error_session_lost);
                    case "forbidden":
                      return t(($) => $.dingtalk.install_error_forbidden);
                    default:
                      return t(($) => $.dingtalk.install_error_generic);
                  }
                })()}
              </p>
              {errorMessage && (
                <p className="text-[10px] text-muted-foreground break-all">
                  {errorMessage}
                </p>
              )}
            </div>
          )}
        </div>

        <DialogFooter>
          {status === "error" ? (
            <>
              <Button variant="outline" size="sm" onClick={onClose}>
                {t(($) => $.dingtalk.install_close)}
              </Button>
              <Button size="sm" onClick={beginSession} disabled={beginning}>
                <RefreshCw className="h-3 w-3" />
                {t(($) => $.dingtalk.install_retry)}
              </Button>
            </>
          ) : (
            <Button variant="outline" size="sm" onClick={onClose}>
              {t(($) => $.dingtalk.install_close)}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
