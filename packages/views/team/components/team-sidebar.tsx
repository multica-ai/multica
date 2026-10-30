"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  Bot,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Hash,
  Lock,
  Mail,
  Plus,
  Search,
  SquarePen,
  UserPlus,
  Users,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { useChatStore } from "@multica/core/chat";
import { chatSessionsWithThreadsOptions } from "@multica/core/chat/queries";
import { unreadChatMessagesByAgent } from "@multica/core/chat/primary-session";
import { useFeatureEnabled } from "@multica/core/config";
import { TEAM_PAGE_FLAG } from "@multica/core/feature-flags";
import { conversationListOptions, directCounterpart } from "@multica/core/conversations";
import {
  agentListOptions,
  invitationListOptions,
  memberListOptions,
  squadListOptions,
  squadMemberStatusOptions,
} from "@multica/core/workspace/queries";
import type { Agent, AgentTask, ChatSession, Conversation, Squad } from "@multica/core/types";
import { AppLink, useNavigation } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { useSearchStore } from "../../search/search-store";
import { useT } from "../../i18n";
import { teamHref, type TeamSelection } from "../selection";
import { TeamCreateChannelDialog } from "./team-create-channel-dialog";
import { TeamDot, useRunningTasksByAgent, useTaskIssue } from "./team-ui";

