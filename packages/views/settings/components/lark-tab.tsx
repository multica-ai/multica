"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, ExternalLink, RefreshCw, Trash2 } from "lucide-react";
// Named import, NOT default: react-qr-code is CJS, and electron-vite's
// dep-optimizer default-import interop handed back the module namespace
// object instead of the component, throwing "Element type is invalid …
// got: object" the moment <QRCode> mounted (the QR step of the install
// dialog) — desktop white-screened while web (Next.js, different interop)
// was fine. The named export maps straight to `exports.QRCode` and
// resolves correctly under both bundlers.
import { QRCode } from "react-qr-code";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
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
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { api, ApiError } from "@multica/core/api";
import type { AgentRuntime, LarkInstallation, LarkInstallStatusResponse } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { LarkNotificationEvents } from "./lark-notification-events";

// MUL-3083: the Lark (international, open.larksuite.com) "connect a Bot"
// entry is temporarily hidden while its install → inbound pipeline is
// stabilized — some Lark installs complete on Lark's side but never land a
// `lark_installation` row, so the Bot silently can't receive messages.
// Mainland Feishu is unaffected and keeps its bind entry. Existing
// installations (either cloud) stay fully manageable. Flip this back to
// `true` to restore the "Bind to Lark" CTA; nothing else needs to change.
const LARK_INTL_CONNECT_ENABLED: boolean = false;

