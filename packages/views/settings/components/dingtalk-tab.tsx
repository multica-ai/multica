"use client";

import { useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronDown, ChevronRight, ExternalLink, Info, Trash2 } from "lucide-react";
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
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { DingTalkMark } from "./dingtalk-mark";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  dingtalkGroupsOptions,
  dingtalkInstallationsOptions,
  dingtalkKeys,
} from "@multica/core/dingtalk";
import { api } from "@multica/core/api";
import type {
  DingTalkGroup,
  DingTalkGroupBot,
  DingTalkInstallation,
} from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { openExternal } from "../../platform";
import { useT, useTimeAgo } from "../../i18n";

const dingTalkChatManagePermission = "qyapi_chat_manage";

// formatInstalledAt renders the install timestamp defensively: the schema
// defaults installed_at to "" and the backend can emit a zero-value timestamp
// (0001-01-01T…) for a never-set time, either of which would otherwise surface
// as "Invalid Date" or a year-1 date. Fall back to a neutral placeholder.
function formatInstalledAt(value: string, locale: string): string {
  const t = Date.parse(value);
  if (!value || Number.isNaN(t) || t <= 0) return "—";
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(t));
}

export function getDingTalkBotIdentity(
  groups: DingTalkGroup[],
  installationId: string,
): DingTalkGroupBot | undefined {
  const bots = groups.flatMap((group) =>
    group.bots.filter((bot) => bot.installation_id === installationId),
  );
  const identity = bots.find((bot) => bot.bot_name) ?? bots[0];
  const issue = bots.find((bot) => bot.bot_identity_issue)?.bot_identity_issue;
  return identity
    ? { ...identity, bot_identity_issue: issue ?? identity.bot_identity_issue }
    : undefined;
}

export function DingTalkConnectionLabel({
  botName,
  botIdentityIssue,
  linkedIdentityIDs = [],
  showBotIdentity = true,
  showPermissionHelp = true,
  className,
}: {
  botName?: string;
  botIdentityIssue?: string;
  linkedIdentityIDs?: string[];
  showBotIdentity?: boolean;
  showPermissionHelp?: boolean;
  className?: string;
}) {
  const { t } = useT("settings");
  const botIdentityText =
    botName || t(($) => $.dingtalk.bot_identity_unavailable);
  const linkedIdentityLabel =
    linkedIdentityIDs.length > 0
      ? t(($) => $.dingtalk.identity_label, {
          identity: linkedIdentityIDs.join(", "),
        })
      : "";
  const missingChatManagePermission =
    botIdentityIssue === "missing_qyapi_chat_manage";
  const permissionTooltipPrefix = t(
    ($) => $.dingtalk.bot_permission_tooltip_prefix,
  );
  const permissionTooltipSuffix = t(
    ($) => $.dingtalk.bot_permission_tooltip_suffix,
  );
  const permissionTooltip = `${permissionTooltipPrefix} ${dingTalkChatManagePermission} ${permissionTooltipSuffix}`;
  const [permissionTooltipOpen, setPermissionTooltipOpen] = useState(false);

  return (
    <span
      className={cn(
        "inline-flex min-w-0 items-center gap-2 text-caption text-muted-foreground",
        className,
      )}
    >
      <span
        className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500"
        aria-hidden="true"
      />
      <span className="inline-flex min-w-0 items-center">
        <span className="shrink-0">
          {t(($) => $.dingtalk.agent_bot_connected_label)}
        </span>
        {showBotIdentity && (
          <>
            {linkedIdentityLabel ? (
              <TooltipProvider delay={0}>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        type="button"
                        className="ml-1 min-w-0 cursor-help truncate rounded-sm text-left text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      >
                        {botIdentityText}
                      </button>
                    }
                  />
                  <TooltipContent
                    side="top"
                    className="max-w-80 whitespace-normal break-all"
                    translate="no"
                  >
                    {linkedIdentityLabel}
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            ) : (
              <span className="ml-1 truncate text-foreground">
                {botIdentityText}
              </span>
            )}
            {missingChatManagePermission && showPermissionHelp && (
              <TooltipProvider delay={0}>
                <Tooltip
                  open={permissionTooltipOpen}
                  onOpenChange={setPermissionTooltipOpen}
                >
                  <TooltipTrigger
                    closeOnClick={false}
                    onClick={() => setPermissionTooltipOpen(true)}
                    render={
                      <button
                        type="button"
                        className="-my-1 ml-0.5 inline-flex size-6 shrink-0 items-center justify-center rounded-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        aria-label={permissionTooltip}
                      >
                        <Info className="size-3.5" aria-hidden="true" />
                      </button>
                    }
                  />
                  <TooltipContent
                    side="top"
                    align="start"
                    className="max-w-80 items-start whitespace-normal border-0 bg-surface-raised px-3 py-2 text-popover-foreground shadow-[var(--menu-shadow)] ring-1 ring-surface-border"
                  >
                    <span className="leading-relaxed">
                      {permissionTooltipPrefix}{" "}
                      <code
                        className="rounded bg-muted px-1.5 py-0.5 font-mono text-micro text-foreground ring-1 ring-border"
                        translate="no"
                      >
                        {dingTalkChatManagePermission}
                      </code>{" "}
                      {permissionTooltipSuffix}
                    </span>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )}
          </>
        )}
      </span>
    </span>
  );
}

