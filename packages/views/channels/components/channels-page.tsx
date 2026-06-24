"use client";

import {
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  closestCenter,
  DndContext,
  DragOverlay,
  PointerSensor,
  useDraggable,
  useDndContext,
  useDroppable,
  useSensor,
  useSensors,
  type Announcements,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ArrowUp,
  Bot,
  ChevronRight,
  FileText,
  FolderPlus,
  GripVertical,
  Hash,
  Link,
  Loader2,
  Lock,
  MessageCircleReply,
  MessagesSquare,
  Plus,
  Search,
  Send,
  Settings,
  Square,
  Trash2,
  Users,
  X,
} from "lucide-react";

import { useAuthStore } from "@multica/core/auth";
import { useModalStore } from "@multica/core/modals";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  channelKeys,
  channelListOptions,
  channelGroupsOptions,
  channelMessagesOptions,
  channelMembersOptions,
  messageThreadOptions,
  useMarkChannelRead,
} from "@multica/core/channels";
import { enterKey, formatShortcut, modKey } from "@multica/core/platform";
import { memberListOptions } from "@multica/core/workspace/queries";
import type {
  ChannelMessage,
  ChannelMember,
  ChannelSummary,
  ListChannelMessagesResponse,
  MemberWithUser,
  MessageThreadResponse,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@multica/ui/components/ui/context-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { cn } from "@multica/ui/lib/utils";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";

import { ActorAvatar } from "../../common/actor-avatar";
import {
  ContentEditor,
  FileDropOverlay,
  ReadonlyContent,
  type ContentEditorRef,
  useFileDropZone,
} from "../../editor";
import { AppLink, useNavigation } from "../../navigation";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { TranscriptButton } from "../../common/task-transcript";
import { TerminateTaskConfirmDialog } from "../../issues/components/terminate-task-confirm-dialog";
import { taskStatusConfig } from "../../agents/config";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../../i18n";

interface ChannelsPageProps {
  channelId?: string;
}

type SidePanelMode = "replies" | "members";

const UNGROUPED_DROP_ID = "ungrouped-drop-zone";
// Drop target for the whole channel list body: a group dropped here (or on a
// non-group) appends to the end of the group order; also a fallback so a
// channel drag never resolves to "no droppable".
const CHANNEL_LIST_ROOT_ID = "channel-list-root";
const GROUP_DRAG_PREFIX = "group:";
const isGroupDragId = (id: string) => id.startsWith(GROUP_DRAG_PREFIX);
const groupIdFromDragId = (id: string) => id.slice(GROUP_DRAG_PREFIX.length);

// Message DnD id scheme (converge/release). A main message is draggable +
// droppable under `msg:<id>` (drag onto another main message to converge it
// into that thread). A reply is draggable under `reply:<id>` (drop on the main
// area to release it back to a top-level message). The main list body is a
// droppable so a reply released onto empty space (not a specific message)
// still lands as top-level.
const MSG_DRAG_PREFIX = "msg:";
const REPLY_DRAG_PREFIX = "reply:";
const MSG_LIST_DROP_ID = "channel-msg-list";
const idFromDragId = (id: string, prefix: string) => id.slice(prefix.length);

// previewOf returns a short, screen-reader-safe preview of a message for the
// drag announcements / aria-labels (a full message can be long). Splitting on
// code points (Array.from) keeps emoji / surrogate pairs intact.
function previewOf(content: string | undefined | null): string | undefined {
  if (!content) return undefined;
  const s = content.trim().replace(/\s+/g, " ");
  if (!s) return undefined;
  const chars = Array.from(s);
  return chars.length > 60 ? `${chars.slice(0, 60).join("")}…` : s;
}

// dragLabel extracts a screen-reader label from a dnd-kit active/over data
// payload. Channel messages/replies store { message }; groups/channels store
// { label }. Returns undefined so a missing label yields no announcement.
type DragData = { message?: { content?: string }; label?: string } | undefined;
function dragLabel(data: unknown): string | undefined {
  const d = data as DragData;
  return d?.label ?? previewOf(d?.message?.content);
}

const SIDE_PANEL_DEFAULT_WIDTH = 360;
const SIDE_PANEL_MIN_WIDTH = 320;
const SIDE_PANEL_MAX_WIDTH = 520;
const SIDE_PANEL_MAIN_MIN_WIDTH = 560;
const CHANNEL_LIST_DEFAULT_WIDTH = 280;
const CHANNEL_LIST_MIN_WIDTH = 180;
const CHANNEL_LIST_MAX_WIDTH = 420;
const CHANNEL_MAIN_MIN_WIDTH = 520;

let persistedChannelListWidth = CHANNEL_LIST_DEFAULT_WIDTH;
let persistedSidePanelWidth = SIDE_PANEL_DEFAULT_WIDTH;

function clampNumber(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function getSidePanelBounds(containerWidth: number, channelListWidth = 0) {
  const availableWidth = Math.max(0, containerWidth - channelListWidth);
  const min = Math.min(
    SIDE_PANEL_MIN_WIDTH,
    Math.max(240, Math.floor(availableWidth * 0.45)),
  );
  const max = Math.max(
    min,
    Math.min(SIDE_PANEL_MAX_WIDTH, availableWidth - SIDE_PANEL_MAIN_MIN_WIDTH),
  );
  return { min, max };
}

function getChannelListBounds(containerWidth: number, showRightPanel: boolean, sidePanelWidth: number) {
  const reservedWidth = showRightPanel ? sidePanelWidth : 0;
  const availableWidth = Math.max(0, containerWidth - reservedWidth);
  const max = Math.max(
    CHANNEL_LIST_MIN_WIDTH,
    Math.min(CHANNEL_LIST_MAX_WIDTH, availableWidth - CHANNEL_MAIN_MIN_WIDTH),
  );
  return { min: CHANNEL_LIST_MIN_WIDTH, max };
}

/** Center `target` vertically within `container` by setting scrollTop directly.
 *  Uses instant (non-smooth) scrolling: deep-links jump a potentially large
 *  distance, where a smooth animation is both disorienting and fragile — a
 *  WS-driven message refetch or a side-panel layout shift mid-animation
 *  cancels it. Callers wrap in requestAnimationFrame as needed so layout is
 *  settled before the offsets are read. */
function scrollToElement(container: HTMLElement | null, target: HTMLElement) {
  if (!container) return;
  const containerRect = container.getBoundingClientRect();
  const targetRect = target.getBoundingClientRect();
  const offset = targetRect.top - containerRect.top + container.scrollTop;
  container.scrollTo({
    top: offset - container.clientHeight / 2 + targetRect.height / 2,
  });
}

// Scroll the message list to its very bottom by setting scrollTop directly.
// Instant and reliable — used for incoming messages and initial page loads.
function scrollListToBottom(container: HTMLElement | null) {
  if (!container) return;
  container.scrollTop = container.scrollHeight;
}

// Animated smooth scroll to bottom using a rAF loop. Unlike native
// scrollTo({behavior:'smooth'}), this can't be cancelled by WS-driven
// refetches/re-renders, and the onDone callback lets the caller keep the
// self-sent flag true for the entire animation duration so the "jump to last
// read" indicator doesn't flash mid-scroll.
function animateScrollToBottom(container: HTMLElement | null, onDone?: () => void) {
  if (!container) { onDone?.(); return; }
  const el = container;
  const start = el.scrollTop;
  const end = el.scrollHeight;
  const distance = end - start;
  if (distance <= 0) { onDone?.(); return; }
  const duration = Math.min(Math.max(distance / 2, 150), 400);
  let startTime: number | undefined;
  function step(now: number) {
    if (startTime === undefined) startTime = now;
    const t = Math.min((now - startTime) / duration, 1);
    el.scrollTop = start + distance * (1 - Math.pow(1 - t, 3));
    if (t < 1) requestAnimationFrame(step);
    else onDone?.();
  }
  requestAnimationFrame(step);
}

function formatTime(ts?: string): string {
  if (!ts) return "";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function memberSearchText(member: MemberWithUser): string {
  return `${member.name} ${member.email}`.toLowerCase();
}

function matchesMember(member: MemberWithUser, rawQuery: string): boolean {
  const query = rawQuery.trim().toLowerCase();
  if (!query) return true;
  return (
    memberSearchText(member).includes(query) ||
    matchesPinyin(member.name, query)
  );
}

// MessageDndProvider wraps the channel main area + reply side panel in a
// DndContext that powers the "converge"/"release" drag. It is deliberately a
// SEPARATE DndContext from the channel list's — dnd-kit does not support
// nested DndContexts, so the channel-list aside (which has its own) stays
// outside this provider.
//
// Long-press (delay 250ms, tolerance 5px) activates the drag: a quick
// click-drag still selects message text / triggers the context menu, while a
// deliberate hold-then-move starts a converge/release. Only main messages and
// replies authored by the user (or managed by them) are practically movable —
// the backend re-checks author-or-canManage, so the UI does not pre-filter.
function MessageDndProvider({
  wsId,
  channelId,
  children,
}: {
  wsId: string;
  channelId: string | null;
  children: ReactNode;
}) {
  const { t } = useT("channels");
  const qc = useQueryClient();
  const [activeMessage, setActiveMessage] = useState<ChannelMessage | null>(null);

  // Long-press activation. A pointer held roughly still for 250ms starts the
  // drag; moving more than 5px before then cancels it so native text selection
  // keeps working on message content.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { delay: 250, tolerance: 5 } }),
  );

  // Localized, pointer-honest screen-reader support. The default dnd-kit
  // announcements read active.id ("msg:<uuid>") and promise keyboard drag
  // ("press the space bar") that this long-press pointer interaction does not
  // support — so override both: announcements read a message preview, and the
  // instructions describe the real long-press interaction.
  const announcements = useMemo<Announcements>(
    () => ({
      onDragStart: ({ active }) => {
        const preview = dragLabel(active.data.current);
        return preview ? t($ => $.drag.picked_up, { preview }) : undefined;
      },
      onDragOver: () => undefined,
      onDragEnd: ({ over }) => {
        const target = over ? dragLabel(over.data.current) : undefined;
        return target ? t($ => $.drag.dropped_over, { target }) : t($ => $.drag.dropped);
      },
      onDragCancel: () => t($ => $.drag.cancelled),
    }),
    [t],
  );
  const screenReaderInstructions = useMemo(
    () => ({ draggable: t($ => $.drag.instructions) }),
    [t],
  );

  const handleDragStart = useCallback((event: DragStartEvent) => {
    const msg = (event.active.data.current as { message?: ChannelMessage } | undefined)?.message;
    setActiveMessage(msg ?? null);
  }, []);

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const activeId = String(event.active.id);
      const over = event.over ? String(event.over.id) : null;
      setActiveMessage(null);
      if (!channelId || !over) return;

      let msgId: string | null = null;
      let targetId: string | null | undefined;

      if (
        activeId.startsWith(MSG_DRAG_PREFIX) &&
        over.startsWith(MSG_DRAG_PREFIX) &&
        activeId !== over
      ) {
        // Converge: main message → latest reply of the hovered main message.
        msgId = idFromDragId(activeId, MSG_DRAG_PREFIX);
        targetId = idFromDragId(over, MSG_DRAG_PREFIX);
      } else if (
        activeId.startsWith(REPLY_DRAG_PREFIX) &&
        (over === MSG_LIST_DROP_ID || over.startsWith(MSG_DRAG_PREFIX))
      ) {
        // Release: reply → top-level. Dropping on a specific main message or
        // the list body both release (the active is a reply, not a main
        // message, so it cannot converge).
        msgId = idFromDragId(activeId, REPLY_DRAG_PREFIX);
        targetId = null;
      }

      if (!msgId) return;
      api
        .moveChannelMessage(channelId, msgId, targetId ?? null)
        .then(() => {
          // channelKeys.all covers channelMessages + the open messageThread +
          // the list (unread/last_activity) — one invalidation refreshes
          // every view the move touched, mirroring the WS handler.
          qc.invalidateQueries({ queryKey: channelKeys.all(wsId) });
          toast.success(targetId ? t($ => $.move.converged) : t($ => $.move.released));
        })
        .catch((err: unknown) => {
          // Surface the backend's specific reason (409 populated-thread, 403
          // permission) instead of a generic label.
          const reason = err instanceof Error && err.message ? err.message : t($ => $.move.failed);
          toast.error(reason);
        });
    },
    [channelId, qc, wsId, t],
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      accessibility={{ announcements, screenReaderInstructions }}
    >
      {children}
      <DragOverlay dropAnimation={null}>
        {activeMessage && (
          <div className="max-w-sm rounded-md border bg-card p-2 text-sm shadow-lg ring-1 ring-border">
            <div className="mb-1 text-xs text-muted-foreground">
              {activeMessage.author_name ?? activeMessage.author_type}
            </div>
            <div className="line-clamp-3 break-words text-foreground/90">{activeMessage.content}</div>
          </div>
        )}
      </DragOverlay>
    </DndContext>
  );
}

