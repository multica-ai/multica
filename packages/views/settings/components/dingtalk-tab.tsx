"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronRight, ExternalLink, Trash2 } from "lucide-react";
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
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
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
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { DingTalkMark } from "./dingtalk-mark";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  dingtalkGroupRoutesOptions,
  dingtalkInstallationsOptions,
  dingtalkKeys,
} from "@multica/core/dingtalk";
import { api } from "@multica/core/api";
import type { DingTalkGroupRoute, DingTalkInstallation } from "@multica/core/types";
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

// DingTalkTab is the workspace settings panel for DingTalk robot installations.
// Listing is member-visible; the disconnect action is admin-only (the backend
// enforces it; the UI hides the button for non-admins to match).
//
// Adding a new installation flows through the Agent detail page: the install
// path selects the robot's default agent (the installation still owns one
// Stream connection). Additional group-specific agents are managed below via
// group routing, so asking the user to pick an installation here would
// re-create the Agent page's picker.
export function DingTalkTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const {
    data: agents,
    isLoading: agentsLoading,
    isError: agentsError,
    isFetching: agentsFetching,
    isSuccess: agentsLoaded,
    refetch: retryAgents,
  } = useQuery(agentListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading } = useQuery({
    ...dingtalkInstallationsOptions(wsId),
  });
  const installations = data?.installations ?? [];
  const configured = data?.configured === true;
  const groupRoutingSupported = data?.group_routing_supported === true;
  const hasActiveInstallation = installations.some(
    (installation) => installation.status === "active",
  );
  const {
    data: groupRouteData,
    isLoading: routesLoading,
    isError: routesError,
    isFetching: routesFetching,
    refetch: retryGroupRoutes,
  } = useQuery({
    ...dingtalkGroupRoutesOptions(wsId),
    enabled: configured && groupRoutingSupported && hasActiveInstallation && !!wsId,
  });
  const groupRoutes = groupRouteData?.routes ?? [];
  const activeAgents = (agents ?? []).filter((agent) => !agent.archived_at);

  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);
  const [updatingRouteId, setUpdatingRouteId] = useState<string | null>(null);

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteDingTalkInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({ queryKey: dingtalkKeys.installations(wsId) });
      toast.success(t(($) => $.dingtalk.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.dingtalk.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  async function handleRouteAgentChange(route: DingTalkGroupRoute, agentId: string) {
    if (!canManage || !agentsLoaded || updatingRouteId || route.agent_id === agentId) return;
    setUpdatingRouteId(route.id);
    try {
      await api.updateDingTalkGroupRoute(wsId, route.id, { agent_id: agentId });
      await qc.invalidateQueries({ queryKey: dingtalkKeys.groupRoutes(wsId) });
      toast.success(t(($) => $.dingtalk.group_routes_updated));
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.dingtalk.group_routes_update_failed),
      );
    } finally {
      setUpdatingRouteId(null);
    }
  }

  return (
    <div className="space-y-8">
      {!configured ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-body font-medium">{t(($) => $.dingtalk.not_enabled_title)}</p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.dingtalk.not_enabled_description_prefix)}{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-micro">
                MULTICA_DINGTALK_SECRET_KEY
              </code>{" "}
              {t(($) => $.dingtalk.not_enabled_description_suffix)}{" "}
              {t(($) => $.dingtalk.not_enabled_self_host_hint)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <h2 className="text-body font-semibold">{t(($) => $.dingtalk.connected_bots)}</h2>
          {isLoading ? (
            <Card>
              <CardContent>
                <p className="text-body text-muted-foreground">{t(($) => $.dingtalk.loading)}</p>
              </CardContent>
            </Card>
          ) : installations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-body font-medium">{t(($) => $.dingtalk.empty_title)}</p>
                <p className="text-caption text-muted-foreground">
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

      {configured && groupRoutingSupported && hasActiveInstallation && (
        <section className="space-y-3">
          <div className="space-y-1">
            <h2 className="text-body font-semibold">
              {t(($) => $.dingtalk.group_routes_title)}
            </h2>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.dingtalk.group_routes_description)}
            </p>
          </div>
          {routesLoading ? (
            <Card>
              <CardContent>
                <p className="text-body text-muted-foreground">
                  {t(($) => $.dingtalk.loading)}
                </p>
              </CardContent>
            </Card>
          ) : routesError ? (
            <Card>
              <CardContent className="space-y-3" role="alert">
                <div className="space-y-1">
                  <p className="text-body font-medium">
                    {t(($) => $.dingtalk.group_routes_error_title)}
                  </p>
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.dingtalk.group_routes_error_description)}
                  </p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={routesFetching}
                  onClick={() => void retryGroupRoutes()}
                >
                  {t(($) => $.dingtalk.group_routes_retry)}
                </Button>
              </CardContent>
            </Card>
          ) : groupRoutes.length === 0 ? (
            <Card>
              <CardContent className="space-y-1">
                <p className="text-body font-medium">
                  {t(($) => $.dingtalk.group_routes_empty_title)}
                </p>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.dingtalk.group_routes_empty_description)}
                </p>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="divide-y">
                {agentsLoading ? (
                  <div className="pb-3">
                    <p className="text-body text-muted-foreground">
                      {t(($) => $.dingtalk.group_routes_agents_loading)}
                    </p>
                  </div>
                ) : agentsError ? (
                  <div className="space-y-3 pb-3" role="alert">
                    <div className="space-y-1">
                      <p className="text-body font-medium">
                        {t(($) => $.dingtalk.group_routes_agents_error_title)}
                      </p>
                      <p className="text-caption text-muted-foreground">
                        {t(($) => $.dingtalk.group_routes_agents_error_description)}
                      </p>
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={agentsFetching}
                      onClick={() => void retryAgents()}
                    >
                      {t(($) => $.dingtalk.group_routes_agents_retry)}
                    </Button>
                  </div>
                ) : agentsLoaded && activeAgents.length === 0 ? (
                  <div className="space-y-1 pb-3">
                    <p className="text-body font-medium">
                      {t(($) => $.dingtalk.group_routes_agents_empty_title)}
                    </p>
                    <p className="text-caption text-muted-foreground">
                      {t(($) => $.dingtalk.group_routes_agents_empty_description)}
                    </p>
                  </div>
                ) : null}
                {groupRoutes.map((route) => (
                  <GroupRouteRow
                    key={route.id}
                    route={route}
                    agents={activeAgents}
                    selectedAgentName={agents?.find((agent) => agent.id === route.agent_id)?.name}
                    canManage={canManage}
                    selectorAvailable={agentsLoaded && activeAgents.length > 0}
                    updating={updatingRouteId !== null}
                    onAgentChange={(agentId) => handleRouteAgentChange(route, agentId)}
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

function GroupRouteRow({
  route,
  agents,
  selectedAgentName,
  canManage,
  selectorAvailable,
  updating,
  onAgentChange,
}: {
  route: DingTalkGroupRoute;
  agents: Array<{ id: string; name: string }>;
  selectedAgentName?: string;
  canManage: boolean;
  selectorAvailable: boolean;
  updating: boolean;
  onAgentChange: (agentId: string) => void;
}) {
  const { t } = useT("settings");
  const title = route.conversation_title || route.conversation_id;
  const selectedAgent = agents.find((agent) => agent.id === route.agent_id);
  const selectedLabel =
    selectedAgentName ??
    selectedAgent?.name ??
    t(($) => $.dingtalk.group_routes_unknown_agent);

  return (
    <div className="flex flex-col gap-3 py-3 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 space-y-1">
        <p className="truncate text-body font-medium">{title}</p>
        <p className="truncate font-mono text-micro text-muted-foreground">
          {route.conversation_id}
        </p>
      </div>
      {canManage ? (
        <Select
          items={agents.map((agent) => ({ value: agent.id, label: agent.name }))}
          value={route.agent_id}
          onValueChange={(agentId) => {
            if (agentId) onAgentChange(agentId);
          }}
          disabled={!selectorAvailable || updating}
        >
          <SelectTrigger className="w-full sm:w-56" aria-label={t(($) => $.dingtalk.group_routes_agent_label)}>
            <SelectValue>
              {selectedLabel}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {agents.map((agent) => (
              <SelectItem key={agent.id} value={agent.id}>
                {agent.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : (
        <p className="text-caption text-muted-foreground">
          {selectedLabel}
        </p>
      )}
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
                {t(($) => $.dingtalk.revoked_badge)}
              </span>
            )}
          </p>
          <p className="text-micro text-muted-foreground">
            {t(($) => $.dingtalk.installed_at_label, {
              when: formatInstalledAt(installation.installed_at),
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

// dingtalkDocsUrl points at the DingTalk integration guide on the docs site,
// localized to the viewer's language. The docs site uses /<lang>/ path
// prefixes (English has none), matching the convention used elsewhere in the
// app for doc links.
function dingtalkDocsUrl(lang: string | undefined): string {
  const prefix = lang?.startsWith("zh")
    ? "/zh"
    : lang?.startsWith("ja")
      ? "/ja"
      : lang?.startsWith("ko")
        ? "/ko"
        : "";
  return `https://multica.ai/docs${prefix}/dingtalk-bot-integration`;
}

// DingTalkAgentBindButton is the per-agent CTA exposed from the agent detail
// page. DingTalk uses the bring-your-own-app model: the button opens a dialog
// where the admin pastes the AppKey (client id) + AppSecret (client secret) of
// the DingTalk robot they created (the backend validates both). Visibility:
//   1. Non-owner/admin viewers see nothing (the backend gates install/revoke).
//   2. If this agent already has an active installation, show the connected
//      badge (already-installed robots stay manageable).
//   3. Otherwise the Connect CTA shows whenever install is available.
export function DingTalkAgentBindButton({
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
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const { data: listing } = useQuery({
    ...dingtalkInstallationsOptions(wsId),
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
      <DingTalkAgentBotStatusRow
        onClick={onShowConnectedDetails}
        className={className}
      />
    ) : (
      <DingTalkAgentBotConnectedBadge installation={existing} className={className} />
    );
  }

  if (!installSupported) return null;

  function closeDialog() {
    if (submitting) return;
    setDialogOpen(false);
    setClientId("");
    setClientSecret("");
  }

  async function handleSubmit() {
    const client_id = clientId.trim();
    const client_secret = clientSecret.trim();
    if (submitting || !agentId || !client_id || !client_secret) return;
    setSubmitting(true);
    try {
      await api.registerDingTalkBYO(wsId, agentId, { client_id, client_secret });
      // The dingtalk_installation realtime event also refreshes this list, but
      // invalidate explicitly so the connected badge appears immediately.
      await qc.invalidateQueries({ queryKey: dingtalkKeys.installations(wsId) });
      toast.success(t(($) => $.dingtalk.byo_success_toast));
      setDialogOpen(false);
      setClientId("");
      setClientSecret("");
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.dingtalk.byo_failed_toast),
      );
    } finally {
      setSubmitting(false);
    }
  }

  const canSubmit =
    clientId.trim() !== "" && clientSecret.trim() !== "" && !submitting;

  return (
    <div
      className={cn("flex flex-wrap items-center gap-2", className)}
      data-testid="dingtalk-agent-bind-buttons"
    >
      <Button
        variant="outline"
        size="sm"
        onClick={() => setDialogOpen(true)}
        disabled={!agentId}
        title={
          agentName
            ? t(($) => $.dingtalk.bind_button_title, { agent: agentName })
            : undefined
        }
        data-testid="dingtalk-agent-connect"
      >
        <DingTalkMark className="h-4 w-4" />
        {t(($) => $.dingtalk.bind_button)}
      </Button>

      <Dialog
        open={dialogOpen}
        onOpenChange={(v) => (v ? setDialogOpen(true) : closeDialog())}
      >
        <DialogContent
          className="gap-0 overflow-hidden p-0 sm:max-w-lg"
          data-testid="dingtalk-byo-dialog"
        >
          <DialogHeader className="gap-1 border-b px-5 py-3">
            <DialogTitle className="text-title-sm font-semibold">
              {t(($) => $.dingtalk.byo_dialog_title)}
            </DialogTitle>

            <button
              type="button"
              onClick={() => openExternal(dingtalkDocsUrl(i18n.language))}
              className="inline-flex w-fit items-center gap-1.5 text-caption text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline"
              data-testid="dingtalk-byo-docs-link"
            >
              <ExternalLink className="h-3.5 w-3.5" />
              {t(($) => $.dingtalk.byo_docs_link)}
            </button>
          </DialogHeader>

          <div className="space-y-4 p-5">
            <div className="space-y-1.5">
              <Label
                htmlFor="dingtalk-byo-client-id"
                className="text-caption text-muted-foreground"
              >
                {t(($) => $.dingtalk.byo_appkey_label)}
              </Label>
              <Input
                id="dingtalk-byo-client-id"
                data-testid="dingtalk-byo-client-id"
                type="password"
                value={clientId}
                onChange={(e) => setClientId(e.target.value)}
                autoComplete="off"
                spellCheck={false}
                disabled={submitting}
              />
            </div>

            <div className="space-y-1.5">
              <Label
                htmlFor="dingtalk-byo-client-secret"
                className="text-caption text-muted-foreground"
              >
                {t(($) => $.dingtalk.byo_appsecret_label)}
              </Label>
              <Input
                id="dingtalk-byo-client-secret"
                data-testid="dingtalk-byo-client-secret"
                type="password"
                value={clientSecret}
                onChange={(e) => setClientSecret(e.target.value)}
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
              {t(($) => $.dingtalk.byo_cancel)}
            </Button>
            <Button
              onClick={handleSubmit}
              disabled={!canSubmit}
              data-testid="dingtalk-byo-submit"
            >
              {submitting
                ? t(($) => $.dingtalk.byo_submitting)
                : t(($) => $.dingtalk.byo_submit)}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// DingTalkAgentBotStatusRow is the compact, read-only connected affordance the
// agent inspector renders instead of the full badge; it deep-links into the
// Integrations tab where Manage / Disconnect live.
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
        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-caption text-muted-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
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

// DingTalkAgentBotConnectedBadge is the full "already connected" affordance the
// Integrations tab renders in place of the Connect button: a status row plus a
// soft-destructive Disconnect. Only owners/admins ever reach this component.
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
        <span className="inline-flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
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
