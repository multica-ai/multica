"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, ExternalLink, RefreshCw, Trash2 } from "lucide-react";
import { QRCode } from "react-qr-code";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
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
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { wecomInstallationsOptions, wecomKeys } from "@multica/core/wecom";
import { api, ApiError } from "@multica/core/api";
import type {
  Agent,
  WecomInstallation,
  WecomInstallStatusResponse,
} from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

function canManageInstallation(
  installation: WecomInstallation,
  agents: Agent[],
  userId: string | undefined,
  isWorkspaceAdmin: boolean,
): boolean {
  if (isWorkspaceAdmin) return true;
  const agent = agents.find((a) => a.id === installation.agent_id);
  return !!userId && agent?.owner_id != null && agent.owner_id === userId;
}

// WecomTab is the workspace settings panel for WeCom bot installations.
// Listing is member-visible; disconnect is per-row via canManageAgent
// (agent owner OR workspace owner/admin), not admin-only for every row.
export function WecomTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const isWorkspaceAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading } = useQuery({
    ...wecomInstallationsOptions(wsId),
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
      await api.deleteWecomInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: wecomKeys.installations(wsId) });
      toast.success(t(($) => $.wecom.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.wecom.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="space-y-8">
      <section className="space-y-1">
        <p className="text-body text-muted-foreground">
          {t(($) => $.wecom.page_description)}
        </p>
      </section>

      {!configured ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-body font-medium">{t(($) => $.wecom.not_enabled_title)}</p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.wecom.not_enabled_description_prefix)}{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-micro">
                MULTICA_WECOM_SECRET_KEY
              </code>{" "}
              {t(($) => $.wecom.not_enabled_description_suffix)}{" "}
              {t(($) => $.wecom.not_enabled_self_host_hint)}
            </p>
          </CardContent>
        </Card>
      ) : !installSupported && installations.length === 0 ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-body font-medium">{t(($) => $.wecom.preview_title)}</p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.wecom.preview_description)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <h2 className="text-body font-semibold">{t(($) => $.wecom.connected_bots)}</h2>
          {isLoading ? (
            <Card>
              <CardContent>
                <p className="text-body text-muted-foreground">{t(($) => $.wecom.loading)}</p>
              </CardContent>
            </Card>
          ) : installations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-body font-medium">{t(($) => $.wecom.empty_title)}</p>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.wecom.empty_description_prefix)}{" "}
                  <strong>{t(($) => $.wecom.empty_description_cta)}</strong>{" "}
                  {t(($) => $.wecom.empty_description_suffix)}
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
                    canManage={canManageInstallation(
                      inst,
                      agents,
                      user?.id,
                      isWorkspaceAdmin,
                    )}
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
              {t(($) => $.wecom.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.wecom.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.wecom.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.wecom.disconnecting)
                : t(($) => $.wecom.disconnect)}
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
  installation: WecomInstallation;
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
                {t(($) => $.wecom.revoked_badge)}
              </span>
            )}
          </p>
          <p className="text-micro text-muted-foreground">
            {t(($) => $.wecom.installed_at_label, {
              when: new Date(installation.installed_at).toLocaleString(),
            })}
          </p>
        </div>
      </div>
      {canManage && isActive && (
        <Button variant="outline" size="sm" onClick={onDisconnect}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.wecom.disconnect)}
        </Button>
      )}
    </div>
  );
}