export function ChannelsPage({ channelId }: ChannelsPageProps) {
  const { t } = useT("channels");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const nav = useNavigation();
  const qc = useQueryClient();

  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [sidePanel, setSidePanel] = useState<SidePanelMode | null>(null);
  const [sidePanelWidth, setSidePanelWidth] = useState(persistedSidePanelWidth);
  const [channelListWidth, setChannelListWidth] = useState(persistedChannelListWidth);
  const [replyMessageId, setReplyMessageId] = useState<string | null>(null);
  const pageRef = useRef<HTMLDivElement>(null);
  const sidePanelRef = useRef<HTMLDivElement>(null);

  const { data: channels, isLoading: loadingChannels } = useQuery(channelListOptions(wsId));
  // ?message=<id> deep-link target. Fed into the messages query as ?around= so
  // the first page loads a window centered on it (the target may be older than
  // the latest page), and also drives the scroll-to-and-highlight on render.
  const linkedMessageId = nav.searchParams.get("message")?.trim() || null;
  // Anchor for the first messages page. A deep-link takes priority; otherwise
  // null (latest). The "jump to last read" button re-anchors to the first
  // unread message so a window around it loads. Reset on channel switch or
  // deep-link change so a stale anchor from another channel never leaks in.
  const [anchorMessageId, setAnchorMessageId] = useState<string | null>(linkedMessageId);
  useEffect(() => { setAnchorMessageId(linkedMessageId); }, [linkedMessageId, channelId]);
  const { data: msgData, isLoading: loadingMessages, hasNextPage, fetchNextPage, isFetchingNextPage } = useInfiniteQuery(channelMessagesOptions(wsId, channelId ?? null, anchorMessageId));
  const messages = msgData?.messages ?? [];
  const highlight = msgData?.highlight ?? null;
  const { data: members = [] } = useQuery(channelMembersOptions(wsId, channelId ?? null));
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);

  const activeChannel = useMemo(
    () => channels?.find((c) => c.id === channelId) ?? null,
    [channels, channelId],
  );
  const currentWorkspaceMember = useMemo(
    () => workspaceMembers.find((m) => m.user_id === currentUserId) ?? null,
    [currentUserId, workspaceMembers],
  );
  // @-mention candidate ids. Open channels let you @ any workspace member
  // (the backend already notifies workspace-wide for open channels); invite
  // channels restrict candidates to channel members only. invite-only is the
  // regression guard — narrowing the candidate set there must not change.
  const mentionMemberIds = useMemo(() => {
    if (activeChannel?.access_mode === "open") {
      return workspaceMembers.map((m) => m.user_id);
    }
    return members.map((m) => m.user_id);
  }, [activeChannel?.access_mode, workspaceMembers, members]);

  // Workspace-level admin/owner — the per-channel half of canManage (the
  // other half is channel member_role === "owner"). Shared with the channel
  // list so each row can decide whether to show its delete affordance.
  const isWorkspaceAdmin =
    currentWorkspaceMember?.role === "owner" ||
    currentWorkspaceMember?.role === "admin";
  const canManageChannel = activeChannel?.member_role === "owner" || isWorkspaceAdmin;

  const threadQuery = useQuery(
    messageThreadOptions(wsId, channelId ?? null, replyMessageId),
  );

  // Unread is cleared on catch-up, not on entry: landing at the read/unread
  // boundary first (see MessageList) so the user can resume where they left off
  // — clearing it on entry would erase the boundary before they see it. The
  // list query's has_unread drives the boundary; MessageList fires onMarkRead
  // once the user scrolls to the bottom (and when a live message lands while
  // they're already watching at the bottom).
  const markReadMutate = useMarkChannelRead().mutate;
  const activeHasUnread = activeChannel?.has_unread ?? false;
  const handleMarkRead = useCallback(() => {
    if (channelId) markReadMutate(channelId);
  }, [channelId, markReadMutate]);

  const closeSidePanel = useCallback(() => {
    setSidePanel(null);
    setReplyMessageId(null);
  }, []);

  const openReplies = useCallback((messageId: string) => {
    setReplyMessageId(messageId);
    setSidePanel("replies");
  }, []);

  const openMembers = useCallback(() => {
    setSidePanel("members");
    setReplyMessageId(null);
  }, []);

  useEffect(() => {
    if (!sidePanel) return;
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target as Node | null;
      if (!target) return;
      if (sidePanelRef.current?.contains(target)) return;
      if ((target as HTMLElement).closest("[data-panel-trigger='channel-side-panel']")) return;
      if ((target as HTMLElement).closest("[data-panel-resize-handle='channel-side-panel']")) return;
      if ((target as HTMLElement).closest("[data-slot='context-menu-content']")) return;
      if ((target as HTMLElement).closest("[data-slot='select-content']")) return;
      if ((target as HTMLElement).closest("[data-slot='mention-suggestion-content']")) return;
      if ((target as HTMLElement).closest("[data-slot='dialog-overlay']")) return;
      if ((target as HTMLElement).closest("[data-slot='dialog-content']")) return;
      closeSidePanel();
    };
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [closeSidePanel, sidePanel]);

  useEffect(() => {
    closeSidePanel();
  }, [channelId, closeSidePanel]);

  const showRightPanel = !!sidePanel && !!channelId && !!activeChannel;

  useEffect(() => {
    const clampToContainer = () => {
      const containerWidth = pageRef.current?.getBoundingClientRect().width;
      if (!containerWidth) return;
      const { min, max } = getChannelListBounds(containerWidth, showRightPanel, sidePanelWidth);
      setChannelListWidth((width) => clampNumber(width, min, max));
    };

    clampToContainer();
    window.addEventListener("resize", clampToContainer);
    return () => window.removeEventListener("resize", clampToContainer);
  }, [showRightPanel, sidePanelWidth]);

  useEffect(() => {
    if (!showRightPanel) return;

    const clampToContainer = () => {
      const containerWidth = pageRef.current?.getBoundingClientRect().width;
      if (!containerWidth) return;
      const { min, max } = getSidePanelBounds(containerWidth, channelListWidth);
      setSidePanelWidth((width) => clampNumber(width, min, max));
    };

    clampToContainer();
    window.addEventListener("resize", clampToContainer);
    return () => window.removeEventListener("resize", clampToContainer);
  }, [channelListWidth, showRightPanel]);

  const startSidePanelResize = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.stopPropagation();

    const handlePointerMove = (moveEvent: PointerEvent) => {
      const containerRect = pageRef.current?.getBoundingClientRect();
      if (!containerRect) return;
      const { min, max } = getSidePanelBounds(containerRect.width, channelListWidth);
      const newWidth = clampNumber(containerRect.right - moveEvent.clientX, min, max);
      setSidePanelWidth(newWidth);
      persistedSidePanelWidth = newWidth;
    };

    const handlePointerUp = () => {
      document.removeEventListener("pointermove", handlePointerMove);
      document.removeEventListener("pointerup", handlePointerUp);
    };

    document.addEventListener("pointermove", handlePointerMove);
    document.addEventListener("pointerup", handlePointerUp);
  }, [channelListWidth]);

  const startChannelListResize = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.stopPropagation();

    const handlePointerMove = (moveEvent: PointerEvent) => {
      const containerRect = pageRef.current?.getBoundingClientRect();
      if (!containerRect) return;
      const { min, max } = getChannelListBounds(containerRect.width, showRightPanel, sidePanelWidth);
      const newWidth = clampNumber(moveEvent.clientX - containerRect.left, min, max);
      setChannelListWidth(newWidth);
      persistedChannelListWidth = newWidth;
    };

    const handlePointerUp = () => {
      document.removeEventListener("pointermove", handlePointerMove);
      document.removeEventListener("pointerup", handlePointerUp);
    };

    document.addEventListener("pointermove", handlePointerMove);
    document.addEventListener("pointerup", handlePointerUp);
  }, [showRightPanel, sidePanelWidth]);

  return (
    <div ref={pageRef} className="flex h-full min-h-0 min-w-0 overflow-hidden">
      <aside className="h-full shrink-0 min-w-0" style={{ width: channelListWidth }}>
        <ChannelList
          channels={channels ?? []}
          activeChannelId={channelId ?? null}
          loading={loadingChannels}
          onCreate={() => setShowCreateDialog(true)}
          onSelect={(id) => nav.push(paths.channelDetail(id))}
          onDelete={(id) => {
            // If the active channel is removed, drop the stale id from the URL
            // so the main area returns to the empty state instead of a 404.
            if (id === channelId) nav.push(paths.channels());
          }}
          isWorkspaceAdmin={isWorkspaceAdmin}
          wsId={wsId}
        />
      </aside>
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label={t($ => $.resize_aria.channel_list)}
        data-panel-resize-handle="channel-list"
        className="relative z-20 flex w-0 shrink-0 cursor-col-resize items-center justify-center before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-transparent before:transition-colors hover:before:bg-foreground/15 after:absolute after:inset-y-0 after:left-1/2 after:w-2 after:-translate-x-1/2"
        onPointerDown={startChannelListResize}
      >
        <div className="z-10 flex h-6 w-1 shrink-0 rounded-lg bg-border" />
      </div>
      <MessageDndProvider wsId={wsId} channelId={channelId ?? null}>
      <main className="min-h-0 min-w-0 flex-1">
        {channelId && activeChannel ? (
          <div className="flex h-full min-w-0 flex-col">
            <ChannelHeader
              channel={activeChannel}
              memberCount={members.length}
              onOpenMembers={openMembers}
            />
            <MessageList
              messages={messages ?? []}
              loading={loadingMessages}
              channelId={channelId}
              wsId={wsId}
              memberIds={mentionMemberIds}
              onOpenReplies={openReplies}
              qc={qc}
              hasMore={hasNextPage}
              loadMore={fetchNextPage}
              loadingMore={isFetchingNextPage}
              canPost={activeChannel.access_mode === "open" || activeChannel.is_member}
              highlight={highlight}
              firstUnreadMessageId={activeChannel.first_unread_message_id ?? null}
              hasUnread={activeHasUnread}
              onMarkRead={handleMarkRead}
              onJumpToMessage={setAnchorMessageId}
            />
          </div>
        ) : (
          <div className="flex h-full items-center justify-center text-muted-foreground">
            <div className="text-center">
              <MessagesSquare className="mx-auto mb-2 h-10 w-10 opacity-30" />
              <p className="text-sm">{t($ => $.empty_prompt)}</p>
            </div>
          </div>
        )}
      </main>
      {showRightPanel && (
        <>
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label={t($ => $.resize_aria.side_panel)}
            data-panel-resize-handle="channel-side-panel"
            className="relative z-20 flex w-0 shrink-0 cursor-col-resize items-center justify-center before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-transparent before:transition-colors hover:before:bg-foreground/15 after:absolute after:inset-y-0 after:left-1/2 after:w-2 after:-translate-x-1/2"
            onPointerDown={startSidePanelResize}
          >
            <div className="z-10 flex h-6 w-1 shrink-0 rounded-lg bg-border" />
          </div>
          <div
            ref={sidePanelRef}
            data-channel-side-panel="true"
            className="h-full shrink-0"
            style={{ width: sidePanelWidth }}
          >
            {sidePanel === "replies" && replyMessageId ? (
              <RepliesPanel
                channelId={channelId}
                messageId={replyMessageId}
                data={threadQuery.data}
                loading={threadQuery.isLoading}
                wsId={wsId}
                memberIds={mentionMemberIds}
                qc={qc}
                onClose={closeSidePanel}
                highlightMessageId={highlight?.message_id ?? null}
              />
            ) : (
              <ChannelMembersPanel
                channel={activeChannel}
                members={members}
                workspaceMembers={workspaceMembers}
                canManage={canManageChannel}
                wsId={wsId}
                qc={qc}
                onClose={closeSidePanel}
              />
            )}
          </div>
        </>
      )}
      </MessageDndProvider>

      <CreateChannelDialog
        open={showCreateDialog}
        onClose={() => setShowCreateDialog(false)}
        wsId={wsId}
        qc={qc}
      />
    </div>
  );
}

