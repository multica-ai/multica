"use client";

import { useEffect, useMemo, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Hash, MessageSquare } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelDetailOptions, channelKeys } from "@multica/core/channels";
import { inboxKeys, inboxListOptions } from "@multica/core/inbox/queries";
import { useMarkInboxRead } from "@multica/core/inbox/mutations";
import type { Channel, InboxItem, TimelineEntry } from "@multica/core/types";
import { useIssueTimeline } from "../../issues/hooks/use-issue-timeline";
import { CommentCard } from "../../issues/components/comment-card";
import { CommentInput } from "../../issues/components/comment-input";
import { ActorAvatar } from "../../common/actor-avatar";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useChannelDisplay } from "./use-channel-display";

interface ChannelDetailProps {
  channelId: string;
  /** Initial data from the inbox-list query, so the panel can render before
   *  the per-channel detail query resolves. */
  initialChannel?: Channel | null;
}

export function ChannelDetail({ channelId, initialChannel }: ChannelDetailProps) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const qc = useQueryClient();

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

  // --- Auto-mark-read ----------------------------------------------------
  // When the panel opens, mark every unread inbox item for this channel as
  // read. Uses the existing per-item endpoint — no new backend needed —
  // and the channel list refetch picks up the new unread_count.
  const markRead = useMarkInboxRead();
  const seenChannelRef = useRef<string | null>(null);
  const { data: inboxItems = [] } = useQuery(inboxListOptions(wsId));

  useEffect(() => {
    if (!channelId || !channel || channel.unread_count === 0) return;
    if (seenChannelRef.current === channelId) return;
    seenChannelRef.current = channelId;

    const unread = inboxItems.filter(
      (i: InboxItem) => i.issue_id === channelId && !i.read,
    );
    if (unread.length === 0) {
      // Inbox doesn't carry the row yet — invalidate so the badge clears
      // as soon as the next fetch lands.
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      return;
    }
    for (const item of unread) {
      markRead.mutate(item.id);
    }
    // Optimistically clear the badge in cached channel list/detail so the UI
    // updates without waiting for a refetch.
    qc.setQueryData<Channel[]>(channelKeys.list(wsId), (old) =>
      old?.map((c) => (c.id === channelId ? { ...c, unread_count: 0 } : c)),
    );
    qc.setQueryData<Channel>(channelKeys.detail(wsId, channelId), (old) =>
      old ? { ...old, unread_count: 0 } : old,
    );
    qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
  }, [channelId, channel, inboxItems, markRead, qc, wsId]);

  const grouped = useMemo(() => groupTimeline(timeline), [timeline]);

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
      <header className="flex shrink-0 items-center gap-2 border-b px-4 py-2">
        <ChannelHeaderIcon channel={channel} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5 text-sm font-medium">
            {display.isChannel && (
              <Hash className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span className="truncate">{display.title}</span>
          </div>
          <p className="truncate text-xs text-muted-foreground">
            {participantsLabel(channel)}
          </p>
        </div>
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
