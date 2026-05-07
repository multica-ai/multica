"use client";

import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, Hash, MessageSquare, Pin } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelDetailOptions, useMarkChannelRead } from "@multica/core/channels";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { useArchiveInbox } from "@multica/core/inbox/mutations";
import type { Channel, ChannelMember, InboxItem, TimelineEntry } from "@multica/core/types";
import { useIssueTimeline } from "../../issues/hooks/use-issue-timeline";
import { CommentCard } from "../../issues/components/comment-card";
import { CommentInput } from "../../issues/components/comment-input";
import { ActorAvatar } from "../../common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { AvatarGroup } from "@multica/ui/components/ui/avatar";
import { useChannelDisplay } from "./use-channel-display";

interface ChannelDetailProps {
  channelId: string;
  /** Initial data from the inbox-list query, so the panel can render before
   *  the per-channel detail query resolves. */
  initialChannel?: Channel | null;
  /** Called after the user archives the channel from the thread header.
   *  Lets the inbox clear its selection. */
  onArchive?: () => void;
}

export function ChannelDetail({ channelId, initialChannel, onArchive }: ChannelDetailProps) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);

  // Channels are issues, so we reuse the issue timeline hook — same comment
  // model, same WS event handlers, same optimistic reaction logic.
  const { timeline, submitComment, submitReply, editComment, deleteComment, toggleReaction } =
    useIssueTimeline(channelId, userId);

  const { data: channelDetail } = useQuery({
    ...channelDetailOptions(wsId, channelId),
    initialData: initialChannel ?? undefined,
  });
  const channel = channelDetail ?? initialChannel ?? null;

  // useChannelDisplay reads the auth/actor stores via hooks, so it must be
  // called unconditionally. The fallback channel keeps it shape-stable.
  const fallbackChannel = useMemo<Channel>(
    () => channel ?? makeFallbackChannel(channelId),
    [channel, channelId],
  );
  const display = useChannelDisplay(fallbackChannel);

  // Inbox query backs the archive button below — auto-mark-read goes through
  // the channel-level endpoint so notifications-routed rows are cleared too.
  const { data: inboxItems = [] } = useQuery(inboxListOptions(wsId));

  // --- Auto-mark-read ----------------------------------------------------
  // When the panel opens, hit POST /api/channels/{id}/read once. The server
  // marks every unread inbox_item for this channel — across both inbox- and
  // notifications-routed rows — so CountUnreadInboxForChannel drops to zero
  // and the badge in the inbox list clears.
  const markChannelRead = useMarkChannelRead();
  const seenChannelRef = useRef<string | null>(null);

  useEffect(() => {
    if (!channelId || !channel || channel.unread_count === 0) return;
    if (seenChannelRef.current === channelId) return;
    seenChannelRef.current = channelId;
    markChannelRead.mutate(channelId);
  }, [channelId, channel, markChannelRead]);

  const grouped = useMemo(() => groupTimeline(timeline), [timeline]);

  const archiveInbox = useArchiveInbox();
  const handleArchive = () => {
    // Channels archive by archiving the inbox row pointing at them. If the
    // user hasn't been notified yet (no inbox row exists) the action is a
    // no-op — the row will get archived next time it lands.
    const item = inboxItems.find(
      (i: InboxItem) => i.issue_id === channelId && !i.archived,
    );
    if (item) archiveInbox.mutate(item.id);
    onArchive?.();
  };

  if (!channel) {
    return (
      <div className="flex flex-1 flex-col p-4 gap-4">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <header className="flex shrink-0 flex-col gap-1 border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <ChannelHeaderIcon channel={channel} />
          <div className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
            {display.isChannel && (
              <Hash className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span className="truncate">{display.title}</span>
          </div>
          {display.otherParticipants.length > 0 && (
            <ParticipantStack participants={display.otherParticipants} />
          )}
          <div className="ml-auto flex items-center gap-1">
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    disabled
                    aria-label="Pin to sidebar"
                    className="inline-flex size-7 items-center justify-center rounded border text-muted-foreground opacity-60"
                  />
                }
              >
                <Pin className="size-3.5" />
              </TooltipTrigger>
              {/* Pin types still need extending — JEH-592. Disabled until
                  pin.item_type accepts 'channel' / 'dm'. */}
              <TooltipContent side="bottom">Pinning channels is coming soon</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    onClick={handleArchive}
                    aria-label="Archive conversation"
                    className="inline-flex size-7 items-center justify-center rounded border text-muted-foreground hover:bg-accent hover:text-foreground"
                  />
                }
              >
                <Archive className="size-3.5" />
              </TooltipTrigger>
              <TooltipContent side="bottom">Archive</TooltipContent>
            </Tooltip>
          </div>
        </div>
        {/* Subtitle slot: prefer description (matches the mockup), fall back
            to a participant count for unnamed channels. DMs have no
            secondary line — the peer name is already the title. */}
        {channel.description ? (
          <p className="truncate pl-10 text-xs text-muted-foreground">
            {channel.description}
          </p>
        ) : display.isChannel && channel.participants.length > 1 ? (
          <p className="truncate pl-10 text-xs text-muted-foreground">
            {participantsLabel(channel)}
          </p>
        ) : null}
      </header>

      <div className="flex-1 min-h-0 overflow-y-auto px-4 py-4 space-y-4">
        {grouped.length === 0 && (
          <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
            <MessageSquare className="mb-3 size-10 text-muted-foreground/30" />
            <p className="text-sm">No messages yet — say hi!</p>
          </div>
        )}
        {grouped.map((entry) => (
          <div key={entry.id} id={`comment-${entry.id}`}>
            <CommentCard
              issueId={channelId}
              entry={entry.entry}
              allReplies={entry.replies}
              currentUserId={userId}
              onReply={submitReply}
              onEdit={editComment}
              onDelete={deleteComment}
              onToggleReaction={toggleReaction}
            />
          </div>
        ))}
      </div>

      <div className="shrink-0 border-t px-4 py-3">
        <CommentInput issueId={channelId} onSubmit={submitComment} />
      </div>
    </div>
  );
}