function ChannelList({
  channels,
  activeChannelId,
  loading,
  onCreate,
  onSelect,
  onDelete,
  isWorkspaceAdmin,
  wsId,
}: {
  channels: ChannelSummary[];
  activeChannelId: string | null;
  loading: boolean;
  onCreate: () => void;
  onSelect: (id: string) => void;
  onDelete: (id: string) => void;
  isWorkspaceAdmin: boolean;
  wsId: string;
}) {
  const { t } = useT("channels");
  const qc = useQueryClient();
  const { data: channelGroups = [] } = useQuery(channelGroupsOptions(wsId));
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [editingGroupId, setEditingGroupId] = useState<string | null>(null);
  const [editingName, setEditingName] = useState("");
  const [activeId, setActiveId] = useState<string | null>(null);
  const [overGroupId, setOverGroupId] = useState<string | null | undefined>(undefined);
  const [showCreateGroupDialog, setShowCreateGroupDialog] = useState(false);
  const [newGroupName, setNewGroupName] = useState("");

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  // Localized, pointer-honest screen-reader support for the channel/group list
  // drag (mirrors the message DndContext): announcements read a group/channel
  // name instead of the bare id, and instructions describe the real interaction.
  const announcements = useMemo<Announcements>(
    () => ({
      onDragStart: ({ active }) => {
        const preview = dragLabel(active.data.current);
        return preview ? t($ => $.drag.picked_up, { preview }) : undefined;
      },
      onDragOver: () => undefined,
      onDragEnd: ({ over }) => {
        const target = over ? dragLabel(over.data.current) : undefined;
        return target ? t($ => $.drag.dropped_over, { target }) : t($ => $.drag.dropped);
      },
      onDragCancel: () => t($ => $.drag.cancelled),
    }),
    [t],
  );
  const screenReaderInstructions = useMemo(
    () => ({ draggable: t($ => $.drag.instructions) }),
    [t],
  );

  const invalidateGroupsAndList = () => {
    qc.invalidateQueries({ queryKey: channelKeys.groups(wsId) });
    qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
  };
  const createGroupMutation = useMutation({
    mutationFn: (name: string) => api.createChannelGroup(name),
    onSuccess: invalidateGroupsAndList,
  });
  const renameGroupMutation = useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) => api.updateChannelGroup(id, name),
    onSuccess: invalidateGroupsAndList,
  });
  const deleteGroupMutation = useMutation({
    mutationFn: (id: string) => api.deleteChannelGroup(id),
    onSuccess: invalidateGroupsAndList,
  });
  const moveChannelMutation = useMutation({
    mutationFn: (args: { channelId: string; groupId: string | null; position: number }) =>
      api.moveChannelToGroup(args.channelId, args.groupId, args.position),
    onSuccess: invalidateGroupsAndList,
  });
  // Reordering a group is a one-level operation: it only moves the group
  // relative to its siblings, never nesting. position is DOUBLE PRECISION so a
  // midpoint between neighbours orders the dragged group without touching the
  // others — the channel list query re-sorts server-side on invalidation.
  const moveGroupMutation = useMutation({
    mutationFn: (args: { groupId: string; position: number }) =>
      api.updateChannelGroupPosition(args.groupId, args.position),
    onSuccess: invalidateGroupsAndList,
  });
  // Channel deletion is gated by canManage (channel owner or workspace
  // admin/owner) — the row only renders the trigger for those users, mirroring
  // the backend DeleteChannel gate (canManage = wsAdmin || channelOwn). The
  // AlertDialog confirms before the irreversible delete.
  const [pendingDelete, setPendingDelete] = useState<ChannelSummary | null>(null);
  // Group deletion is non-destructive to channels (they fall back to Ungrouped
  // via channel.group_id ON DELETE SET NULL), but it scatters them out of the
  // group — confirm before applying, mirroring the channel-delete dialog.
  const [pendingGroupDelete, setPendingGroupDelete] = useState<{ id: string; name: string } | null>(null);
  const deleteChannelMutation = useMutation({
    mutationFn: (id: string) => api.deleteChannel(id),
    onSuccess: (_data, id) => {
      invalidateGroupsAndList();
      toast.success(t($ => $.channel_deleted));
      onDelete(id);
      setPendingDelete(null);
    },
    onError: () => toast.error(t($ => $.delete_failed)),
  });

  const { groups, ungrouped, groupMap } = useMemo(() => {
    const gMap = new Map<string, { name: string; position: number; channels: ChannelSummary[] }>();
    for (const g of channelGroups) {
      gMap.set(g.id, { name: g.name, position: g.position, channels: [] });
    }
    const ug: ChannelSummary[] = [];
    for (const ch of channels) {
      if (ch.group_id) {
        if (!gMap.has(ch.group_id)) {
          gMap.set(ch.group_id, { name: ch.group_name ?? "", position: ch.group_position, channels: [] });
        }
        gMap.get(ch.group_id)!.channels.push(ch);
      } else {
        ug.push(ch);
      }
    }
    for (const g of gMap.values()) {
      g.channels.sort((a, b) => a.position - b.position);
    }
    ug.sort((a, b) => a.position - b.position);
    // Empty groups (including a freshly-created one with no channels yet) MUST
    // render — hiding them made "create group" look like a no-op. A group is a
    // user-created container that persists until explicitly deleted; the
    // .filter(channels.length > 0) added in b7a684da8 traded a cosmetic nit
    // (orphaned header after moving all channels out) for breaking creation.
    const sortedGroups = [...gMap.entries()]
      .sort((a, b) => a[1].position - b[1].position);
    return { groups: sortedGroups, ungrouped: ug, groupMap: gMap };
  }, [channels, channelGroups]);

  const allIds = useMemo(() => {
    const ids: string[] = [];
    for (const [gId, g] of groups) {
      if (!collapsed[gId]) {
        for (const ch of g.channels) ids.push(ch.id);
      }
    }
    for (const ch of ungrouped) ids.push(ch.id);
    return ids;
  }, [groups, ungrouped, collapsed]);

  // Resolve which group a drag would land in, plus the target position.
  // `groupId === null` means the ungrouped zone. Returns null when the
  // pointer is over nothing droppable. Position is computed as a midpoint
  // between neighbours (position is a DOUBLE PRECISION) so dragging onto a
  // channel reorders it rather than colliding with the target's own position
  // — the previous impl returned overChannel.position directly, so any drop
  // onto a channel produced two channels sharing one position and the
  // within-group sort order became nondeterministic.
  const resolveDropTarget = useCallback(
    (overId: string): { groupId: string | null; position: number } | null => {
      // Channels in display (position-ASC) order for a given group, or the
      // ungrouped zone. `groupId === null` selects the ungrouped zone.
      const orderedChannels = (groupId: string | null): ChannelSummary[] => {
        if (groupId === null) return ungrouped;
        return groupMap.get(groupId)?.channels ?? [];
      };
      if (overId === UNGROUPED_DROP_ID) {
        const last = ungrouped[ungrouped.length - 1];
        return { groupId: null, position: last ? last.position + 1 : 1 };
      }
      if (overId.startsWith("group:")) {
        const gId = overId.slice(6);
        const gChannels = orderedChannels(gId);
        const last = gChannels[gChannels.length - 1];
        return { groupId: gId, position: last ? last.position + 1 : 1 };
      }
      const overChannel = channels.find((c) => c.id === overId);
      if (overChannel) {
        const groupId = overChannel.group_id ?? null;
        const siblings = orderedChannels(groupId);
        const idx = siblings.findIndex((c) => c.id === overChannel.id);
        // Drop *before* the hovered channel → midpoint with the previous
        // sibling; at the head of the list → half of the first position.
        if (idx <= 0) {
          const first = siblings[0];
          return { groupId, position: first ? first.position / 2 : 1 };
        }
        const prev = siblings[idx - 1]!;
        const cur = siblings[idx]!;
        return { groupId, position: (prev.position + cur.position) / 2 };
      }
      return null;
    },
    [channels, groupMap, ungrouped],
  );

  // Compute the position a dragged group should land at, given what it was
  // dropped over. Groups are one level only — reordering never nests. A drop
  // onto another group inserts before it. A drop onto a CHANNEL resolves to
  // that channel's group (so dragging a tall group over a sibling group's
  // channels reorders relative to that group, not "append to end"); a drop on
  // the root or the ungrouped zone appends to the end. position is a midpoint
  // between the dragged group's new neighbours (DOUBLE PRECISION), so only the
  // dragged group's row changes — the others keep their positions.
  const resolveGroupDropTarget = useCallback(
    (overId: string, draggedGroupId: string): { groupId: string; position: number } | null => {
      const sorted = groups
        .map(([id, g]) => ({ id, position: g.position }))
        .sort((a, b) => a.position - b.position);
      const siblings = sorted.filter((g) => g.id !== draggedGroupId);
      let targetGroupId: string | null = null;
      let append = false;
      if (isGroupDragId(overId)) {
        targetGroupId = groupIdFromDragId(overId);
      } else if (overId === CHANNEL_LIST_ROOT_ID || overId === UNGROUPED_DROP_ID) {
        append = true;
      } else {
        // A channel: resolve to its group (ungrouped channel → append).
        const ch = channels.find((c) => c.id === overId);
        targetGroupId = ch?.group_id ?? null;
        if (!targetGroupId) append = true;
      }
      let insertIdx: number;
      if (append) {
        insertIdx = siblings.length;
      } else if (targetGroupId) {
        const idx = siblings.findIndex((g) => g.id === targetGroupId);
        insertIdx = idx < 0 ? siblings.length : idx;
      } else {
        return null;
      }
      const prev = siblings[insertIdx - 1];
      const next = siblings[insertIdx];
      let position: number;
      if (prev && next) position = (prev.position + next.position) / 2;
      else if (prev) position = prev.position + 1;
      else if (next) position = next.position / 2;
      else position = 1;
      return { groupId: draggedGroupId, position };
    },
    [groups],
  );

  const handleDragStart = useCallback((event: DragStartEvent) => {
    setActiveId(String(event.active.id));
    setOverGroupId(undefined);
  }, []);

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      const { over } = event;
      if (!over) {
        setOverGroupId(undefined);
        return;
      }
      // Group reordering uses its own drop logic; don't paint the channel-drop
      // highlight (it would misread as a channel append target).
      if (activeId && isGroupDragId(activeId)) return;
      const target = resolveDropTarget(String(over.id));
      setOverGroupId(target ? target.groupId : undefined);
    },
    [resolveDropTarget, activeId],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const draggedId = String(event.active.id);
      const wasGroup = isGroupDragId(draggedId);
      setActiveId(null);
      setOverGroupId(undefined);
      const { over } = event;
      if (!over) return;

      if (wasGroup) {
        // Group reorder (one level): insert before the resolved target group,
        // or append when dropped on the root / ungrouped. No-op when the drop
        // resolves to the dragged group's own slot (dropped on itself or one
        // of its own channels).
        const draggedGroupId = groupIdFromDragId(draggedId);
        const target = resolveGroupDropTarget(String(over.id), draggedGroupId);
        if (!target) return;
        // Did the pointer land on the dragged group itself, or on a channel
        // belonging to it? Either way the group isn't moving — skip.
        const overStr = String(over.id);
        const overIsOwnGroup = isGroupDragId(overStr) && groupIdFromDragId(overStr) === draggedGroupId;
        const overOwnChannel = !isGroupDragId(overStr) && overStr !== CHANNEL_LIST_ROOT_ID &&
          overStr !== UNGROUPED_DROP_ID &&
          channels.find((c) => c.id === overStr)?.group_id === draggedGroupId;
        if (overIsOwnGroup || overOwnChannel) return;
        moveGroupMutation.mutate(target);
        return;
      }

      const dragged = channels.find((c) => c.id === draggedId);
      if (!dragged) return;

      const target = resolveDropTarget(String(over.id));
      if (!target) return;

      const currentGroup = dragged.group_id ?? null;
      // No-op when dropped back onto itself. Position is a midpoint now, so
      // a same-position equality check no longer applies — dragging onto
      // oneself resolves to a drop-before-self, which still moves the channel
      // in front of its current slot only when there's a preceding sibling.
      if (target.groupId === currentGroup && String(over.id) === draggedId) {
        return;
      }

      moveChannelMutation.mutate({
        channelId: draggedId,
        groupId: target.groupId,
        position: target.position,
      });
    },
    [channels, resolveDropTarget, resolveGroupDropTarget, moveChannelMutation, moveGroupMutation],
  );

  const handleCreateGroup = () => {
    setNewGroupName("");
    setShowCreateGroupDialog(true);
  };

  const handleCreateGroupSubmit = () => {
    if (newGroupName.trim()) {
      createGroupMutation.mutate(newGroupName.trim());
    }
    setShowCreateGroupDialog(false);
  };

  const handleRenameSubmit = (groupId: string) => {
    if (editingName.trim()) {
      renameGroupMutation.mutate({ id: groupId, name: editingName.trim() });
    }
    setEditingGroupId(null);
  };

  const activeDragChannel = activeId && !isGroupDragId(activeId)
    ? channels.find((c) => c.id === activeId) ?? null
    : null;
  const activeDragGroup = activeId && isGroupDragId(activeId)
    ? channelGroups.find((g) => g.id === groupIdFromDragId(activeId)) ?? null
    : null;

  return (
    <div className="flex h-full min-w-0 flex-col border-r bg-muted/30">
      <div className="flex h-11 items-center justify-between border-b px-3">
        <h2 className="text-sm font-semibold">{t($ => $.header)}</h2>
        <div className="flex items-center gap-0.5">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button variant="ghost" size="icon" className="h-7 w-7" onClick={handleCreateGroup}>
                  <FolderPlus className="h-4 w-4" />
                </Button>
              }
            />
            <TooltipContent>{t($ => $.new_group)}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onCreate}>
                  <Plus className="h-4 w-4" />
                </Button>
              }
            />
            <TooltipContent>{t($ => $.new_channel)}</TooltipContent>
          </Tooltip>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {loading ? (
          <div className="space-y-2 p-3">
            {[1, 2, 3].map((i) => <Skeleton key={i} className="h-8 w-full" />)}
          </div>
        ) : (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragStart={handleDragStart}
            onDragOver={handleDragOver}
            onDragEnd={handleDragEnd}
            accessibility={{ announcements, screenReaderInstructions }}
          >
            <SortableContext items={allIds} strategy={verticalListSortingStrategy}>
              <RootDropZone>
                {groups.map(([gId, g]) => (
                  <ChannelGroupSection
                    key={gId}
                    groupId={gId}
                    name={g.name}
                    channels={g.channels}
                    collapsed={!!collapsed[gId]}
                    isDragging={activeId !== null}
                    isDropTarget={overGroupId === gId}
                    activeChannelId={activeChannelId}
                    editing={editingGroupId === gId}
                    editingName={editingName}
                    onToggle={() => setCollapsed((s) => ({ ...s, [gId]: !s[gId] }))}
                    onStartRename={() => { setEditingGroupId(gId); setEditingName(g.name); }}
                    onRenameChange={setEditingName}
                    onRenameSubmit={() => handleRenameSubmit(gId)}
                    onRenameCancel={() => setEditingGroupId(null)}
                    onDelete={() => setPendingGroupDelete({ id: gId, name: g.name })}
                    onSelect={onSelect}
                    isWorkspaceAdmin={isWorkspaceAdmin}
                    onRequestDeleteChannel={setPendingDelete}
                  />
                ))}
                <UngroupedDropZone
                  channels={ungrouped}
                  isDragging={activeId !== null}
                  isDropTarget={overGroupId === null}
                  activeChannelId={activeChannelId}
                  onSelect={onSelect}
                  isWorkspaceAdmin={isWorkspaceAdmin}
                  onRequestDeleteChannel={setPendingDelete}
                />
              </RootDropZone>
            </SortableContext>
            <DragOverlay>
              {activeDragChannel && (
                <div className="flex h-8 items-center gap-2 rounded-md bg-accent px-2 text-sm shadow-md ring-1 ring-border">
                  <Hash className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate">{activeDragChannel.name}</span>
                </div>
              )}
              {activeDragGroup && (
                <div className="flex h-7 items-center gap-1 rounded-md bg-accent px-1 text-xs font-medium text-muted-foreground shadow-md ring-1 ring-border">
                  <GripVertical className="h-3 w-3 shrink-0" />
                  <ChevronRight className="h-3 w-3 shrink-0" />
                  <span className="min-w-0 flex-1 truncate uppercase">{activeDragGroup.name}</span>
                </div>
              )}
            </DragOverlay>
          </DndContext>
        )}
      </div>

      <Dialog open={showCreateGroupDialog} onOpenChange={(v) => !v && setShowCreateGroupDialog(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t($ => $.group_dialog.title)}</DialogTitle>
            <DialogDescription>{t($ => $.group_dialog.description)}</DialogDescription>
          </DialogHeader>
          <div className="py-2">
            <Input
              placeholder={t($ => $.group_dialog.name_placeholder)}
              value={newGroupName}
              onChange={(e) => setNewGroupName(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") handleCreateGroupSubmit(); }}
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowCreateGroupDialog(false)}>{t($ => $.group_dialog.cancel)}</Button>
            <Button onClick={handleCreateGroupSubmit} disabled={!newGroupName.trim()}>{t($ => $.group_dialog.create)}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <AlertDialog open={!!pendingDelete} onOpenChange={(v) => !v && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t($ => $.delete_channel.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t($ => $.delete_channel.description, { name: pendingDelete?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteChannelMutation.isPending}>
              {t($ => $.delete_channel.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => pendingDelete && deleteChannelMutation.mutate(pendingDelete.id)}
              disabled={deleteChannelMutation.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t($ => $.delete_channel.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={!!pendingGroupDelete} onOpenChange={(v) => !v && setPendingGroupDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t($ => $.delete_group.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t($ => $.delete_group.description, { name: pendingGroupDelete?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteGroupMutation.isPending}>
              {t($ => $.delete_group.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingGroupDelete) deleteGroupMutation.mutate(pendingGroupDelete.id);
                setPendingGroupDelete(null);
              }}
              disabled={deleteGroupMutation.isPending}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t($ => $.delete_group.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// Root drop zone for the channel list body. A group dropped here (or on a
// non-group) appends to the end of the group order; it also ensures a channel
// drag never resolves to "no droppable" when the pointer lands on padding.
function RootDropZone({ children }: { children: ReactNode }) {
  const { setNodeRef } = useDroppable({ id: CHANNEL_LIST_ROOT_ID });
  return <div ref={setNodeRef} className="space-y-0.5 p-1.5">{children}</div>;
}

function ChannelGroupSection({
  groupId,
  name,
  channels,
  collapsed,
  isDragging,
  isDropTarget,
  activeChannelId,
  editing,
  editingName,
  onToggle,
  onStartRename,
  onRenameChange,
  onRenameSubmit,
  onRenameCancel,
  onDelete,
  onSelect,
  isWorkspaceAdmin,
  onRequestDeleteChannel,
}: {
  groupId: string;
  name: string;
  channels: ChannelSummary[];
  collapsed: boolean;
  isDragging: boolean;
  isDropTarget: boolean;
  activeChannelId: string | null;
  editing: boolean;
  editingName: string;
  onToggle: () => void;
  onStartRename: () => void;
  onRenameChange: (v: string) => void;
  onRenameSubmit: () => void;
  onRenameCancel: () => void;
  onDelete: () => void;
  onSelect: (id: string) => void;
  isWorkspaceAdmin: boolean;
  onRequestDeleteChannel: (channel: ChannelSummary) => void;
}) {
  const { t } = useT("channels");
  const { setNodeRef } = useDroppable({ id: `group:${groupId}` });
  // The group header is the drag handle for reordering groups (one level —
  // never nesting). distance activation (5px) keeps a plain click as a
  // collapse toggle and a right-click as the context menu, so a drag must be a
  // deliberate move. Interactive children opt out by stopping pointerdown.
  const { attributes, listeners, setNodeRef: setDragRef, isDragging: isGroupDragging } = useDraggable({
    id: `group:${groupId}`,
    data: { label: name },
  });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "rounded-md transition-colors",
        isDropTarget && "bg-primary/10 ring-1 ring-primary/40",
        isGroupDragging && "opacity-50",
      )}
    >
      <ContextMenu>
        <ContextMenuTrigger>
          <div
            ref={setDragRef}
            {...attributes}
            {...listeners}
            aria-label={t($ => $.drag.aria_group, { name })}
            className="group/section flex h-7 cursor-grab items-center gap-1 rounded-md px-1 text-xs font-medium text-muted-foreground hover:bg-accent active:cursor-grabbing"
            onClick={onToggle}
          >
            <GripVertical className="h-3 w-3 shrink-0 cursor-grab text-muted-foreground/40 opacity-0 transition-opacity group-hover/section:opacity-100" />
            <ChevronRight className={cn("h-3 w-3 shrink-0 transition-transform", !collapsed && "rotate-90")} />
            {editing ? (
              <input
                autoFocus
                className="min-w-0 flex-1 rounded bg-background px-1 text-xs outline-none ring-1 ring-ring"
                value={editingName}
                onChange={(e) => onRenameChange(e.target.value)}
                onBlur={onRenameSubmit}
                onKeyDown={(e) => {
                  if (e.key === "Enter") onRenameSubmit();
                  if (e.key === "Escape") onRenameCancel();
                }}
                onClick={(e) => e.stopPropagation()}
                onPointerDown={(e) => e.stopPropagation()}
              />
            ) : (
              <span className="min-w-0 flex-1 truncate uppercase">{name}</span>
            )}
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onClick={onStartRename}>{t($ => $.context_menu.rename)}</ContextMenuItem>
          <ContextMenuItem className="text-destructive" onClick={onDelete}>{t($ => $.context_menu.delete_group)}</ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>
      {!collapsed && (
        <div className="space-y-0.5">
          {channels.map((ch) => (
            <SortableChannelItem
              key={ch.id}
              channel={ch}
              isActive={ch.id === activeChannelId}
              onSelect={onSelect}
              isWorkspaceAdmin={isWorkspaceAdmin}
              onRequestDelete={onRequestDeleteChannel}
            />
          ))}
          {channels.length === 0 && isDragging && isDropTarget && (
            <div
              className="mx-1 mb-0.5 ml-5 h-8 rounded-md border border-dashed border-primary/50 transition-colors"
            />
          )}
        </div>
      )}
    </div>
  );
}

function UngroupedDropZone({
  channels,
  isDragging,
  isDropTarget,
  activeChannelId,
  onSelect,
  isWorkspaceAdmin,
  onRequestDeleteChannel,
}: {
  channels: ChannelSummary[];
  isDragging: boolean;
  isDropTarget: boolean;
  activeChannelId: string | null;
  onSelect: (id: string) => void;
  isWorkspaceAdmin: boolean;
  onRequestDeleteChannel: (channel: ChannelSummary) => void;
}) {
  const { setNodeRef } = useDroppable({ id: UNGROUPED_DROP_ID });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "min-h-1 space-y-0.5 rounded-md transition-colors",
        isDropTarget && "bg-primary/10 ring-1 ring-primary/40",
      )}
    >
      {channels.map((ch) => (
        <SortableChannelItem
          key={ch.id}
          channel={ch}
          isActive={ch.id === activeChannelId}
          onSelect={onSelect}
          isWorkspaceAdmin={isWorkspaceAdmin}
          onRequestDelete={onRequestDeleteChannel}
        />
      ))}
      {channels.length === 0 && isDragging && (
        <div
          className={cn(
            "mx-1 h-8 rounded-md border border-dashed transition-colors",
            isDropTarget ? "border-primary/50" : "border-border/60",
          )}
        />
      )}
    </div>
  );
}

function SortableChannelItem({
  channel,
  isActive,
  onSelect,
  isWorkspaceAdmin,
  onRequestDelete,
}: {
  channel: ChannelSummary;
  isActive: boolean;
  onSelect: (id: string) => void;
  isWorkspaceAdmin: boolean;
  onRequestDelete: (channel: ChannelSummary) => void;
}) {
  const { t } = useT("channels");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: channel.id,
    data: { label: channel.name },
  });
  const style = { transform: CSS.Transform.toString(transform), transition };
  // Mirrors the backend DeleteChannel gate (canManage = wsAdmin || channelOwn):
  // workspace admin/owner, or this user is the channel's owner member.
  const canDelete = channel.member_role === "owner" || isWorkspaceAdmin;

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      aria-label={t($ => $.drag.aria_channel, { name: channel.name })}
      className={cn("group/item relative", isDragging && "opacity-50")}
    >
      <button
        onClick={() => onSelect(channel.id)}
        className={cn(
          "flex h-8 w-full items-center gap-2 rounded-md px-2 pr-8 text-left text-sm hover:bg-accent",
          isActive && "bg-accent font-medium",
          channel.group_id && "pl-5",
        )}
      >
        {channel.access_mode === "invite" ? (
          <Lock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <Hash className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className={cn("min-w-0 flex-1 truncate", channel.has_unread && !isActive && "font-medium")}>
          {channel.name}
        </span>
        {channel.has_unread && !isActive && (
          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-brand/60" />
        )}
      </button>
      {canDelete && (
        <button
          type="button"
          aria-label={t($ => $.delete_channel.aria_label, { name: channel.name })}
          onClick={(e) => {
            e.stopPropagation();
            onRequestDelete(channel);
          }}
          onPointerDown={(e) => e.stopPropagation()}
          className="absolute right-1 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-destructive/10 hover:text-destructive focus-visible:opacity-100 group-hover/item:opacity-100"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}

function ChannelHeader({
  channel,
  memberCount,
  onOpenMembers,
}: {
  channel: ChannelSummary;
  memberCount: number;
  onOpenMembers: () => void;
}) {
  return (
    <div className="flex h-11 shrink-0 items-center justify-between border-b px-4">
      <div className="flex min-w-0 items-center gap-2">
        {channel.access_mode === "invite" ? (
          <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
        ) : (
          <Hash className="h-4 w-4 shrink-0 text-muted-foreground" />
        )}
        <h1 className="truncate text-base font-semibold">{channel.name}</h1>
        {channel.description && (
          <span className="hidden truncate text-xs text-muted-foreground md:inline">
            {channel.description}
          </span>
        )}
      </div>
      <Button
        variant="ghost"
        size="sm"
        data-panel-trigger="channel-side-panel"
        onClick={onOpenMembers}
      >
        <Users className="mr-1 h-4 w-4" />
        {memberCount}
      </Button>
    </div>
  );
}

function MessageList({
  messages,
  loading,
  channelId,
  wsId,
  memberIds,
  onOpenReplies,
  qc,
  hasMore,
  loadMore,
  loadingMore,
  canPost = true,
  highlight = null,
  firstUnreadMessageId = null,
  hasUnread = false,
  onMarkRead,
  onJumpToMessage,
}: {
  messages: ChannelMessage[];
  loading: boolean;
  channelId: string;
  wsId: string;
  memberIds: string[];
  onOpenReplies: (id: string) => void;
  qc: ReturnType<typeof useQueryClient>;
  hasMore?: boolean;
  loadMore?: () => void;
  loadingMore?: boolean;
  canPost?: boolean;
  highlight?: ListChannelMessagesResponse["highlight"] | null;
  // First top-level message newer than the user's last_read_at (the read/unread
  // boundary). Drives the "jump to last read" landing + divider.
  firstUnreadMessageId?: string | null;
  hasUnread?: boolean;
  onMarkRead?: () => void;
  onJumpToMessage?: (messageId: string) => void;
}) {
  const { t } = useT("channels");
  const editorRef = useRef<ContentEditorRef>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  // The message list body is the release drop target: dragging a reply here
  // (onto a message or the padding) releases it to a top-level message. Merged
  // with scrollRef so the droppable covers the whole scrollable area without a
  // wrapper DOM node. Highlighted while a reply is being dragged to signal the
  // affordance — a main message dragged here is a no-op (already top-level).
  const listDrop = useDroppable({ id: MSG_LIST_DROP_ID });
  const dndCtx = useDndContext();
  const releaseActive = !!dndCtx.active && String(dndCtx.active.id).startsWith(REPLY_DRAG_PREFIX);
  const setListRef = useCallback(
    (node: HTMLDivElement | null) => {
      scrollRef.current = node;
      listDrop.setNodeRef(node);
    },
    [listDrop],
  );
  const { uploadWithToast } = useFileUpload(api, (err) => toast.error(err.message));
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => editorRef.current?.uploadFiles(files),
  });
  const [isEmpty, setIsEmpty] = useState(true);
  const prevMsgCount = useRef(messages.length);
  // Set to true when the current user sends a message so the live-follow
  // effect can scroll to bottom unconditionally (regardless of isNearBottom).
  const pendingSelfScrollRef = useRef(false);
  const paths = useWorkspacePaths();
  const nav = useNavigation();
  const linkedMessageId = nav.searchParams.get("message")?.trim() || null;
  const didScrollToLinked = useRef<string | null>(null);

  // Whether the first-unread message is in the loaded page. False when there
  // are more unreads than one page — the boundary then sits above the window
  // and the "jump to last read" control re-anchors the query around it.
  const firstUnreadInPage = !!firstUnreadMessageId && messages.some((m) => m.id === firstUnreadMessageId);
  const [showJumpToLastRead, setShowJumpToLastRead] = useState(false);

  const isNearBottom = useCallback((threshold = 120) => {
    const el = scrollRef.current;
    if (!el) return true;
    return el.scrollHeight - el.scrollTop - el.clientHeight < threshold;
  }, []);

  const landAt = useCallback((messageId: string | null) => {
    requestAnimationFrame(() => {
      const el = messageId ? document.getElementById(`channel-msg-${messageId}`) : null;
      if (el) scrollToElement(scrollRef.current, el);
      else scrollListToBottom(scrollRef.current);
    });
  }, []);

  // Entry: land at the read/unread boundary when resuming an unread channel
  // (so the user picks up where they left off), otherwise at the latest
  // message. Runs once per (channel, deep-link) — the guard keeps the boundary
  // scroll from re-firing as messages stream in.
  const entryKey = `${channelId}:${linkedMessageId ?? ""}`;
  const didEntry = useRef<string | null>(null);
  useEffect(() => {
    if (messages.length === 0 || didEntry.current === entryKey) return;
    if (linkedMessageId) { didEntry.current = entryKey; return; } // deep-link effect handles it
    didEntry.current = entryKey;
    const target = hasUnread && firstUnreadInPage ? firstUnreadMessageId : null;
    landAt(target);
  }, [entryKey, messages.length, linkedMessageId, hasUnread, firstUnreadInPage, firstUnreadMessageId, landAt]);

  // Reset the live-follow baseline on channel switch so the first page of the
  // new channel isn't mistaken for a newly-arrived message (which would yank to
  // the bottom and override the unread-boundary landing).
  useEffect(() => { prevMsgCount.current = 0; }, [channelId]);

  // Live follow: a new message arriving while the user is already pinned to the
  // bottom keeps them there. Don't yank if they've scrolled up to read history
  // or are parked at the unread boundary.
  // Exception: if the user just sent a message themselves (pendingSelfScrollRef),
  // always scroll to bottom so their own message is immediately visible.
  useEffect(() => {
    const prev = prevMsgCount.current;
    prevMsgCount.current = messages.length;
    if (linkedMessageId) return;
    if (messages.length > prev && prev > 0) {
      const selfSent = pendingSelfScrollRef.current;
      if (selfSent) {
        // Keep the flag true during the entire animation so updateJumpVisibility
        // suppresses the "jump to last read" indicator (the channel-list refetch
        // may set hasUnread=true before the messages refetch lands, and the
        // scroll may not have reached the bottom yet).
        requestAnimationFrame(() =>
          animateScrollToBottom(scrollRef.current, () => {
            pendingSelfScrollRef.current = false;
          }),
        );
      } else if (isNearBottom()) {
        requestAnimationFrame(() => scrollListToBottom(scrollRef.current));
        pendingSelfScrollRef.current = false;
      } else {
        pendingSelfScrollRef.current = false;
      }
    }
  }, [messages.length, linkedMessageId, isNearBottom]);

  // Catch-up: clear unread once the user reaches the bottom (seen all new
  // messages). Also fires when a live message lands while already at the
  // bottom — the live-follow effect above keeps them pinned, so this clears it.
  // Skip when the unread boundary sits above the loaded window (many unread):
  // the user landed at the latest but hasn't seen the boundary yet, so the
  // "jump to last read" control must stay until they actually scroll back to it.
  useEffect(() => {
    if (!hasUnread || !onMarkRead || messages.length === 0) return;
    if (firstUnreadMessageId && !firstUnreadInPage) return;
    if (isNearBottom()) onMarkRead();
  }, [messages.length, hasUnread, onMarkRead, firstUnreadMessageId, firstUnreadInPage, isNearBottom]);

  // Floating "jump to last read" control: show when the unread boundary is out
  // of view (scrolled past it) or above the loaded window (many unread).
  const updateJumpVisibility = useCallback(() => {
    if (!hasUnread || !firstUnreadMessageId) { setShowJumpToLastRead(false); return; }
    // Suppress during self-sent scroll: the channel-list refetch may set
    // hasUnread=true before the messages refetch lands (!firstUnreadInPage),
    // and the smooth scroll may not have reached the bottom yet. The flag
    // stays true for the entire animation (cleared in animateScrollToBottom's
    // onDone callback).
    if (pendingSelfScrollRef.current) { setShowJumpToLastRead(false); return; }
    if (!firstUnreadInPage) { setShowJumpToLastRead(true); return; }
    // At the bottom: the user has seen all messages and onMarkRead is about to
    // fire. Suppress the indicator so it doesn't flash during the brief window
    // between the scroll-to-bottom and hasUnread clearing.
    if (isNearBottom()) { setShowJumpToLastRead(false); return; }
    const el = document.getElementById(`channel-msg-${firstUnreadMessageId}`);
    const container = scrollRef.current;
    if (!el || !container) { setShowJumpToLastRead(false); return; }
    const c = container.getBoundingClientRect();
    const e = el.getBoundingClientRect();
    setShowJumpToLastRead(e.bottom < c.top || e.top > c.bottom);
  }, [hasUnread, firstUnreadMessageId, firstUnreadInPage, isNearBottom]);

  useEffect(() => {
    updateJumpVisibility();
  }, [messages.length, hasUnread, firstUnreadMessageId, firstUnreadInPage, updateJumpVisibility]);

  const pendingJumpRef = useRef<string | null>(null);
  const handleJumpToLastRead = useCallback(() => {
    if (!firstUnreadMessageId) return;
    if (firstUnreadInPage) {
      const el = document.getElementById(`channel-msg-${firstUnreadMessageId}`);
      if (el) { scrollToElement(scrollRef.current, el); return; }
    }
    // Boundary is older than the loaded window — re-anchor the query around it,
    // then scroll to it once the new page arrives (effect below).
    pendingJumpRef.current = firstUnreadMessageId;
    onJumpToMessage?.(firstUnreadMessageId);
  }, [firstUnreadMessageId, firstUnreadInPage, onJumpToMessage]);

  // After a "jump to last read" re-anchor, scroll to the boundary once it loads.
  useEffect(() => {
    const target = pendingJumpRef.current;
    if (!target || !firstUnreadInPage) return;
    const el = document.getElementById(`channel-msg-${target}`);
    if (!el) return;
    pendingJumpRef.current = null;
    requestAnimationFrame(() => scrollToElement(scrollRef.current, el));
  }, [messages.length, firstUnreadInPage]);

  useEffect(() => {
    if (messages.length === 0) return;
    // Reply deep-link (?around targeted a reply): center the window on the
    // thread root message, then auto-open its reply panel so the reply —
    // which lives inside the panel, not the top-level list — is reachable.
    // The RepliesPanel handles scrolling to / highlighting the reply itself.
    if (highlight?.root_message_id) {
      if (didScrollToLinked.current === highlight.message_id) return;
      const el = document.getElementById(`channel-msg-${highlight.root_message_id}`);
      if (!el) return;
      didScrollToLinked.current = highlight.message_id;
      // Open the replies panel first, then center the root message. The panel
      // narrows the main list from full-width to (width - 360px), which reflows
      // every message and shifts the root's vertical position. Centering before
      // the panel mounts scrolls against pre-reflow coordinates and lands off
      // target; the double rAF waits for the panel to mount + paint so the
      // offsets we read are the final ones.
      onOpenReplies(highlight.root_message_id);
      requestAnimationFrame(() =>
        requestAnimationFrame(() => scrollToElement(scrollRef.current, el)),
      );
      return;
    }
    // Top-level deep-link: scroll to + briefly highlight the message.
    if (!linkedMessageId) return;
    if (didScrollToLinked.current === linkedMessageId) return;
    const el = document.getElementById(`channel-msg-${linkedMessageId}`);
    if (!el) return;
    didScrollToLinked.current = linkedMessageId;
    requestAnimationFrame(() => {
      scrollToElement(scrollRef.current, el);
      el.classList.add("ring-2", "ring-brand/50", "rounded-md");
      setTimeout(() => el.classList.remove("ring-2", "ring-brand/50", "rounded-md"), 3000);
    });
  }, [linkedMessageId, messages.length, highlight, onOpenReplies]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (hasMore && !loadingMore && el.scrollTop < 80) {
      loadMore?.();
    }
    const boundaryAboveWindow = !!(firstUnreadMessageId && !firstUnreadInPage);
    if (hasUnread && onMarkRead && !boundaryAboveWindow && isNearBottom()) onMarkRead();
    updateJumpVisibility();
  }, [hasMore, loadingMore, loadMore, hasUnread, onMarkRead, firstUnreadMessageId, firstUnreadInPage, isNearBottom, updateJumpVisibility]);

  const handleCopyLink = useCallback(async (msg: ChannelMessage) => {
    const url = nav.getShareableUrl(paths.channelDetail(channelId, { messageId: msg.id }));
    if (await copyText(url)) {
      toast.success(t($ => $.link_copied));
    } else {
      toast.error(t($ => $.copy_failed));
    }
  }, [channelId, nav, paths, t]);

  const sendMutation = useMutation({
    mutationFn: (content: string) => api.sendChannelMessage(channelId, { content }),
    // Set the self-sent flag before the HTTP request starts — not in onSuccess.
    // If the message triggers an agent task, the server emits a task:queued WS
    // event that invalidates channelMessages. That event can arrive (and its
    // refetch can complete, delivering the new message) before the HTTP response
    // triggers onSuccess. Setting the flag in onMutate ensures it's ready
    // regardless of which path delivers the message to the list.
    onMutate: () => { pendingSelfScrollRef.current = true; },
    onSuccess: () => {
      editorRef.current?.clearContent();
      setIsEmpty(true);
      qc.invalidateQueries({ queryKey: channelKeys.channelMessages(wsId, channelId) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
    onError: () => {
      pendingSelfScrollRef.current = false;
      toast.error(t($ => $.send_failed));
    },
  });

  const openModal = useModalStore((s) => s.open);

  const handleSend = useCallback(() => {
    const content = editorRef.current?.getMarkdown().trim();
    if (!content || editorRef.current?.hasActiveUploads()) return;
    sendMutation.mutate(content);
  }, [sendMutation]);

  if (loading) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <>
      <div className="relative min-h-0 flex-1">
        <div ref={setListRef} onScroll={handleScroll} className={cn("absolute inset-0 overflow-y-auto px-4 py-3", releaseActive && "ring-2 ring-inset ring-brand/30")}>
          {loadingMore && (
            <div className="flex justify-center py-2">
              <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
            </div>
          )}
          {messages.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              {t($ => $.no_messages)}
            </div>
          ) : (
            <ul className="space-y-3">
              {messages.map((msg) => (
                <Fragment key={msg.id}>
                  {hasUnread && firstUnreadMessageId === msg.id && (
                    <li className="flex items-center gap-3 py-1" aria-label={t($ => $.new_messages)}>
                      <span className="h-px flex-1 bg-brand/40" />
                      <span className="text-xs font-medium text-brand">{t($ => $.new_messages)}</span>
                      <span className="h-px flex-1 bg-brand/40" />
                    </li>
                  )}
                  <MessageRow
                    message={msg}
                    onOpenReplies={onOpenReplies}
                    onCopyLink={handleCopyLink}
                    onConvertManual={(m) =>
                      openModal("create-issue", {
                        description: m.content,
                        source_channel_id: channelId,
                        source_message_id: m.id,
                      })
                    }
                    onConvertAgent={(m) =>
                      openModal("quick-create-issue", {
                        prompt: m.content,
                        source_channel_id: channelId,
                        source_message_id: m.id,
                      })
                    }
                  />
                </Fragment>
              ))}
            </ul>
          )}
        </div>
        {showJumpToLastRead && hasUnread && firstUnreadMessageId && (
          <button
            type="button"
            onClick={handleJumpToLastRead}
            className="absolute bottom-3 left-1/2 z-10 flex -translate-x-1/2 items-center gap-1 rounded-full border bg-card px-3 py-1.5 text-xs font-medium shadow-md ring-1 ring-border transition-colors hover:bg-accent"
          >
            <ArrowUp className="h-3.5 w-3.5" />
            {t($ => $.jump_to_last_read)}
          </button>
        )}
      </div>
      {canPost ? (
        <div className="border-t px-4 py-3">
          <div
            {...dropZoneProps}
            className="relative flex min-h-16 max-h-44 flex-col rounded-lg border bg-card pb-9 focus-within:border-brand"
          >
            <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
              <ContentEditor
                ref={editorRef}
                placeholder={t($ => $.input_placeholder)}
                onUpdate={(md) => setIsEmpty(!md.trim())}
                onSubmit={handleSend}
                onUploadFile={(file) => uploadWithToast(file)}
                debounceMs={100}
                showBubbleMenu={false}
                mentionScope={{ memberIds }}
              />
            </div>
            <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
              <FileUploadButton
                size="sm"
                multiple
                onSelect={(file) => editorRef.current?.uploadFile(file)}
                onSelectMany={(files) => editorRef.current?.uploadFiles(files)}
              />
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      size="icon"
                      className="h-7 w-7"
                      onClick={handleSend}
                      disabled={isEmpty || sendMutation.isPending}
                    >
                      {sendMutation.isPending ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Send className="h-3.5 w-3.5" />
                      )}
                    </Button>
                  }
                />
                <TooltipContent side="top">
                  {t($ => $.send_tooltip, { shortcut: formatShortcut(modKey, enterKey) })}
                </TooltipContent>
              </Tooltip>
            </div>
            {isDragOver && <FileDropOverlay />}
          </div>
        </div>
      ) : (
        <div className="border-t px-4 py-3">
          <p className="text-center text-sm text-muted-foreground">
            {t($ => $.invite_only_notice)}
          </p>
        </div>
      )}

    </>
  );
}

function MessageRow({
  message,
  onOpenReplies,
  onConvertManual,
  onConvertAgent,
  onCopyLink,
}: {
  message: ChannelMessage;
  onOpenReplies: (id: string) => void;
  onConvertManual: (msg: ChannelMessage) => void;
  onConvertAgent: (msg: ChannelMessage) => void;
  onCopyLink: (msg: ChannelMessage) => void;
}) {
  const { t } = useT("channels");
  const isSystem = message.author_type === "system" || !message.author_id;
  const authorId = message.author_id ?? "";
  // Draggable (long-press to converge into another thread) + droppable (a
  // converge target). System activity rows stay non-interactive for DnD by
  // skipping the listeners, but keep a stable droppable id so a stray drop
  // resolves cleanly.
  const dragId = `${MSG_DRAG_PREFIX}${message.id}`;
  const drag = useDraggable({ id: dragId, data: { message } });
  const drop = useDroppable({ id: dragId });
  const dnd = useDndContext();
  const activeId = dnd.active ? String(dnd.active.id) : null;
  const overId = dnd.over ? String(dnd.over.id) : null;
  const isDragging = activeId === dragId;
  const isConvergeTarget =
    !isDragging && overId === dragId && activeId != null && activeId.startsWith(MSG_DRAG_PREFIX);
  const setRowRef = useCallback(
    (node: HTMLLIElement | null) => {
      drag.setNodeRef(node);
      drop.setNodeRef(node);
    },
    [drag, drop],
  );
  const dragProps = isSystem ? {} : { ...drag.attributes, ...drag.listeners };
  return (
    <ContextMenu>
      <ContextMenuTrigger className="block select-text">
        <li
          ref={setRowRef}
          id={`channel-msg-${message.id}`}
          aria-label={
            isSystem ? undefined : t($ => $.drag.aria_message, { preview: previewOf(message.content) ?? "" })
          }
          {...dragProps}
          className={cn(
            "group flex cursor-grab items-start gap-3 rounded-md p-2 hover:bg-muted/50 active:cursor-grabbing",
            isSystem && "bg-muted/25 cursor-default",
            isDragging && "opacity-40",
            isConvergeTarget && "ring-2 ring-brand/50 bg-brand/5",
          )}
        >
          {isSystem ? (
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
              <Settings className="h-4 w-4" />
            </div>
          ) : (
            <ActorAvatar
              actorType={message.author_type}
              actorId={authorId}
              size={32}
              enableHoverCard
            />
          )}
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-baseline gap-2">
              <span className="text-sm font-medium">
                {message.author_name ?? (isSystem ? "Multica" : message.author_type)}
              </span>
              {message.author_type === "agent" && (
                <Badge variant="secondary" className="px-1.5 py-0 text-[10px]">Agent</Badge>
              )}
              <span className="text-xs text-muted-foreground">{formatTime(message.created_at)}</span>
            </div>
            <ReadonlyContent
              content={message.content}
              className="mt-1 select-text break-words text-foreground/90"
            />
            {(message.reply_count ?? 0) > 0 && (
              <button
                data-panel-trigger="channel-side-panel"
                onClick={() => onOpenReplies(message.id)}
                onPointerDown={(e) => e.stopPropagation()}
                className="mt-1 text-xs text-primary hover:underline"
              >
                {t($ => $.replies_count, { count: message.reply_count ?? 0 })}
              </button>
            )}
            <ChannelAgentTaskStrip tasks={message.agent_tasks ?? []} />
            <ChannelLinkedIssuesStrip issues={message.issues ?? []} />
          </div>
        </li>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onClick={() => onCopyLink(message)}>
          <Link className="mr-2 h-4 w-4" />
          {t($ => $.context_menu.copy_link)}
        </ContextMenuItem>
        <ContextMenuItem onClick={() => onOpenReplies(message.id)}>
          <MessageCircleReply className="mr-2 h-4 w-4" />
          {t($ => $.context_menu.reply)}
        </ContextMenuItem>
        <ContextMenuItem onClick={() => onConvertManual(message)}>
          <FileText className="mr-2 h-4 w-4" />
          {t($ => $.context_menu.convert_issue)}
        </ContextMenuItem>
        <ContextMenuItem onClick={() => onConvertAgent(message)}>
          <Bot className="mr-2 h-4 w-4" />
          {t($ => $.context_menu.convert_agent)}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

function ChannelAgentTaskStrip({ tasks }: { tasks: ChannelMessage["agent_tasks"] }) {
  const { t } = useT("channels");
  const channelTaskStatusLabel = useChannelTaskStatusLabel();
  const [cancelTaskId, setCancelTaskId] = useState<string | null>(null);
  const [cancelling, setCancelling] = useState(false);

  const handleConfirmCancel = useCallback(async () => {
    if (!cancelTaskId) return;
    setCancelling(true);
    try {
      await api.cancelTaskById(cancelTaskId);
    } finally {
      setCancelling(false);
      setCancelTaskId(null);
    }
  }, [cancelTaskId]);

  if (!tasks || tasks.length === 0) return null;
  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5">
      {tasks.map((task) => {
        const cfg = taskStatusConfig[task.status] ?? taskStatusConfig.queued!;
        const Icon = cfg.icon;
        const isActive = task.status === "running" || task.status === "dispatched" || task.status === "queued";
        const isRunning = task.status === "running";
        const agentName = task.agent_name || "Agent";
        return (
          <div
            key={task.id}
            className={cn(
              "inline-flex h-7 max-w-full items-center gap-1.5 rounded-md border bg-background px-2 text-xs text-muted-foreground",
              isRunning && "border-brand/40 bg-brand/5 text-foreground",
              task.status === "failed" && "border-destructive/30 bg-destructive/5",
            )}
          >
            <Bot className="h-3.5 w-3.5 shrink-0" />
            <span className="max-w-32 truncate text-foreground/85">{agentName}</span>
            <Icon className={cn("h-3.5 w-3.5 shrink-0", cfg.color, isRunning && "animate-spin")} />
            <span className="shrink-0">{channelTaskStatusLabel(task.status)}</span>
            {task.status !== "queued" && (
              <TranscriptButton
                task={task}
                agentName={agentName}
                isLive={isRunning}
                className="-mr-1 h-5 w-5 p-0"
                title={t($ => $.task_strip.view_history)}
              />
            )}
            {isActive && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      className="-mr-1 inline-flex h-5 w-5 items-center justify-center rounded text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                      onClick={() => setCancelTaskId(task.id)}
                      disabled={cancelling}
                    >
                      {cancelling && cancelTaskId === task.id ? (
                        <Loader2 className="h-3 w-3 animate-spin" />
                      ) : (
                        <Square className="h-3 w-3" />
                      )}
                    </button>
                  }
                />
                <TooltipContent side="top">{t($ => $.task_strip.stop)}</TooltipContent>
              </Tooltip>
            )}
          </div>
        );
      })}
      <TerminateTaskConfirmDialog
        open={!!cancelTaskId}
        onOpenChange={(open) => { if (!open) setCancelTaskId(null); }}
        onConfirm={handleConfirmCancel}
        showRunningNote={tasks.some((t) => t.id === cancelTaskId && t.status === "running")}
      />
    </div>
  );
}

// ChannelLinkedIssuesStrip renders the issues produced from this message's
// thread as clickable chips — the channel-side half of the OPE-1943
// bidirectional display. Each chip links to the issue detail page.
function ChannelLinkedIssuesStrip({ issues }: { issues: ChannelMessage["issues"] }) {
  const paths = useWorkspacePaths();
  if (!issues || issues.length === 0) return null;
  return (
    <div className="mt-1.5 flex flex-wrap gap-1.5">
      {issues.map((issue) => (
        <AppLink
          key={issue.id}
          href={paths.issueDetail(issue.identifier ?? issue.id)}
          // Stop propagation so the surrounding message ContextMenuTrigger
          // doesn't capture the right-click — the native menu must surface for
          // "Copy link" to work on the chip's href.
          onContextMenu={(e) => e.stopPropagation()}
          className="inline-flex items-center gap-1 rounded-md border bg-muted/30 px-1.5 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
        >
          <FileText className="h-3 w-3" />
          <span className="font-medium text-foreground">{issue.identifier ?? `#${issue.number}`}</span>
          <span className="max-w-[12rem] truncate">{issue.title}</span>
        </AppLink>
      ))}
    </div>
  );
}

function useChannelTaskStatusLabel() {
  const { t } = useT("channels");
  return (status: string): string => {
    switch (status) {
      case "queued":
        return t($ => $.task_status.queued);
      case "dispatched":
        return t($ => $.task_status.dispatched);
      case "waiting_local_directory":
        return t($ => $.task_status.waiting_directory);
      case "running":
        return t($ => $.task_status.running);
      case "completed":
        return t($ => $.task_status.completed);
      case "failed":
        return t($ => $.task_status.failed);
      case "cancelled":
        return t($ => $.task_status.cancelled);
      default:
        return status;
    }
  };
}

function RepliesPanel({
  channelId,
  messageId,
  data,
  loading,
  wsId,
  memberIds,
  qc,
  onClose,
  highlightMessageId = null,
}: {
  channelId: string;
  messageId: string;
  data: MessageThreadResponse | undefined;
  loading: boolean;
  wsId: string;
  memberIds: string[];
  qc: ReturnType<typeof useQueryClient>;
  onClose: () => void;
  highlightMessageId?: string | null;
}) {
  const { t } = useT("channels");
  const editorRef = useRef<ContentEditorRef>(null);
  const { uploadWithToast } = useFileUpload(api, (err) => toast.error(err.message));
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => editorRef.current?.uploadFiles(files),
  });
  const [isEmpty, setIsEmpty] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const paths = useWorkspacePaths();
  const nav = useNavigation();
  const openModal = useModalStore((s) => s.open);

  const handleCopyLink = useCallback(async (msg: ChannelMessage) => {
    const url = nav.getShareableUrl(paths.channelDetail(channelId, { messageId: msg.id }));
    if (await copyText(url)) {
      toast.success(t($ => $.link_copied));
    } else {
      toast.error(t($ => $.copy_failed));
    }
  }, [channelId, nav, paths, t]);

  const handleConvertManual = useCallback((msg: ChannelMessage) => {
    openModal("create-issue", {
      description: msg.content,
      source_channel_id: channelId,
      source_message_id: msg.id,
    });
  }, [channelId, openModal]);

  const handleConvertAgent = useCallback((msg: ChannelMessage) => {
    openModal("quick-create-issue", {
      prompt: msg.content,
      source_channel_id: channelId,
      source_message_id: msg.id,
    });
  }, [channelId, openModal]);

  const replyMutation = useMutation({
    mutationFn: (content: string) => api.replyToMessage(channelId, messageId, { content }),
    onSuccess: () => {
      editorRef.current?.clearContent();
      setIsEmpty(true);
      qc.invalidateQueries({ queryKey: channelKeys.messageThread(wsId, channelId, messageId) });
      qc.invalidateQueries({ queryKey: channelKeys.channelMessages(wsId, channelId) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
    onError: (err) => {
      // Surface the backend's specific failure reason (ApiError.message carries
      // the server's `error` string) instead of a generic label, so intermittent
      // reply failures on specific messages are diagnosable.
      const reason = err instanceof Error && err.message ? err.message : t($ => $.reply_failed);
      toast.error(reason);
    },
  });

  const handleReply = useCallback(() => {
    const content = editorRef.current?.getMarkdown().trim();
    if (!content || editorRef.current?.hasActiveUploads()) return;
    replyMutation.mutate(content);
  }, [replyMutation]);

  const didHighlightReply = useRef<string | null>(null);
  // Scroll to + briefly highlight the deep-linked reply once the thread data
  // has loaded. Only fires when this panel was opened via a reply deep-link
  // (highlightMessageId set by the ?around reply-resolution path).
  useEffect(() => {
    if (!highlightMessageId || !data || didHighlightReply.current === highlightMessageId) return;
    const el = document.getElementById(`channel-reply-${highlightMessageId}`);
    if (!el) return;
    didHighlightReply.current = highlightMessageId;
    requestAnimationFrame(() => {
      scrollToElement(scrollRef.current, el);
      el.classList.add("ring-2", "ring-brand/50", "rounded-md");
      setTimeout(() => el.classList.remove("ring-2", "ring-brand/50", "rounded-md"), 3000);
    });
  }, [highlightMessageId, data]);

  return (
    <div className="flex h-full min-w-0 flex-col border-l bg-background shadow-sm">
      <PanelHeader title={t($ => $.panel_header.replies)} onClose={onClose} />
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : data ? (
          <div className="space-y-4">
            <PanelMessage
              message={data.root_message}
              framed
              onCopyLink={handleCopyLink}
              onConvertManual={handleConvertManual}
              onConvertAgent={handleConvertAgent}
            />
            <div className="space-y-3">
              {data.replies.length > 0 ? (
                data.replies.map((reply) => (
                  <PanelMessage
                    key={reply.id}
                    message={reply}
                    onCopyLink={handleCopyLink}
                    onConvertManual={handleConvertManual}
                    onConvertAgent={handleConvertAgent}
                  />
                ))
              ) : (
                <p className="py-4 text-center text-xs text-muted-foreground">{t($ => $.no_replies)}</p>
              )}
            </div>
          </div>
        ) : (
          <p className="py-4 text-center text-xs text-muted-foreground">{t($ => $.no_replies)}</p>
        )}
      </div>
      <div className="border-t px-3 py-3">
        <div
          {...dropZoneProps}
          className="relative flex min-h-16 max-h-40 flex-col rounded-lg border bg-card pb-8 focus-within:border-brand"
        >
          <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
            <ContentEditor
              ref={editorRef}
              placeholder={t($ => $.reply_placeholder)}
              onUpdate={(md) => setIsEmpty(!md.trim())}
              onSubmit={handleReply}
              onUploadFile={(file) => uploadWithToast(file)}
              debounceMs={100}
              showBubbleMenu={false}
              mentionScope={{ memberIds }}
            />
          </div>
          <div className="absolute bottom-1 right-1.5 flex items-center gap-1">
            <FileUploadButton
              size="sm"
              multiple
              onSelect={(file) => editorRef.current?.uploadFile(file)}
              onSelectMany={(files) => editorRef.current?.uploadFiles(files)}
            />
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    size="icon"
                    className="h-7 w-7"
                    onClick={handleReply}
                    disabled={isEmpty || replyMutation.isPending}
                  >
                    {replyMutation.isPending ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Send className="h-3.5 w-3.5" />
                    )}
                  </Button>
                }
              />
              <TooltipContent side="top">
                {t($ => $.send_tooltip, { shortcut: formatShortcut(modKey, enterKey) })}
              </TooltipContent>
            </Tooltip>
          </div>
          {isDragOver && <FileDropOverlay />}
        </div>
      </div>
    </div>
  );
}

