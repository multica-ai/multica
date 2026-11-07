"use client";

import { useId, useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { BarChart3, BookOpenText, Bot, Monitor, Plus, User, UserPlus, Users } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { agentTaskSnapshotOptions, type AgentPresenceDetail } from "@multica/core/agents";
import { useAgentsViewStore } from "@multica/core/agents/stores";
import { useAuthStore } from "@multica/core/auth";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { memberListOptions, skillListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import type { Agent, AgentTask } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { availabilityConfig } from "../../agents/presence";
import { useT } from "../../i18n";
import { buildRuntimeMachines, runtimeMachineCounts } from "../../runtimes/components/runtime-machines";
import {
  PanelBody,
  PanelCount,
  PanelHeader,
  PanelLink,
  PanelList,
  PanelSection,
  PANEL_ROW_CLASS,
  isPathWithin,
  panelRowStateClass,
} from "../panel-nav";
import { useTeamAgentStatus } from "./team-status";

const EMPTY_TASKS: AgentTask[] = [];
const MEMBER_AVATAR_LIMIT = 4;
const AGENT_PREVIEW_LIMIT = 5;

function AgentStatusSection({
  label,
  agents,
  collapsed,
  onCollapsedChange,
  countClassName,
  renderAgent,
}: {
  label: string;
  agents: Agent[];
  collapsed: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
  countClassName?: string;
  renderAgent: (agent: Agent) => ReactNode;
}) {
  const { t } = useT("layout");
  const [expanded, setExpanded] = useState(false);
  const listId = useId();
  const displayed = expanded ? agents : agents.slice(0, AGENT_PREVIEW_LIMIT);

  return (
    <PanelSection
      label={label}
      collapsed={collapsed}
      onCollapsedChange={onCollapsedChange}
      action={<PanelCount value={agents.length} className={cn("pr-2", countClassName)} />}
    >
      <PanelList id={listId}>{displayed.map(renderAgent)}</PanelList>
      {agents.length > AGENT_PREVIEW_LIMIT && (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={listId}
          className={cn(PANEL_ROW_CLASS, panelRowStateClass(false), "pl-[2.375rem] text-caption")}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded
            ? t(($) => $.panels.team.show_fewer_agents)
            : t(($) => $.panels.team.show_more_agents, { count: agents.length - AGENT_PREVIEW_LIMIT })}
        </button>
      )}
    </PanelSection>
  );
}

function AgentAvatar({ agent, detail }: { agent: Agent; detail: AgentPresenceDetail | undefined }) {
  return (
    <span className="relative shrink-0">
      <ActorAvatar
        name={agent.name}
        initials={agent.name.charAt(0).toUpperCase()}
        avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
        isAgent
        size="md"
      />
      {detail && (
        <span
          className={cn(
            "absolute -right-0.5 -bottom-0.5 size-2 rounded-full ring-2 ring-page-canvas",
            availabilityConfig[detail.availability].dotClass,
          )}
        />
      )}
    </span>
  );
}

/** "MUL-8317 Redesign the sidebar" for the issue the agent is running, if any. */
function useRunningIssueLabel(wsId: string, task: AgentTask | undefined) {
  const issueId = task?.issue_id ?? "";
  const { data: issue } = useQuery({ ...issueDetailOptions(wsId, issueId), enabled: !!issueId });
  return issue ? `${issue.identifier} ${issue.title}` : null;
}

function WorkingAgentRow({
  agent,
  detail,
  task,
  wsId,
  active,
}: {
  agent: Agent;
  detail: AgentPresenceDetail | undefined;
  task: AgentTask | undefined;
  wsId: string;
  active: boolean;
}) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const issueLabel = useRunningIssueLabel(wsId, task);
  const running = detail?.runningCount ?? 0;
  return (
    <PanelLink
      href={p.agentDetail(agent.id)}
      label={agent.name}
      active={active}
      icon={<AgentAvatar agent={agent} detail={detail} />}
      description={
        running > 1 || !issueLabel ? t(($) => $.panels.team.running_tasks, { count: running }) : issueLabel
      }
    />
  );
}

