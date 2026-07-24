"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import QRCode from "react-qr-code";
import { RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
import { wechatInstallationsOptions, wechatKeys } from "@multica/core/wechat";
import { api, ApiError } from "@multica/core/api";
import type { WechatInstallation, WechatInstallStatusResponse } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

// WechatTab is the workspace settings panel for WeChat ClawBot (iLink)
// installations. Listing is member-visible; the disconnect action is admin-only
// (the backend enforces it; the UI hides the button for non-admins to match).
//
// Adding a new installation flows through the Agent detail page: the install
// path is per-agent (each Multica agent gets exactly one bot — the
// (workspace_id, agent_id, channel_type) UNIQUE in channel_installation), so
// asking the user to pick an agent here would re-create that page's picker.
export function WechatTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading } = useQuery({
    ...wechatInstallationsOptions(wsId),
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
      await api.deleteWechatInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: wechatKeys.installations(wsId) });
      toast.success(t(($) => $.wechat.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.wechat.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="space-y-8">
      <section className="space-y-1">
        <p className="text-sm text-muted-foreground">
          {t(($) => $.wechat.page_description)}
        </p>
      </section>

      {!configured ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-sm font-medium">{t(($) => $.wechat.not_enabled_title)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.wechat.not_enabled_description_prefix)}{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                MULTICA_WECHAT_SECRET_KEY
              </code>{" "}
              {t(($) => $.wechat.not_enabled_description_suffix)}{" "}
              {t(($) => $.wechat.not_enabled_self_host_hint)}
            </p>
          </CardContent>
        </Card>
      ) : !installSupported && installations.length === 0 ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-sm font-medium">{t(($) => $.wechat.preview_title)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.wechat.preview_description)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <h2 className="text-sm font-semibold">{t(($) => $.wechat.connected_bots)}</h2>
          {isLoading ? (
            <Card>
              <CardContent>
                <p className="text-sm text-muted-foreground">{t(($) => $.wechat.loading)}</p>
              </CardContent>
            </Card>
          ) : installations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-sm font-medium">{t(($) => $.wechat.empty_title)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.wechat.empty_description_prefix)}{" "}
                  <strong>{t(($) => $.wechat.empty_description_cta)}</strong>{" "}
                  {t(($) => $.wechat.empty_description_suffix)}
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
              {t(($) => $.wechat.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.wechat.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.wechat.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.wechat.disconnecting)
                : t(($) => $.wechat.disconnect)}
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
  installation: WechatInstallation;
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
          <p className="text-sm font-medium">
            {agentName}
            {!isActive && (
              <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                {t(($) => $.wechat.revoked_badge)}
              </span>
            )}
          </p>
          <p className="text-[10px] text-muted-foreground">
            {installation.ilink_user_id || installation.app_id}
          </p>
        </div>
      </div>
      {canManage && isActive && (
        <Button variant="outline" size="sm" onClick={onDisconnect}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.wechat.disconnect)}
        </Button>
      )}
    </div>
  );
}