// Cap the inline avatar stack at MAX participants — anything beyond that
// becomes a "+N" chip so the header stays single-line on narrow panes.
const MAX_AVATARS = 4;

function ParticipantStack({ participants }: { participants: ChannelMember[] }) {
  const visible = participants.slice(0, MAX_AVATARS);
  const overflow = participants.length - visible.length;
  return (
    <div className="ml-1 flex shrink-0 items-center">
      <AvatarGroup className="-space-x-1.5">
        {visible.map((p) => (
          <ActorAvatar
            key={`${p.user_type}:${p.user_id}`}
            actorType={p.user_type}
            actorId={p.user_id}
            size={18}
          />
        ))}
      </AvatarGroup>
      {overflow > 0 && (
        <span className="ml-1 text-xs text-muted-foreground">+{overflow}</span>
      )}
    </div>
  );
}

function ChannelHeaderIcon({ channel }: { channel: Channel }) {
  const display = useChannelDisplay(channel);
  if (display.isChannel) {
    return (
      <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <Hash className="size-4" />
      </span>
    );
  }
  const peer = display.otherParticipants[0];
  if (!peer) {
    return (
      <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <MessageSquare className="size-4" />
      </span>
    );
  }
  return (
    <ActorAvatar actorType={peer.user_type} actorId={peer.user_id} size={32} />
  );
}

function makeFallbackChannel(id: string): Channel {
  return {
    id,
    workspace_id: "",
    number: 0,
    identifier: "",
    kind: "channel",
    title: "",
    description: null,
    status: "todo",
    project_id: null,
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "",
    participants: [],
    unread_count: 0,
    created_at: "",
    updated_at: "",
  };
}

function participantsLabel(channel: Channel): string {
  const count = channel.participants.length;
  if (channel.kind === "dm") return "Direct message";
  if (count <= 1) return "Just you";
  return `${count} participant${count === 1 ? "" : "s"}`;
}

interface GroupedComment {
  id: string;
  entry: TimelineEntry;
  replies: Map<string, TimelineEntry[]>;
}

/**
 * Channels treat comments as messages and don't need the activity coalescing
 * the issue timeline does. We just split top-level comments out, keeping a
 * map of replies for thread expansion in CommentCard.
 */
function groupTimeline(timeline: TimelineEntry[]): GroupedComment[] {
  const repliesByParent = new Map<string, TimelineEntry[]>();
  const topLevel: TimelineEntry[] = [];
  for (const e of timeline) {
    if (e.type !== "comment") continue;
    if (e.parent_id) {
      const list = repliesByParent.get(e.parent_id) ?? [];
      list.push(e);
      repliesByParent.set(e.parent_id, list);
    } else {
      topLevel.push(e);
    }
  }
  return topLevel.map((entry) => ({
    id: entry.id,
    entry,
    replies: repliesByParent,
  }));
}
