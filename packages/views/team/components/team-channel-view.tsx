"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Archive,
  ArchiveRestore,
  AtSign,
  Hash,
  Link2,
  Lock,
  LogOut,
  Plus,
  UserPlus,
  Users,
  X,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Input } from "@multica/ui/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { DropdownMenuItem, DropdownMenuSeparator } from "@multica/ui/components/ui/dropdown-menu";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import {
  conversationOptions,
  useAddConversationMembers,
  useOpenConversationThread,
  useRemoveConversationMember,
  useUpdateConversation,
} from "@multica/core/conversations";
import type { Conversation, ConversationMember } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { teamHref } from "../selection";
import { TeamDetailHeader } from "./team-detail-header";
import { TeamDetailSkeleton } from "./team-agent-view";
import { ThreadPaneHeader } from "./team-agent-chat";
import { ConversationMessageRow, TeamConversation } from "./team-conversation";
import { agentStatus, TeamDot, TeamPanel, TeamPanelRow, TeamPanelSection, TeamPill, useTeamCopy } from "./team-ui";

const AGENT_MENTION = "mention://agent/";

export function TeamChannelView({ channelId, threadId }: { channelId: string; threadId: string | null }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const { data: channel, isLoading } = useQuery(conversationOptions(wsId, channelId));
  const openThread = useOpenConversationThread();
  const addMembers = useAddConversationMembers();
  const removeMember = useRemoveConversationMember();
  const update = useUpdateConversation();
  const { copyLink } = useTeamCopy();
  const [membersOpen, setMembersOpen] = useState(false);
  const [panelOpen, setPanelOpen] = useState(true);

  if (isLoading) return <TeamDetailSkeleton />;
  if (!channel || channel.kind !== "channel") {
    return (
      <div className="flex flex-1 items-center justify-center px-6 text-center text-body text-muted-foreground">
        {t(($) => $.channel.not_found)}
      </div>
    );
  }

  const archived = !!channel.archived_at;
  const channelHref = teamHref(wsPaths.team(), { kind: "channel", id: channel.id });
  const hasAgents = channel.members.some((m) => m.member_type === "agent");
  const isPrivate = channel.visibility === "private";

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TeamDetailHeader
        kind="channel"
        id={channel.id}
        tabs={["chat"]}
        activeTab="chat"
        icon={
          isPrivate ? (
            <Lock className="size-4 shrink-0 text-muted-foreground" />
          ) : (
            <Hash className="size-4 shrink-0 text-muted-foreground" />
          )
        }
        name={channel.name}
        menu={
          <>
            {channel.is_member && !archived && (
              <DropdownMenuItem onClick={() => setMembersOpen(true)}>
                <UserPlus className="size-3.5" />
                {t(($) => $.channel.add_members)}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={() => copyLink(channelHref)}>
              <Link2 className="size-3.5" />
              {t(($) => $.detail.copy_link)}
            </DropdownMenuItem>
            {channel.is_member && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={() =>
                    removeMember.mutate({ id: channel.id, member: { member_type: "member", member_id: userId } })
                  }
                >
                  <LogOut className="size-3.5" />
                  {t(($) => $.channel.leave)}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => update.mutate({ id: channel.id, archived: !archived })}>
                  {archived ? <ArchiveRestore className="size-3.5" /> : <Archive className="size-3.5" />}
                  {archived ? t(($) => $.channel.unarchive) : t(($) => $.channel.archive)}
                </DropdownMenuItem>
              </>
            )}
          </>
        }
        status={
          archived ? (
            <TeamPill tone="muted" dot={false}>{t(($) => $.status.archived)}</TeamPill>
          ) : isPrivate ? (
            <TeamPill tone="muted" dot={false}>{t(($) => $.channel.private)}</TeamPill>
          ) : null
        }
        actions={
          !channel.is_member && !archived ? (
            <Button
              size="sm"
              variant="outline"
              disabled={addMembers.isPending}
              onClick={() =>
                addMembers.mutate({ id: channel.id, members: [{ member_type: "member", member_id: userId }] })
              }
            >
              {t(($) => $.channel.join)}
            </Button>
          ) : null
        }
        panelOpen={panelOpen}
        onTogglePanel={() => setPanelOpen((v) => !v)}
      />
      <div className="flex min-h-0 flex-1">
        <TeamConversation
          key={channel.id}
          conversationId={channel.id}
          placeholder={t(($) => $.channel.placeholder, { name: channel.name })}
          empty={t(($) => $.channel.empty, { name: channel.name })}
          readOnly={archived}
          readOnlyNotice={t(($) => $.channel.archived_hint)}
          threadHref={(id) => teamHref(wsPaths.team(), { kind: "channel", id: channel.id, threadId: id })}
          openThreadId={threadId}
          onReplyInThread={(message) =>
            openThread.mutate(
              { conversationId: channel.id, rootMessageId: message.id },
              {
                onSuccess: (thread) =>
                  push(teamHref(wsPaths.team(), { kind: "channel", id: channel.id, threadId: thread.id })),
                onError: () => toast.error(t(($) => $.chat.thread.open_failed)),
              },
            )
          }
          composerHint={
            hasAgents ? (
              <>
                <AtSign className="size-3.5 shrink-0" />
                {t(($) => $.channel.mention_hint)}
              </>
            ) : null
          }
          privateHint={(content) =>
            hasAgents && !content.includes(AGENT_MENTION) ? t(($) => $.channel.no_mention_note) : null
          }
        />
        {threadId ? (
          <TeamConversationThreadPane
            key={threadId}
            threadId={threadId}
            where={`#${channel.name}`}
            closeHref={channelHref}
            readOnly={archived}
          />
        ) : (
          panelOpen && <ChannelPanel channel={channel} onAddMembers={() => setMembersOpen(true)} />
        )}
      </div>
      {membersOpen && <ChannelMembersDialog channel={channel} onClose={() => setMembersOpen(false)} />}
    </div>
  );
}

