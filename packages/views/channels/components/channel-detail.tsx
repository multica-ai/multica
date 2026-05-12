// CEREBRO-PATCH(channels-rename-participants): editable channel title + participants side-panel (JEH-700)
"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, Hash, MessageSquare, Pin, PinOff } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  channelDetailOptions,
  useMarkChannelRead,
  useUpdateChannel,
} from "@multica/core/channels";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { useArchiveInbox } from "@multica/core/inbox/mutations";
// CEREBRO-PATCH(channel-detail-archive): JEH-851 — per-user channel archive mutation.
import { useArchiveChannel } from "@multica/cerebro-channels";
import { pinListOptions, useCreatePin, useDeletePin } from "@multica/core/pins";
import type { Channel, ChannelMember, InboxItem, TimelineEntry } from "@multica/core/types";
import { useIssueTimeline } from "../../issues/hooks/use-issue-timeline";
import { CommentInput } from "../../issues/components/comment-input";
import { ActorAvatar } from "../../common/actor-avatar";
// CEREBRO-PATCH(channels-slack-message-view): JEH-1017 — Slack-style message
// view + right slide-in thread panel replace the inline CommentCard stream
// for channels and DMs.
import { SlackMessageView } from "./slack-message-view";
import { ThreadSidePanel } from "./thread-side-panel";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { AvatarGroup } from "@multica/ui/components/ui/avatar";
import { Input } from "@multica/ui/components/ui/input";
import { useChannelDisplay } from "./use-channel-display";
import { ParticipantsPanel } from "./participants-panel";
// CEREBRO-PATCH(channel-detail-listeners): JEH-699 — listeners popover.
import { ChannelListenersPanel } from "./channel-listeners-panel";
// CEREBRO-PATCH(channel-agent-inline-row): JEH-698 inline "agent is working" row mounted between the comment stream and CommentInput.
import { ChannelAgentInlineRow } from "@multica/cerebro-channels";

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

  // CEREBRO-PATCH(channels-slack-message-view): JEH-1017 — produce {topLevel,
  // repliesByParent} for SlackMessageView + ThreadSidePanel.
  const { topLevel, repliesByParent } = useMemo(() => splitTimeline(timeline), [timeline]);
  const [activeThreadId, setActiveThreadId] = useState<string | null>(null);
  const activeThreadEntry = useMemo(
    () => (activeThreadId ? topLevel.find((e) => e.id === activeThreadId) ?? null : null),
    [activeThreadId, topLevel],
  );
  // If the active thread parent disappears from the timeline (deleted), close
  // the panel rather than leaving a dangling header.
  useEffect(() => {
    if (activeThreadId && !activeThreadEntry) setActiveThreadId(null);
  }, [activeThreadId, activeThreadEntry]);

  // CEREBRO-PATCH(channels-slack-message-view): hide the floating agent-chat
  // bubble while the channel/DM is open — the ThreadSidePanel docks where the
  // bubble sits and the two overlap. Mirrors the same pattern in
  // InboxChatPanel for agent chats.
  const setHideFloatingChat = useChatStore((s) => s.setHideFloatingChat);
  const setChatOpen = useChatStore((s) => s.setOpen);
  useEffect(() => {
    setHideFloatingChat(true);
    if (useChatStore.getState().isOpen) setChatOpen(false);
    return () => setHideFloatingChat(false);
  }, [setHideFloatingChat, setChatOpen]);

  // --- Pin to sidebar ----------------------------------------------------
  // Pins are scoped per user; we look up whether this channel/dm is already
  // pinned to flip the button into "unpin" mode.
  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });
  const isPinned = pinnedItems.some(
    (p) => p.item_type === channel?.kind && p.item_id === channelId,
  );
  const createPin = useCreatePin();
  const deletePin = useDeletePin();

  const [participantsOpen, setParticipantsOpen] = useState(false);

  const archiveInbox = useArchiveInbox();
  // CEREBRO-PATCH(channel-detail-archive): JEH-851 — channels/DMs archive
  // via per-user `cerebro_channel_archived` row, then any open inbox
  // notifications for the channel are also archived in the same gesture.
  const archiveChannelMutation = useArchiveChannel();
  const handleArchive = () => {
    archiveChannelMutation.mutate(channelId);
    inboxItems
      .filter((i: InboxItem) => i.issue_id === channelId && !i.archived)
      .forEach((i: InboxItem) => archiveInbox.mutate(i.id));
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
          <ChannelTitle channel={channel} display={display} />
          {display.otherParticipants.length > 0 && (
            <button
              type="button"
              onClick={() => setParticipantsOpen(true)}
              aria-label="Open participants"
              className="rounded p-0.5 hover:bg-accent/60 cursor-pointer"
            >
              <ParticipantStack participants={display.otherParticipants} />
            </button>
          )}
          <div className="ml-auto flex items-center gap-1">
            <ChannelListenersPanel channel={channel} />
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    type="button"
                    onClick={() => {
                      if (isPinned) {
                        deletePin.mutate({ itemType: channel.kind, itemId: channelId });
                      } else {
                        createPin.mutate({ item_type: channel.kind, item_id: channelId });
                      }
                    }}
                    aria-label={isPinned ? "Unpin from sidebar" : "Pin to sidebar"}
                    aria-pressed={isPinned}
                    className={
                      isPinned
                        ? "inline-flex size-7 items-center justify-center rounded border text-foreground hover:bg-accent"
                        : "inline-flex size-7 items-center justify-center rounded border text-muted-foreground hover:bg-accent hover:text-foreground"
                    }
                  />
                }
              >
                {isPinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />}
              </TooltipTrigger>
              <TooltipContent side="bottom">
                {isPinned ? "Unpin from sidebar" : "Pin to sidebar"}
              </TooltipContent>
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

      {/* CEREBRO-PATCH(channels-slack-message-view): JEH-1017 — horizontal
          split so the right-slide-in ThreadSidePanel can dock against the
          message column without floating over it. The panel returns null
          when closed, so the message column reclaims the full width. */}
      <div className="flex flex-1 min-h-0">
        <div className="flex flex-1 min-h-0 flex-col">
          <div className="flex-1 min-h-0 overflow-y-auto">
            {topLevel.length === 0 ? (
              <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
                <MessageSquare className="mb-3 size-10 text-muted-foreground/30" />
                <p className="text-sm">No messages yet — say hi!</p>
              </div>
            ) : (
              <SlackMessageView
                channelId={channelId}
                topLevel={topLevel}
                repliesByParent={repliesByParent}
                currentUserId={userId}
                activeThreadId={activeThreadId}
                onOpenThread={setActiveThreadId}
                onEdit={editComment}
                onDelete={deleteComment}
                onToggleReaction={toggleReaction}
              />
            )}
          </div>

          {/* JEH-698: inline "agent is working" rows sit between the
              message stream and the input — see channels-favorites-cycle-break
              for the avatar-injection rationale. */}
          <div className="shrink-0 px-4 pt-2 empty:hidden">
            {/* CEREBRO-PATCH(channels-favorites-cycle-break): inject ActorAvatar so cerebro-channels stays free of an @multica/views dep (JEH-718). */}
            <ChannelAgentInlineRow channelId={channelId} AvatarComponent={ActorAvatar} />
          </div>
          <div className="shrink-0 border-t px-4 py-3">
            {/* CEREBRO-PATCH(input-autofocus): JEH-756 — channels & DMs are
                chat-like; entering one should land the caret in the input. */}
            <CommentInput issueId={channelId} onSubmit={submitComment} autoFocus />
          </div>
        </div>

        <ThreadSidePanel
          channelId={channelId}
          parentEntry={activeThreadEntry}
          repliesByParent={repliesByParent}
          open={!!activeThreadEntry}
          onClose={() => setActiveThreadId(null)}
          onSubmit={submitReply}
          onEdit={editComment}
          onDelete={deleteComment}
          onToggleReaction={toggleReaction}
          currentUserId={userId}
        />
      </div>

      <ParticipantsPanel
        channel={channel}
        open={participantsOpen}
        onOpenChange={setParticipantsOpen}
      />
    </div>
  );
}