export function TeamPanel() {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const { pathname, push } = useNavigation();
  const wsId = useCurrentWorkspace()?.id ?? "";
  const userId = useAuthStore((s) => s.user?.id);
  const agentsScope = useAgentsViewStore((s) => s.scope);
  const setAgentsScope = useAgentsViewStore((s) => s.setScope);
  const { agents, stuck, working, byAgent } = useTeamAgentStatus(wsId || undefined);
  const workingCollapsed = useSidebarTreeStore((s) => s.workingCollapsed);
  const setWorkingCollapsed = useSidebarTreeStore((s) => s.setWorkingCollapsed);
  const needsAttentionCollapsed = useSidebarTreeStore((s) => s.needsAttentionCollapsed);
  const setNeedsAttentionCollapsed = useSidebarTreeStore((s) => s.setNeedsAttentionCollapsed);

  const { data: snapshot = EMPTY_TASKS } = useQuery({
    ...agentTaskSnapshotOptions(wsId),
    enabled: !!wsId,
  });
  const runningTaskByAgent = useMemo(() => {
    const map = new Map<string, AgentTask>();
    for (const task of snapshot) {
      if (task.status === "running" && !map.has(task.agent_id)) map.set(task.agent_id, task);
    }
    return map;
  }, [snapshot]);

  const { data: squads = [] } = useQuery({ ...squadListOptions(wsId), enabled: !!wsId });
  const liveSquads = useMemo(() => squads.filter((squad) => !squad.archived_at), [squads]);
  const { data: members = [] } = useQuery({ ...memberListOptions(wsId), enabled: !!wsId });
  const { data: skills = [] } = useQuery({ ...skillListOptions(wsId), enabled: !!wsId });
  const { data: runtimes = [] } = useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });
  const machineCounts = useMemo(() => {
    const machines = buildRuntimeMachines(runtimes, { now: Date.now() }).filter(
      (machine) => !machine.runtimes.every((runtime) => runtime.provider === "qoder_cloud"),
    );
    return runtimeMachineCounts(machines);
  }, [runtimes]);

  const mineCount = userId ? agents.filter((agent) => agent.owner_id === userId).length : 0;
  const myRole = members.find((member) => member.user_id === userId)?.role;
  const canInvite = myRole === "owner" || myRole === "admin";
  const onAgents = pathname === p.agents();
  // Member pages live under /members/:id; the list itself is a Settings tab.
  const membersRoot = p.memberDetail("").replace(/\/$/, "");

  return (
    <>
      <PanelHeader title={t(($) => $.spaces.team)}>
        {canInvite && (
          <IconButton
            label={t(($) => $.panels.team.invite_member)}
            tooltipSide="bottom"
            className="text-muted-foreground"
            onClick={() => push(`${p.settings()}?tab=members&invite=1`)}
          >
            <UserPlus />
          </IconButton>
        )}
        <IconButton
          label={t(($) => $.panels.team.new_agent)}
          tooltipSide="bottom"
          className="text-muted-foreground"
          onClick={() => push(p.newAgent())}
        >
          <Plus />
        </IconButton>
      </PanelHeader>
      <PanelBody>
        {stuck.length > 0 && (
          <AgentStatusSection
            key={`attention:${wsId}`}
            label={t(($) => $.panels.team.needs_attention)}
            agents={stuck}
            collapsed={needsAttentionCollapsed}
            onCollapsedChange={setNeedsAttentionCollapsed}
            countClassName="text-warning"
            renderAgent={(agent) => {
              const href = p.agentDetail(agent.id);
              return (
                <PanelLink
                  key={agent.id}
                  href={href}
                  label={agent.name}
                  active={isPathWithin(pathname, href)}
                  icon={<AgentAvatar agent={agent} detail={byAgent.get(agent.id)} />}
                  description={
                    <span className="text-warning">
                      {t(($) => $.panels.team.stuck, { count: byAgent.get(agent.id)?.queuedCount ?? 0 })}
                    </span>
                  }
                />
              );
            }}
          />
        )}

        {working.length > 0 && (
          <AgentStatusSection
            key={`working:${wsId}`}
            label={t(($) => $.panels.team.working_now)}
            agents={working}
            collapsed={workingCollapsed}
            onCollapsedChange={setWorkingCollapsed}
            renderAgent={(agent) => (
              <WorkingAgentRow
                key={agent.id}
                agent={agent}
                detail={byAgent.get(agent.id)}
                task={runningTaskByAgent.get(agent.id)}
                wsId={wsId}
                active={isPathWithin(pathname, p.agentDetail(agent.id))}
              />
            )}
          />
        )}

        <PanelSection label={t(($) => $.nav.agents)}>
          <PanelList>
            <PanelLink
              href={p.agents()}
              icon={<Bot />}
              label={t(($) => $.panels.team.all_agents)}
              active={onAgents && agentsScope === "all"}
              trailing={<PanelCount value={agents.length} />}
              onNavigate={() => setAgentsScope("all")}
            />
            <PanelLink
              href={p.agents()}
              icon={<User />}
              label={t(($) => $.panels.team.my_agents)}
              active={onAgents && agentsScope === "mine"}
              trailing={<PanelCount value={mineCount} />}
              onNavigate={() => setAgentsScope("mine")}
            />
          </PanelList>
        </PanelSection>

        <PanelSection label={t(($) => $.panels.team.squads_and_members)}>
          <PanelList>
            <PanelLink
              href={p.squads()}
              icon={<Users />}
              label={t(($) => $.nav.squads)}
              active={isPathWithin(pathname, p.squads())}
              trailing={<PanelCount value={liveSquads.length} />}
            />
            <PanelLink
              href={`${p.settings()}?tab=members`}
              label={t(($) => $.panels.team.members, { count: members.length })}
              active={isPathWithin(pathname, membersRoot)}
              icon={
                <span className="flex -space-x-1">
                  {members.slice(0, MEMBER_AVATAR_LIMIT).map((member) => (
                    <ActorAvatar
                      key={member.user_id}
                      name={member.name}
                      initials={member.name.charAt(0).toUpperCase()}
                      avatarUrl={resolvePublicFileUrl(member.avatar_url)}
                      size="xs"
                      className="ring-1 ring-page-canvas"
                    />
                  ))}
                </span>
              }
            />
          </PanelList>
        </PanelSection>

        <PanelSection label={t(($) => $.panels.team.resources)}>
          <PanelList>
            <PanelLink
              href={p.skills()}
              icon={<BookOpenText />}
              label={t(($) => $.nav.skills)}
              active={isPathWithin(pathname, p.skills())}
              trailing={<PanelCount value={skills.length} />}
            />
            <PanelLink
              href={p.runtimes()}
              icon={<Monitor />}
              label={t(($) => $.nav.runtimes)}
              active={isPathWithin(pathname, p.runtimes())}
              trailing={
                machineCounts.all > 0 ? (
                  <span className={cn(
                    "shrink-0 text-caption",
                    machineCounts.online > 0 ? "text-success" : "text-warning",
                  )}>
                    {t(($) => $.panels.team.runtimes_online, { count: machineCounts.online })}
                  </span>
                ) : undefined
              }
            />
            <PanelLink
              href={p.usage()}
              icon={<BarChart3 />}
              label={t(($) => $.nav.usage)}
              active={isPathWithin(pathname, p.usage())}
            />
          </PanelList>
        </PanelSection>
      </PanelBody>
    </>
  );
}
