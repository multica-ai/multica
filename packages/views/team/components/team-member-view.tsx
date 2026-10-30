"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Mail } from "@multica/ui/icons";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import {
  conversationListOptions,
  findDirectConversation,
  useOpenConversationThread,
  useOpenDirectConversation,
} from "@multica/core/conversations";
import type { MemberWithUser } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { ActorIssuesPanel } from "../../common/actor-issues-panel";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { TEAM_TABS, teamHref, type TeamTab } from "../selection";
import { TeamDetailHeader } from "./team-detail-header";
import { TeamDetailSkeleton } from "./team-agent-view";
import { TeamNotFound } from "./team-not-found";
import { TeamConversation } from "./team-conversation";
import { TeamConversationThreadPane } from "./team-channel-view";
import { agentStatus, TeamDot, TeamPanel, TeamPanelRow, TeamPanelSection, TeamPill, useTeamCopy } from "./team-ui";

export function TeamMemberView({
  userId,
  tab,
  threadId,
}: {
  userId: string;
  tab: TeamTab;
  threadId: string | null;
}) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const { data: members = [], isLoading } = useQuery(memberListOptions(wsId));
  const member = members.find((m) => m.user_id === userId) ?? null;
  const { copyValue } = useTeamCopy();
  const [panelOpen, setPanelOpen] = useState(true);

  if (!member) return isLoading ? <TeamDetailSkeleton /> : <TeamNotFound />;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TeamDetailHeader
        kind="member"
        id={member.user_id}
        tabs={TEAM_TABS.member}
        activeTab={tab}
        icon={<ActorAvatar actorType="member" actorId={member.user_id} size="md" />}
        name={member.name}
        menu={
          <DropdownMenuItem onClick={() => copyValue(member.email)}>
            <Mail className="size-3.5" />
            {t(($) => $.member.copy_email)}
          </DropdownMenuItem>
        }
        status={
          <TeamPill tone="muted" dot={false}>
            {t(($) => $.member.role[member.role])}
          </TeamPill>
        }
        panelOpen={panelOpen}
        onTogglePanel={() => setPanelOpen((v) => !v)}
      />
      <div className="flex min-h-0 flex-1">
        {tab === "chat" ? (
          <TeamMemberChat member={member} threadId={threadId} panel={panelOpen ? <MemberPanel member={member} /> : null} />
        ) : (
          <>
            <div className="flex min-h-0 min-w-0 flex-1 flex-col">
              <ActorIssuesPanel actorType="member" actorId={member.user_id} />
            </div>
            {panelOpen && <MemberPanel member={member} />}
          </>
        )}
      </div>
    </div>
  );
}

/**
 * The viewer's direct conversation with a member — or their notes to self on
 * their own row. It is created by the first message, so opening a member
 * never leaves an empty conversation behind.
 */
function TeamMemberChat({
  member,
  threadId,
  panel,
}: {
  member: MemberWithUser;
  threadId: string | null;
  panel: ReactNode;
}) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const viewerId = useAuthStore((s) => s.user?.id ?? "");
  const { data: conversations = [], isLoading } = useQuery(conversationListOptions(wsId));
  const openDirect = useOpenDirectConversation();
  const openThread = useOpenConversationThread();
  const userId = member.user_id;
  const name = member.name;
  const direct = findDirectConversation(conversations, viewerId, userId);
  const self = viewerId === userId;
  const memberHref = (thread?: string) => teamHref(wsPaths.team(), { kind: "member", id: userId, threadId: thread });

  if (isLoading) return <TeamDetailSkeleton />;

  return (
    <>
      <TeamConversation
        key={direct?.id ?? "new"}
        conversationId={direct?.id ?? null}
        ensureConversation={async () => (await openDirect.mutateAsync(userId)).id}
        placeholder={self ? t(($) => $.member.notes_placeholder) : t(($) => $.member.dm_placeholder, { name })}
        empty={self ? t(($) => $.member.notes_empty) : t(($) => $.member.dm_empty, { name })}
        threadHref={(id) => memberHref(id)}
        openThreadId={threadId}
        onReplyInThread={(message) =>
          openThread.mutate(
            { conversationId: message.conversation_id, rootMessageId: message.id },
            {
              onSuccess: (thread) => push(memberHref(thread.id)),
              onError: () => toast.error(t(($) => $.chat.thread.open_failed)),
            },
          )
        }
      />
      {threadId ? (
        <TeamConversationThreadPane key={threadId} threadId={threadId} where={name} closeHref={memberHref()} />
      ) : (
        panel
      )}
    </>
  );
}

function MemberPanel({ member }: { member: MemberWithUser }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { byAgent } = useWorkspacePresenceMap(wsId);
  const owned = agents.filter((a) => !a.archived_at && a.owner_id === member.user_id);
  const memberSquads = squads.filter(
    (s) =>
      !s.archived_at &&
      (s.creator_id === member.user_id ||
        s.member_preview?.some((m) => m.member_type === "member" && m.member_id === member.user_id)),
  );

  return (
    <TeamPanel>
      <TeamPanelSection title={t(($) => $.panel.properties)}>
        <dl className="flex flex-col gap-1">
          <TeamPanelRow label={t(($) => $.member.email)}>
            <span className="truncate">{member.email}</span>
          </TeamPanelRow>
          <TeamPanelRow label={t(($) => $.member.role_label)}>
            <span>{t(($) => $.member.role[member.role])}</span>
          </TeamPanelRow>
          <TeamPanelRow label={t(($) => $.member.joined)}>
            <span>{new Date(member.created_at).toLocaleDateString()}</span>
          </TeamPanelRow>
        </dl>
      </TeamPanelSection>
      {owned.length > 0 && (
        <TeamPanelSection title={t(($) => $.member.agents)} trailing={owned.length}>
          <ul className="-mx-2 flex flex-col gap-px">
            {owned.map((agent) => {
              const status = agentStatus(byAgent.get(agent.id), false);
              return (
                <li key={agent.id}>
                  <AppLink
                    href={teamHref(wsPaths.team(), { kind: "agent", id: agent.id })}
                    className="flex h-8 items-center gap-2 rounded-md px-2 text-caption hover:bg-surface-hover"
                  >
                    <ActorAvatar actorType="agent" actorId={agent.id} size="sm" />
                    <span className="min-w-0 flex-1 truncate">{agent.name}</span>
                    <span className="flex shrink-0 items-center gap-1.5 text-micro text-muted-foreground">
                      <TeamDot tone={status.tone} />
                      {t(($) => $.status[status.key])}
                    </span>
                  </AppLink>
                </li>
              );
            })}
          </ul>
        </TeamPanelSection>
      )}
      {memberSquads.length > 0 && (
        <TeamPanelSection title={t(($) => $.member.squads)} trailing={memberSquads.length}>
          <ul className="-mx-2 flex flex-col gap-px">
            {memberSquads.map((squad) => (
              <li key={squad.id}>
                <AppLink
                  href={teamHref(wsPaths.team(), { kind: "squad", id: squad.id })}
                  className="flex h-8 items-center gap-2 rounded-md px-2 text-caption hover:bg-surface-hover"
                >
                  <ActorAvatar actorType="squad" actorId={squad.id} size="sm" />
                  <span className="min-w-0 flex-1 truncate">{squad.name}</span>
                </AppLink>
              </li>
            ))}
          </ul>
        </TeamPanelSection>
      )}
    </TeamPanel>
  );
}
