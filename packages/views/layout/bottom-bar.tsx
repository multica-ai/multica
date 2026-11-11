"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, ArrowUpRight, TriangleAlert } from "@multica/ui/icons";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@multica/ui/components/ui/hover-card";
import { useChatStore } from "@multica/core/chat";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import type { Agent, AgentTask } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { ChatBar } from "../chat/components/chat-bar";
import { isFloatingChatRouteSuppressed } from "../chat/floating-chat-visibility";
import { useT } from "../i18n";
import { AppLink, useNavigation } from "../navigation";
import { useAppShell } from "./app-shell-context";
import { BOTTOM_BAR_ITEM_CLASS } from "./bottom-bar-styles";
import { useTeamAgentStatus } from "./panels/team-status";

const MAX_PREVIEW_AGENTS = 5;
const EMPTY_TASKS: AgentTask[] = [];

function WorkingAgentPreview({
  wsId,
  agent,
  task,
  runningCount,
}: {
  wsId: string;
  agent: Agent;
  task: AgentTask | undefined;
  runningCount: number;
}) {
  const { t } = useT("layout");
  const issueId = task?.issue_id ?? "";
  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, issueId),
    enabled: !!issueId,
  });

  return (
    <div className="flex min-w-0 items-center gap-2 text-caption">
      <ActorAvatar
        name={agent.name}
        initials={agent.name.charAt(0).toUpperCase()}
        avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
        isAgent
        size="md"
      />
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{agent.name}</div>
        <div className="truncate text-muted-foreground">
          {issue?.title ?? t(($) => $.panels.team.running_tasks, { count: runningCount })}
        </div>
      </div>
    </div>
  );
}

function WorkingAgentsPreview({
  wsId,
  working,
  byAgent,
  teamHref,
}: {
  wsId: string;
  working: Agent[];
  byAgent: Map<string, AgentPresenceDetail>;
  teamHref: string;
}) {
  const { t } = useT("layout");
  const { data: tasks = EMPTY_TASKS } = useQuery(agentTaskSnapshotOptions(wsId));
  const runningByAgent = useMemo(() => {
    const result = new Map<string, AgentTask>();
    for (const task of tasks) {
      if (task.status === "running" && task.issue_id && !result.has(task.agent_id)) {
        result.set(task.agent_id, task);
      }
    }
    return result;
  }, [tasks]);

  return (
    <div className="flex flex-col gap-3">
      <div className="text-caption font-medium text-muted-foreground">
        {t(($) => $.bottom_bar.working, { count: working.length })}
      </div>
      {working.slice(0, MAX_PREVIEW_AGENTS).map((agent) => (
        <WorkingAgentPreview
          key={agent.id}
          wsId={wsId}
          agent={agent}
          task={runningByAgent.get(agent.id)}
          runningCount={byAgent.get(agent.id)?.runningCount ?? 0}
        />
      ))}
      <AppLink
        href={teamHref}
        className="flex items-center gap-1.5 border-t border-surface-border pt-2.5 text-caption text-muted-foreground outline-none hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
      >
        {working.length > MAX_PREVIEW_AGENTS
          ? t(($) => $.panels.team.show_more_agents, { count: working.length - MAX_PREVIEW_AGENTS })
          : t(($) => $.spaces.team)}
        <ArrowUpRight aria-hidden className="size-3.5" />
      </AppLink>
    </div>
  );
}

/**
 * What the team is doing, at a glance: how many agents are working, and work
 * that cannot start because its runtime is offline. Both lead to Team, which
 * names the agents. Renders nothing while the team is idle.
 */
function TeamStatus({ wsId }: { wsId: string }) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const shell = useAppShell();
  const { working, stuck, byAgent } = useTeamAgentStatus(wsId);
  const teamHref = shell?.spaceHref("team", p.agents()) ?? p.agents();
  const waitingTasks = stuck.reduce(
    (total, agent) => total + (byAgent.get(agent.id)?.queuedCount ?? 0),
    0,
  );

  return (
    <div className="flex min-w-0 items-center gap-1">
      {working.length > 0 && (
        <HoverCard>
          <HoverCardTrigger
            render={<AppLink href={teamHref} className={BOTTOM_BAR_ITEM_CLASS} />}
          >
            <Activity aria-hidden className="size-3.5 shrink-0" />
            <span className="truncate">
              {t(($) => $.bottom_bar.working, { count: working.length })}
            </span>
          </HoverCardTrigger>
          <HoverCardContent side="top" align="start" sideOffset={8} className="w-80">
            <WorkingAgentsPreview wsId={wsId} working={working} byAgent={byAgent} teamHref={teamHref} />
          </HoverCardContent>
        </HoverCard>
      )}
      {waitingTasks > 0 && (
        <AppLink
          href={teamHref}
          className="inline-flex h-[22px] min-w-0 items-center gap-1.5 rounded-md bg-page-canvas pr-2 pl-1.5 text-caption font-medium text-foreground ring-1 ring-surface-border outline-none transition-colors hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-ring"
        >
          <TriangleAlert aria-hidden className="size-3.5 shrink-0 text-warning" />
          <span className="truncate">{t(($) => $.panels.team.stuck, { count: waitingTasks })}</span>
        </AppLink>
      )}
    </div>
  );
}

/**
 * The bar under the content card: workspace status on the left, quick chat on
 * the right. It sits on the app shell, in the strip the card's bottom margin
 * used to take. Below `md` there is no bar and quick chat is the floating
 * launcher.
 */
export function AppBottomBar() {
  const { t } = useT("layout");
  const wsId = useCurrentWorkspace()?.id;
  const isMobile = useIsMobile();
  const floatingChatEnabled = useChatStore((s) => s.floatingChatEnabled);
  const { pathname } = useNavigation();
  const chatPath = useWorkspacePaths().chat();

  if (!wsId || isMobile) return null;
  // Same gates as the floating window: the Settings preference, and the Chat
  // tab, which already shows the conversation full-page.
  const showChat = floatingChatEnabled && !isFloatingChatRouteSuppressed(pathname, chatPath);

  return (
    <footer
      aria-label={t(($) => $.bottom_bar.label)}
      data-slot="app-bottom-bar"
      data-mc="bottom-bar"
      // pr: the card's mr-2 plus a gutter that puts the last icon on the same
      // column as the page header's trailing action.
      className="hidden h-8 shrink-0 items-center justify-between gap-4 pr-[18px] pl-6 md:flex"
    >
      <TeamStatus wsId={wsId} />
      {showChat && <ChatBar />}
    </footer>
  );
}
