"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ExternalLink, RefreshCw, Trash2 } from "lucide-react";
// Named import, NOT default: react-qr-code is CJS, and electron-vite's
// dep-optimizer default-import interop handed back the module namespace
// object instead of the component, throwing "Element type is invalid …
// got: object" the moment <QRCode> mounted (the QR step of the install
// dialog) — desktop white-screened while web (Next.js, different interop)
// was fine. The named export maps straight to `exports.QRCode` and
// resolves correctly under both bundlers.
import { QRCode } from "react-qr-code";
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
import { larkInstallationsOptions, larkKeys } from "@multica/core/lark";
import { api, ApiError } from "@multica/core/api";
import type { LarkInstallation, LarkInstallStatusResponse } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

// LarkTab is the workspace settings panel for Lark Bot installations.
// Listing is member-visible; the disconnect action is admin-only (the
// backend enforces it; the UI hides the button for non-admins to match).
//
// Adding a new installation flows through the Agent detail page: the
// install path is per-agent (each Multica Agent gets exactly one Bot —
// see the (workspace_id, agent_id) UNIQUE in lark_installation), so
// asking the user to pick an agent here would re-create that page's
// picker. The "Bind your first agent" copy in the empty state hints
// users at the right entry point.
export function LarkTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading } = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const installations = data?.installations ?? [];
  const configured = data?.configured === true;
  // install_supported tracks whether the device-flow install path is
  // wired end-to-end on the server. When false, scan-to-bind would
  // fail at the post-poll bot-info step, so we hide install entry
  // points and surface a "coming soon" notice in their place rather
  // than send users into a broken flow. Already-installed bots still
  // appear in the listing below and remain manageable.
  const installSupported = data?.install_supported === true;

  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteLarkInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: larkKeys.installations(wsId) });
      toast.success(t(($) => $.lark.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.lark.toast_disconnect_failed));
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="space-y-8">
      <section className="space-y-1">
        <p className="text-sm text-muted-foreground">
          {t(($) => $.lark.page_description)}
        </p>
      </section>

      {!configured ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-sm font-medium">{t(($) => $.lark.not_enabled_title)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.lark.not_enabled_description_prefix)}{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-[10px]">
                MULTICA_LARK_SECRET_KEY
              </code>{" "}
              {t(($) => $.lark.not_enabled_description_suffix)}{" "}
              {t(($) => $.lark.not_enabled_self_host_hint)}
            </p>
          </CardContent>
        </Card>
      ) : !installSupported && installations.length === 0 ? (
        // Device-flow install path is not wired (HTTP client is the stub
        // or RegistrationService didn't initialize). We deliberately do
        // NOT direct users to the agent-detail "Bind" button because the
        // backend would 503 anyway. Existing installations still render
        // via the branch below; this only hides the empty-state CTA
        // when there is nothing to manage.
        <Card>
          <CardContent className="space-y-2">
            <p className="text-sm font-medium">{t(($) => $.lark.preview_title)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.lark.preview_description)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <h2 className="text-sm font-semibold">{t(($) => $.lark.connected_bots)}</h2>
          {isLoading ? (
            <Card>
              <CardContent>
                <p className="text-sm text-muted-foreground">{t(($) => $.lark.loading)}</p>
              </CardContent>
            </Card>
          ) : installations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-sm font-medium">{t(($) => $.lark.empty_title)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.lark.empty_description_prefix)}{" "}
                  <strong>{t(($) => $.lark.empty_description_cta)}</strong>{" "}
                  {t(($) => $.lark.empty_description_suffix)}
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
              {t(($) => $.lark.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.lark.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.lark.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.lark.disconnecting)
                : t(($) => $.lark.disconnect)}
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
  installation: LarkInstallation;
  canManage: boolean;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  // The bot is bound 1:1 to a Multica Agent (per the (workspace_id,
  // agent_id) UNIQUE in lark_installation). Render the Multica agent's
  // identity here rather than the raw Lark app_id / bot_open_id — those
  // mean nothing to product users. getAgentName falls back to
  // "Unknown Agent" when the agent has been deleted; the Disconnect
  // affordance below is the recovery path for that orphan row.
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
                {t(($) => $.lark.revoked_badge)}
              </span>
            )}
          </p>
          <p className="text-[10px] text-muted-foreground">
            {t(($) => $.lark.installed_at_label, {
              when: new Date(installation.installed_at).toLocaleString(),
            })}
          </p>
        </div>
      </div>
      {canManage && isActive && (
        <Button variant="outline" size="sm" onClick={onDisconnect}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.lark.disconnect)}
        </Button>
      )}
    </div>
  );
}

