"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, ExternalLink, Trash2 } from "lucide-react";
import { MattermostMark } from "./mattermost-mark";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
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
import { mattermostInstallationsOptions, mattermostKeys } from "@multica/core/mattermost";
import { api } from "@multica/core/api";
import type { MattermostInstallation } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { openExternal } from "../../platform";
import { useT } from "../../i18n";

// MattermostTab is the workspace settings panel for Mattermost bot
// installations, mirroring TelegramTab: listing is member-visible; the
// disconnect action is admin-only (backend-enforced; the UI hides the button
// to match). Adding a new installation flows through the Agent detail page —
// the install path is per-agent (one bot per agent, the
// (workspace_id, agent_id, channel_type) UNIQUE in channel_installation).
export function MattermostTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading, isError } = useQuery({
    ...mattermostInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const installations = data?.installations ?? [];
  const configured = data?.configured === true;

  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      // Await the server before touching cache/UI (repo rule: no optimistic
      // removal on flows that confirm/destroy).
      await api.deleteMattermostInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: mattermostKeys.installations(wsId) });
      toast.success(t(($) => $.mattermost.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.mattermost.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="space-y-8">
      {isError ? (
        <Card>
          <CardContent>
            <p className="text-body text-muted-foreground">
              {t(($) => $.mattermost.load_failed)}
            </p>
          </CardContent>
        </Card>
      ) : isLoading ? (
        <Card>
          <CardContent>
            <p className="text-body text-muted-foreground">{t(($) => $.mattermost.loading)}</p>
          </CardContent>
        </Card>
      ) : !configured ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-body font-medium">{t(($) => $.mattermost.not_enabled_title)}</p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.mattermost.not_enabled_description_prefix)}{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-micro">
                MULTICA_MATTERMOST_SECRET_KEY
              </code>{" "}
              {t(($) => $.mattermost.not_enabled_description_suffix)}{" "}
              {t(($) => $.mattermost.not_enabled_self_host_hint)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <h2 className="text-body font-semibold">{t(($) => $.mattermost.connected_bots)}</h2>
          {installations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-body font-medium">{t(($) => $.mattermost.empty_title)}</p>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.mattermost.empty_description_prefix)}{" "}
                  <strong>{t(($) => $.mattermost.empty_description_cta)}</strong>{" "}
                  {t(($) => $.mattermost.empty_description_suffix)}
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
              {t(($) => $.mattermost.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.mattermost.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.mattermost.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.mattermost.disconnecting)
                : t(($) => $.mattermost.disconnect)}
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
  installation: MattermostInstallation;
  canManage: boolean;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  const { getAgentName } = useActorName();
  const isActive = installation.status === "active";
  const agentName = getAgentName(installation.agent_id);
  return (
    <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
      <div className="flex min-w-0 items-start gap-3">
        <ActorAvatar
          actorType="agent"
          actorId={installation.agent_id}
          size="lg"
          enableHoverCard
          profileLink
        />
        <div className="min-w-0 space-y-1">
          <p className="text-body font-medium">
            {agentName}
            {installation.bot_username ? (
              <span className="ml-2 text-caption text-muted-foreground">
                @{installation.bot_username}
              </span>
            ) : null}
            {!isActive && (
              <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                {t(($) => $.mattermost.revoked_badge)}
              </span>
            )}
          </p>
          {/* Mattermost is self-hosted, so the server is part of the row's
              identity: two agents can be connected to bots on two different
              deployments, and the username alone would not distinguish them. */}
          {installation.server_url ? (
            <p className="truncate text-micro text-muted-foreground">{installation.server_url}</p>
          ) : null}
          <p className="text-micro text-muted-foreground">
            {t(($) => $.mattermost.installed_at_label, {
              when: new Date(installation.installed_at).toLocaleString(),
            })}
          </p>
        </div>
      </div>
      {canManage && isActive && (
        <Button variant="outline" size="sm" onClick={onDisconnect}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.mattermost.disconnect)}
        </Button>
      )}
    </div>
  );
}

// mattermostDocsUrl points at the Mattermost integration guide on the docs
// site, localized like the Slack and Telegram docs links.
function mattermostDocsUrl(lang: string | undefined): string {
  const prefix = lang?.startsWith("zh")
    ? "/zh"
    : lang?.startsWith("ja")
      ? "/ja"
      : lang?.startsWith("ko")
        ? "/ko"
        : "";
  return `https://multica.ai/docs${prefix}/mattermost-bot-integration`;
}

// MattermostAgentBindButton is the per-agent CTA on the agent detail page.
// Mattermost uses the paste-two-fields model: the admin creates a bot account
// in the System Console and pastes its server URL and access token; the
// backend validates them live before persisting. Visibility mirrors
// TelegramAgentBindButton (owner/admin only).
export function MattermostAgentBindButton({
  agentId,
  agentName,
  className,
  onShowConnectedDetails,
}: {
  agentId: string;
  agentName?: string;
  className?: string;
  /** Compact read-only connected row that invokes this instead of the full
   * badge — the agent inspector passes a "jump to the Integrations tab"
   * handler so management actions live in one place. */
  onShowConnectedDetails?: () => void;
}) {
  const { t, i18n } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const [dialogOpen, setDialogOpen] = useState(false);
  const [serverUrl, setServerUrl] = useState("");
  const [accessToken, setAccessToken] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const { data: listing } = useQuery({
    ...mattermostInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const installSupported = listing?.install_supported === true;

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  if (!canManage) return null;

  const existing = listing?.installations.find(
    (inst) => inst.agent_id === agentId && inst.status === "active",
  );
  if (existing) {
    return onShowConnectedDetails ? (
      <MattermostAgentBotStatusRow
        onClick={onShowConnectedDetails}
        className={className}
      />
    ) : (
      <MattermostAgentBotConnectedBadge installation={existing} className={className} />
    );
  }

  if (!installSupported) return null;

  function closeDialog() {
    if (submitting) return;
    setDialogOpen(false);
    setServerUrl("");
    setAccessToken("");
  }

  async function handleSubmit() {
    const server_url = serverUrl.trim();
    const access_token = accessToken.trim();
    if (submitting || !agentId || !server_url || !access_token) return;
    setSubmitting(true);
    try {
      const installation = await api.registerMattermostBot(wsId, agentId, {
        server_url,
        access_token,
      });
      if (!installation.id || installation.status !== "active") {
        throw new Error("Mattermost connection returned an invalid installation");
      }
      // The mattermost_installation realtime event also refreshes this list,
      // but invalidate explicitly so the connected badge appears immediately.
      await qc.invalidateQueries({ queryKey: mattermostKeys.installations(wsId) });
      toast.success(t(($) => $.mattermost.connect_success_toast));
      setDialogOpen(false);
      setServerUrl("");
      setAccessToken("");
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.mattermost.connect_failed_toast),
      );
    } finally {
      setSubmitting(false);
    }
  }

  const canSubmit = serverUrl.trim() !== "" && accessToken.trim() !== "" && !submitting;

  return (
    <div
      className={cn("flex flex-wrap items-center gap-2", className)}
      data-testid="mattermost-agent-bind-buttons"
    >
      <Button
        variant="outline"
        size="sm"
        onClick={() => setDialogOpen(true)}
        disabled={!agentId}
        title={
          agentName
            ? t(($) => $.mattermost.bind_button_title, { agent: agentName })
            : undefined
        }
        data-testid="mattermost-agent-connect"
      >
        <MattermostMark className="h-3 w-3" />
        {t(($) => $.mattermost.bind_button)}
      </Button>

      <Dialog
        open={dialogOpen}
        onOpenChange={(v) => (v ? setDialogOpen(true) : closeDialog())}
      >
        <DialogContent className="sm:max-w-lg" data-testid="mattermost-connect-dialog">
          <DialogHeader>
            <DialogTitle>{t(($) => $.mattermost.connect_dialog_title)}</DialogTitle>
          </DialogHeader>

          <p className="text-caption text-muted-foreground">
            {t(($) => $.mattermost.connect_dialog_description)}
          </p>

          <button
            type="button"
            onClick={() => openExternal(mattermostDocsUrl(i18n.language))}
            className="inline-flex w-fit items-center gap-2 text-body font-medium text-primary underline-offset-2 hover:underline"
            data-testid="mattermost-docs-link"
          >
            <ExternalLink className="h-4 w-4" />
            {t(($) => $.mattermost.connect_docs_link)}
          </button>

          <div className="space-y-1.5">
            <Label htmlFor="mattermost-server-url">
              {t(($) => $.mattermost.server_url_label)}
            </Label>
            <Input
              id="mattermost-server-url"
              data-testid="mattermost-server-url"
              type="url"
              value={serverUrl}
              onChange={(e) => setServerUrl(e.target.value)}
              // Server URL shape: a format hint, not copy.
              // eslint-disable-next-line no-restricted-syntax
              placeholder="https://mattermost.example.com"
              autoComplete="off"
              spellCheck={false}
              disabled={submitting}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="mattermost-access-token">
              {t(($) => $.mattermost.access_token_label)}
            </Label>
            <Input
              id="mattermost-access-token"
              data-testid="mattermost-access-token"
              type="password"
              value={accessToken}
              onChange={(e) => setAccessToken(e.target.value)}
              autoComplete="off"
              spellCheck={false}
              disabled={submitting}
            />
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              onClick={closeDialog}
              disabled={submitting}
            >
              {t(($) => $.mattermost.connect_cancel)}
            </Button>
            <Button
              size="sm"
              onClick={handleSubmit}
              disabled={!canSubmit}
              data-testid="mattermost-connect-submit"
            >
              {submitting
                ? t(($) => $.mattermost.connect_submitting)
                : t(($) => $.mattermost.connect_submit)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// MattermostAgentBotStatusRow is the compact, read-only connected affordance
// the agent inspector renders; it deep-links into the Integrations tab.
function MattermostAgentBotStatusRow({
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
      data-testid="mattermost-agent-bot-status"
    >
      <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
      <span className="truncate">{t(($) => $.mattermost.agent_bot_connected_label)}</span>
      <ChevronRight className="ml-auto h-3.5 w-3.5 shrink-0" />
    </button>
  );
}

// MattermostAgentBotConnectedBadge is the full "already connected" affordance:
// status + Disconnect, then an "Open in Mattermost" link to the deployment.
function MattermostAgentBotConnectedBadge({
  installation,
  className,
}: {
  installation: MattermostInstallation;
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
      await api.deleteMattermostInstallation(wsId, installation.id);
      await qc.invalidateQueries({ queryKey: mattermostKeys.installations(wsId) });
      toast.success(t(($) => $.mattermost.toast_disconnected));
      setConfirmOpen(false);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.mattermost.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div
      className={cn("space-y-2", className)}
      data-testid="mattermost-agent-bot-connected"
    >
      <div className="flex items-center justify-between gap-3">
        <span className="inline-flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
          <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
          <span className="truncate">
            {t(($) => $.mattermost.agent_bot_connected_label)}
            {installation.bot_username ? ` · @${installation.bot_username}` : ""}
          </span>
        </span>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => setConfirmOpen(true)}
          disabled={disconnecting}
          title={t(($) => $.mattermost.agent_bot_disconnect_tooltip)}
          aria-label={t(($) => $.mattermost.disconnect)}
          data-testid="mattermost-agent-bot-disconnect"
        >
          <Trash2 className="h-3 w-3" />
          {disconnecting
            ? t(($) => $.mattermost.disconnecting)
            : t(($) => $.mattermost.disconnect)}
        </Button>
      </div>

      {installation.server_url && (
        <button
          type="button"
          onClick={() => openExternal(installation.server_url)}
          className="inline-flex items-center gap-1 text-caption text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline"
          title={t(($) => $.mattermost.agent_bot_manage_tooltip)}
        >
          <ExternalLink className="h-3 w-3" />
          {t(($) => $.mattermost.agent_bot_manage_link)}
        </button>
      )}

      <AlertDialog
        open={confirmOpen}
        onOpenChange={(v) => {
          if (!v && !disconnecting) setConfirmOpen(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.mattermost.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.mattermost.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.mattermost.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.mattermost.disconnecting)
                : t(($) => $.mattermost.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
