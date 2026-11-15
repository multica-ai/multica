"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { dingtalkAgentGroupsOptions } from "@multica/core/dingtalk";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { ExternalLink, MoreHorizontal, Plus } from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";

import { docsLocalePrefix } from "../../common/docs-locale";
import { ObjectSection, ObjectSectionAction } from "../../layout/object-page";
import { useT, useTimeAgo } from "../../i18n";
import { openExternal } from "../../platform";
import { useNavigation } from "../../navigation";
import { IntegrationChannelIcon } from "../../settings/components/integration-channel-icon";
import { ChannelDisconnectDialog } from "../../settings/components/channel-disconnect-dialog";
import { ChannelConnectDialog } from "../../settings/components/channel-connect-dialog";
import {
  DingTalkBotGroups,
  getDingTalkBotIdentity,
} from "../../settings/components/dingtalk-bot-groups";
import { CHANNEL_INTEGRATIONS } from "../../settings/components/settings-navigation";
import {
  channelChatLink,
  useMessagingChannels,
  type ChannelConnection,
} from "../../settings/components/use-messaging-channels";

const DINGTALK_CHAT_MANAGE_PERMISSION = "qyapi_chat_manage";

export function AgentChannelLinks({ agent }: { agent: Agent }) {
  const { connections } = useMessagingChannels();
  const links = connections.filter(
    (entry) => entry.installation.agent_id === agent.id,
  );
  if (links.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-2">
      {links.map((entry) => (
        <AgentChannelContact key={entry.installation.id} connection={entry} />
      ))}
    </div>
  );
}

function AgentChannelContact({
  connection,
}: {
  connection: ChannelConnection;
}) {
  const { t } = useT("settings");
  const { channel, installation } = connection;
  const { data } = useQuery({
    ...dingtalkAgentGroupsOptions(
      installation.workspace_id,
      installation.agent_id,
    ),
    enabled: channel === "dingtalk",
  });
  const name =
    connection.channel === "wecom"
      ? connection.installation.bot_name
      : (data?.bot_identities?.[installation.id]?.bot_name ??
        getDingTalkBotIdentity(data?.groups ?? [], installation.id)?.bot_name);
  const href = channelChatLink(connection);
  return href ? (
    <Button variant="outline" size="sm" onClick={() => openExternal(href)}>
      <IntegrationChannelIcon channel={channel} />
      {t(($) => $.channels.find_me, {
        platform: t(($) => $[channel].section_title),
      })}
    </Button>
  ) : name ? (
    <span className="inline-flex items-center gap-1.5 text-caption text-muted-foreground">
      <IntegrationChannelIcon channel={channel} />
      {name}
    </span>
  ) : null;
}

export function AgentChannelsSection({ agent }: { agent: Agent }) {
  const { t } = useT("settings");
  const { queries, connections, canManage, available, isAdmin } =
    useMessagingChannels();
  const [open, setOpen] = useState(false);
  const sectionRef = useRef<HTMLDivElement>(null);
  const { searchParams } = useNavigation();
  const view = searchParams.get("view") ?? searchParams.get("tab");
  const installed = connections.filter(
    (entry) => entry.installation.agent_id === agent.id,
  );
  const canAdd =
    !agent.archived_at &&
    CHANNEL_INTEGRATIONS.some(
      (id) =>
        canManage(id, agent) &&
        available(id) &&
        !installed.some((entry) => entry.channel === id),
    );
  const failed = CHANNEL_INTEGRATIONS.filter((id) => queries[id].isError);
  const visible = installed.length > 0 || canAdd || failed.length > 0;
  useEffect(() => {
    if (visible && (view === "channels" || view === "integrations"))
      sectionRef.current?.scrollIntoView({ block: "start" });
  }, [visible, view]);
  if (!visible) return null;

  return (
    <div
      ref={sectionRef}
      id="messaging-channels"
      data-settings-anchor="channels"
    >
      <ObjectSection
        title={t(($) => $.page.tabs.channels)}
        count={installed.length}
        action={
          canAdd ? (
            <ObjectSectionAction icon={Plus} onClick={() => setOpen(true)}>
              {t(($) => $.channels.add)}
            </ObjectSectionAction>
          ) : undefined
        }
      >
        {installed.length ? (
          <div className="divide-y rounded-lg border">
            {installed.map((connection) => (
              <AgentChannelRow
                key={connection.installation.id}
                connection={connection}
                canManage={canManage(connection.channel, agent)}
                canForget={isAdmin}
              />
            ))}
          </div>
        ) : canAdd ? (
          <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
            <Plus />
            {t(($) => $.channels.add)}
          </Button>
        ) : null}
        {failed.map((id) => (
          <div
            key={id}
            role="alert"
            className="mt-2 flex items-center justify-between gap-3 text-caption text-muted-foreground"
          >
            <span>
              {t(($) => $[id].section_title)} ·{" "}
              {t(($) => $.integrations.status_unknown)}
            </span>
            <Button
              size="xs"
              variant="outline"
              onClick={() => void queries[id].refetch()}
            >
              {t(($) => $.lark.install_retry)}
            </Button>
          </div>
        ))}
      </ObjectSection>
      <ChannelConnectDialog
        agents={[agent]}
        initialAgent={agent}
        open={open}
        onOpenChange={setOpen}
      />
    </div>
  );
}

