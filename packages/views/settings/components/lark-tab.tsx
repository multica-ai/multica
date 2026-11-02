"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, ExternalLink, RefreshCw, Trash2 } from "@multica/ui/icons";
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
import { larkInstallationsOptions, larkKeys } from "@multica/core/lark";
import { api, ApiError } from "@multica/core/api";
import type { LarkInstallation, LarkInstallStatusResponse } from "@multica/core/types";
import { docsLocalePrefix } from "../../common/docs-locale";
import { useT } from "../../i18n";
import { LarkConversationForm } from "./lark-conversation-form";
import { LarkInstallationList } from "./lark-installation-list";
import { TeamSubscriptionsSection } from "../../message-delivery";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";

// MUL-3083: the Lark (international, open.larksuite.com) "connect a Bot"
// entry is temporarily hidden while its install → inbound pipeline is
// stabilized — some Lark installs complete on Lark's side but never land a
// `lark_installation` row, so the Bot silently can't receive messages.
// Mainland Feishu is unaffected and keeps its bind entry. Existing
// installations (either cloud) stay fully manageable. Flip this back to
// `true` to restore the "Bind to Lark" CTA; nothing else needs to change.
const LARK_INTL_CONNECT_ENABLED: boolean = false;

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

  const { data, isLoading, isError, isFetching, refetch } = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: !!wsId,
  });
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
      {isError && (
        <Card role="alert">
          <CardContent className="space-y-2">
            <p className="text-body text-destructive">{t(($) => $.lark.load_error)}</p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={isFetching}
              aria-busy={isFetching}
              onClick={() => void refetch()}
            >
              {t(($) => $.lark.retry)}
            </Button>
          </CardContent>
        </Card>
      )}
      <LarkInstallationList
        data={data}
        isLoading={isLoading}
        canManage={canManage}
        conversationWritable={data?.conversation_supported === true && !isError && !isFetching}
        workspaceId={wsId}
        onDisconnect={setDisconnectTarget}
      />

      {/* OL-28: team event subscriptions (activity/comment → group/topic).
          Self-gated on the member's role; a plain member sees an explanatory
          note, never the admin-only configuration. */}
      <TeamSubscriptionsSection />

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

