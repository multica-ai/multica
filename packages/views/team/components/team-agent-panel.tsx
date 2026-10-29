"use client";

import { useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, Crown, Hash, History, Server, Sparkles } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import {
  earlierChatSessions,
  pickPrimaryChatSession,
} from "@multica/core/chat/primary-session";
import { squadListOptions } from "@multica/core/workspace/queries";
import type { Agent, AgentRuntime, ChatSession, MemberWithUser } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { AgentPresenceIndicator } from "../../agents/components/agent-presence-indicator";
import { useT, useTimeAgo } from "../../i18n";
import { teamHref } from "../selection";

/** Right rail of an agent: what it is doing, its properties, earlier chats. */
export function TeamAgentPanel({
  agent,
  runtime,
  owner,
  presence,
  openSessionId,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  owner: MemberWithUser | null;
  presence: AgentPresenceDetail | null;
  openSessionId: string | null;
}) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const primary = pickPrimaryChatSession(sessions, agent.id);
  const earlier = useMemo(
    () => earlierChatSessions(sessions, agent.id, primary?.id ?? null),
    [sessions, agent.id, primary?.id],
  );
  const agentSquads = squads.filter(
    (s) =>
      !s.archived_at &&
      (s.leader_id === agent.id ||
        s.member_preview?.some((m) => m.member_type === "agent" && m.member_id === agent.id)),
  );

  return (
    <aside className="hidden w-72 shrink-0 flex-col overflow-y-auto border-l px-4 py-3 lg:flex">
      <PanelHeading>{t(($) => $.panel.now)}</PanelHeading>
      {agent.archived_at ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.panel.archived)}</p>
      ) : (
        <AgentPresenceIndicator detail={presence} />
      )}

      <PanelHeading>{t(($) => $.panel.properties)}</PanelHeading>
      <dl className="flex flex-col gap-1.5 text-caption">
        <PanelRow label={t(($) => $.panel.runtime)}>
          <Server className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">
            {runtime ? runtimeDisplayLabel(runtime) : t(($) => $.panel.no_runtime)}
          </span>
        </PanelRow>
        {agent.model && (
          <PanelRow label={t(($) => $.panel.model)}>
            <Sparkles className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{agent.model}</span>
          </PanelRow>
        )}
        {owner && (
          <PanelRow label={t(($) => $.panel.owner)}>
            <ActorAvatar actorType="member" actorId={owner.user_id} size="xs" />
            <span className="truncate">{owner.name}</span>
          </PanelRow>
        )}
        {agentSquads.map((squad) => (
          <PanelRow key={squad.id} label={t(($) => $.panel.squad)}>
            <AppLink
              href={teamHref(wsPaths.team(), { kind: "squad", id: squad.id })}
              className="flex min-w-0 items-center gap-1.5 hover:underline"
            >
              <ActorAvatar actorType="squad" actorId={squad.id} size="xs" />
              <span className="truncate">{squad.name}</span>
              {squad.leader_id === agent.id && (
                <Crown className="size-3 shrink-0 text-muted-foreground" aria-label={t(($) => $.panel.leader)} />
              )}
            </AppLink>
          </PanelRow>
        ))}
      </dl>

      {earlier.length > 0 && (
        <>
          <PanelHeading trailing={earlier.length}>{t(($) => $.panel.earlier)}</PanelHeading>
          <ul className="-mx-2 flex flex-col gap-px">
            {earlier.map((session) => (
              <EarlierRow
                key={session.id}
                session={session}
                href={teamHref(wsPaths.team(), {
                  kind: "agent",
                  id: agent.id,
                  tab: "chat",
                  sessionId: session.id,
                })}
                selected={openSessionId === session.id}
              />
            ))}
          </ul>
        </>
      )}
    </aside>
  );
}

function EarlierRow({
  session,
  href,
  selected,
}: {
  session: ChatSession;
  href: string;
  selected: boolean;
}) {
  const { t } = useT("team");
  const timeAgo = useTimeAgo();
  const archived = session.status === "archived";
  const channel = session.channel_source?.channel_type;
  const Icon = channel ? Hash : archived ? Archive : History;
  const unread = !archived && (session.unread_count ?? 0) > 0;
  return (
    <li>
      <AppLink
        href={href}
        aria-current={selected ? "page" : undefined}
        className={cn(
          "flex h-8 items-center gap-2 rounded-md px-2 text-caption transition-colors",
          selected
            ? "bg-surface-selected text-surface-selected-foreground hover:bg-surface-selected"
            : "hover:bg-surface-hover",
          archived && !selected && "text-muted-foreground",
        )}
      >
        <Icon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate">
          {session.title?.trim() || t(($) => $.chat.untitled)}
        </span>
        {unread && <span className="size-1.5 shrink-0 rounded-full bg-brand" aria-label={t(($) => $.panel.unread)} />}
        <span className="shrink-0 text-micro text-muted-foreground">
          {channel ? `${t(($) => $.chat.channel_label[channelLabelKey(channel)])} · ` : ""}
          {timeAgo(session.last_message?.created_at ?? session.updated_at)}
        </span>
      </AppLink>
    </li>
  );
}

export function channelLabelKey(
  channel: string,
): "slack" | "lark" | "dingtalk" | "wecom" | "telegram" | "other" {
  switch (channel) {
    case "slack":
    case "lark":
    case "dingtalk":
    case "wecom":
    case "telegram":
      return channel;
    default:
      return "other";
  }
}

function PanelHeading({ children, trailing }: { children: ReactNode; trailing?: ReactNode }) {
  return (
    <h3 className="mb-2 mt-4 flex items-center text-caption font-medium text-muted-foreground first:mt-0">
      <span className="flex-1">{children}</span>
      {trailing != null && <span className="text-micro font-normal tabular-nums">{trailing}</span>}
    </h3>
  );
}

function PanelRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center gap-2">
      <dt className="w-16 shrink-0 text-muted-foreground">{label}</dt>
      <dd className="flex min-w-0 flex-1 items-center gap-1.5">{children}</dd>
    </div>
  );
}
