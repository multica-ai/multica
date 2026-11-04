"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, ExternalLink, Trash2 } from "lucide-react";
import { DiscordMark } from "./discord-mark";
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
import { discordInstallationsOptions, discordKeys } from "@multica/core/discord";
import { api } from "@multica/core/api";
import type { DiscordInstallation } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { docsLocalePrefix } from "../../common/docs-locale";
import { openExternal } from "../../platform";
import { useLocale, useT } from "../../i18n";

// DiscordTab is the workspace settings panel for Discord bot installations,
// mirroring TelegramTab: listing is member-visible; the disconnect action is
// admin-only (backend-enforced; the UI hides the button to match). Adding a new
// installation flows through the Agent detail page — the install path is
// per-agent (one bot per agent, the (workspace_id, agent_id, channel_type)
// UNIQUE in channel_installation).
export function DiscordTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading, isError } = useQuery({
    ...discordInstallationsOptions(wsId),
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
      await api.deleteDiscordInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: discordKeys.installations(wsId) });
      toast.success(t(($) => $.discord.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.discord.toast_disconnect_failed),
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
              {t(($) => $.discord.load_failed)}
            </p>
          </CardContent>
        </Card>
      ) : isLoading ? (
        <Card>
          <CardContent>
            <p className="text-body text-muted-foreground">{t(($) => $.discord.loading)}</p>
          </CardContent>
        </Card>
      ) : !configured ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-body font-medium">{t(($) => $.discord.not_enabled_title)}</p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.discord.not_enabled_description_prefix)}{" "}
              <code className="rounded-xs bg-muted px-1 py-0.5 text-micro">
                MULTICA_DISCORD_SECRET_KEY
              </code>{" "}
              {t(($) => $.discord.not_enabled_description_suffix)}{" "}
              {t(($) => $.discord.not_enabled_self_host_hint)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <h2 className="text-body font-semibold">{t(($) => $.discord.connected_bots)}</h2>
          {installations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-body font-medium">{t(($) => $.discord.empty_title)}</p>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.discord.empty_description_prefix)}{" "}
                  <strong>{t(($) => $.discord.empty_description_cta)}</strong>{" "}
                  {t(($) => $.discord.empty_description_suffix)}
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
              {t(($) => $.discord.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.discord.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.discord.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.discord.disconnecting)
                : t(($) => $.discord.disconnect)}
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
  installation: DiscordInstallation;
  canManage: boolean;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  const locale = useLocale();
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
            {installation.bot_username ? (
              <span className="ml-2 text-caption text-muted-foreground">
                @{installation.bot_username}
              </span>
            ) : null}
            {!isActive && (
              <span className="ml-2 rounded-xs bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                {t(($) => $.discord.revoked_badge)}
              </span>
            )}
          </p>
          <p className="text-micro text-muted-foreground">
            {t(($) => $.discord.installed_at_label, {
              when: new Date(installation.installed_at).toLocaleString(locale),
            })}
          </p>
        </div>
      </div>
      {canManage && isActive && (
        <Button variant="outline" size="sm" onClick={onDisconnect}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.discord.disconnect)}
        </Button>
      )}
    </div>
  );
}

// discordDocsUrl points at the Discord integration guide on the docs site,
// localized like the Telegram docs link.
function discordDocsUrl(lang: string | undefined): string {
  return `https://multica.ai/docs${docsLocalePrefix(lang)}/discord-bot-integration`;
}