/**
 * A thread under one message of a channel or direct conversation, beside the
 * conversation: the message it replies to on top, then the replies.
 */
export function TeamConversationThreadPane({
  threadId,
  where,
  closeHref,
  readOnly = false,
}: {
  threadId: string;
  where: string;
  closeHref: string;
  readOnly?: boolean;
}) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const { data: thread, isLoading } = useQuery(conversationOptions(wsId, threadId));
  const replies = thread?.last_message_seq ?? 0;
  let body: ReactNode;
  if (isLoading) {
    body = null;
  } else if (!thread || thread.kind !== "thread") {
    body = (
      <div className="flex flex-1 items-center justify-center px-6 text-center text-body text-muted-foreground">
        {t(($) => $.detail.not_found)}
      </div>
    );
  } else {
    body = (
      <TeamConversation
        conversationId={thread.id}
        placeholder={t(($) => $.conversation.thread_placeholder)}
        empty={t(($) => $.conversation.thread_empty)}
        readOnly={readOnly}
        header={
          thread.root_message && (
            <div className="border-b px-5 pb-2 pt-1 @2xl:px-8">
              <ConversationMessageRow message={thread.root_message} />
              {replies > 0 && (
                <div className="flex items-center gap-3 pt-2 text-micro text-muted-foreground">
                  {t(($) => $.chat.thread.replies, { count: replies })}
                  <span className="h-px flex-1 bg-border" />
                </div>
              )}
            </div>
          )
        }
      />
    );
  }
  return (
    <aside className="flex w-[400px] shrink-0 flex-col border-l @container">
      <ThreadPaneHeader where={where} closeHref={closeHref} />
      {body}
    </aside>
  );
}