// WechatAgentBindButton is the agent-detail-page entry point for the QR-scan
// device-flow install. It is wired where SlackAgentBindButton / LarkAgentBindButton
// are (agent integrations tab). The dialog mirrors LarkInstallDialog: render a
// QR, poll status, surface terminal copy. Mirrors LarkInstallDialog.
export function WechatAgentBindButton({
  wsId,
  agentId,
  agentName,
  existing,
}: {
  wsId: string;
  agentId: string;
  agentName?: string;
  existing?: WechatInstallation;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [disconnectOpen, setDisconnectOpen] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);

  async function handleDisconnect() {
    if (!existing || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteWechatInstallation(wsId, existing.id);
      await qc.invalidateQueries({ queryKey: wechatKeys.installations(wsId) });
      toast.success(t(($) => $.wechat.toast_disconnected));
      setDisconnectOpen(false);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.wechat.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  // When already connected, show the connected badge alongside a "rebind"
  // button and a "disconnect" button — mirroring Lark's connected badge which
  // has inline Manage / Disconnect actions. Disconnect is a soft revoke
  // (status→revoked); the bot stops receiving messages immediately.
  if (existing && existing.status === "active") {
    return (
      <>
        <WechatAgentBotConnectedBadge installation={existing} />
        <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
          {t(($) => $.wechat.rebind_button)}
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => setDisconnectOpen(true)}
        >
          <Trash2 className="h-3 w-3" />
          {t(($) => $.wechat.disconnect)}
        </Button>
        {open && (
          <WechatInstallDialog
            wsId={wsId}
            agentId={agentId}
            agentName={agentName}
            onClose={() => setOpen(false)}
          />
        )}
        <AlertDialog
          open={disconnectOpen}
          onOpenChange={(v) => {
            if (!v && !disconnecting) setDisconnectOpen(false);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.wechat.disconnect_confirm_title)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.wechat.disconnect_confirm_description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={disconnecting}>
                {t(($) => $.wechat.disconnect_confirm_cancel)}
              </AlertDialogCancel>
              <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
                {disconnecting
                  ? t(($) => $.wechat.disconnecting)
                  : t(($) => $.wechat.disconnect)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </>
    );
  }

  return (
    <>
      <Button size="sm" onClick={() => setOpen(true)}>
        {t(($) => $.wechat.agent_bind_button)}
      </Button>
      {open && (
        <WechatInstallDialog
          wsId={wsId}
          agentId={agentId}
          agentName={agentName}
          onClose={() => setOpen(false)}
        />
      )}
    </>
  );
}

function WechatAgentBotConnectedBadge({ installation }: { installation: WechatInstallation }) {
  const { t } = useT("settings");
  const { getAgentName } = useActorName();
  const agentName = getAgentName(installation.agent_id);
  return (
    <div className="flex items-center gap-2 rounded-md border bg-muted/30 px-3 py-1.5">
      <ActorAvatar actorType="agent" actorId={installation.agent_id} size="sm" />
      <span className="text-xs text-muted-foreground">
        {t(($) => $.wechat.connected_as)} {agentName}
      </span>
    </div>
  );
}

// WechatInstallDialog renders the QR-login flow. Mirrors LarkInstallDialog
// closely: the iLink backend returns a QR URL the installer scans with their
// personal WeChat; the frontend polls /install/{sessionId}/status until success
// or terminal failure. The StrictMode double-mount cancellation guard is copied
// verbatim from Lark (without it the second mount's beginSession early-exits
// and the QR never appears).
function WechatInstallDialog({
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
  const [status, setStatus] = useState<WechatInstallStatusResponse["status"]>("pending");
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
      const res = await api.beginWechatInstall(wsId, agentId);
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

  // Reset closedRef AT THE START of every mount (React StrictMode runs effects
  // twice in dev). Without this, mount #1's cleanup flips closedRef=true and
  // mount #2's beginSession early-exits → "QR never appears" bug. See Lark.
  useEffect(() => {
    closedRef.current = false;
    void beginSession();
    return () => {
      closedRef.current = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Polling loop, bounded by the session expiry. Terminal HTTP states (404/403/
  // 401) are NOT retried; transient errors (network, 5xx) retry.
  useEffect(() => {
    if (!session || status !== "pending") return;
    const intervalMs = Math.max(2000, session.pollIntervalSeconds * 1000);
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const poll = async () => {
      if (cancelled) return;
      try {
        const res = await api.getWechatInstallStatus(wsId, session.sessionId);
        if (cancelled) return;
        setStatus(res.status);
        if (res.status === "success") {
          await qc.invalidateQueries({ queryKey: wechatKeys.installations(wsId) });
          toast.success(t(($) => $.wechat.install_success_toast));
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
        toast.message(t(($) => $.wechat.install_poll_retry), {
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
          <DialogTitle>{t(($) => $.wechat.install_dialog_title)}</DialogTitle>
          <DialogDescription>
            {agentName
              ? t(($) => $.wechat.install_dialog_description_for_agent, { agent: agentName })
              : t(($) => $.wechat.install_dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-4 py-2">
          {beginning && !session && (
            <p className="text-sm text-muted-foreground">{t(($) => $.wechat.install_starting)}</p>
          )}

          {session && status === "pending" && (
            <>
              <div className="rounded-md border bg-white p-3">
                <QRCode value={session.qrCodeURL} size={192} />
              </div>
              <p className="text-center text-xs text-muted-foreground">
                {t(($) => $.wechat.install_scan_hint)}
              </p>
              <a
                href={session.qrCodeURL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs underline text-muted-foreground"
              >
                {t(($) => $.wechat.install_open_link_fallback)}
              </a>
            </>
          )}

          {status === "success" && (
            <p className="text-sm font-medium">{t(($) => $.wechat.install_success)}</p>
          )}

          {status === "error" && (
            <div className="space-y-2 text-center">
              <p className="text-sm font-medium text-destructive">
                {(() => {
                  switch (errorReason) {
                    case "expired":
                      return t(($) => $.wechat.install_error_expired);
                    case "access_denied":
                      return t(($) => $.wechat.install_error_access_denied);
                    case "ilink_protocol_error":
                      return t(($) => $.wechat.install_error_protocol);
                    case "installation_conflict":
                      return t(($) => $.wechat.install_error_conflict);
                    case "installer_bind_failed":
                      return t(($) => $.wechat.install_error_installer_bind);
                    case "session_lost":
                      return t(($) => $.wechat.install_error_session_lost);
                    case "forbidden":
                      return t(($) => $.wechat.install_error_forbidden);
                    default:
                      return t(($) => $.wechat.install_error_generic);
                  }
                })()}
              </p>
              {errorMessage && (
                <p className="text-[10px] text-muted-foreground break-all">{errorMessage}</p>
              )}
            </div>
          )}
        </div>

        <DialogFooter>
          {status === "error" ? (
            <>
              <Button variant="outline" size="sm" onClick={onClose}>
                {t(($) => $.wechat.install_close)}
              </Button>
              <Button size="sm" onClick={beginSession} disabled={beginning}>
                <RefreshCw className="h-3 w-3" />
                {t(($) => $.wechat.install_retry)}
              </Button>
            </>
          ) : (
            <Button variant="outline" size="sm" onClick={onClose}>
              {t(($) => $.wechat.install_close)}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
