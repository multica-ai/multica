"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Archive,
  ArchiveRestore,
  ArrowLeft,
  Hash,
  Lock,
  LogOut,
  MoreHorizontal,
  Plus,
  UserPlus,
  Users,
  X,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Input } from "@multica/ui/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
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
import { ConversationMessageRow, TeamConversation } from "./team-conversation";

export function TeamChannelView({ channelId, threadId }: { channelId: string; threadId: string | null }) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { push } = useNavigation();
  const { data: channel, isLoading } = useQuery(conversationOptions(wsId, channelId));
  const openThread = useOpenConversationThread();
  const [membersOpen, setMembersOpen] = useState(false);

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

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <TeamDetailHeader
        kind="channel"
        id={channel.id}
        tabs={["chat"]}
        activeTab="chat"
        identity={
          <>
            {channel.visibility === "private" ? (
              <Lock className="size-4 shrink-0 text-muted-foreground" />
            ) : (
              <Hash className="size-4 shrink-0 text-muted-foreground" />
            )}
            <h2 className="truncate text-body font-medium">{channel.name}</h2>
            {channel.about && (
              <span className="truncate text-caption text-muted-foreground">{channel.about}</span>
            )}
          </>
        }
        actions={<ChannelActions channel={channel} onOpenMembers={() => setMembersOpen(true)} />}
      />
      {threadId ? (
        <TeamConversationThread
          key={threadId}
          threadId={threadId}
          backHref={channelHref}
          readOnly={archived}
        />
      ) : (
        <TeamConversation
          key={channel.id}
          conversationId={channel.id}
          placeholder={t(($) => $.channel.placeholder, { name: channel.name })}
          empty={t(($) => $.channel.empty, { name: channel.name })}
          readOnly={archived}
          readOnlyNotice={t(($) => $.channel.archived_hint)}
          threadHref={(id) => teamHref(wsPaths.team(), { kind: "channel", id: channel.id, threadId: id })}
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
        />
      )}
      {membersOpen && <ChannelMembersDialog channel={channel} onClose={() => setMembersOpen(false)} />}
    </div>
  );
}

/**
 * A thread under one message of a channel or direct conversation: the message
 * it replies to on top, then the replies.
 */
export function TeamConversationThread({
  threadId,
  backHref,
  readOnly = false,
}: {
  threadId: string;
  backHref: string;
  readOnly?: boolean;
}) {
  const { t } = useT("team");
  const wsId = useWorkspaceId();
  const { data: thread, isLoading } = useQuery(conversationOptions(wsId, threadId));
  if (isLoading) return <TeamDetailSkeleton />;
  if (!thread || thread.kind !== "thread") {
    return (
      <div className="flex flex-1 items-center justify-center px-6 text-center text-body text-muted-foreground">
        {t(($) => $.detail.not_found)}
      </div>
    );
  }
  return (
    <TeamConversation
      conversationId={thread.id}
      placeholder={t(($) => $.conversation.thread_placeholder)}
      empty={t(($) => $.conversation.thread_empty)}
      readOnly={readOnly}
      header={
        <div className="border-b pb-2">
          <div className="flex items-center gap-2 px-4 pt-3 pb-1">
            <Button size="xs" variant="ghost" render={<AppLink href={backHref} />} nativeButton={false}>
              <ArrowLeft className="size-3" />
              {t(($) => $.conversation.back)}
            </Button>
            <span className="text-caption font-medium text-muted-foreground">{t(($) => $.chat.thread.title)}</span>
          </div>
          {thread.root_message && <ConversationMessageRow message={thread.root_message} />}
        </div>
      }
    />
  );
}

function ChannelActions({ channel, onOpenMembers }: { channel: Conversation; onOpenMembers: () => void }) {
  const { t } = useT("team");
  const { t: tCommon } = useT("common");
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const addMembers = useAddConversationMembers();
  const removeMember = useRemoveConversationMember();
  const update = useUpdateConversation();
  const archived = !!channel.archived_at;
  const memberAvatars = channel.members.slice(0, 3);

  return (
    <>
      <Button size="sm" variant="ghost" onClick={onOpenMembers} className="gap-1.5 text-muted-foreground">
        <span className="flex -space-x-1.5">
          {memberAvatars.map((m) => (
            <ActorAvatar key={`${m.member_type}:${m.member_id}`} actorType={m.member_type} actorId={m.member_id} size="xs" />
          ))}
        </span>
        {t(($) => $.channel.members_count, { count: channel.members.length })}
      </Button>
      {!channel.is_member && !archived && (
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
      )}
      {channel.is_member && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={<IconButton label={tCommon(($) => $.more_actions)} tooltipSide="bottom" className="text-muted-foreground" />}
          >
            <MoreHorizontal className="h-4 w-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuItem onClick={onOpenMembers}>
              <UserPlus className="h-4 w-4" />
              {t(($) => $.channel.add_members)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => update.mutate({ id: channel.id, archived: !archived })}>
              {archived ? <ArchiveRestore className="h-4 w-4" /> : <Archive className="h-4 w-4" />}
              {archived ? t(($) => $.channel.unarchive) : t(($) => $.channel.archive)}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() =>
                removeMember.mutate({ id: channel.id, member: { member_type: "member", member_id: userId } })
              }
            >
              <LogOut className="h-4 w-4" />
              {t(($) => $.channel.leave)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </>
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

function MemberRow({ member, action }: { member: ConversationMember; action?: React.ReactNode }) {
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
  return <span className="min-w-0 truncate text-body">{name ?? ""}</span>;
}