function PanelMessage({
  message,
  framed,
  onCopyLink,
  onConvertManual,
  onConvertAgent,
}: {
  message?: ChannelMessage;
  framed?: boolean;
  onCopyLink?: (msg: ChannelMessage) => void;
  onConvertManual?: (msg: ChannelMessage) => void;
  onConvertAgent?: (msg: ChannelMessage) => void;
}) {
  const { t } = useT("channels");
  // A reply (non-framed, authored) is draggable to release it back to the
  // top-level timeline. The framed root is already top-level and system rows
  // are activity logs, so neither is draggable. Hooks run unconditionally with
  // a stable id; `disabled` opts the row out without breaking hook order.
  const msgId = message?.id ?? "panel-msg-none";
  const canDragReply = !!message && !framed && message.author_type !== "system" && !!message.author_id;
  const drag = useDraggable({
    id: `${REPLY_DRAG_PREFIX}${msgId}`,
    data: { message },
    disabled: !canDragReply,
  });
  const dnd = useDndContext();
  const isDragging = dnd.active != null && String(dnd.active.id) === `${REPLY_DRAG_PREFIX}${msgId}`;
  if (!message) return null;
  const isSystem = message.author_type === "system" || !message.author_id;
  const authorId = message.author_id ?? "";

  const content = (
    <div
      ref={drag.setNodeRef}
      id={`channel-reply-${message.id}`}
      aria-label={canDragReply ? t($ => $.drag.aria_reply, { preview: previewOf(message.content) ?? "" }) : undefined}
      {...(canDragReply ? { ...drag.attributes, ...drag.listeners } : {})}
      className={cn(
        "flex items-start gap-2",
        framed && "rounded-md border bg-muted/30 p-2",
        canDragReply && "cursor-grab active:cursor-grabbing",
        isDragging && "opacity-40",
      )}
    >
      {isSystem ? (
        <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <Settings className="h-3.5 w-3.5" />
        </div>
      ) : (
        <ActorAvatar
          actorType={message.author_type}
          actorId={authorId}
          size={24}
          enableHoverCard
        />
      )}
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-1.5">
          <span className="text-xs font-medium">
            {message.author_name ?? (isSystem ? "Multica" : message.author_type)}
          </span>
          <span className="text-[10px] text-muted-foreground">{formatTime(message.created_at)}</span>
        </div>
        <ReadonlyContent content={message.content} className="mt-1 select-text break-words text-sm" />
        <ChannelAgentTaskStrip tasks={message.agent_tasks ?? []} />
        <ChannelLinkedIssuesStrip issues={message.issues ?? []} />
      </div>
    </div>
  );

  if (!onCopyLink && !onConvertManual && !onConvertAgent) return content;

  return (
    <ContextMenu>
      <ContextMenuTrigger className="block select-text">
        {content}
      </ContextMenuTrigger>
      <ContextMenuContent>
        {onCopyLink && (
          <ContextMenuItem onClick={() => onCopyLink(message)}>
            <Link className="mr-2 h-4 w-4" />
            {t($ => $.context_menu.copy_link)}
          </ContextMenuItem>
        )}
        {onConvertManual && (
          <ContextMenuItem onClick={() => onConvertManual(message)}>
            <FileText className="mr-2 h-4 w-4" />
            {t($ => $.context_menu.convert_issue)}
          </ContextMenuItem>
        )}
        {onConvertAgent && (
          <ContextMenuItem onClick={() => onConvertAgent(message)}>
            <Bot className="mr-2 h-4 w-4" />
            {t($ => $.context_menu.convert_agent)}
          </ContextMenuItem>
        )}
      </ContextMenuContent>
    </ContextMenu>
  );
}