function AgentChannelRow({
  connection,
  canManage,
  canForget,
}: {
  connection: ChannelConnection;
  canManage: boolean;
  canForget: boolean;
}) {
  const { t, i18n } = useT("settings");
  const timeAgo = useTimeAgo();
  const { channel, installation } = connection;
  const [confirm, setConfirm] = useState(false);
  const groups = useQuery({
    ...dingtalkAgentGroupsOptions(
      installation.workspace_id,
      installation.agent_id,
    ),
    enabled: channel === "dingtalk",
  });
  const identity =
    groups.data?.bot_identities?.[installation.id] ??
    getDingTalkBotIdentity(groups.data?.groups ?? [], installation.id);
  const identityIssue =
    channel === "dingtalk" ? identity?.bot_identity_issue : undefined;
  const missingChatPermission = identityIssue === "missing_qyapi_chat_manage";
  const name =
    channel === "dingtalk"
      ? identity?.bot_name
      : connection.channel === "wecom"
        ? connection.installation.bot_name
        : connection.channel === "telegram" &&
            connection.installation.bot_username
          ? `@${connection.installation.bot_username}`
          : undefined;
  const pending =
    connection.channel === "lark" &&
    !connection.installation.first_message_received_at;
  const href = channelChatLink(connection);
  const larkManageHref =
    connection.channel === "lark" && connection.installation.app_id
      ? `https://${connection.installation.region === "lark" ? "open.larksuite.com" : "open.feishu.cn"}/app/${encodeURIComponent(connection.installation.app_id)}`
      : undefined;
  const lastMessage = (groups.data?.groups ?? [])
    .flatMap((group) => group.bots)
    .filter(
      (bot) =>
        bot.installation_id === installation.id &&
        Date.parse(bot.last_active_at ?? "") > 0,
    )
    .map((bot) => bot.last_active_at!)
    .sort((left, right) => Date.parse(left) - Date.parse(right))
    .at(-1);

  return (
    <div className="px-3 py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <IntegrationChannelIcon channel={channel} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
            <span className="text-body font-medium">
              {t(($) => $[channel].section_title)}
            </span>
            {name ? (
              <span className="truncate text-caption text-muted-foreground">
                {name}
              </span>
            ) : null}
          </div>
          <span
            className={
              identityIssue
                ? "text-caption text-warning"
                : pending
                  ? "text-caption text-muted-foreground"
                  : "text-caption text-success"
            }
          >
            {identityIssue
              ? t(($) => $.dingtalk.bot_identity_unavailable)
              : pending
                ? t(($) => $.channels.waiting_message)
                : t(($) => $.channels.normal)}
            {lastMessage ? (
              <span className="ml-2 text-muted-foreground">
                {t(($) => $.channels.last_message, {
                  time: timeAgo(lastMessage),
                })}
              </span>
            ) : null}
          </span>
        </div>
        {href ? (
          <Button size="xs" variant="ghost" onClick={() => openExternal(href)}>
            <ExternalLink />
            {t(($) => $.channels.open)}
          </Button>
        ) : null}
        {canManage ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <IconButton
                  label={t(($) => $.channels.manage, {
                    platform: t(($) => $[channel].section_title),
                  })}
                  size="icon-xs"
                >
                  <MoreHorizontal />
                </IconButton>
              }
            />
            <DropdownMenuContent align="end">
              {connection.channel === "lark" ? (
                <>
                  {larkManageHref ? (
                    <DropdownMenuItem
                      onClick={() => openExternal(larkManageHref)}
                    >
                      <ExternalLink />
                      {connection.installation.region === "lark"
                        ? t(($) => $.lark.agent_bot_manage_link_lark)
                        : t(($) => $.lark.agent_bot_manage_link_feishu)}
                    </DropdownMenuItem>
                  ) : null}
                  <DropdownMenuItem
                    onClick={() =>
                      openExternal(
                        `https://multica.ai/docs${docsLocalePrefix(i18n.language)}/lark-bot-integration`,
                      )
                    }
                  >
                    <ExternalLink />
                    {t(($) => $.lark.agent_bot_silent_hint_link)}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                </>
              ) : null}
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setConfirm(true)}
              >
                {t(($) => $.channels.disconnect)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </div>
      {missingChatPermission && canManage ? (
        <p role="status" className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.dingtalk.bot_permission_tooltip_prefix)}{" "}
          <code
            className="rounded-xs bg-muted px-1 py-0.5 font-mono text-micro"
            translate="no"
          >
            {DINGTALK_CHAT_MANAGE_PERMISSION}
          </code>{" "}
          {t(($) => $.dingtalk.bot_permission_tooltip_suffix)}
        </p>
      ) : null}
      {channel === "dingtalk" &&
      (groups.isPending ||
        groups.isError ||
        groups.data?.group_discovery_supported) ? (
        <details className="mt-3">
          <summary className="cursor-pointer text-caption text-muted-foreground">
            {t(($) => $.dingtalk.groups_title)}
          </summary>
          <DingTalkBotGroups
            workspaceId={installation.workspace_id}
            agentId={installation.agent_id}
            installationId={installation.id}
            groups={groups.data?.groups ?? []}
            inactiveCount={
              groups.data?.inactive_group_counts?.[installation.id] ?? 0
            }
            isLoading={groups.isPending}
            isError={groups.isError}
            onRetry={() => void groups.refetch()}
            showDescription={false}
            canForget={canForget}
          />
        </details>
      ) : null}
      <ChannelDisconnectDialog
        connection={connection}
        open={confirm}
        onOpenChange={setConfirm}
      />
    </div>
  );
}
