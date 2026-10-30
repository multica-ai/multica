"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, Crown, Hash, History, MessageSquare, Server, Sparkles } from "@multica/ui/icons";
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
import type { Agent, AgentRuntime, AgentTask, ChatSession, MemberWithUser } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { StatusIcon } from "../../issues/components/status-icon";
import { useT, useTimeAgo } from "../../i18n";
import { teamHref } from "../selection";
import {
  AgentStatusPill,
  elapsedLabel,
  TeamPanel,
  TeamPanelRow,
  TeamPanelSection,
  useQueuedTasksByAgent,
  useRunningTasksByAgent,
  useTaskIssue,
} from "./team-ui";

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
  const running = useRunningTasksByAgent(wsId).get(agent.id) ?? [];
  const queued = useQueuedTasksByAgent(wsId).get(agent.id) ?? [];
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
  const offline = presence?.availability === "offline";

  return (
    <TeamPanel>
      <TeamPanelSection
        title={t(($) => $.panel.now)}
        trailing={
          !agent.archived_at && running.length + queued.length > 0
            ? t(($) => $.panel.now_counts, { running: running.length, queued: queued.length })
            : undefined
        }
      >
        {agent.archived_at ? (
          <p className="text-caption text-muted-foreground">{t(($) => $.panel.archived)}</p>
        ) : running.length > 0 ? (
          <div className="flex flex-col gap-1.5">
            {running.map((task) => (
              <RunningTaskCard key={task.id} wsId={wsId} task={task} />
            ))}
          </div>
        ) : (
          <p className="text-caption text-muted-foreground">
            {offline ? t(($) => $.panel.offline_note) : t(($) => $.panel.idle_note)}
          </p>
        )}
        {!agent.archived_at && queued.length > 0 && <QueuedLine wsId={wsId} tasks={queued} />}
      </TeamPanelSection>

      <TeamPanelSection title={t(($) => $.panel.properties)}>
        <dl className="flex flex-col gap-1">
          <TeamPanelRow label={t(($) => $.panel.status)}>
            <AgentStatusPill presence={presence} archived={!!agent.archived_at} />
          </TeamPanelRow>
          <TeamPanelRow label={t(($) => $.panel.runtime)}>
            <Server className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">
              {runtime ? runtimeDisplayLabel(runtime) : t(($) => $.panel.no_runtime)}
            </span>
          </TeamPanelRow>
          {agent.model && (
            <TeamPanelRow label={t(($) => $.panel.model)}>
              <Sparkles className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate">{agent.model}</span>
            </TeamPanelRow>
          )}
          {owner && (
            <TeamPanelRow label={t(($) => $.panel.owner)}>
              <ActorAvatar actorType="member" actorId={owner.user_id} size="xs" />
              <span className="truncate">{owner.name}</span>
            </TeamPanelRow>
          )}
          {agentSquads.map((squad) => (
            <TeamPanelRow key={squad.id} label={t(($) => $.panel.squad)}>
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
            </TeamPanelRow>
          ))}
        </dl>
      </TeamPanelSection>

      {earlier.length > 0 && (
        <TeamPanelSection title={t(($) => $.panel.earlier)} trailing={earlier.length}>
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
        </TeamPanelSection>
      )}
    </TeamPanel>
  );
}

function RunningTaskCard({ wsId, task }: { wsId: string; task: AgentTask }) {
  const { t } = useT("team");
  const wsPaths = useWorkspacePaths();
  const issue = useTaskIssue(wsId, task);
  const elapsed = elapsedLabel(task.started_at ?? task.created_at);
  const body = (
    <>
      {issue ? (
        <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
      ) : (
        <MessageSquare className="size-3.5 shrink-0 text-muted-foreground" />
      )}
      <span className="min-w-0 flex-1 truncate">
        {issue ? (
          <>
            <span className="font-medium">{issue.identifier}</span>{" "}
            <span className="text-muted-foreground">{issue.title}</span>
          </>
        ) : (
          t(($) => $.panel.replying_in_chat)
        )}
      </span>
      {elapsed && <span className="shrink-0 text-micro tabular-nums text-muted-foreground">{elapsed}</span>}
    </>
  );
  const className = "flex h-9 items-center gap-2 rounded-md border bg-background px-2.5 text-caption";
  return issue ? (
    <AppLink href={wsPaths.issueDetail(issue.id)} className={cn(className, "transition-colors hover:bg-surface-hover")}>
      {body}
    </AppLink>
  ) : (
    <div className={className}>{body}</div>
  );
}

function QueuedLine({ wsId, tasks }: { wsId: string; tasks: AgentTask[] }) {
  const { t } = useT("team");
  return (
    <p className="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
      <span className="shrink-0">{t(($) => $.panel.queued)}</span>
      <span className="truncate">
        {tasks.slice(0, 3).map((task, i) => (
          <QueuedIdentifier key={task.id} wsId={wsId} task={task} separator={i > 0} />
        ))}
        {tasks.length > 3 && ` +${tasks.length - 3}`}
      </span>
    </p>
  );
}

function QueuedIdentifier({ wsId, task, separator }: { wsId: string; task: AgentTask; separator: boolean }) {
  const { t } = useT("team");
  const issue = useTaskIssue(wsId, task);
  return (
    <>
      {separator && ", "}
      <span className="text-foreground">{issue?.identifier ?? t(($) => $.panel.chat_reply)}</span>
    </>
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