// LarkAgentBindButton is the per-agent CTA we expose from the agent
// detail page. The Settings panel above is the management view; this
// button is the entry point.
//
// Visibility rules, in order:
//   1. Only the agent's owner or a workspace owner/admin see anything —
//      the backend authorizes `POST /lark/install/begin`, the status
//      poll, AND disconnect with canManageAgent (agent owner OR ws
//      owner/admin; see server/internal/handler/lark.go, MUL-4213), so
//      the gate here mirrors that. `agentOwnerId` is what lets a
//      non-admin owner through; when it is omitted the button stays
//      workspace owner/admin-only.
//   2. If this agent ALREADY has an active installation, they see
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
  agentOwnerId,
  className,
  onShowConnectedDetails,
}: {
  agentId: string;
  agentName?: string;
  /**
   * The bound agent's owner (`agent.owner_id`). When it matches the
   * current user, the button treats them as able to manage the bot even
   * if they are not a workspace owner/admin — mirroring the backend's
   * canManageAgent authorization (MUL-4213). Omit it to keep the button
   * workspace owner/admin-only.
   */
  agentOwnerId?: string | null;
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

  const { data: listing, isError, isFetching } = useQuery({
    ...larkInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const installSupported = listing?.install_supported === true;

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const isWorkspaceAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";
  const isAgentOwner =
    !!user?.id && agentOwnerId != null && agentOwnerId === user.id;
  // Mirror the backend canManageAgent gate: the agent's owner OR a
  // workspace owner/admin may bind/manage the bot (MUL-4213).
  const canManage = isWorkspaceAdmin || isAgentOwner;

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
    return onShowConnectedDetails ? (
      <LarkAgentBotStatusRow
        installation={existing}
        onClick={onShowConnectedDetails}
        className={className}
      />
    ) : (
      <div className={className}>
        <LarkAgentBotConnectedBadge installation={existing} />
        {(listing?.conversation_supported === true || existing.conversation != null) && (
          <LarkConversationForm workspaceId={wsId} installation={existing}
            disabled={listing?.conversation_supported !== true || isError || isFetching} />
        )}
      </div>
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
        <TooltipHint
          label={
            agentName
              ? t(($) => $.lark.bind_button_feishu_title, { agent: agentName })
              : undefined
          }
        >
          <Button
            variant="outline"
            size="sm"
            onClick={() => setDialogRegion("feishu")}
            disabled={!agentId}
            data-testid="lark-agent-bind-feishu"
          >
            <ExternalLink className="h-3 w-3" />
            {t(($) => $.lark.bind_button_feishu)}
          </Button>
        </TooltipHint>
        {/* MUL-3083: Lark (international) bind entry is temporarily hidden —
            see LARK_INTL_CONNECT_ENABLED. Mainland Feishu (above) is
            unaffected. */}
        {LARK_INTL_CONNECT_ENABLED && (
          <TooltipHint
            label={
              agentName
                ? t(($) => $.lark.bind_button_lark_title, { agent: agentName })
                : undefined
            }
          >
            <Button
              variant="outline"
              size="sm"
              onClick={() => setDialogRegion("lark")}
              disabled={!agentId}
              data-testid="lark-agent-bind-lark"
            >
              <ExternalLink className="h-3 w-3" />
              {t(($) => $.lark.bind_button_lark)}
            </Button>
          </TooltipHint>
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
        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-caption text-muted-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        className,
      )}
      data-testid="lark-agent-bot-status"
    >
      <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
      <span className="rounded-xs bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
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
// only the agent's owner or a workspace owner/admin ever reach this
// component, so the unbind affordance is unconditionally shown — the
// backend authorizes DELETE with the same canManageAgent check (MUL-4213)
// and would 403 anyone else, which makes a redundant gate here dead code.
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

// larkDocsUrl points at the Lark/Feishu integration guide, localized the
// same way as the Telegram and Slack docs links.
function larkDocsUrl(lang: string | undefined): string {
  return `https://multica.ai/docs${docsLocalePrefix(lang)}/lark-bot-integration`;
}

function LarkAgentBotConnectedBadge({
  installation,
  className,
}: {
  installation: LarkInstallation;
  className?: string;
}) {
  const { t, i18n } = useT("settings");
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
        <span className="inline-flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
          <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
          <span className="rounded-xs bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
            {installation.region === "lark"
              ? t(($) => $.lark.region_lark)
              : t(($) => $.lark.region_feishu)}
          </span>
          <span className="truncate">{t(($) => $.lark.agent_bot_connected_label)}</span>
        </span>
        <TooltipHint label={t(($) => $.lark.agent_bot_disconnect_tooltip)}>
          <Button
            variant="destructive"
            size="sm"
            onClick={() => setConfirmOpen(true)}
            disabled={disconnecting}
            aria-label={t(($) => $.lark.disconnect)}
            data-testid="lark-agent-bot-disconnect"
          >
            <Trash2 className="h-3 w-3" />
            {disconnecting
              ? t(($) => $.lark.disconnecting)
              : t(($) => $.lark.disconnect)}
          </Button>
        </TooltipHint>
      </div>

      {/* Row 2: secondary "Manage in Lark" link to the Bot's dev-console
          app page. Demoted below the status row so it no longer competes
          with the primary connect/disconnect intents. Region-aware tooltip
          keeps the Feishu vs Lark distinction this branch introduced. */}
      <a
        href={manageHref}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-1 text-caption text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline"
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

      {/* Row 3: the check a silent Bot almost always needs (#8496). An app
          whose events go to a request URL instead of the long connection
          binds fine and shows exactly this badge while receiving nothing,
          and this row is where someone looks when the Bot stays quiet —
          the install dialog closes itself a beat after success. */}
      <p className="text-caption text-muted-foreground">
        {t(($) => $.lark.agent_bot_silent_hint)}{" "}
        <a
          href={larkDocsUrl(i18n.language)}
          target="_blank"
          rel="noopener noreferrer"
          className="underline underline-offset-2 transition-colors hover:text-foreground"
        >
          {t(($) => $.lark.agent_bot_silent_hint_link)}
        </a>
      </p>

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
  region,
  onClose,
}: {
  wsId: string;
  agentId: string;
  agentName?: string;
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
      const res = await api.beginLarkInstall(wsId, agentId, region);
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
              ? agentName
                ? t(($) => $.lark.install_dialog_description_for_agent_lark, { agent: agentName })
                : t(($) => $.lark.install_dialog_description_lark)
              : agentName
                ? t(($) => $.lark.install_dialog_description_for_agent_feishu, { agent: agentName })
                : t(($) => $.lark.install_dialog_description_feishu)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-4 py-2">
          {beginning && !session && (
            <p className="text-body text-muted-foreground">{t(($) => $.lark.install_starting)}</p>
          )}

          {session && status === "pending" && (
            <>
              <div className="rounded-md border bg-white p-3">
                {/* react-qr-code renders an inline SVG — no external
                  network image dependency, prints at any DPI. */}
                <QRCode value={session.qrCodeURL} size={192} />
              </div>
              <a
                href={session.qrCodeURL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-caption underline text-muted-foreground"
              >
                {region === "lark"
                  ? t(($) => $.lark.install_open_link_fallback_lark)
                  : t(($) => $.lark.install_open_link_fallback_feishu)}
              </a>
            </>
          )}

          {status === "success" && (
            <p className="text-body font-medium">{t(($) => $.lark.install_success)}</p>
          )}

          {status === "error" && (
            <div className="space-y-2 text-center">
              <p className="text-body font-medium text-destructive">
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
                <p className="text-micro text-muted-foreground break-all">
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