export function DingTalkBotGroups({
  workspaceId,
  agentId,
  installationId,
  groups,
  inactiveCount = 0,
  canForget = false,
  isLoading,
  isError,
  onRetry,
  showDescription = true,
  className,
}: {
  workspaceId: string;
  agentId?: string;
  installationId: string;
  groups: DingTalkGroup[];
  inactiveCount?: number;
  canForget?: boolean;
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  showDescription?: boolean;
  className?: string;
}) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const qc = useQueryClient();
  const [inactiveExpanded, setInactiveExpanded] = useState(false);
  const [forgetTarget, setForgetTarget] = useState<DingTalkGroup | null>(null);
  const [forgetting, setForgetting] = useState(false);
  const inactiveQuery = useInfiniteQuery({
    queryKey: agentId
      ? dingtalkKeys.agentInactiveGroups(workspaceId, agentId, installationId)
      : dingtalkKeys.inactiveGroups(workspaceId, installationId),
    initialPageParam: 0,
    queryFn: ({ pageParam }) => {
      const params = {
        activity: "inactive" as const,
        installationId,
        offset: pageParam,
      };
      return agentId
        ? api.listAgentDingTalkGroups(agentId, params)
        : api.listDingTalkGroups(workspaceId, params);
    },
    getNextPageParam: (page) => page.next_offset,
    enabled: inactiveExpanded && inactiveCount > 0,
  });

  const collectObserved = (source: DingTalkGroup[]) => source
    .flatMap((group) => {
      const bot = group.bots.find(
        (candidate) => candidate.installation_id === installationId,
      );
      return bot ? [{ group, bot }] : [];
    })
    .sort((left, right) => {
      const leftActiveAt = Date.parse(left.bot.last_active_at ?? "");
      const rightActiveAt = Date.parse(right.bot.last_active_at ?? "");
      const leftIsActive = !Number.isNaN(leftActiveAt);
      const rightIsActive = !Number.isNaN(rightActiveAt);

      if (leftIsActive && rightIsActive && leftActiveAt !== rightActiveAt) {
        return rightActiveAt - leftActiveAt;
      }
      if (leftIsActive !== rightIsActive) return leftIsActive ? -1 : 1;

      const leftTitle = left.group.conversation_title;
      const rightTitle = right.group.conversation_title;
      if (leftTitle !== rightTitle) {
        if (!leftTitle) return 1;
        if (!rightTitle) return -1;
        return leftTitle < rightTitle ? -1 : 1;
      }
      if (left.group.conversation_id === right.group.conversation_id) return 0;
      return left.group.conversation_id < right.group.conversation_id ? -1 : 1;
    });
  const observed = collectObserved(groups);
  const inactiveObserved = collectObserved(
    inactiveQuery.data?.pages.flatMap((page) => page.groups) ?? [],
  );

  async function handleForget() {
    if (!forgetTarget || forgetting) return;
    setForgetting(true);
    try {
      await api.forgetDingTalkGroup(
        workspaceId,
        installationId,
        forgetTarget.conversation_id,
      );
      await qc.invalidateQueries({ queryKey: dingtalkKeys.groups(workspaceId) });
      toast.success(t(($) => $.dingtalk.group_forget_success));
      setForgetTarget(null);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.dingtalk.group_forget_failed),
      );
    } finally {
      setForgetting(false);
    }
  }

  const renderObserved = (
    entries: Array<{ group: DingTalkGroup; bot: DingTalkGroupBot }>,
  ) => (
    <div>
      {entries.map(({ group, bot }) => (
        <div
          key={group.conversation_id}
          className="group space-y-1.5 border-t py-2.5"
          data-testid="dingtalk-group-item"
        >
          <div className="flex min-w-0 items-baseline justify-between gap-3">
            <div className="flex min-w-0 flex-1 items-baseline gap-3">
              <p className="min-w-0 truncate text-caption font-medium">
                {group.conversation_title || t(($) => $.dingtalk.group_untitled)}
              </p>
              {canForget && (
                <button
                  type="button"
                  className="pointer-events-none shrink-0 text-micro text-muted-foreground opacity-0 underline-offset-2 transition-opacity group-hover:pointer-events-auto group-hover:opacity-100 group-focus-within:pointer-events-auto group-focus-within:opacity-100 hover:text-foreground hover:underline focus-visible:pointer-events-auto focus-visible:opacity-100"
                  onClick={() => setForgetTarget(group)}
                >
                  {t(($) => $.dingtalk.group_forget)}
                </button>
              )}
            </div>
            <div className="flex min-w-0 max-w-[60%] items-center">
              <TooltipProvider delay={0}>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <code
                        tabIndex={0}
                        translate="no"
                        aria-label={`${t(($) => $.dingtalk.conversation_id_label)} ${group.conversation_id}`}
                        className="block min-w-0 truncate font-mono text-micro text-faint-foreground transition-colors group-hover:text-muted-foreground focus-visible:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      >
                        {group.conversation_id}
                      </code>
                    }
                  />
                  <TooltipContent side="top">
                    {t(($) => $.dingtalk.conversation_id_label)}
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </div>
          </div>
          {bot.last_active_at && (
            <p
              className="text-micro tabular-nums text-muted-foreground"
              data-testid="dingtalk-group-activity"
            >
              {t(($) => $.dingtalk.group_last_active, {
                time: timeAgo(bot.last_active_at),
              })}
              <span className="mx-1.5" aria-hidden="true">·</span>
              {t(($) => $.dingtalk.group_mentions, {
                count: bot.mention_count ?? 0,
              })}
            </p>
          )}
        </div>
      ))}
    </div>
  );

  return (
    <div
      className={cn("space-y-2 pt-3", className)}
      data-testid="dingtalk-bot-groups"
    >
      <div className="flex items-center gap-2">
        <h4 className="text-body font-medium text-pretty">
          {t(($) => $.dingtalk.groups_title)}
        </h4>
        {!isLoading && !isError && (
          <span className="rounded-full bg-muted px-2 py-0.5 text-micro tabular-nums text-muted-foreground">
            {t(($) => $.dingtalk.groups_count, { count: observed.length })}
          </span>
        )}
      </div>
      {showDescription && (
        <p className="text-caption leading-relaxed text-muted-foreground">
          {t(($) => $.dingtalk.groups_description)}
        </p>
      )}
      {isLoading ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.dingtalk.groups_loading)}
        </p>
      ) : isError ? (
        <div className="flex items-center justify-between gap-3" role="alert">
          <p className="text-caption text-muted-foreground">
            {t(($) => $.dingtalk.groups_error)}
          </p>
          <button
            type="button"
            className="shrink-0 text-caption font-medium underline-offset-2 hover:underline"
            onClick={onRetry}
          >
            {t(($) => $.dingtalk.groups_retry)}
          </button>
        </div>
      ) : observed.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {inactiveCount > 0
            ? t(($) => $.dingtalk.groups_no_recent)
            : t(($) => $.dingtalk.groups_empty)}
        </p>
      ) : (
        renderObserved(observed)
      )}
      {!isLoading && !isError && inactiveCount > 0 && (
        <div className="border-t pt-2">
          <button
            type="button"
            className="flex w-full items-center gap-1.5 py-1 text-caption font-medium text-muted-foreground hover:text-foreground"
            onClick={() => setInactiveExpanded((value) => !value)}
            aria-expanded={inactiveExpanded}
          >
            {inactiveExpanded
              ? <ChevronDown className="h-3.5 w-3.5" aria-hidden="true" />
              : <ChevronRight className="h-3.5 w-3.5" aria-hidden="true" />}
            {t(($) => $.dingtalk.groups_inactive, { count: inactiveCount })}
          </button>
          {inactiveExpanded && (
            <div className="pt-1">
              {inactiveQuery.isLoading ? (
                <p className="py-2 text-caption text-muted-foreground">
                  {t(($) => $.dingtalk.groups_loading)}
                </p>
              ) : inactiveQuery.isError ? (
                <button
                  type="button"
                  className="py-2 text-caption font-medium underline-offset-2 hover:underline"
                  onClick={() => void inactiveQuery.refetch()}
                >
                  {t(($) => $.dingtalk.groups_retry)}
                </button>
              ) : (
                <>
                  {renderObserved(inactiveObserved)}
                  {inactiveQuery.hasNextPage && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={inactiveQuery.isFetchingNextPage}
                      onClick={() => void inactiveQuery.fetchNextPage()}
                    >
                      {inactiveQuery.isFetchingNextPage
                        ? t(($) => $.dingtalk.groups_loading)
                        : t(($) => $.dingtalk.groups_load_more)}
                    </Button>
                  )}
                </>
              )}
            </div>
          )}
        </div>
      )}
      <AlertDialog
        open={!!forgetTarget}
        onOpenChange={(open) => {
          if (!open && !forgetting) setForgetTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.dingtalk.group_forget_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.dingtalk.group_forget_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={forgetting}>
              {t(($) => $.dingtalk.group_forget_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleForget} disabled={forgetting}>
              {t(($) => $.dingtalk.group_forget_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// DingTalkTab is the workspace settings panel for DingTalk robot installations.
// Installation rows and group metadata follow Agent view permissions on the
// server. Disconnect, linked Staff IDs, and permission remediation remain
// workspace owner/admin-only in Settings.
//
// Adding a new installation flows through the Agent detail page: the install
// path is per-agent (each Multica agent gets exactly one robot — the
// (workspace_id, agent_id, channel_type) UNIQUE in channel_installation), so
// asking the user to pick an agent here would re-create that page's picker.
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
  });
  const installations = data?.installations ?? [];
  const { data: visibleAgents = [], isLoading: agentsLoading } = useQuery({
    ...agentListOptions(wsId),
    enabled: !canManage && !!wsId,
  });
  const visibleAgentIDs = new Set(visibleAgents.map((agent) => agent.id));
  // New servers already filter this response. Keep the client-side join as a
  // compatibility guard for older servers that returned every workspace
  // installation to members, which otherwise creates Unknown Agent dead links.
  const displayedInstallations = canManage
    ? installations
    : installations.filter(
        (installation) =>
          installation.agent_available !== false &&
          visibleAgentIDs.has(installation.agent_id),
      );
  const configured = data?.configured === true;
  const {
    data: groupsData,
    isLoading: groupsLoading,
    isError: groupsError,
    refetch: retryGroups,
  } = useQuery({
    ...dingtalkGroupsOptions(wsId),
    enabled:
      configured &&
      displayedInstallations.some(
        (installation) => installation.status === "active",
      ),
  });
  const groupDiscoverySupported =
    groupsData?.group_discovery_supported === true;
  const showGroupDiscovery =
    groupDiscoverySupported || groupsLoading || groupsError;

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
      toast.error(
        e instanceof Error ? e.message : t(($) => $.dingtalk.toast_disconnect_failed),
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
        <section className="space-y-4">
          <div className="space-y-1.5">
            <h2 className="text-body font-semibold">
              {t(($) => $.dingtalk.connections_title)}
            </h2>
            {!isLoading &&
              displayedInstallations.length > 0 &&
              groupDiscoverySupported && (
              <p className="max-w-3xl text-caption leading-relaxed text-muted-foreground">
                {t(($) => $.dingtalk.groups_overview_description)}
              </p>
            )}
          </div>
          {isLoading || (!canManage && agentsLoading) ? (
            <Card>
              <CardContent>
                <p className="text-body text-muted-foreground">{t(($) => $.dingtalk.loading)}</p>
              </CardContent>
            </Card>
          ) : displayedInstallations.length === 0 ? (
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
            <Card className="py-0">
              <CardContent className="divide-y divide-border/70">
                {displayedInstallations.map((inst) => (
                  <InstallationRow
                    key={inst.id}
                    workspaceId={wsId}
                    installation={inst}
                    canManage={canManage}
                    onDisconnect={() => setDisconnectTarget(inst.id)}
                    groups={groupsData?.groups ?? []}
                    botIdentity={groupsData?.bot_identities?.[inst.id]}
                    inactiveCount={groupsData?.inactive_group_counts?.[inst.id] ?? 0}
                    groupsLoading={groupsLoading}
                    groupsError={groupsError}
                    groupDiscoverySupported={groupDiscoverySupported}
                    showGroupDiscovery={showGroupDiscovery}
                    onRetryGroups={() => void retryGroups()}
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
  workspaceId,
  installation,
  canManage,
  onDisconnect,
  groups,
  botIdentity: suppliedBotIdentity,
  inactiveCount,
  groupsLoading,
  groupsError,
  groupDiscoverySupported,
  showGroupDiscovery,
  onRetryGroups,
}: {
  workspaceId: string;
  installation: DingTalkInstallation;
  canManage: boolean;
  onDisconnect: () => void;
  groups: DingTalkGroup[];
  botIdentity?: DingTalkGroupBot;
  inactiveCount: number;
  groupsLoading: boolean;
  groupsError: boolean;
  groupDiscoverySupported: boolean;
  showGroupDiscovery: boolean;
  onRetryGroups: () => void;
}) {
  const { t, i18n } = useT("settings");
  const { getAgentName } = useActorName();
  const isActive = installation.status === "active";
  const agentAvailable = installation.agent_available !== false;
  const agentName = agentAvailable
    ? getAgentName(installation.agent_id)
    : t(($) => $.dingtalk.deleted_agent);
  const linkedIdentityIDs = canManage
    ? (installation.bound_dingtalk_user_ids ?? [])
    : [];
  const botIdentity = suppliedBotIdentity ?? getDingTalkBotIdentity(groups, installation.id);
  return (
    <div
      className="py-6"
      data-testid="dingtalk-installation-row"
    >
      <div className="flex items-start justify-between gap-6">
        <div className="flex min-w-0 items-start gap-3">
          <ActorAvatar
            actorType="agent"
            actorId={installation.agent_id}
            size="lg"
            enableHoverCard={agentAvailable}
            profileLink={agentAvailable}
          />
          <div className="min-w-0 space-y-1.5">
            <h3 className="truncate text-title-sm font-medium text-pretty">
              {agentName}
              {!isActive && (
                <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                  {t(($) => $.dingtalk.revoked_badge)}
                </span>
              )}
            </h3>
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              {isActive && (
                <>
                  <DingTalkConnectionLabel
                    botName={botIdentity?.bot_name ?? ""}
                    botIdentityIssue={botIdentity?.bot_identity_issue ?? ""}
                    linkedIdentityIDs={linkedIdentityIDs}
                    showBotIdentity={
                      groupDiscoverySupported && !groupsLoading
                    }
                    showPermissionHelp={canManage}
                  />
                  <span
                    className="text-micro text-muted-foreground"
                    aria-hidden="true"
                  >
                    ·
                  </span>
                </>
              )}
              <span
                className="text-micro text-muted-foreground"
                data-testid="dingtalk-installation-metadata"
              >
                {t(($) => $.dingtalk.installed_at_label, {
                  when: formatInstalledAt(installation.installed_at, i18n.language),
                })}
              </span>
            </div>
          </div>
        </div>
        {canManage && isActive && (
          <Button variant="outline" size="sm" onClick={onDisconnect}>
            <Trash2 className="h-3 w-3" aria-hidden="true" />
            {t(($) => $.dingtalk.disconnect)}
          </Button>
        )}
      </div>
      {isActive && showGroupDiscovery && (
        <DingTalkBotGroups
          workspaceId={workspaceId}
          installationId={installation.id}
          groups={groups}
          inactiveCount={inactiveCount}
          canForget={canManage}
          isLoading={groupsLoading}
          isError={groupsError}
          onRetry={onRetryGroups}
          showDescription={false}
          className="space-y-3 pt-5"
        />
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
// where an authorized manager pastes the AppKey (client id) + AppSecret (client
// secret) of the DingTalk robot they created (the backend validates both).
// Visibility:
//   1. Only the agent's owner or a workspace owner/admin sees management UI.
//   2. If this agent already has an active installation, show the connected
//      badge (already-installed robots stay manageable).
//   3. Otherwise the Connect CTA shows whenever install is available.
export function DingTalkAgentBindButton({
  agentId,
  agentName,
  agentOwnerId,
  botName,
  botIdentityIssue,
  className,
  onShowConnectedDetails,
}: {
  agentId: string;
  agentName?: string;
  /** Mirrors the backend canManageAgent rule for the per-agent entry point. */
  agentOwnerId?: string | null;
  botName?: string;
  botIdentityIssue?: string;
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
  const isWorkspaceAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";
  const isAgentOwner =
    currentMember != null &&
    !!user?.id &&
    agentOwnerId != null &&
    agentOwnerId === user.id;
  const canManage = isWorkspaceAdmin || isAgentOwner;

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
      <DingTalkAgentBotConnectedBadge
        installation={existing}
        botName={botName}
        botIdentityIssue={botIdentityIssue}
        className={className}
      />
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
              <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
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
      <DingTalkConnectionLabel showBotIdentity={false} />
      <ChevronRight
        className="ml-auto h-3.5 w-3.5 shrink-0"
        aria-hidden="true"
      />
    </button>
  );
}

// DingTalkAgentBotConnectedBadge is the full "already connected" affordance the
// Integrations tab renders in place of the Connect button: a status row plus a
// soft-destructive Disconnect. Only owners/admins ever reach this component.
function DingTalkAgentBotConnectedBadge({
  installation,
  botName,
  botIdentityIssue,
  className,
}: {
  installation: DingTalkInstallation;
  botName?: string;
  botIdentityIssue?: string;
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
        <DingTalkConnectionLabel
          botName={botName}
          botIdentityIssue={botIdentityIssue}
          showBotIdentity={botName !== undefined || botIdentityIssue !== undefined}
        />
        <Button
          variant="destructive"
          size="sm"
          onClick={() => setConfirmOpen(true)}
          disabled={disconnecting}
          title={t(($) => $.dingtalk.agent_bot_disconnect_tooltip)}
          aria-label={t(($) => $.dingtalk.disconnect)}
          data-testid="dingtalk-agent-bot-disconnect"
        >
          <Trash2 className="h-3 w-3" aria-hidden="true" />
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