interface ChannelTitleProps {
  channel: Channel;
  display: ReturnType<typeof useChannelDisplay>;
}

// DMs derive their title from the peer name, so they aren't renameable. Only
// kind='channel' surfaces the inline edit affordance.
function ChannelTitle({ channel, display }: ChannelTitleProps) {
  const updateChannel = useUpdateChannel();
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(channel.title);

  useEffect(() => {
    if (!editing) setValue(channel.title);
  }, [channel.title, editing]);

  if (!display.isChannel) {
    return (
      <div className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
        <span className="truncate">{display.title}</span>
      </div>
    );
  }

  const commit = () => {
    const next = value.trim();
    setEditing(false);
    if (!next || next === channel.title) {
      setValue(channel.title);
      return;
    }
    updateChannel.mutate(
      { id: channel.id, title: next },
      {
        onError: (err) => {
          toast.error(
            err instanceof Error ? err.message : "Failed to rename channel",
          );
          setValue(channel.title);
        },
      },
    );
  };

  if (editing) {
    return (
      <div className="flex min-w-0 flex-1 items-center gap-1.5 text-sm font-medium">
        <Hash className="size-3.5 shrink-0 text-muted-foreground" />
        <Input
          autoFocus
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commit();
            } else if (e.key === "Escape") {
              e.preventDefault();
              setValue(channel.title);
              setEditing(false);
            }
          }}
          className="h-7 max-w-[16rem] py-0 text-sm"
          aria-label="Channel name"
        />
      </div>
    );
  }

  return (
    <button
      type="button"
      onClick={() => setEditing(true)}
      className="flex min-w-0 items-center gap-1.5 rounded px-1 py-0.5 text-sm font-medium hover:bg-accent/60 cursor-pointer"
      aria-label="Rename channel"
    >
      <Hash className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="truncate">{display.title}</span>
    </button>
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
    last_message: null,
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

/**
 * Split the timeline into top-level messages and a map of direct replies.
 * Channels treat comments as messages and don't need the activity coalescing
 * the issue timeline does. SlackMessageView walks `repliesByParent` to count
 * descendants; ThreadSidePanel flattens it via `collectThreadReplies`.
 */
function splitTimeline(timeline: TimelineEntry[]): {
  topLevel: TimelineEntry[];
  repliesByParent: Map<string, TimelineEntry[]>;
} {
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
  return { topLevel, repliesByParent };
}