function ChannelMembersPanel({
  channel,
  members,
  workspaceMembers,
  canManage,
  wsId,
  qc,
  onClose,
}: {
  channel: ChannelSummary;
  members: ChannelMember[];
  workspaceMembers: MemberWithUser[];
  canManage: boolean;
  wsId: string;
  qc: ReturnType<typeof useQueryClient>;
  onClose: () => void;
}) {
  const { t } = useT("channels");
  const [query, setQuery] = useState("");
  const memberUserIds = useMemo(() => new Set(members.map((m) => m.user_id)), [members]);
  const ownerCount = members.filter((m) => m.role === "owner").length;

  const addMutation = useMutation({
    mutationFn: (userId: string) => api.addChannelMember(channel.id, { user_id: userId }),
    onSuccess: () => {
      toast.success(t($ => $.member_added));
      qc.invalidateQueries({ queryKey: channelKeys.members(wsId, channel.id) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
    onError: () => toast.error(t($ => $.add_failed)),
  });

  const removeMutation = useMutation({
    mutationFn: (userId: string) => api.removeChannelMember(channel.id, userId),
    onSuccess: () => {
      toast.success(t($ => $.member_removed));
      qc.invalidateQueries({ queryKey: channelKeys.members(wsId, channel.id) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
    onError: () => toast.error(t($ => $.remove_failed)),
  });

  const updateMutation = useMutation({
    mutationFn: (accessMode: "open" | "invite") =>
      api.updateChannel(channel.id, { access_mode: accessMode }),
    onSuccess: () => {
      toast.success(t($ => $.settings_updated));
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: channelKeys.detail(wsId, channel.id) });
    },
    onError: () => toast.error(t($ => $.update_failed)),
  });

  const candidates = useMemo(
    () =>
      workspaceMembers
        .filter((member) => matchesMember(member, query))
        .sort((a, b) => a.name.localeCompare(b.name))
        .slice(0, 80),
    [query, workspaceMembers],
  );

  const isInviteMode = channel.access_mode === "invite";

  return (
    <div className="flex h-full min-w-0 flex-col border-l bg-background shadow-sm">
      <PanelHeader title={isInviteMode ? t($ => $.panel_header.members_settings) : t($ => $.panel_header.participants)} onClose={onClose} />
      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {isInviteMode && (
          <>
            <section className="space-y-2">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <h3 className="text-sm font-medium">{t($ => $.members.channel_nature)}</h3>
                  <p className="text-xs text-muted-foreground">{t($ => $.members.nature_description)}</p>
                </div>
                <Select
                  value={channel.access_mode}
                  onValueChange={(value) => updateMutation.mutate(value as "open" | "invite")}
                  disabled={!canManage || updateMutation.isPending}
                >
                  <SelectTrigger size="sm" className="w-24">
                    <SelectValue>{channel.access_mode === "open" ? t($ => $.create_channel_dialog.open) : t($ => $.create_channel_dialog.invite)}</SelectValue>
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectItem value="open">{t($ => $.create_channel_dialog.open)}</SelectItem>
                    <SelectItem value="invite">{t($ => $.create_channel_dialog.invite)}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </section>
            <div className="my-4 h-px bg-border" />
          </>
        )}

        <section className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium">{t($ => $.members.current_members)}</h3>
            <Badge variant="outline" className="text-[10px]">{members.length}</Badge>
          </div>
          <ul className="space-y-1.5">
            {members.map((member) => {
              const isOnlyOwner = member.role === "owner" && ownerCount <= 1;
              const disableRemove = !canManage || isOnlyOwner || removeMutation.isPending;
              return (
                <li
                  key={member.user_id}
                  className="flex items-center justify-between gap-2 rounded-md px-2 py-1.5 hover:bg-muted/50"
                >
                  <div className="flex min-w-0 items-center gap-2">
                    <ActorAvatar actorType="member" actorId={member.user_id} size={28} enableHoverCard />
                    <div className="min-w-0">
                      <div className="truncate text-sm">{member.user_name || member.user_email}</div>
                      <div className="truncate text-xs text-muted-foreground">{member.user_email}</div>
                    </div>
                    <Badge variant="outline" className="shrink-0 text-[10px]">{member.role}</Badge>
                  </div>
                  {isInviteMode && (disableRemove ? (
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <span>
                            <Button variant="ghost" size="sm" className="h-7 text-xs" disabled>
                              {t($ => $.members.remove)}
                            </Button>
                          </span>
                        }
                      />
                      <TooltipContent side="left">
                        {!canManage ? t($ => $.members.cannot_remove_admin) : t($ => $.members.cannot_remove_only_owner)}
                      </TooltipContent>
                    </Tooltip>
                  ) : (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 text-xs text-destructive"
                      onClick={() => removeMutation.mutate(member.user_id)}
                    >
                      {t($ => $.members.remove)}
                    </Button>
                  ))}
                </li>
              );
            })}
          </ul>
        </section>

        {isInviteMode && (
          <>
            <div className="my-4 h-px bg-border" />

            <section className="space-y-2">
              <div>
                <h3 className="text-sm font-medium">{t($ => $.members.add_member)}</h3>
                <p className="text-xs text-muted-foreground">{t($ => $.members.search_description)}</p>
              </div>
              <div className="relative">
                <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder={t($ => $.members.search_placeholder)}
                  className="h-8 pl-7 text-sm"
                />
              </div>
              <ul className="space-y-1.5">
                {candidates.map((member) => {
                  const alreadyInChannel = memberUserIds.has(member.user_id);
                  return (
                    <li
                      key={member.user_id}
                      className="flex items-center justify-between gap-2 rounded-md px-2 py-1.5 hover:bg-muted/50"
                    >
                      <div className="flex min-w-0 items-center gap-2">
                        <ActorAvatar actorType="member" actorId={member.user_id} size={28} enableHoverCard />
                        <div className="min-w-0">
                          <div className="truncate text-sm">{member.name || member.email}</div>
                          <div className="truncate text-xs text-muted-foreground">{member.email}</div>
                        </div>
                      </div>
                      <Button
                        variant={alreadyInChannel ? "outline" : "secondary"}
                        size="sm"
                        className="h-7 text-xs"
                        disabled={!canManage || alreadyInChannel || addMutation.isPending}
                        onClick={() => addMutation.mutate(member.user_id)}
                      >
                        {alreadyInChannel ? t($ => $.members.already_added) : t($ => $.members.add)}
                      </Button>
                    </li>
                  );
                })}
                {candidates.length === 0 && (
                  <li className="py-6 text-center text-xs text-muted-foreground">{t($ => $.members.no_match)}</li>
                )}
              </ul>
            </section>
          </>
        )}
      </div>
    </div>
  );
}

function PanelHeader({ title, onClose }: { title: string; onClose: () => void }) {
  return (
    <div className="flex h-11 shrink-0 items-center justify-between border-b px-3">
      <h3 className="text-sm font-semibold">{title}</h3>
      <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onClose}>
        <X className="h-4 w-4" />
      </Button>
    </div>
  );
}

function CreateChannelDialog({
  open,
  onClose,
  wsId,
  qc,
}: {
  open: boolean;
  onClose: () => void;
  wsId: string;
  qc: ReturnType<typeof useQueryClient>;
}) {
  const { t } = useT("channels");
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [access, setAccess] = useState<"open" | "invite">("open");
  const nav = useNavigation();
  const paths = useWorkspacePaths();

  const createMutation = useMutation({
    mutationFn: () => api.createChannel({ name, description: desc || undefined, access_mode: access }),
    onSuccess: (ch) => {
      toast.success(t($ => $.channel_created));
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      onClose();
      setName("");
      setDesc("");
      setAccess("open");
      nav.push(paths.channelDetail(ch.id));
    },
    onError: () => toast.error(t($ => $.create_failed)),
  });

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t($ => $.create_channel_dialog.title)}</DialogTitle>
          <DialogDescription>{t($ => $.create_channel_dialog.description)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <Input placeholder={t($ => $.create_channel_dialog.name_placeholder)} value={name} onChange={(e) => setName(e.target.value)} />
          <Input placeholder={t($ => $.create_channel_dialog.desc_placeholder)} value={desc} onChange={(e) => setDesc(e.target.value)} />
          <div className="flex items-center gap-4 text-sm">
            <label className="flex items-center gap-1.5">
              <input type="radio" checked={access === "open"} onChange={() => setAccess("open")} />
              {t($ => $.create_channel_dialog.open)}
            </label>
            <label className="flex items-center gap-1.5">
              <input type="radio" checked={access === "invite"} onChange={() => setAccess("invite")} />
              {t($ => $.create_channel_dialog.invite)}
            </label>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t($ => $.create_channel_dialog.cancel)}</Button>
          <Button onClick={() => createMutation.mutate()} disabled={!name.trim() || createMutation.isPending}>
            {createMutation.isPending ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : null}
            {t($ => $.create_channel_dialog.create)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

