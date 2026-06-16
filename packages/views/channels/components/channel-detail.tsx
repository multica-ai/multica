// CEREBRO-PATCH(channels-rename-participants): editable channel title + participants side-panel (JEH-700)
"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, Hash, MessageSquare, Pencil, Pin, PinOff } from "lucide-react"; // CEREBRO-PATCH(channel-detail-edit-icon): FIR-2660 edit-channel button.
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  channelDetailOptions,
  useUpdateChannel,
} from "@multica/core/channels";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { useArchiveInbox } from "@multica/core/inbox/mutations";
// CEREBRO-PATCH(channel-detail-archive): JEH-851 — per-user channel archive mutation.
// CEREBRO-PATCH(channel-auto-mark-read-hook): TECH-3352 — open-time read hook.
import { useArchiveChannel, useChannelAutoMarkRead } from "@multica/cerebro-channels";
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
import { cn } from "@multica/ui/lib/utils";
// CEREBRO-PATCH(channels-thread-fullscreen-mobile): JEH-1045 — detect narrow
// viewport so the ThreadSidePanel can take over the full pane and the message
// column collapses out of the way.
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useChannelDisplay } from "./use-channel-display";
import { ParticipantsPanel } from "./participants-panel";
// CEREBRO-PATCH(channel-detail-edit-dialog-import): FIR-2660 edit-channel modal (name + description in one round-trip).
import { EditChannelDialog } from "./edit-channel-dialog";
// CEREBRO-PATCH(channel-detail-listeners): JEH-699 — listeners popover.
import { ChannelListenersPanel } from "./channel-listeners-panel";
// CEREBRO-PATCH(channel-agent-inline-row): JEH-698 inline "agent is working" row mounted between the comment stream and CommentInput.
import { ChannelAgentInlineRow } from "@multica/cerebro-channels";
// CEREBRO-PATCH(channel-typing): TECH-3664 — "X is typing…" line + typing ping.
import { ChannelTypingIndicator } from "@multica/cerebro-channels";
import { useChannelTyping } from "@multica/core/presence";
// CEREBRO-PATCH(channel-cost-chip-import): FIR-39 channel-wide total cost chip in the header.
import { ChannelCostChip } from "@multica/cerebro-channels";
// CEREBRO-PATCH(channels-scroll-to-bottom): FIR-2522 — DM/Channel åbner i bunden + sticky-bottom + "Ny besked"-pille.
import { useStickyBottom } from "@multica/cerebro-ui/hooks/use-sticky-bottom";
import { JumpToLatestButton } from "@multica/cerebro-ui/components/jump-to-latest-button";

interface ChannelDetailProps {
  channelId: string;
  /** Initial data from the inbox-list query, so the panel can render before
   *  the per-channel detail query resolves. */
  initialChannel?: Channel | null;
  /** Called after the user archives the channel from the thread header.
   *  Lets the inbox clear its selection. */
  onArchive?: () => void;
  // CEREBRO-PATCH(channel-thread-inbox-autoopen): TECH-3004 — comment ID from
  // the inbox notification that triggered this channel open. On first load we
  // find this comment in the timeline and auto-open its parent thread so the
  // user lands directly on the relevant reply instead of having to scroll/guess.
  initialCommentId?: string;
}