export function WecomAgentBindButton({
  agentId,
  agentName,
  agentOwnerId,
  className,
  onShowConnectedDetails,
}: {
  agentId: string;
  agentName?: string;
  agentOwnerId?: string | null;
  className?: string;
  onShowConnectedDetails?: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const [dialogOpen, setDialogOpen] = useState(false);

  const { data: listing } = useQuery({
    ...wecomInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const installSupported = listing?.install_supported === true;
  const manualInstallSupported = listing?.manual_install_supported === true;

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const isWorkspaceAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";
  const isAgentOwner =
    !!user?.id && agentOwnerId != null && agentOwnerId === user.id;
  const canManage = isWorkspaceAdmin || isAgentOwner;

  if (!canManage) return null;

  const existing = listing?.installations.find(
    (inst) => inst.agent_id === agentId && inst.status === "active",
  );
  if (existing) {
    return onShowConnectedDetails ? (
      <WecomAgentBotStatusRow
        installation={existing}
        onClick={onShowConnectedDetails}
        className={className}
      />
    ) : (
      <WecomAgentBotConnectedBadge installation={existing} className={className} />
    );
  }

  // Scan and manual entry are independently available: manual needs only the
  // secretbox key, so a deployment without a provisioned WeCom scan source can
  // still connect a bot by hand.
  if (!installSupported && !manualInstallSupported) return null;

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
            ? t(($) => $.wecom.bind_button_title, { agent: agentName })
            : undefined
        }
        data-testid="wecom-agent-bind-button"
      >
        <ExternalLink className="h-3 w-3" />
        {t(($) => $.wecom.bind_button)}
      </Button>
      {dialogOpen && (
        <WecomInstallDialog
          wsId={wsId}
          agentId={agentId}
          agentName={agentName}
          scanSupported={installSupported}
          manualSupported={manualInstallSupported}
          onClose={() => setDialogOpen(false)}
        />
      )}
    </>
  );
}

function WecomAgentBotStatusRow({
  onClick,
  className,
}: {
  installation: WecomInstallation;
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
      data-testid="wecom-agent-bot-status"
    >
      <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
      <span className="truncate">{t(($) => $.wecom.agent_bot_connected_label)}</span>
      <ChevronRight className="ml-auto h-3.5 w-3.5 shrink-0" />
    </button>
  );
}

function WecomAgentBotConnectedBadge({
  installation,
  className,
}: {
  installation: WecomInstallation;
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
      await api.deleteWecomInstallation(wsId, installation.id);
      await qc.invalidateQueries({ queryKey: wecomKeys.installations(wsId) });
      toast.success(t(($) => $.wecom.toast_disconnected));
      setConfirmOpen(false);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.wecom.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className={cn("space-y-2", className)} data-testid="wecom-agent-bot-connected">
      <div className="flex items-center justify-between gap-3">
        <span className="inline-flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
          <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
          <span className="truncate">{t(($) => $.wecom.agent_bot_connected_label)}</span>
        </span>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => setConfirmOpen(true)}
          disabled={disconnecting}
          title={t(($) => $.wecom.agent_bot_disconnect_tooltip)}
          aria-label={t(($) => $.wecom.disconnect)}
          data-testid="wecom-agent-bot-disconnect"
        >
          <Trash2 className="h-3 w-3" />
          {disconnecting
            ? t(($) => $.wecom.disconnecting)
            : t(($) => $.wecom.disconnect)}
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
              {t(($) => $.wecom.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.wecom.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.wecom.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.wecom.disconnecting)
                : t(($) => $.wecom.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function WecomInstallDialog({
  wsId,
  agentId,
  agentName,
  scanSupported,
  manualSupported,
  onClose,
}: {
  wsId: string;
  agentId: string;
  agentName?: string;
  scanSupported: boolean;
  manualSupported: boolean;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();

  const idempotencyKeyRef = useRef(crypto.randomUUID());
  const closedRef = useRef(false);

  // Scan is the default; manual entry is the opt-in for connecting a bot that
  // already exists in WeCom. When scan is unavailable on this deployment the
  // dialog opens straight into the manual form.
  const [mode, setMode] = useState<"scan" | "manual">(
    scanSupported ? "scan" : "manual",
  );
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [qrCodeURL, setQrCodeURL] = useState<string | null>(null);
  const [status, setStatus] = useState<WecomInstallStatusResponse["status"]>("creating");
  const [errorReason, setErrorReason] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [beginning, setBeginning] = useState(false);
  const [pollIntervalSeconds, setPollIntervalSeconds] = useState(1);

  async function beginSession() {
    setBeginning(true);
    setStatus("creating");
    setErrorReason(null);
    setErrorMessage(null);
    setQrCodeURL(null);
    setSessionId(null);
    try {
      const beginRes = await api.beginWecomInstall(
        wsId,
        agentId,
        idempotencyKeyRef.current,
      );
      if (closedRef.current) return;
      setSessionId(beginRes.session_id);
      setStatus(beginRes.status);
      setPollIntervalSeconds(beginRes.poll_interval_seconds);

      let firstStatus;
      try {
        firstStatus = await api.getWecomInstallStatus(wsId, beginRes.session_id);
      } catch (e) {
        if (closedRef.current) return;
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
        throw e;
      }
      if (closedRef.current) return;
      setStatus(firstStatus.status);
      setPollIntervalSeconds(firstStatus.poll_interval_seconds);
      if (firstStatus.status === "pending" && firstStatus.qr_code_url) {
        setQrCodeURL(firstStatus.qr_code_url);
      }
      if (firstStatus.status === "error") {
        setErrorReason(firstStatus.error_reason ?? "internal_error");
        setErrorMessage(firstStatus.error_message ?? null);
        return;
      }
      if (firstStatus.status === "success") {
        await qc.invalidateQueries({ queryKey: wecomKeys.installations(wsId) });
        toast.success(t(($) => $.wecom.install_success_toast));
        setTimeout(() => {
          if (!closedRef.current) onClose();
        }, 800);
      }
    } catch (e) {
      if (closedRef.current) return;
      setStatus("error");
      setErrorReason("internal_error");
      setErrorMessage(e instanceof Error ? e.message : String(e));
    } finally {
      if (!closedRef.current) setBeginning(false);
    }
  }

  useEffect(() => {
    closedRef.current = false;
    if (mode === "scan") void beginSession();
    return () => {
      closedRef.current = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    // No session to poll in manual mode, and a stale scan session must stop
    // polling the moment the user switches away from it.
    if (mode !== "scan") return;
    if (!sessionId || status === "success" || status === "error") return;

    const intervalMs = Math.max(1000, pollIntervalSeconds * 1000);
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const poll = async () => {
      if (cancelled) return;
      try {
        const res = await api.getWecomInstallStatus(wsId, sessionId);
        if (cancelled) return;
        setStatus(res.status);
        setPollIntervalSeconds(res.poll_interval_seconds);
        if (res.status === "pending" && res.qr_code_url) {
          setQrCodeURL(res.qr_code_url);
        }
        if (res.status === "success") {
          await qc.invalidateQueries({ queryKey: wecomKeys.installations(wsId) });
          toast.success(t(($) => $.wecom.install_success_toast));
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
        toast.message(t(($) => $.wecom.install_poll_retry), {
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
  }, [sessionId, status, mode]);

  function handleRetry() {
    idempotencyKeyRef.current = crypto.randomUUID();
    void beginSession();
  }

  // switchMode clears the previous mode's outcome so a failed scan does not
  // leave its error banner sitting above the manual form (and vice versa).
  function switchMode(next: "scan" | "manual") {
    setErrorReason(null);
    setErrorMessage(null);
    setMode(next);
    if (next === "scan") {
      idempotencyKeyRef.current = crypto.randomUUID();
      void beginSession();
    }
  }

  async function handleManualSubmit(botId: string, secret: string) {
    setErrorReason(null);
    setErrorMessage(null);
    try {
      await api.manualWecomInstall(wsId, agentId, botId, secret);
      if (closedRef.current) return;
      await qc.invalidateQueries({ queryKey: wecomKeys.installations(wsId) });
      setStatus("success");
      toast.success(t(($) => $.wecom.install_success_toast));
      setTimeout(() => {
        if (!closedRef.current) onClose();
      }, 800);
    } catch (e) {
      if (closedRef.current) return;
      // ApiError.message is the server's stable code, which the switch below
      // localizes. Anything else is an unexpected failure.
      setStatus("error");
      setErrorReason(e instanceof ApiError ? e.message : "internal_error");
      if (!(e instanceof ApiError)) {
        setErrorMessage(e instanceof Error ? e.message : String(e));
      }
      throw e;
    }
  }

  const showCreating = mode === "scan" && (beginning || status === "creating");
  const showQr = mode === "scan" && status === "pending" && !!qrCodeURL;

  return (
    <Dialog
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{t(($) => $.wecom.install_dialog_title)}</DialogTitle>
          <DialogDescription>
            {mode === "manual"
              ? t(($) => $.wecom.manual_dialog_description)
              : agentName
                ? t(($) => $.wecom.install_dialog_description_for_agent, { agent: agentName })
                : t(($) => $.wecom.install_dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-4 py-2">
          {showCreating && !showQr && (
            <p className="text-body text-muted-foreground">
              {t(($) => $.wecom.install_starting)}
            </p>
          )}

          {showQr && (
            <>
              <div className="rounded-md border bg-white p-3">
                <QRCode value={qrCodeURL} size={192} />
              </div>
              <p className="text-center text-caption text-muted-foreground">
                {t(($) => $.wecom.install_scan_hint)}
              </p>
              <a
                href={qrCodeURL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-caption underline text-muted-foreground"
              >
                {t(($) => $.wecom.install_open_link_fallback)}
              </a>
            </>
          )}

          {status === "success" && (
            <p className="text-body font-medium">{t(($) => $.wecom.install_success)}</p>
          )}

          {status === "error" && (
            <div className="space-y-2 text-center">
              <p className="text-body font-medium text-destructive">
                {(() => {
                  switch (errorReason) {
                    case "expired":
                      return t(($) => $.wecom.install_error_expired);
                    case "generate_failed":
                      return t(($) => $.wecom.install_error_generate_failed);
                    case "integration_unconfigured":
                      return t(($) => $.wecom.install_error_unconfigured);
                    case "installation_conflict":
                      return t(($) => $.wecom.install_error_conflict);
                    case "wecom_protocol_error":
                      return t(($) => $.wecom.install_error_protocol);
                    case "session_lost":
                      return t(($) => $.wecom.install_error_session_lost);
                    case "forbidden":
                      return t(($) => $.wecom.install_error_forbidden);
                    case "bot_credentials_required":
                      return t(($) => $.wecom.manual_error_credentials_required);
                    case "invalid_bot_credentials":
                      return t(($) => $.wecom.manual_error_invalid_credentials);
                    case "verify_unavailable":
                      return t(($) => $.wecom.manual_error_verify_unavailable);
                    case "bot_id_owned_by_another_workspace":
                      return t(($) => $.wecom.manual_error_bot_id_other_workspace);
                    case "bot_id_owned_in_this_workspace":
                      return t(($) => $.wecom.manual_error_bot_id_this_workspace);
                    case "bot_id_owned_by_archived_agent":
                      return t(($) => $.wecom.manual_error_bot_id_archived_agent);
                    default:
                      return t(($) => $.wecom.install_error_generic);
                  }
                })()}
              </p>
              {errorMessage && (
                <p className="text-micro text-muted-foreground break-all">
                  {errorMessage}
                </p>
              )}
            </div>
          )}

          {mode === "manual" && status !== "success" && (
            <WecomManualInstallForm onSubmit={handleManualSubmit} />
          )}

          {status !== "success" && (
            <>
              {mode === "scan" && manualSupported && (
                <Button
                  variant="link"
                  size="sm"
                  className="h-auto p-0 text-caption"
                  onClick={() => switchMode("manual")}
                  data-testid="wecom-install-switch-manual"
                >
                  {t(($) => $.wecom.manual_switch_cta)}
                </Button>
              )}
              {mode === "manual" && scanSupported && (
                <Button
                  variant="link"
                  size="sm"
                  className="h-auto p-0 text-caption"
                  onClick={() => switchMode("scan")}
                  data-testid="wecom-install-switch-scan"
                >
                  {t(($) => $.wecom.manual_back_to_scan_cta)}
                </Button>
              )}
            </>
          )}
        </div>

        <DialogFooter>
          {status === "error" && mode === "scan" ? (
            <>
              <Button variant="outline" size="sm" onClick={onClose}>
                {t(($) => $.wecom.install_close)}
              </Button>
              <Button size="sm" onClick={handleRetry} disabled={beginning}>
                <RefreshCw className="h-3 w-3" />
                {t(($) => $.wecom.install_retry)}
              </Button>
            </>
          ) : (
            <Button variant="outline" size="sm" onClick={onClose}>
              {t(($) => $.wecom.install_close)}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// WecomManualInstallForm collects an existing bot's credentials. The secret is
// masked and never echoed back by the server, so there is no "current value"
// to prefill on a retry — a failed submit keeps what the user typed so they can
// fix a typo instead of re-entering both fields.
function WecomManualInstallForm({
  onSubmit,
}: {
  onSubmit: (botId: string, secret: string) => Promise<void>;
}) {
  const { t } = useT("settings");
  const [botId, setBotId] = useState("");
  const [secret, setSecret] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const canSubmit = botId.trim() !== "" && secret.trim() !== "" && !submitting;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      await onSubmit(botId.trim(), secret.trim());
    } catch {
      // The dialog owns error display; the form only needs to re-enable.
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form
      className="w-full space-y-3"
      onSubmit={handleSubmit}
      data-testid="wecom-manual-install-form"
    >
      <div className="space-y-1.5">
        <Label htmlFor="wecom-manual-bot-id">
          {t(($) => $.wecom.manual_bot_id_label)}
        </Label>
        <Input
          id="wecom-manual-bot-id"
          value={botId}
          onChange={(e) => setBotId(e.target.value)}
          placeholder={t(($) => $.wecom.manual_bot_id_placeholder)}
          autoComplete="off"
          spellCheck={false}
          disabled={submitting}
        />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="wecom-manual-secret">
          {t(($) => $.wecom.manual_secret_label)}
        </Label>
        <Input
          id="wecom-manual-secret"
          type="password"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          placeholder={t(($) => $.wecom.manual_secret_placeholder)}
          autoComplete="off"
          spellCheck={false}
          disabled={submitting}
        />
      </div>
      <p className="text-micro text-muted-foreground">
        {t(($) => $.wecom.manual_hint)}
      </p>
      <Button type="submit" size="sm" className="w-full" disabled={!canSubmit}>
        {submitting
          ? t(($) => $.wecom.manual_submitting)
          : t(($) => $.wecom.manual_submit)}
      </Button>
    </form>
  );
}
