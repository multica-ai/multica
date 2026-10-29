"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Plus, Search } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Input } from "@multica/ui/components/ui/input";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import { CappedNumberFlow } from "@multica/ui/components/ui/number-flow";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { useChatStore } from "@multica/core/chat";
import { chatSessionsWithThreadsOptions } from "@multica/core/chat/queries";
import { unreadChatMessagesByAgent } from "@multica/core/chat/primary-session";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import type { Agent, ChatSession } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { teamHref, type TeamSelection } from "../selection";

function lastActivity(sessions: ChatSession[], agentId: string): number {
  let latest = 0;
  for (const s of sessions) {
    if (s.agent_id !== agentId) continue;
    const at = new Date(s.last_message?.created_at ?? s.updated_at).getTime();
    if (at > latest) latest = at;
  }
  return latest;
}

/** Agents the viewer has chatted with come first, newest chat on top. */
export function sortTeamAgents(agents: Agent[], sessions: ChatSession[]): Agent[] {
  const activity = new Map(agents.map((a) => [a.id, lastActivity(sessions, a.id)]));
  return [...agents].sort((a, b) => {
    const diff = (activity.get(b.id) ?? 0) - (activity.get(a.id) ?? 0);
    return diff !== 0 ? diff : a.name.localeCompare(b.name);
  });
}

export function TeamSidebar({ selection }: { selection: TeamSelection }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const user = useAuthStore((s) => s.user);
  const [filter, setFilter] = useState("");
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: sessions = [] } = useQuery(chatSessionsWithThreadsOptions(wsId));
  const activeSessionId = useChatStore((s) => s.activeSessionId);

  // The open agent chat is being read; its unread is about to clear.
  const viewedSessionId = selection.kind === "agent" ? activeSessionId : null;
  const unreadByAgent = useMemo(
    () => unreadChatMessagesByAgent(sessions, viewedSessionId),
    [sessions, viewedSessionId],
  );

  const q = filter.trim().toLowerCase();
  const matches = (name: string) => !q || name.toLowerCase().includes(q);
  const base = wsPaths.team();
  const selectedKey = selection.kind === "none" ? null : `${selection.kind}:${selection.id}`;

  const liveAgents = useMemo(
    () => sortTeamAgents(agents.filter((a) => !a.archived_at), sessions),
    [agents, sessions],
  );
  const archivedAgents = agents.filter((a) => !!a.archived_at);
  const liveSquads = squads.filter((s) => !s.archived_at);
  const sortedMembers = useMemo(
    () =>
      [...members].sort((a, b) => {
        if (a.user_id === user?.id) return -1;
        if (b.user_id === user?.id) return 1;
        return a.name.localeCompare(b.name);
      }),
    [members, user?.id],
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader>
        <h1 className="flex-1 text-body font-semibold">{t(($) => $.page.title)}</h1>
      </PageHeader>
      <div className="px-2 pt-2">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder={t(($) => $.sidebar.filter_placeholder)}
            aria-label={t(($) => $.sidebar.filter_placeholder)}
            className="h-8 pl-8 text-body"
          />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
        <TeamSection
          label={t(($) => $.sidebar.agents)}
          count={liveAgents.length}
          addLabel={t(($) => $.sidebar.add_agent)}
          addHref={wsPaths.newAgent()}
        >
          {liveAgents.filter((a) => matches(a.name)).map((agent) => (
            <TeamRow
              key={agent.id}
              href={teamHref(base, { kind: "agent", id: agent.id })}
              selected={selectedKey === `agent:${agent.id}`}
              avatar={<ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />}
              label={agent.name}
              unread={unreadByAgent.get(agent.id) ?? 0}
            />
          ))}
          {archivedAgents.length > 0 && (
            <Collapsible>
              <CollapsibleTrigger className="group/arch flex h-7 w-full items-center gap-1 rounded-md px-2 text-caption text-muted-foreground hover:bg-surface-hover">
                <ChevronRight className="size-3 transition-transform group-data-[panel-open]/arch:rotate-90" />
                {t(($) => $.sidebar.archived_agents, { count: archivedAgents.length })}
              </CollapsibleTrigger>
              <CollapsibleContent>
                {archivedAgents.filter((a) => matches(a.name)).map((agent) => (
                  <TeamRow
                    key={agent.id}
                    href={teamHref(base, { kind: "agent", id: agent.id })}
                    selected={selectedKey === `agent:${agent.id}`}
                    avatar={<ActorAvatar actorType="agent" actorId={agent.id} size="sm" />}
                    label={agent.name}
                    muted
                  />
                ))}
              </CollapsibleContent>
            </Collapsible>
          )}
        </TeamSection>

        <TeamSection
          label={t(($) => $.sidebar.squads)}
          count={liveSquads.length}
          addLabel={t(($) => $.sidebar.add_squad)}
          onAdd={() => useModalStore.getState().open("create-squad")}
        >
          {liveSquads.filter((s) => matches(s.name)).map((squad) => (
            <TeamRow
              key={squad.id}
              href={teamHref(base, { kind: "squad", id: squad.id })}
              selected={selectedKey === `squad:${squad.id}`}
              avatar={<ActorAvatar actorType="squad" actorId={squad.id} size="sm" />}
              label={squad.name}
            />
          ))}
        </TeamSection>

        <TeamSection
          label={t(($) => $.sidebar.members)}
          count={members.length}
          addLabel={t(($) => $.sidebar.invite_member)}
          addHref={`${wsPaths.settings()}?tab=members`}
        >
          {sortedMembers.filter((m) => matches(m.name)).map((member) => (
            <TeamRow
              key={member.user_id}
              href={teamHref(base, { kind: "member", id: member.user_id })}
              selected={selectedKey === `member:${member.user_id}`}
              avatar={<ActorAvatar actorType="member" actorId={member.user_id} size="sm" />}
              label={member.name}
              trailing={
                member.user_id === user?.id ? (
                  <span className="text-micro text-muted-foreground">{t(($) => $.sidebar.you)}</span>
                ) : null
              }
            />
          ))}
        </TeamSection>
      </div>
    </div>
  );
}