// Runtime Bot is kept behind a local product flag while the workspace
// Notification Bot remains the only settings-level Feishu entry. The
// backend and existing installations stay intact for a later rollout.
const RUNTIME_BOT_CONNECT_ENABLED: boolean = false;

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
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery({
    ...runtimeListOptions(wsId),
    enabled: RUNTIME_BOT_CONNECT_ENABLED && !!wsId,
  });
  const installations = data?.installations ?? [];
  const agentInstallations = installations.filter(
    (inst) => (inst.installation_kind ?? "agent") === "agent",
  );
  const memberBot = installations.find(
    (inst) => inst.installation_kind === "member" && inst.status === "active",
  );
  const notificationBot = installations.find(
    (inst) => inst.installation_kind === "notification" && inst.status === "active",
  );
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
  const [notificationDialogOpen, setNotificationDialogOpen] = useState(false);
  const [runtimeDialog, setRuntimeDialog] = useState<null | {
    runtimeId: string;
    runtimeName: string;
  }>(null);
  const [selectedRuntimeId, setSelectedRuntimeId] = useState("");
  const selectedRuntime =
    runtimes.find((runtime) => runtime.id === selectedRuntimeId) ?? runtimes[0] ?? null;
  const selectedRuntimeInstallation = selectedRuntime
    ? agentInstallations.find(
        (inst) =>
          inst.runtime_id === selectedRuntime.id && inst.status === "active",
      ) ?? null
    : null;

  useEffect(() => {
    if (selectedRuntimeId || runtimes.length === 0) return;
    const preferred = runtimes.find((runtime) => runtime.status === "online") ?? runtimes[0];
    setSelectedRuntimeId(preferred?.id ?? "");
  }, [runtimes, selectedRuntimeId]);

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

      <section className="space-y-3">
        <div>
          <h2 className="text-sm font-semibold">{t(($) => $.notifications.lark.title)}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t(($) => $.notifications.lark.description)}
          </p>
        </div>

        <Card>
          <CardContent>
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="space-y-0.5">
                <p className="text-sm font-medium">
                  {memberBot
                    ? t(($) => $.notifications.lark.connected_label)
                    : t(($) => $.notifications.lark.disconnected_label)}
                </p>
                <p className="text-xs text-muted-foreground">
                  {memberBot
                    ? t(($) => $.notifications.lark.connected_hint)
                    : configured && installSupported
                      ? t(($) => $.notifications.lark.disconnected_hint)
                      : t(($) => $.notifications.lark.unavailable_hint)}
                </p>
              </div>
              {memberBot ? (
                <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                  <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
                  {t(($) => $.notifications.lark.feishu_badge)}
                </span>
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={!configured || !installSupported}
                  onClick={() => setNotificationDialogOpen(true)}
                >
                  <ExternalLink className="h-3 w-3" />
                  {t(($) => $.notifications.lark.connect_feishu)}
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
        {(memberBot || notificationBot) && (
          <LarkNotificationEvents
            workspaceId={wsId}
            installation={memberBot ?? notificationBot}
            eventTypes={data?.notification_event_types}
            canManage={canManage}
          />
        )}
      </section>

      {RUNTIME_BOT_CONNECT_ENABLED && <section className="space-y-3">
        <div>
          <h2 className="text-sm font-semibold">{t(($) => $.lark.runtime_bot_title)}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t(($) => $.lark.runtime_bot_description)}
          </p>
        </div>

        <Card>
          <CardContent>
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div className="space-y-2">
                <p className="text-sm font-medium">
                  {selectedRuntimeInstallation
                    ? t(($) => $.lark.runtime_bot_connected_label)
                    : t(($) => $.lark.runtime_bot_disconnected_label)}
                </p>
                <p className="text-xs text-muted-foreground">
                  {!canManage
                    ? t(($) => $.lark.runtime_bot_admin_required)
                    : !configured || !installSupported
                    ? t(($) => $.lark.runtime_bot_unavailable)
                    : runtimes.length === 0
                    ? t(($) => $.lark.runtime_bot_no_runtime)
                    : selectedRuntimeInstallation
                    ? t(($) => $.lark.runtime_bot_connected_hint)
                    : t(($) => $.lark.runtime_bot_disconnected_hint)}
                </p>
              </div>
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <NativeSelect
                  size="sm"
                  className="w-full sm:w-56"
                  value={selectedRuntimeId}
                  disabled={!canManage || runtimesLoading || runtimes.length === 0}
                  onChange={(e) => setSelectedRuntimeId(e.target.value)}
                  aria-label={t(($) => $.lark.runtime_bot_select_label)}
                >
                  {runtimes.length === 0 ? (
                    <NativeSelectOption value="">
                      {t(($) => $.lark.runtime_bot_select_empty)}
                    </NativeSelectOption>
                  ) : (
                    runtimes.map((runtime) => (
                      <NativeSelectOption key={runtime.id} value={runtime.id}>
                        {runtimeLabel(runtime)}
                      </NativeSelectOption>
                    ))
                  )}
                </NativeSelect>
                {selectedRuntimeInstallation ? (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!canManage}
                    onClick={() => setDisconnectTarget(selectedRuntimeInstallation.id)}
                  >
                    <Trash2 className="h-3 w-3" />
                    {t(($) => $.lark.disconnect)}
                  </Button>
                ) : (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!canManage || !configured || !installSupported || !selectedRuntime}
                    onClick={() => {
                      if (!selectedRuntime) return;
                      setRuntimeDialog({
                        runtimeId: selectedRuntime.id,
                        runtimeName: runtimeLabel(selectedRuntime),
                      });
                    }}
                  >
                    <ExternalLink className="h-3 w-3" />
                    {t(($) => $.lark.runtime_bot_connect_feishu)}
                  </Button>
                )}
              </div>
            </div>
          </CardContent>
        </Card>
      </section>}

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
      ) : !installSupported && agentInstallations.length === 0 ? (
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
          ) : agentInstallations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-sm font-medium">{t(($) => $.lark.empty_title)}</p>
                <p className="text-xs text-muted-foreground">
                  {t(($) => $.lark.empty_description_runtime)}
                </p>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="divide-y">
                {agentInstallations.map((inst) => (
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

      {notificationDialogOpen && (
        <LarkNotificationInstallDialog
          wsId={wsId}
          onClose={() => setNotificationDialogOpen(false)}
          onInstalled={() => {
            void qc.invalidateQueries({ queryKey: larkKeys.installations(wsId) });
          }}
        />
      )}
      {RUNTIME_BOT_CONNECT_ENABLED && runtimeDialog && (
        <LarkInstallDialog
          wsId={wsId}
          runtimeId={runtimeDialog.runtimeId}
          runtimeName={runtimeDialog.runtimeName}
          region="feishu"
          onClose={() => setRuntimeDialog(null)}
        />
      )}
    </div>
  );
}

function runtimeLabel(runtime: AgentRuntime): string {
  const name = runtime.name?.trim() || runtime.provider || "Runtime";
  const status = runtime.status === "online" ? "online" : runtime.status || "offline";
  return `${name} · ${status}`;
}

function LarkNotificationInstallDialog({
  wsId,
  onClose,
  onInstalled,
}: {
  wsId: string;
  onClose: () => void;
  onInstalled: () => void;
}) {
  const { t } = useT("settings");
  const [session, setSession] = useState<null | {
    sessionId: string;
    qrCodeURL: string;
    expiresInSeconds: number;
    pollIntervalSeconds: number;
  }>(null);
  const [status, setStatus] = useState<LarkInstallStatusResponse["status"]>("pending");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [beginning, setBeginning] = useState(false);
  const closedRef = useRef(false);

  async function beginSession() {
    setBeginning(true);
    setStatus("pending");
    setErrorMessage(null);
    setSession(null);
    try {
      const res = await api.beginLarkMemberInstall(wsId, "feishu");
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
    let cancelled = false;
    let timeout: ReturnType<typeof setTimeout> | null = null;
    const startedAt = Date.now();

    async function poll() {
      if (cancelled || !session) return;
      if (Date.now() - startedAt > session.expiresInSeconds * 1000) {
        setStatus("error");
        setErrorMessage(t(($) => $.notifications.lark.install_expired));
        return;
      }
      try {
        const res = await api.getLarkInstallStatus(wsId, session.sessionId);
        if (cancelled) return;
        if (res.status === "success") {
          setStatus("success");
          toast.success(t(($) => $.notifications.lark.toast_connected));
          onInstalled();
          onClose();
          return;
        }
        if (res.status === "error") {
          setStatus("error");
          setErrorMessage(res.error_message || res.error_reason || t(($) => $.notifications.lark.install_failed));
          return;
        }
      } catch (e) {
        if (cancelled) return;
        setStatus("error");
        setErrorMessage(e instanceof Error ? e.message : t(($) => $.notifications.lark.install_failed));
        return;
      }
      timeout = setTimeout(poll, Math.max(1, session.pollIntervalSeconds) * 1000);
    }

    timeout = setTimeout(poll, Math.max(1, session.pollIntervalSeconds) * 1000);
    return () => {
      cancelled = true;
      if (timeout) clearTimeout(timeout);
    };
  }, [session, status, wsId, t, onClose, onInstalled]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.notifications.lark.install_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.notifications.lark.install_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex min-h-56 flex-col items-center justify-center gap-4">
          {beginning && (
            <p className="text-sm text-muted-foreground">
              {t(($) => $.notifications.lark.install_starting)}
            </p>
          )}
          {!beginning && session && status === "pending" && (
            <>
              <div className="rounded-md border bg-white p-3">
                <QRCode value={session.qrCodeURL} size={184} />
              </div>
              <a
                href={session.qrCodeURL}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
              >
                <ExternalLink className="h-3 w-3" />
                {t(($) => $.notifications.lark.open_qr_link)}
              </a>
            </>
          )}
          {status === "error" && (
            <div className="space-y-3 text-center">
              <p className="text-sm text-destructive">
                {errorMessage || t(($) => $.notifications.lark.install_failed)}
              </p>
              <Button variant="outline" size="sm" onClick={() => void beginSession()}>
                <RefreshCw className="h-3 w-3" />
                {t(($) => $.notifications.lark.retry)}
              </Button>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.notifications.lark.cancel)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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
  if (!installation.agent_id) {
    return null;
  }
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
            <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
              {installation.region === "lark"
                ? t(($) => $.lark.region_lark)
                : t(($) => $.lark.region_feishu)}
            </span>
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
  onShowConnectedDetails,
}: {
  agentId: string;
  agentName?: string;
  className?: string;
  /**
   * When set, the connected state renders as a compact read-only status
   * row that invokes this callback on click instead of the full badge with
   * inline Manage / Disconnect actions. The agent inspector passes a
   * "jump to the Integrations tab" handler so the left column stays a
   * glanceable summary and the management actions live in one place (the
   * tab). The tab itself omits this prop and gets the full badge.
   */
  onShowConnectedDetails?: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  // dialogRegion carries two pieces of state in one variable: which
  // cloud the dialog should target (drives the device-flow `begin`
  // host and the dialog copy), AND whether the dialog is open at all
  // (null = closed). A separate boolean would have to be kept in sync
  // with the region — collapsing them prevents an "open but with no
  // region picked" intermediate state from existing.
  const [dialogRegion, setDialogRegion] = useState<"feishu" | "lark" | null>(
    null,
  );

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
    (inst) =>
      (inst.installation_kind ?? "agent") === "agent" &&
      inst.agent_id === agentId &&
      inst.status === "active",
  );
  if (existing) {
    return onShowConnectedDetails ? (
      <LarkAgentBotStatusRow
        installation={existing}
        onClick={onShowConnectedDetails}
        className={className}
      />
    ) : (
      <LarkAgentBotConnectedBadge installation={existing} className={className} />
    );
  }

  // No existing bot and the device-flow transport isn't wired end-to-end:
  // a fresh scan would fail at the post-poll bot-info step, so hide the CTA.
  if (!installSupported) return null;

  // Two CTAs, one per cloud — Feishu (mainland) on the left, Lark
  // (international) on the right. We deliberately render two explicit
  // entry points instead of one auto-detect QR because Lark only emits
  // tenant_brand="lark" mid-poll AFTER the user has authorized; until
  // then a Lark user has to scan a QR served from accounts.feishu.cn,
  // which has surfaced as confusing for international users (MUL-3083
  // follow-up). Each button passes its region to the install dialog,
  // which threads it to the backend so the device-flow `begin` POSTs
  // directly against the matching accounts host. The mid-poll
  // tenant-brand auto-switch in RegistrationService is preserved as a
  // safety net for users who pick the wrong entry.
  return (
    <>
      <div
        className={cn("flex flex-wrap items-center gap-2", className)}
        data-testid="lark-agent-bind-buttons"
      >
        <Button
          variant="outline"
          size="sm"
          onClick={() => setDialogRegion("feishu")}
          disabled={!agentId}
          title={
            agentName
              ? t(($) => $.lark.bind_button_feishu_title, { agent: agentName })
              : undefined
          }
          data-testid="lark-agent-bind-feishu"
        >
          <ExternalLink className="h-3 w-3" />
          {t(($) => $.lark.bind_button_feishu)}
        </Button>
        {/* MUL-3083: Lark (international) bind entry is temporarily hidden —
            see LARK_INTL_CONNECT_ENABLED. Mainland Feishu (above) is
            unaffected. */}
        {LARK_INTL_CONNECT_ENABLED && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setDialogRegion("lark")}
            disabled={!agentId}
            title={
              agentName
                ? t(($) => $.lark.bind_button_lark_title, { agent: agentName })
                : undefined
            }
            data-testid="lark-agent-bind-lark"
          >
            <ExternalLink className="h-3 w-3" />
            {t(($) => $.lark.bind_button_lark)}
          </Button>
        )}
      </div>
      {dialogRegion && (
        <LarkInstallDialog
          wsId={wsId}
          agentId={agentId}
          agentName={agentName}
          region={dialogRegion}
          onClose={() => setDialogRegion(null)}
        />
      )}
    </>
  );
}