// DiscordAgentBindButton is the per-agent CTA on the agent detail page. Discord
// uses the paste-a-token model: the admin creates an application with a Bot
// user and pastes its token; the backend validates via GET /users/@me before
// persisting. Visibility mirrors TelegramAgentBindButton (owner/admin only).
export function DiscordAgentBindButton({
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
  const [botToken, setBotToken] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const { data: listing } = useQuery({
    ...discordInstallationsOptions(wsId),
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
      <DiscordAgentBotStatusRow
        onClick={onShowConnectedDetails}
        className={className}
      />
    ) : (
      <DiscordAgentBotConnectedBadge installation={existing} className={className} />
    );
  }

  if (!installSupported) return null;

  function closeDialog() {
    if (submitting) return;
    setDialogOpen(false);
    setBotToken("");
  }

  async function handleSubmit() {
    const bot_token = botToken.trim();
    if (submitting || !agentId || !bot_token) return;
    setSubmitting(true);
    try {
      const installation = await api.registerDiscordBot(wsId, agentId, { bot_token });
      if (!installation.id || installation.status !== "active") {
        throw new Error("Discord connection returned an invalid installation");
      }
      // The discord_installation realtime event also refreshes this list, but
      // invalidate explicitly so the connected badge appears immediately.
      await qc.invalidateQueries({ queryKey: discordKeys.installations(wsId) });
      toast.success(t(($) => $.discord.connect_success_toast));
      setDialogOpen(false);
      setBotToken("");
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.discord.connect_failed_toast),
      );
    } finally {
      setSubmitting(false);
    }
  }

  const canSubmit = botToken.trim() !== "" && !submitting;

  return (
    <div
      className={cn("flex flex-wrap items-center gap-2", className)}
      data-testid="discord-agent-bind-buttons"
    >
      <Button
        variant="outline"
        size="sm"
        onClick={() => setDialogOpen(true)}
        disabled={!agentId}
        title={
          agentName
            ? t(($) => $.discord.bind_button_title, { agent: agentName })
            : undefined
        }
        data-testid="discord-agent-connect"
      >
        <DiscordMark className="h-3 w-3" />
        {t(($) => $.discord.bind_button)}
      </Button>

      <Dialog
        open={dialogOpen}
        onOpenChange={(v) => (v ? setDialogOpen(true) : closeDialog())}
      >
        <DialogContent className="sm:max-w-lg" data-testid="discord-connect-dialog">
          <DialogHeader>
            <DialogTitle>{t(($) => $.discord.connect_dialog_title)}</DialogTitle>
          </DialogHeader>

          <p className="text-caption text-muted-foreground">
            {t(($) => $.discord.connect_dialog_description)}
          </p>

          <button
            type="button"
            onClick={() => openExternal(discordDocsUrl(i18n.language))}
            className="inline-flex w-fit items-center gap-2 text-body font-medium text-primary underline-offset-2 hover:underline"
            data-testid="discord-docs-link"
          >
            <ExternalLink className="h-4 w-4" />
            {t(($) => $.discord.connect_docs_link)}
          </button>

          <div className="space-y-1.5">
            <Label htmlFor="discord-bot-token">
              {t(($) => $.discord.bot_token_label)}
            </Label>
            <Input
              id="discord-bot-token"
              data-testid="discord-bot-token"
              type="password"
              value={botToken}
              onChange={(e) => setBotToken(e.target.value)}
              // Discord token shape: a format hint, not copy.
              // eslint-disable-next-line no-restricted-syntax
              placeholder="MTIz…"
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
              {t(($) => $.discord.connect_cancel)}
            </Button>
            <Button
              size="sm"
              onClick={handleSubmit}
              disabled={!canSubmit}
              data-testid="discord-connect-submit"
            >
              {submitting
                ? t(($) => $.discord.connect_submitting)
                : t(($) => $.discord.connect_submit)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// DiscordAgentBotStatusRow is the compact, read-only connected affordance the
// agent inspector renders; it deep-links into the Integrations tab.
function DiscordAgentBotStatusRow({
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
      data-testid="discord-agent-bot-status"
    >
      <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
      <span className="truncate">{t(($) => $.discord.agent_bot_connected_label)}</span>
      <ChevronRight className="ml-auto h-3.5 w-3.5 shrink-0" />
    </button>
  );
}

// DiscordAgentBotConnectedBadge is the full "already connected" affordance:
// status + Disconnect.
function DiscordAgentBotConnectedBadge({
  installation,
  className,
}: {
  installation: DiscordInstallation;
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
      await api.deleteDiscordInstallation(wsId, installation.id);
      await qc.invalidateQueries({ queryKey: discordKeys.installations(wsId) });
      toast.success(t(($) => $.discord.toast_disconnected));
      setConfirmOpen(false);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.discord.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div
      className={cn("space-y-2", className)}
      data-testid="discord-agent-bot-connected"
    >
      <div className="flex items-center justify-between gap-3">
        <span className="inline-flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
          <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
          <span className="truncate">
            {t(($) => $.discord.agent_bot_connected_label)}
            {installation.bot_username ? ` · @${installation.bot_username}` : ""}
          </span>
        </span>
        <Button
          variant="destructive"
          size="sm"
          onClick={() => setConfirmOpen(true)}
          disabled={disconnecting}
          title={t(($) => $.discord.agent_bot_disconnect_tooltip)}
          aria-label={t(($) => $.discord.disconnect)}
          data-testid="discord-agent-bot-disconnect"
        >
          <Trash2 className="h-3 w-3" />
          {disconnecting
            ? t(($) => $.discord.disconnecting)
            : t(($) => $.discord.disconnect)}
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
              {t(($) => $.discord.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.discord.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.discord.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.discord.disconnecting)
                : t(($) => $.discord.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