// LarkAgentBindButton is the per-agent CTA we expose from the agent
// detail page. The Settings panel above is the management view; this
// button is the entry point.
//
// Visibility rules, in order:
//   1. Non-owner/admin viewers see nothing — the backend gates
//      `POST /lark/install/begin`, the status poll, AND disconnect on
//      those roles (see server/cmd/server/router.go), and `canEditAgent`
//      lets agent owners through even when they're not workspace admins,
//      so the parent's `canEdit` gate alone would expose controls that
//      are guaranteed to 403.
//   2. If this agent ALREADY has an active installation, owner/admins see
//      the "Connected + Manage in Lark" badge — regardless of
//      install_supported. install_supported governs only whether NEW
//      scan-installs can complete; already-installed bots stay manageable
//      when the device-flow transport is unwired (see
//      server/internal/handler/lark.go — "already-installed bots still
//      appear and remain manageable"). Gating the badge on it would hide a
//      bound agent's connected state the moment the transport went away.
//   3. Otherwise the Bind CTA shows only when install_supported is true —
//      a fresh scan against a stub transport would fail at the post-poll
//      bot-info step, so we don't surface a flow that's guaranteed to fail.
export function LarkAgentBindButton({
  agentId,
  agentName,
  className,
}: {
  agentId: string;
  agentName?: string;
  className?: string;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const [dialogOpen, setDialogOpen] = useState(false);

  const { data: listing } = useQuery({
    ...larkInstallationsOptions(wsId),
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

  // Existing-installation check runs BEFORE the install_supported gate:
  // already-installed bots stay manageable even when new scan-installs are
  // unavailable (server/internal/handler/lark.go). Surfacing the badge here
  // also closes the re-scan zombie-bot trap — re-scanning the same agent
  // upserts the row and orphans the previously-created PersonalAgent, so we
  // close the install entry point and link to the Bot's Lark app page where
  // scopes / display name / additional permissions are actually managed.
  const existing = listing?.installations.find(
    (inst) => inst.agent_id === agentId && inst.status === "active",
  );
  if (existing) {
    return (
      <LarkAgentBotConnectedBadge installation={existing} className={className} />
    );
  }

  // No existing bot and the device-flow transport isn't wired end-to-end:
  // a fresh scan would fail at the post-poll bot-info step, so hide the CTA.
  if (!installSupported) return null;

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        onClick={() => setDialogOpen(true)}
        disabled={!agentId}
        className={className}
        title={agentName ? t(($) => $.lark.bind_button_title, { agent: agentName }) : undefined}
      >
        <ExternalLink className="h-3 w-3" />
        {t(($) => $.lark.bind_button)}
      </Button>
      {dialogOpen && (
        <LarkInstallDialog
          wsId={wsId}
          agentId={agentId}
          agentName={agentName}
          onClose={() => setDialogOpen(false)}
        />
      )}
    </>
  );
}

// LarkAgentBotConnectedBadge is the "already connected" affordance the
// agent inspector renders in place of the Bind button when this agent
// has an active Lark installation. The badge is non-interactive (just
// a status pill), the Manage link opens the Bot's dev console page in
// a new tab so the user can manage scopes / display name / additional
// permissions without re-scanning the QR.
//
// The dev console URL host follows the same default as the backend's
// LARK_BASE_URL (open.feishu.cn for mainland Lark). Operators on the
// Lark international tenant currently see the wrong host; future-
// proofing requires the backend to surface a per-installation
// `dev_console_url` on the listings response. Tracked separately.
const LARK_DEV_CONSOLE_HOST = "https://open.feishu.cn";

function LarkAgentBotConnectedBadge({
  installation,
  className,
}: {
  installation: LarkInstallation;
  className?: string;
}) {
  const { t } = useT("settings");
  const manageHref = `${LARK_DEV_CONSOLE_HOST}/app/${encodeURIComponent(installation.app_id)}`;
  return (
    <div className={className} data-testid="lark-agent-bot-connected">
      <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
        <span className="inline-block h-2 w-2 rounded-full bg-emerald-500" />
        {t(($) => $.lark.agent_bot_connected_label)}
      </span>
      <a
        href={manageHref}
        target="_blank"
        rel="noopener noreferrer"
        className="ml-3 inline-flex items-center gap-1 text-xs text-primary underline-offset-2 hover:underline"
        title={t(($) => $.lark.agent_bot_manage_tooltip)}
      >
        <ExternalLink className="h-3 w-3" />
        {t(($) => $.lark.agent_bot_manage_link)}
      </a>
    </div>
  );
}

// LarkInstallDialog walks the user through the device-flow install:
// 1) POST /lark/install/begin → render QR
// 2) poll /lark/install/{sessionId}/status until success | error | expiry
// 3) on success: toast, close, invalidate installations cache
//
// The dialog deliberately re-fetches a fresh session on each "retry"
// rather than reusing a stale device_code — Lark's device_code is
// single-use and a re-render of the same QR after an error would just
// fail again at the next poll.
function LarkInstallDialog({
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

  // We track session lifecycle as local state because TanStack Query is
  // optimized for cached server reads, and this dialog is a one-shot
  // flow whose entire state collapses on close. Using `useQuery` for
  // the polling would also fight TanStack's default refetch heuristics
  // (window focus, online/offline, retries) that have the wrong shape
  // for a single bounded session.
  const [session, setSession] = useState<null | {
    sessionId: string;
    qrCodeURL: string;
    expiresInSeconds: number;
    pollIntervalSeconds: number;
  }>(null);
  const [status, setStatus] = useState<LarkInstallStatusResponse["status"]>("pending");
  const [errorReason, setErrorReason] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [beginning, setBeginning] = useState(false);
  const closedRef = useRef(false);

  // beginSession is callable from both the initial mount and the
  // "scan again" action. Wrapping in a function (instead of a useEffect
  // dependency cascade) makes the retry path explicit.
  async function beginSession() {
    setBeginning(true);
    setStatus("pending");
    setErrorReason(null);
    setErrorMessage(null);
    setSession(null);
    try {
      const res = await api.beginLarkInstall(wsId, agentId);
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

  // Kick off on mount.
  //
  // Reset closedRef AT THE START of every mount, not just at construction.
  // React 18+ / 19 StrictMode dev runs effects twice (mount → cleanup →
  // mount) on the same component instance, which preserves useRef across
  // the simulated remount. Without resetting, the cleanup from mount #1
  // flips closedRef.current=true, and on mount #2 every beginSession
  // promise sees closedRef=true at the await boundary and early-exits
  // before calling setSession — leaving the dialog body empty (no
  // "starting" placeholder, no QR, no error), which is exactly the
  // "QR never appears" bug. Reset on entry so the second mount
  // re-arms the in-flight cancellation guard.
  useEffect(() => {
    closedRef.current = false;
    void beginSession();
    return () => {
      closedRef.current = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Polling loop. Bounded by the device-flow expiry — once that
  // elapses Lark's server returns expired_token and our backend marks
  // the session errored, so we don't need a separate client-side
  // expiry timer.
  useEffect(() => {
    if (!session || status !== "pending") return;
    const intervalMs = Math.max(2000, session.pollIntervalSeconds * 1000);
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const poll = async () => {
      if (cancelled) return;
      try {
        const res = await api.getLarkInstallStatus(wsId, session.sessionId);
        if (cancelled) return;
        setStatus(res.status);
        if (res.status === "success") {
          await qc.invalidateQueries({ queryKey: larkKeys.installations(wsId) });
          toast.success(t(($) => $.lark.install_success_toast));
          // Close after a tiny beat so the user sees the success state
          // briefly before the dialog disappears.
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
        // Terminal HTTP states must NOT be retried — the session is
        // gone or the caller has lost permission, and polling forever
        // would trap the user on a stale QR with no error feedback.
        // 404: server restarted, multi-instance routed elsewhere, or
        //      the in-process GC swept the session. Treat as session
        //      lost — user has to scan a fresh QR.
        // 403: permission revoked mid-session (role downgrade, etc.).
        //      The CTA gate prevents this on entry, but a role change
        //      while the dialog is open would land here.
        // 401: the global ApiClient interceptor handles re-auth, so
        //      reaching the catch with 401 means re-auth itself
        //      failed — treat as terminal so the user doesn't loop.
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
        // Transient errors (network blip, 5xx) — schedule another
        // poll rather than killing the session. The next backend
        // status read will either confirm pending or surface the
        // terminal error the polling goroutine recorded.
        timer = setTimeout(poll, intervalMs);
        // Surface the message as a non-blocking toast for diagnostics.
        toast.message(t(($) => $.lark.install_poll_retry), {
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
          <DialogTitle>{t(($) => $.lark.install_dialog_title)}</DialogTitle>
          <DialogDescription>
            {agentName
              ? t(($) => $.lark.install_dialog_description_for_agent, { agent: agentName })
              : t(($) => $.lark.install_dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-4 py-2">
          {beginning && !session && (
            <p className="text-sm text-muted-foreground">{t(($) => $.lark.install_starting)}</p>
          )}

          {session && status === "pending" && (
            <>
              <div className="rounded-md border bg-white p-3">
                {/* react-qr-code renders an inline SVG — no external
                  network image dependency, prints at any DPI. */}
                <QRCode value={session.qrCodeURL} size={192} />
              </div>
              <p className="text-center text-xs text-muted-foreground">
                {t(($) => $.lark.install_scan_hint)}
              </p>
              <a
                href={session.qrCodeURL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs underline text-muted-foreground"
              >
                {t(($) => $.lark.install_open_link_fallback)}
              </a>
            </>
          )}

          {status === "success" && (
            <p className="text-sm font-medium">{t(($) => $.lark.install_success)}</p>
          )}

          {status === "error" && (
            <div className="space-y-2 text-center">
              <p className="text-sm font-medium text-destructive">
                {(() => {
                  switch (errorReason) {
                    case "expired":
                      return t(($) => $.lark.install_error_expired);
                    case "access_denied":
                      return t(($) => $.lark.install_error_access_denied);
                    case "lark_protocol_error":
                      return t(($) => $.lark.install_error_protocol);
                    case "bot_info_failed":
                      return t(($) => $.lark.install_error_bot_info);
                    case "installation_conflict":
                      return t(($) => $.lark.install_error_conflict);
                    case "installer_bind_failed":
                      return t(($) => $.lark.install_error_installer_bind);
                    case "session_lost":
                      return t(($) => $.lark.install_error_session_lost);
                    case "forbidden":
                      return t(($) => $.lark.install_error_forbidden);
                    default:
                      return t(($) => $.lark.install_error_generic);
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
                {t(($) => $.lark.install_close)}
              </Button>
              <Button size="sm" onClick={beginSession} disabled={beginning}>
                <RefreshCw className="h-3 w-3" />
                {t(($) => $.lark.install_retry)}
              </Button>
            </>
          ) : (
            <Button variant="outline" size="sm" onClick={onClose}>
              {t(($) => $.lark.install_close)}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
