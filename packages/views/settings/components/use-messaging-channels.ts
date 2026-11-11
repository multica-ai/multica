"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { larkInstallationsOptions } from "@multica/core/lark";
import { slackInstallationsOptions } from "@multica/core/slack";
import { dingtalkInstallationsOptions } from "@multica/core/dingtalk";
import { wecomInstallationsOptions } from "@multica/core/wecom";
import { telegramInstallationsOptions } from "@multica/core/telegram";
import type {
  Agent,
  LarkInstallation,
  SlackInstallation,
  DingTalkInstallation,
  WecomInstallation,
  TelegramInstallation,
} from "@multica/core/types";
import type { ChannelIntegration } from "./settings-navigation";

export type ChannelConnection =
  | { channel: "lark"; installation: LarkInstallation }
  | { channel: "slack"; installation: SlackInstallation }
  | { channel: "dingtalk"; installation: DingTalkInstallation }
  | { channel: "wecom"; installation: WecomInstallation }
  | { channel: "telegram"; installation: TelegramInstallation };

export function channelChatLink(
  connection: ChannelConnection,
): string | undefined {
  switch (connection.channel) {
    case "lark":
      return connection.installation.app_id
        ? `https://${connection.installation.region === "lark" ? "applink.larksuite.com" : "applink.feishu.cn"}/client/bot/open?appId=${encodeURIComponent(connection.installation.app_id)}`
        : undefined;
    case "slack":
      return connection.installation.team_id &&
        connection.installation.bot_user_id
        ? `https://app.slack.com/client/${encodeURIComponent(connection.installation.team_id)}/${encodeURIComponent(connection.installation.bot_user_id)}`
        : undefined;
    case "telegram":
      return connection.installation.bot_username
        ? `https://t.me/${encodeURIComponent(connection.installation.bot_username)}`
        : undefined;
    default:
      return undefined;
  }
}

export function useMessagingChannels() {
  const wsId = useWorkspaceId();
  const { member, userId, role } = useCurrentMember(wsId);
  const enabled = !!wsId && !!member;
  const lark = useQuery({ ...larkInstallationsOptions(wsId), enabled });
  const slack = useQuery({ ...slackInstallationsOptions(wsId), enabled });
  const dingtalk = useQuery({ ...dingtalkInstallationsOptions(wsId), enabled });
  const wecom = useQuery({ ...wecomInstallationsOptions(wsId), enabled });
  const telegram = useQuery({ ...telegramInstallationsOptions(wsId), enabled });
  const queries = { lark, slack, dingtalk, wecom, telegram };
  const connections: ChannelConnection[] = [
    ...(lark.data?.installations ?? []).map((installation) => ({
      channel: "lark" as const,
      installation,
    })),
    ...(slack.data?.installations ?? []).map((installation) => ({
      channel: "slack" as const,
      installation,
    })),
    ...(dingtalk.data?.installations ?? []).map((installation) => ({
      channel: "dingtalk" as const,
      installation,
    })),
    ...(wecom.data?.installations ?? []).map((installation) => ({
      channel: "wecom" as const,
      installation,
    })),
    ...(telegram.data?.installations ?? []).map((installation) => ({
      channel: "telegram" as const,
      installation,
    })),
  ].filter(({ installation }) => installation.status === "active");
  const isAdmin = role === "owner" || role === "admin";
  const canManage = (channel: ChannelIntegration, agent: Agent) =>
    !!member &&
    (isAdmin ||
      ((channel === "lark" || channel === "dingtalk") &&
        agent.owner_id === userId));
  const available = (channel: ChannelIntegration) =>
    queries[channel].data?.configured === true &&
    queries[channel].data?.install_supported === true;

  return { wsId, queries, connections, isAdmin, canManage, available };
}