const EMPTY_CONVERSATIONS: Conversation[] = [];
// Agents shown before the rest fold under "N more"; unread and selected rows always show.
const AGENT_LIMIT = 6;

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
  const { push } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const myRole = members.find((m) => m.user_id === user?.id)?.role;
  const canInvite = myRole === "owner" || myRole === "admin";
  const { data: invitations = [] } = useQuery({ ...invitationListOptions(wsId), enabled: canInvite });
  const { data: sessions = [] } = useQuery(chatSessionsWithThreadsOptions(wsId));
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const teamPageEnabled = useFeatureEnabled(TEAM_PAGE_FLAG, false);
  const { data: conversations = EMPTY_CONVERSATIONS } = useQuery({
    ...conversationListOptions(wsId),
    enabled: teamPageEnabled,
  });
  const { byAgent: presence } = useWorkspacePresenceMap(wsId);
  const running = useRunningTasksByAgent(wsId);
  const [creatingChannel, setCreatingChannel] = useState(false);
  const [showAllAgents, setShowAllAgents] = useState(false);

  const channels = useMemo(
    () => conversations.filter((c) => c.kind === "channel" && !c.archived_at && (c.is_member || c.visibility === "public")),
    [conversations],
  );
  // Unread in the viewer's direct conversation with each member.
  const unreadByMember = useMemo(() => {
    const out = new Map<string, number>();
    for (const c of conversations) {
      if (c.kind !== "direct" || c.unread_count === 0 || !user) continue;
      const other = directCounterpart(c, user.id);
      if (other) out.set(other, c.unread_count);
    }
    return out;
  }, [conversations, user]);

  // The open agent chat is being read; its unread is about to clear.
  const viewedSessionId = selection.kind === "agent" ? activeSessionId : null;
  const unreadByAgent = useMemo(
    () => unreadChatMessagesByAgent(sessions, viewedSessionId),
    [sessions, viewedSessionId],
  );

  const base = wsPaths.team();
  const selectedKey = selection.kind === "none" ? null : `${selection.kind}:${selection.id}`;

  const liveAgents = useMemo(
    () => sortTeamAgents(agents.filter((a) => !a.archived_at), sessions),
    [agents, sessions],
  );
  const shownAgents = showAllAgents
    ? liveAgents
    : liveAgents.filter(
        (a, i) => i < AGENT_LIMIT || selectedKey === `agent:${a.id}` || (unreadByAgent.get(a.id) ?? 0) > 0,
      );
  const hiddenAgents = liveAgents.length - shownAgents.length;
  const archivedAgents = agents.filter((a) => !!a.archived_at);
  const liveSquads = squads.filter((s) => !s.archived_at);
  const pendingInvites = invitations.filter((i) => i.status === "pending");
  const sortedMembers = useMemo(
    () =>
      [...members].sort((a, b) => {
        if (a.user_id === user?.id) return -1;
        if (b.user_id === user?.id) return 1;
        return a.name.localeCompare(b.name);
      }),
    [members, user?.id],
  );

  const newChannel = () => setCreatingChannel(true);
  const newAgent = () => push(wsPaths.newAgent());
  const newSquad = () => useModalStore.getState().open("create-squad");
  const invite = () => push(`${wsPaths.settings()}?tab=members&invite=1`);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="gap-1 pr-2">
        <h1 className="flex-1 text-body font-semibold">{t(($) => $.page.title)}</h1>
        <IconButton
          label={t(($) => $.sidebar.jump_to)}
          tooltipSide="bottom"
          className="text-muted-foreground"
          onClick={() => useSearchStore.getState().setOpen(true)}
        >
          <Search />
        </IconButton>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={<IconButton label={t(($) => $.sidebar.new)} tooltipSide="bottom" className="text-muted-foreground" />}
          >
            <SquarePen />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-52">
            {teamPageEnabled && (
              <DropdownMenuItem onClick={newChannel}>
                <Hash className="size-4" />
                {t(($) => $.sidebar.add_channel)}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={newAgent}>
              <Bot className="size-4" />
              {t(($) => $.sidebar.add_agent)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={newSquad}>
              <Users className="size-4" />
              {t(($) => $.sidebar.add_squad)}
            </DropdownMenuItem>
            {canInvite && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={invite}>
                  <UserPlus className="size-4" />
                  {t(($) => $.sidebar.invite_member)}
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
        {teamPageEnabled && (
          <TeamSection
            label={t(($) => $.sidebar.channels)}
            count={channels.length}
            addLabel={t(($) => $.sidebar.add_channel)}
            onAdd={newChannel}
            empty={channels.length === 0}
          >
            {channels.map((channel) => (
              <TeamRow
                key={channel.id}
                href={teamHref(base, { kind: "channel", id: channel.id })}
                selected={selectedKey === `channel:${channel.id}`}
                avatar={
                  <span className="flex size-5 shrink-0 items-center justify-center text-muted-foreground">
                    {channel.visibility === "private" ? <Lock className="size-3.5" /> : <Hash className="size-3.5" />}
                  </span>
                }
                label={channel.name}
                unread={selectedKey === `channel:${channel.id}` ? 0 : channel.unread_count}
                muted={!channel.is_member}
              />
            ))}
          </TeamSection>
        )}

        <TeamSection
          label={t(($) => $.sidebar.agents)}
          count={liveAgents.length}
          addLabel={t(($) => $.sidebar.add_agent)}
          onAdd={newAgent}
          empty={liveAgents.length === 0}
        >
          {shownAgents.map((agent) => {
            const detail = presence.get(agent.id);
            return (
              <TeamRow
                key={agent.id}
                href={teamHref(base, { kind: "agent", id: agent.id })}
                selected={selectedKey === `agent:${agent.id}`}
                avatar={<ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />}
                label={agent.name}
                unread={unreadByAgent.get(agent.id) ?? 0}
                muted={detail?.availability === "offline"}
                trailing={
                  detail?.workload === "queued" ? (
                    <AlertTriangle className="size-3.5 shrink-0 text-warning" aria-label={t(($) => $.status.waiting)} />
                  ) : (
                    <RunningIssueTag wsId={wsId} task={running.get(agent.id)?.[0]} />
                  )
                }
              />
            );
          })}
          {(hiddenAgents > 0 || showAllAgents) && liveAgents.length > AGENT_LIMIT && (
            <button
              type="button"
              onClick={() => setShowAllAgents((v) => !v)}
              className="flex h-8 items-center gap-2 rounded-md px-2 text-body text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
            >
              <span className="flex size-5 items-center justify-center">
                {showAllAgents ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}
              </span>
              {showAllAgents ? t(($) => $.sidebar.show_less) : t(($) => $.sidebar.more, { count: hiddenAgents })}
            </button>
          )}
          {archivedAgents.length > 0 && (
            <Collapsible>
              <CollapsibleTrigger className="group/arch flex h-7 w-full items-center gap-1 rounded-md px-2 text-caption text-muted-foreground hover:bg-surface-hover">
                <ChevronRight className="size-3 transition-transform group-data-[panel-open]/arch:rotate-90" />
                {t(($) => $.sidebar.archived_agents, { count: archivedAgents.length })}
              </CollapsibleTrigger>
              <CollapsibleContent>
                {archivedAgents.map((agent) => (
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
          onAdd={newSquad}
          empty={liveSquads.length === 0}
        >
          {liveSquads.map((squad) => (
            <TeamRow
              key={squad.id}
              href={teamHref(base, { kind: "squad", id: squad.id })}
              selected={selectedKey === `squad:${squad.id}`}
              avatar={<ActorAvatar actorType="squad" actorId={squad.id} size="sm" />}
              label={squad.name}
              trailing={<SquadWorkingTag wsId={wsId} squad={squad} />}
            />
          ))}
        </TeamSection>

        <TeamSection
          label={t(($) => $.sidebar.members)}
          count={members.length}
          addLabel={t(($) => $.sidebar.invite_member)}
          onAdd={canInvite ? invite : undefined}
        >
          {sortedMembers.map((member) => (
            <TeamRow
              key={member.user_id}
              href={teamHref(base, { kind: "member", id: member.user_id })}
              selected={selectedKey === `member:${member.user_id}`}
              avatar={<ActorAvatar actorType="member" actorId={member.user_id} size="sm" />}
              label={member.name}
              unread={selectedKey === `member:${member.user_id}` ? 0 : (unreadByMember.get(member.user_id) ?? 0)}
              trailing={
                member.user_id === user?.id ? <TrailingText>{t(($) => $.sidebar.you)}</TrailingText> : null
              }
            />
          ))}
          {pendingInvites.map((inv) => (
            <AppLink
              key={inv.id}
              href={`${wsPaths.settings()}?tab=members`}
              className="flex h-8 items-center gap-2 rounded-md px-2 text-body text-muted-foreground transition-colors hover:bg-surface-hover"
            >
              <span className="flex size-5 shrink-0 items-center justify-center">
                <Mail className="size-3.5" />
              </span>
              <span className="min-w-0 flex-1 truncate">{inv.invitee_email}</span>
              <TrailingText>{t(($) => $.sidebar.invited)}</TrailingText>
            </AppLink>
          ))}
        </TeamSection>
      </div>
      {creatingChannel && <TeamCreateChannelDialog onClose={() => setCreatingChannel(false)} />}
    </div>
  );
}

function TrailingText({ children }: { children: ReactNode }) {
  return <span className="shrink-0 text-micro text-muted-foreground">{children}</span>;
}

/** The issue an agent is running, by identifier, next to its name. */
function RunningIssueTag({ wsId, task }: { wsId: string; task: AgentTask | undefined }) {
  const issue = useTaskIssue(wsId, task);
  if (!task) return null;
  return (
    <span className="flex shrink-0 items-center gap-1 text-micro tabular-nums text-muted-foreground">
      <TeamDot tone="brand" />
      {issue?.identifier}
    </span>
  );
}

/** How many of a squad's agents are working right now. */
function SquadWorkingTag({ wsId, squad }: { wsId: string; squad: Squad }) {
  const { data } = useQuery(squadMemberStatusOptions(wsId, squad.id));
  const working = data?.members.filter((m) => m.status === "working").length ?? 0;
  if (working === 0) return null;
  return (
    <span className="flex shrink-0 items-center gap-1 text-micro tabular-nums text-muted-foreground">
      <TeamDot tone="brand" />
      {working}
    </span>
  );
}

function TeamSection({
  label,
  count,
  addLabel,
  onAdd,
  empty = false,
  children,
}: {
  label: string;
  count: number;
  addLabel: string;
  onAdd?: () => void;
  /** An empty section offers its add action as a row. */
  empty?: boolean;
  children: ReactNode;
}) {
  return (
    <Collapsible defaultOpen className="group/section mb-3">
      <div className="flex h-7 items-center gap-1 pl-2 pr-1">
        <CollapsibleTrigger className="group/trigger flex flex-1 items-center gap-1 text-caption font-medium text-muted-foreground hover:text-foreground">
          <span>{label}</span>
          <ChevronDown className="size-3 -rotate-90 transition-transform group-data-[panel-open]/trigger:rotate-0" />
          <span className="ml-auto text-micro font-normal tabular-nums">{count}</span>
        </CollapsibleTrigger>
        {onAdd && (
          <button
            type="button"
            aria-label={addLabel}
            title={addLabel}
            onClick={onAdd}
            className="flex size-6 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-surface-hover hover:text-foreground focus-visible:opacity-100 group-hover/section:opacity-100"
          >
            <Plus className="size-3.5" />
          </button>
        )}
      </div>
      <CollapsibleContent className="flex flex-col gap-px">
        {children}
        {empty && onAdd && (
          <button
            type="button"
            onClick={onAdd}
            className="flex h-8 items-center gap-2 rounded-md px-2 text-body text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
          >
            <span className="flex size-5 items-center justify-center">
              <Plus className="size-3.5" />
            </span>
            {addLabel}
          </button>
        )}
      </CollapsibleContent>
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
  const hasUnread = unread > 0 && !selected;
  return (
    <AppLink
      href={href}
      aria-current={selected ? "page" : undefined}
      className={cn(
        "flex h-8 items-center gap-2 rounded-md px-2 text-body transition-colors",
        selected
          ? "bg-surface-selected text-surface-selected-foreground hover:bg-surface-selected"
          : "hover:bg-surface-hover",
        muted && !selected && !hasUnread && "text-muted-foreground",
        hasUnread && "font-semibold",
      )}
    >
      {avatar}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {hasUnread ? (
        <span className="flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-brand px-1 text-micro font-medium tabular-nums text-brand-foreground">
          {unread > 99 ? "99+" : unread}
        </span>
      ) : (
        trailing
      )}
    </AppLink>
  );
}