function TeamSection({
  label,
  count,
  addLabel,
  addHref,
  onAdd,
  children,
}: {
  label: string;
  count: number;
  addLabel: string;
  addHref?: string;
  onAdd?: () => void;
  children: ReactNode;
}) {
  const addClass =
    "flex size-6 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-surface-hover hover:text-foreground focus-visible:opacity-100 group-hover/section:opacity-100";
  return (
    <Collapsible defaultOpen className="group/section mb-2">
      <div className="flex h-7 items-center gap-1 pl-2 pr-1">
        <CollapsibleTrigger className="group/trigger flex flex-1 items-center gap-1 text-caption font-medium text-muted-foreground hover:text-foreground">
          <span>{label}</span>
          <ChevronRight className="size-3 transition-transform group-data-[panel-open]/trigger:rotate-90" />
          <span className="ml-auto text-micro font-normal tabular-nums">{count}</span>
        </CollapsibleTrigger>
        {addHref ? (
          <AppLink href={addHref} aria-label={addLabel} title={addLabel} className={addClass}>
            <Plus className="size-3.5" />
          </AppLink>
        ) : (
          <button type="button" aria-label={addLabel} title={addLabel} onClick={onAdd} className={addClass}>
            <Plus className="size-3.5" />
          </button>
        )}
      </div>
      <CollapsibleContent className="flex flex-col gap-px">{children}</CollapsibleContent>
    </Collapsible>
  );
}

function TeamRow({
  href,
  selected,
  avatar,
  label,
  unread = 0,
  trailing,
  muted = false,
}: {
  href: string;
  selected: boolean;
  avatar: ReactNode;
  label: string;
  unread?: number;
  trailing?: ReactNode;
  muted?: boolean;
}) {
  return (
    <AppLink
      href={href}
      aria-current={selected ? "page" : undefined}
      className={cn(
        "flex h-8 items-center gap-2 rounded-md px-2 text-body transition-colors",
        selected
          ? "bg-surface-selected text-surface-selected-foreground hover:bg-surface-selected"
          : "hover:bg-surface-hover",
        muted && !selected && "text-muted-foreground",
        unread > 0 && !selected && "font-medium",
      )}
    >
      {avatar}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {unread > 0 ? (
        <CappedNumberFlow value={unread} animated={false} className="text-caption text-brand" />
      ) : (
        trailing
      )}
    </AppLink>
  );
}