export function ChannelDetail({ channelId, initialChannel, onArchive, initialCommentId }: ChannelDetailProps) {
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
  // Opening a channel/DM clears its unread state. The hook marks read once on
  // open (explicit intent, no focus check — fixes TECH-3352 where the inbox row
  // stayed unread when focus flickered) and keeps the TECH-3300 focus guard only
  // for new messages that arrive while the conversation is already open.
  // CEREBRO-PATCH(channel-auto-mark-read-hook): TECH-3352 — see cerebro-channels.
  useChannelAutoMarkRead(channelId, channel?.unread_count);

  // CEREBRO-PATCH(channel-typing): TECH-3664 — live "is typing…" for this channel/DM.
  const { typingUserIds, notifyTyping } = useChannelTyping(channelId);

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

  // CEREBRO-PATCH(channel-thread-inbox-autoopen): TECH-3004 — when the channel
  // is opened from an inbox notification, find the notified comment in the
  // timeline and auto-open its parent thread. A ref prevents re-triggering
  // after the user manually closes/changes the thread.
  const autoOpenedCommentRef = useRef<string | null>(null);
  useEffect(() => {
    if (!initialCommentId || autoOpenedCommentRef.current === initialCommentId) return;
    if (!timeline.length) return;
    const entry = timeline.find((e) => e.id === initialCommentId && e.type === "comment");
    if (!entry) return;
    autoOpenedCommentRef.current = initialCommentId;
    if (entry.parent_id) setActiveThreadId(entry.parent_id);
  }, [initialCommentId, timeline]);

  // CEREBRO-PATCH(channel-thread-responsive-overlay): TECH-3004 — observe the
  // component's own width so the thread overlays on any narrow container (e.g.
  // when the channel is embedded in the inbox split view at medium viewport
  // sizes), not only on mobile-width viewports.
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState<number>(Infinity);
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    // CEREBRO-PATCH(channel-thread-responsive-overlay): TECH-3009 fix — defer
    // setState via rAF so it never fires during react-resizable-panels' commit,
    // which caused React error #310 in the inbox split view.
    let raf = 0;
    const obs = new ResizeObserver((entries) => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() =>
        setContainerWidth(entries[0]?.contentRect.width ?? Infinity),
      );
    });
    obs.observe(el);
    return () => { obs.disconnect(); cancelAnimationFrame(raf); };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [!!channel]); // re-run when channel data loads (containerRef div only mounts after channel resolves)

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
  const [editOpen, setEditOpen] = useState(false); // CEREBRO-PATCH(channel-detail-edit-state): FIR-2660 edit-channel dialog state.

  // CEREBRO-PATCH(channels-scroll-to-bottom): FIR-2522 — pin DM/Channel
  // scroll to bottom on open; surface "Ny besked"-pille when scrolled up.
  const scrollRef = useRef<HTMLDivElement>(null);
  const { hasNewBelow, scrollToBottom } = useStickyBottom(scrollRef);
  // CEREBRO-PATCH(dm-channel-message-fixes): TECH-3316 — keep latest channel messages visible on open and after sending.
  const openedChannelRef = useRef<string | null>(null);
  const lastOwnMessageRef = useRef<string | null>(null);
  const latestTopLevel = topLevel[topLevel.length - 1] ?? null;
  const latestOwnTopLevelId =
    latestTopLevel?.actor_type === "member" && latestTopLevel.actor_id === userId
      ? latestTopLevel.id
      : null;
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el || topLevel.length === 0) return;
    if (openedChannelRef.current !== channelId) {
      el.scrollTop = el.scrollHeight;
      openedChannelRef.current = channelId;
    }
  }, [channelId, topLevel.length]);
  useLayoutEffect(() => {
    if (!latestOwnTopLevelId || lastOwnMessageRef.current === latestOwnTopLevelId) return;
    lastOwnMessageRef.current = latestOwnTopLevelId;
    scrollToBottom(false);
  }, [latestOwnTopLevelId, scrollToBottom]);

  // CEREBRO-PATCH(channels-thread-fullscreen-mobile): JEH-1045 — on a narrow
  // viewport the side-by-side message + thread layout squeezes both columns
  // unreadably (see screenshot on the issue). When a thread is open and the
  // viewport is mobile-wide, hide the message column and let the thread panel
  // render full-screen with a Slack-style back-header.
  // CEREBRO-PATCH(channel-thread-responsive-overlay): TECH-3004 — also overlay
  // when the container itself is narrow (< 700px), e.g. when the channel is
  // embedded inside the inbox split view on medium-sized screens.
  const isMobile = useIsMobile();
  const threadOpen = !!activeThreadEntry;
  const threadFullScreen = (isMobile || containerWidth < 700) && threadOpen;
  const threadBackLabel = display.isChannel ? `#${display.title}` : display.title;

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
    // CEREBRO-PATCH(channel-thread-responsive-overlay): TECH-3004 — ref for ResizeObserver.
    <div ref={containerRef} className="flex h-full min-h-0 flex-1 flex-col">
      {/* CEREBRO-PATCH(channels-thread-fullscreen-mobile): JEH-1045 — hide the
          channel chrome (title, pin/archive, participants) while a thread is
          open on a narrow viewport. The thread panel's own back-header
          ("← #channel") replaces it, matching Slack's mobile thread view —
          two stacked headers would just compete for the same vertical space.
          CSS-hidden (not unmounted) so inline-rename in-flight state is
          preserved across the round-trip. */}
      <header
        className={cn(
          "flex shrink-0 flex-col gap-1 border-b px-4 py-3",
          threadFullScreen && "hidden",
        )}
      >
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
            {/* CEREBRO-PATCH(channel-cost-chip): FIR-39 channel-wide total cost. */}
            <ChannelCostChip channelId={channelId} />
            <ChannelListenersPanel channel={channel} />
            {display.isChannel && (
              // CEREBRO-PATCH(channel-detail-edit-button): FIR-2660 edit-channel modal (name + description).
              <Tooltip>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      onClick={() => setEditOpen(true)}
                      aria-label="Edit channel"
                      className="inline-flex size-7 items-center justify-center rounded border text-muted-foreground hover:bg-accent hover:text-foreground"
                    />
                  }
                >
                  <Pencil className="size-3.5" />
                </TooltipTrigger>
                <TooltipContent side="bottom">Edit channel</TooltipContent>
              </Tooltip>
            )}
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
        {/* CEREBRO-PATCH(channels-thread-fullscreen-mobile): JEH-1045 — hide
            the message column when the thread takes over on narrow viewports.
            CSS-hidden (not unmounted) so `CommentInput`'s Tiptap draft +
            upload map survive the thread round-trip; otherwise typing a
            channel reply, opening a thread, and tapping ← back would wipe
            the draft. `display: none` removes the column from flex layout
            so the thread panel reclaims the full width. */}
        <div
          className={cn(
            // CEREBRO-PATCH(channels-scroll-to-bottom): FIR-2522 — `relative` so JumpToLatestButton docks to the message column.
            // CEREBRO-PATCH(channels-mobile-col-min-width): TECH-3490 — min-w-0 lets this flex column shrink below its content's intrinsic width; without it the message stream (long URLs, file-chip filenames) forces the column past the viewport on mobile and text is clipped at the right edge.
            "relative flex min-w-0 flex-1 min-h-0 flex-col",
            threadFullScreen && "hidden",
          )}
        >
          <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto">
            {/* CEREBRO-PATCH(channels-scroll-to-bottom): FIR-2522 — "Ny besked"-pille når man læser ældre beskeder. */}
            <JumpToLatestButton
              visible={hasNewBelow}
              onClick={() => scrollToBottom()}
              label="Ny besked"
            />
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
          {/* CEREBRO-PATCH(channel-typing): TECH-3664 — "X is typing…" above composer. */}
          <ChannelTypingIndicator typingUserIds={typingUserIds} />
          <div className="shrink-0 border-t px-4 py-3">
            {/* CEREBRO-PATCH(input-autofocus): JEH-756 — channels & DMs are
                chat-like; entering one should land the caret in the input. */}
            {/* CEREBRO-PATCH(channel-typing): TECH-3664 — emit typing ping (throttled in the hook). */}
            <CommentInput issueId={channelId} onSubmit={submitComment} autoFocus onTyping={notifyTyping} />
          </div>
        </div>

        <ThreadSidePanel
          channelId={channelId}
          parentEntry={activeThreadEntry}
          repliesByParent={repliesByParent}
          open={!!activeThreadEntry}
          fullScreen={threadFullScreen}
          backLabel={threadBackLabel}
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
      {/* CEREBRO-PATCH(channel-detail-edit-dialog-mount): FIR-2660 edit-channel modal (name + description). */}
      {display.isChannel && (
        <EditChannelDialog
          channel={channel}
          open={editOpen}
          onOpenChange={setEditOpen}
        />
      )}
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