function ChannelPanel({ channel, onAddMembers }: { channel: Conversation; onAddMembers: () => void }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { byAgent } = useWorkspacePresenceMap(wsId);
  const people = channel.members.filter((m) => m.member_type === "member");
  const agents = channel.members.filter((m) => m.member_type === "agent");
  const canEdit = channel.is_member && !channel.archived_at;

  return (
    <TeamPanel>
      <TeamPanelSection title={t(($) => $.channel.about)}>
        <p className="text-caption text-muted-foreground">{channel.about || t(($) => $.channel.no_about)}</p>
      </TeamPanelSection>
      <TeamPanelSection title={t(($) => $.channel.people)} trailing={people.length}>
        <ul className="-mx-2 flex flex-col gap-px">
          {people.map((m) => (
            <li key={m.member_id}>
              <AppLink
                href={teamHref(wsPaths.team(), { kind: "member", id: m.member_id })}
                className="flex h-8 items-center gap-2 rounded-md px-2 text-caption hover:bg-surface-hover"
              >
                <ActorAvatar actorType="member" actorId={m.member_id} size="sm" />
                <MemberName member={m} />
              </AppLink>
            </li>
          ))}
        </ul>
      </TeamPanelSection>
      {agents.length > 0 && (
        <TeamPanelSection title={t(($) => $.channel.agents)} trailing={agents.length}>
          <ul className="-mx-2 flex flex-col gap-px">
            {agents.map((m) => {
              const status = agentStatus(byAgent.get(m.member_id), false);
              return (
                <li key={m.member_id}>
                  <AppLink
                    href={teamHref(wsPaths.team(), { kind: "agent", id: m.member_id })}
                    className="flex h-8 items-center gap-2 rounded-md px-2 text-caption hover:bg-surface-hover"
                  >
                    <ActorAvatar actorType="agent" actorId={m.member_id} size="sm" />
                    <MemberName member={m} />
                    <span className="flex-1" />
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
      {canEdit && (
        <button
          type="button"
          onClick={onAddMembers}
          className="-mx-2 -mt-3 flex h-8 items-center gap-2 rounded-md px-2 text-caption text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
        >
          <Plus className="size-3.5" />
          {t(($) => $.channel.add_members)}
        </button>
      )}
      <TeamPanelSection title={t(($) => $.channel.settings)}>
        <dl className="flex flex-col gap-1">
          <TeamPanelRow label={t(($) => $.channel.visibility)}>
            {channel.visibility === "private" ? (
              <Lock className="size-3.5 shrink-0 text-muted-foreground" />
            ) : (
              <Hash className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span>
              {channel.visibility === "private"
                ? t(($) => $.channel.private)
                : t(($) => $.channel.public)}
            </span>
          </TeamPanelRow>
        </dl>
      </TeamPanelSection>
    </TeamPanel>
  );
}

function ChannelMembersDialog({ channel, onClose }: { channel: Conversation; onClose: () => void }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: people = [] } = useQuery(memberListOptions(wsId));
  const addMembers = useAddConversationMembers();
  const removeMember = useRemoveConversationMember();
  const [filter, setFilter] = useState("");
  const canEdit = channel.is_member && !channel.archived_at;

  const inChannel = useMemo(
    () => new Set(channel.members.map((m) => `${m.member_type}:${m.member_id}`)),
    [channel.members],
  );
  const q = filter.trim().toLowerCase();
  const candidates: (ConversationMember & { name: string })[] = useMemo(
    () => [
      ...agents
        .filter((a) => !a.archived_at)
        .map((a) => ({ member_type: "agent" as const, member_id: a.id, name: a.name })),
      ...people.map((p) => ({ member_type: "member" as const, member_id: p.user_id, name: p.name })),
    ].filter((c) => !inChannel.has(`${c.member_type}:${c.member_id}`) && (!q || c.name.toLowerCase().includes(q))),
    [agents, people, inChannel, q],
  );

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-md gap-0 p-0">
        <DialogHeader className="border-b px-5 py-3">
          <DialogTitle className="flex items-center gap-2 text-body font-semibold">
            <Users className="size-4 text-muted-foreground" />
            {t(($) => $.channel.members_title, { name: channel.name })}
          </DialogTitle>
        </DialogHeader>
        <div className="flex max-h-[60vh] flex-col gap-3 overflow-y-auto px-5 py-3">
          <p className="text-caption text-muted-foreground">{t(($) => $.channel.agents_hint)}</p>
          <div className="flex flex-col gap-px">
            {channel.members.map((m) => (
              <MemberRow
                key={`${m.member_type}:${m.member_id}`}
                member={m}
                action={
                  canEdit && (
                    <IconButton
                      label={t(($) => $.channel.remove)}
                      className="text-muted-foreground"
                      onClick={() => removeMember.mutate({ id: channel.id, member: m })}
                    >
                      <X className="size-3.5" />
                    </IconButton>
                  )
                }
              />
            ))}
          </div>
          {canEdit && (
            <div className="flex flex-col gap-2 border-t pt-3">
              <p className="text-caption font-medium">{t(($) => $.channel.add_members)}</p>
              <Input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={t(($) => $.sidebar.filter_placeholder)}
                aria-label={t(($) => $.sidebar.filter_placeholder)}
                className="h-8"
              />
              <div className="flex flex-col gap-px">
                {candidates.map((c) => (
                  <MemberRow
                    key={`${c.member_type}:${c.member_id}`}
                    member={c}
                    action={
                      <IconButton
                        label={t(($) => $.channel.add_members)}
                        className="text-muted-foreground"
                        disabled={addMembers.isPending}
                        onClick={() =>
                          addMembers.mutate({
                            id: channel.id,
                            members: [{ member_type: c.member_type, member_id: c.member_id }],
                          })
                        }
                      >
                        <Plus className="size-3.5" />
                      </IconButton>
                    }
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function MemberRow({ member, action }: { member: ConversationMember; action?: ReactNode }) {
  const { t } = useT("team");
  return (
    <div className="flex h-9 items-center gap-2 rounded-md px-2 hover:bg-surface-hover">
      <ActorAvatar actorType={member.member_type} actorId={member.member_id} size="sm" showStatusDot={member.member_type === "agent"} />
      <MemberName member={member} />
      {member.member_type === "agent" && (
        <span className="rounded bg-muted px-1 text-micro text-muted-foreground">{t(($) => $.conversation.agent_badge)}</span>
      )}
      <span className="flex-1" />
      {action}
    </div>
  );
}

function MemberName({ member }: { member: ConversationMember }) {
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: people = [] } = useQuery(memberListOptions(wsId));
  const name =
    member.member_type === "agent"
      ? agents.find((a) => a.id === member.member_id)?.name
      : people.find((p) => p.user_id === member.member_id)?.name;
  return <span className="min-w-0 truncate">{name ?? ""}</span>;
}