// LarkAgentBotStatusRow is the compact, read-only connected affordance the
// agent inspector (left column) renders instead of the full badge. It shows
// only the status — green dot, Feishu/Lark region chip, "Connected to Lark"
// — and is a single full-width button that deep-links into the Integrations
// tab, where Manage / Disconnect live. Keeping the destructive action out of
// the always-visible sidebar means it exists in exactly one place.
function LarkAgentBotStatusRow({
  installation,
  onClick,
  className,
}: {
  installation: LarkInstallation;
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
      data-testid="lark-agent-bot-status"
    >
      <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
      <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
        {installation.region === "lark"
          ? t(($) => $.lark.region_lark)
          : t(($) => $.lark.region_feishu)}
      </span>
      <span className="truncate">{t(($) => $.lark.agent_bot_connected_label)}</span>
      <ChevronRight className="ml-auto h-3.5 w-3.5 shrink-0" />
    </button>
  );
}

// LarkAgentBotConnectedBadge is the full "already connected" affordance the
// Integrations tab renders in place of the Bind button when this agent has
// an active Lark installation. (The inspector's left column uses the compact
// LarkAgentBotStatusRow instead, which deep-links here.) It lays out as two
// rows: row 1 pairs a green-dot status (with the Feishu/Lark region chip) on
// the left with a soft-destructive Disconnect button on the right; row 2
// carries the secondary "Manage in Lark" link to the Bot's dev console page
// (new tab). Disconnect removes the installation after a confirm dialog.
//
// Visibility rules carry over from the parent `LarkAgentBindButton`:
// only owners and admins ever reach this component, so the unbind
// affordance is unconditionally shown — the backend gates DELETE on
// the same role and would 403 anyone else, which makes a redundant
// `canManage` check here dead code.
//
// The dev-console host depends on which Lark cloud the bot lives on:
// Feishu (mainland) bots are managed at open.feishu.cn, Lark
// (international) bots at open.larksuite.com. The region is auto-detected
// at install time and surfaced per installation on the listings
// response; an older server that omits `region` defaults to Feishu
// (API-compat — see CLAUDE.md).
function larkDevConsoleHost(region?: string): string {
  return region === "lark"
    ? "https://open.larksuite.com"
    : "https://open.feishu.cn";
}

function LarkAgentBotConnectedBadge({
  installation,
  className,
}: {
  installation: LarkInstallation;
  className?: string;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const manageHref = `${larkDevConsoleHost(installation.region)}/app/${encodeURIComponent(installation.app_id)}`;

  const [confirmOpen, setConfirmOpen] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);

  async function handleDisconnect() {
    if (disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteLarkInstallation(wsId, installation.id);
      // Invalidate before closing the dialog: the badge unmounts when
      // the listings query updates (the parent swaps to the Bind CTA),
      // so leaving the open state behind is fine — but doing the
      // network call before the close prevents a flash of "stale
      // connected" state.
      await qc.invalidateQueries({ queryKey: larkKeys.installations(wsId) });
      toast.success(t(($) => $.lark.toast_disconnected));
      setConfirmOpen(false);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.lark.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div
      className={cn("space-y-2", className)}
      data-testid="lark-agent-bot-connected"
    >
      {/* Row 1: connection status (left) and the destructive unbind
          action (right). The Disconnect uses the soft-tinted
          `destructive` button variant — it reads dangerous without the
          loud solid-red, and stays visible because it is the user-facing
          recovery path for the install_supported=false / re-scan
          zombie-bot trap (server/internal/handler/lark.go). Confirmation
          is mandatory: the backend disconnect tears down the WebSocket
          and stops message delivery. */}
      <div className="flex items-center justify-between gap-3">
        <span className="inline-flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
          <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
          <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
            {installation.region === "lark"
              ? t(($) => $.lark.region_lark)
              : t(($) => $.lark.region_feishu)}
          </span>
          <span className="truncate">{t(($) => $.lark.agent_bot_connected_label)}</span>
        </span>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => setConfirmOpen(true)}
          disabled={disconnecting}
          title={t(($) => $.lark.agent_bot_disconnect_tooltip)}
          aria-label={t(($) => $.lark.disconnect)}
          data-testid="lark-agent-bot-disconnect"
        >
          <Trash2 className="h-3 w-3" />
          {disconnecting
            ? t(($) => $.lark.disconnecting)
            : t(($) => $.lark.disconnect)}
        </Button>
      </div>

      {/* Row 2: secondary "Manage in Lark" link to the Bot's dev-console
          app page. Demoted below the status row so it no longer competes
          with the primary connect/disconnect intents. Region-aware tooltip
          keeps the Feishu vs Lark distinction this branch introduced. */}
      <a
        href={manageHref}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline"
        title={
          installation.region === "lark"
            ? t(($) => $.lark.agent_bot_manage_tooltip_lark)
            : t(($) => $.lark.agent_bot_manage_tooltip_feishu)
        }
      >
        <ExternalLink className="h-3 w-3" />
        {installation.region === "lark"
          ? t(($) => $.lark.agent_bot_manage_link_lark)
          : t(($) => $.lark.agent_bot_manage_link_feishu)}
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
            <AlertDialogAction
              onClick={handleDisconnect}
              disabled={disconnecting}
            >
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

// LarkInstallDialog walks the user through the device-flow install:
// 1) POST /lark/install/begin?region=<feishu|lark> → render QR
// 2) poll /lark/install/{sessionId}/status until success | error | expiry
// 3) on success: toast, close, invalidate installations cache
//
// The dialog deliberately re-fetches a fresh session on each "retry"
// rather than reusing a stale device_code — Lark's device_code is
// single-use and a re-render of the same QR after an error would just
// fail again at the next poll.
//
// region is a required prop so the begin POST hits the right cloud
// (accounts.feishu.cn vs accounts.larksuite.com) and the dialog copy
// (title, scan hint, link fallback) reflects the cloud the user
// picked. Defaulting it would silently route Lark users to a Feishu QR
// — exactly the confusion this split-CTA refactor is meant to remove.
function LarkInstallDialog({
  wsId,
  agentId,
  agentName,
  runtimeId,
  runtimeName,
  region,
  onClose,
}: {
  wsId: string;
  agentId?: string;
  agentName?: string;
  runtimeId?: string;
  runtimeName?: string;
  region: "feishu" | "lark";
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
      const res = runtimeId
        ? await api.beginLarkRuntimeInstall(wsId, runtimeId, region)
        : await api.beginLarkInstall(wsId, agentId ?? "", region);
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
          toast.success(
            region === "lark"
              ? t(($) => $.lark.install_success_toast_lark)
              : t(($) => $.lark.install_success_toast_feishu),
          );
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
          <DialogTitle>
            {region === "lark"
              ? t(($) => $.lark.install_dialog_title_lark)
              : t(($) => $.lark.install_dialog_title_feishu)}
          </DialogTitle>
          <DialogDescription>
            {region === "lark"
              ? runtimeName
                ? t(($) => $.lark.install_dialog_description_for_runtime_lark, { runtime: runtimeName })
                : agentName
                ? t(($) => $.lark.install_dialog_description_for_agent_lark, { agent: agentName })
                : t(($) => $.lark.install_dialog_description_lark)
              : runtimeName
                ? t(($) => $.lark.install_dialog_description_for_runtime_feishu, { runtime: runtimeName })
                : agentName
                ? t(($) => $.lark.install_dialog_description_for_agent_feishu, { agent: agentName })
                : t(($) => $.lark.install_dialog_description_feishu)}
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
                {region === "lark"
                  ? t(($) => $.lark.install_scan_hint_lark)
                  : t(($) => $.lark.install_scan_hint_feishu)}
              </p>
              <a
                href={session.qrCodeURL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs underline text-muted-foreground"
              >
                {region === "lark"
                  ? t(($) => $.lark.install_open_link_fallback_lark)
                  : t(($) => $.lark.install_open_link_fallback_feishu)}
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
