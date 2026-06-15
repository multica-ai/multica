"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  closestCenter,
  DndContext,
  DragOverlay,
  PointerSensor,
  useDroppable,
  useSensor,
  useSensors,
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
  Bot,
  ChevronRight,
  FileText,
  FolderPlus,
  Hash,
  Loader2,
  Lock,
  MessageCircleReply,
  MessagesSquare,
  Plus,
  Search,
  Send,
  Settings,
  Users,
  X,
} from "lucide-react";

import { useAuthStore } from "@multica/core/auth";
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
} from "@multica/core/channels";
import { memberListOptions } from "@multica/core/workspace/queries";
import type {
  ChannelMessage,
  ChannelMember,
  ChannelSummary,
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
import { useNavigation } from "../../navigation";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { TranscriptButton } from "../../common/task-transcript";
import { taskStatusConfig } from "../../agents/config";

interface ChannelsPageProps {
  channelId?: string;
}

type SidePanelMode = "replies" | "members";

const UNGROUPED_DROP_ID = "ungrouped-drop-zone";

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

export function ChannelsPage({ channelId }: ChannelsPageProps) {
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
  const { data: messages, isLoading: loadingMessages, hasNextPage, fetchNextPage, isFetchingNextPage } = useInfiniteQuery(channelMessagesOptions(wsId, channelId ?? null));
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

  const canManageChannel =
    activeChannel?.member_role === "owner" ||
    currentWorkspaceMember?.role === "owner" ||
    currentWorkspaceMember?.role === "admin";

  const threadQuery = useQuery(
    messageThreadOptions(wsId, channelId ?? null, replyMessageId),
  );

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
          wsId={wsId}
        />
      </aside>
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="调整频道列表宽度"
        data-panel-resize-handle="channel-list"
        className="relative z-20 flex w-0 shrink-0 cursor-col-resize items-center justify-center before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-transparent before:transition-colors hover:before:bg-foreground/15 after:absolute after:inset-y-0 after:left-1/2 after:w-2 after:-translate-x-1/2"
        onPointerDown={startChannelListResize}
      >
        <div className="z-10 flex h-6 w-1 shrink-0 rounded-lg bg-border" />
      </div>
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
              memberIds={members.map((m) => m.user_id)}
              onOpenReplies={openReplies}
              qc={qc}
              hasMore={hasNextPage}
              loadMore={fetchNextPage}
              loadingMore={isFetchingNextPage}
            />
          </div>
        ) : (
          <div className="flex h-full items-center justify-center text-muted-foreground">
            <div className="text-center">
              <MessagesSquare className="mx-auto mb-2 h-10 w-10 opacity-30" />
              <p className="text-sm">选择一个频道开始聊天</p>
            </div>
          </div>
        )}
      </main>
      {showRightPanel && (
        <>
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label="调整右侧面板宽度"
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
                memberIds={members.map((m) => m.user_id)}
                qc={qc}
                onClose={closeSidePanel}
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
  wsId,
}: {
  channels: ChannelSummary[];
  activeChannelId: string | null;
  loading: boolean;
  onCreate: () => void;
  onSelect: (id: string) => void;
  wsId: string;
}) {
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
    const sortedGroups = [...gMap.entries()].sort((a, b) => a[1].position - b[1].position);
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
  // pointer is over nothing droppable.
  const resolveDropTarget = useCallback(
    (overId: string): { groupId: string | null; position: number } | null => {
      if (overId === UNGROUPED_DROP_ID) {
        const last = ungrouped[ungrouped.length - 1];
        return { groupId: null, position: last ? last.position + 1 : 1 };
      }
      if (overId.startsWith("group:")) {
        const gId = overId.slice(6);
        const gChannels = groupMap.get(gId)?.channels ?? [];
        const last = gChannels[gChannels.length - 1];
        return { groupId: gId, position: last ? last.position + 1 : 1 };
      }
      const overChannel = channels.find((c) => c.id === overId);
      if (overChannel) {
        return { groupId: overChannel.group_id ?? null, position: overChannel.position };
      }
      return null;
    },
    [channels, groupMap, ungrouped],
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
      const target = resolveDropTarget(String(over.id));
      setOverGroupId(target ? target.groupId : undefined);
    },
    [resolveDropTarget],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      setActiveId(null);
      setOverGroupId(undefined);
      const { active, over } = event;
      if (!over) return;

      const draggedId = String(active.id);
      const dragged = channels.find((c) => c.id === draggedId);
      if (!dragged) return;

      const target = resolveDropTarget(String(over.id));
      if (!target) return;

      const currentGroup = dragged.group_id ?? null;
      // No-op when dropped back onto itself or its current position.
      if (target.groupId === currentGroup && target.position === dragged.position) {
        return;
      }

      moveChannelMutation.mutate({
        channelId: draggedId,
        groupId: target.groupId,
        position: target.position,
      });
    },
    [channels, resolveDropTarget, moveChannelMutation],
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

  const activeDragChannel = activeId ? channels.find((c) => c.id === activeId) : null;

  return (
    <div className="flex h-full min-w-0 flex-col border-r bg-muted/30">
      <div className="flex h-11 items-center justify-between border-b px-3">
        <h2 className="text-sm font-semibold">频道</h2>
        <div className="flex items-center gap-0.5">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button variant="ghost" size="icon" className="h-7 w-7" onClick={handleCreateGroup}>
                  <FolderPlus className="h-4 w-4" />
                </Button>
              }
            />
            <TooltipContent>新建分组</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onCreate}>
                  <Plus className="h-4 w-4" />
                </Button>
              }
            />
            <TooltipContent>新建频道</TooltipContent>
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
          >
            <SortableContext items={allIds} strategy={verticalListSortingStrategy}>
              <div className="space-y-0.5 p-1.5">
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
                    onDelete={() => deleteGroupMutation.mutate(gId)}
                    onSelect={onSelect}
                  />
                ))}
                <UngroupedDropZone
                  channels={ungrouped}
                  isDragging={activeId !== null}
                  isDropTarget={overGroupId === null}
                  activeChannelId={activeChannelId}
                  onSelect={onSelect}
                />
              </div>
            </SortableContext>
            <DragOverlay>
              {activeDragChannel && (
                <div className="flex h-8 items-center gap-2 rounded-md bg-accent px-2 text-sm shadow-md ring-1 ring-border">
                  <Hash className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate">{activeDragChannel.name}</span>
                </div>
              )}
            </DragOverlay>
          </DndContext>
        )}
      </div>

      <Dialog open={showCreateGroupDialog} onOpenChange={(v) => !v && setShowCreateGroupDialog(false)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建分组</DialogTitle>
            <DialogDescription>为频道创建一个新的分组</DialogDescription>
          </DialogHeader>
          <div className="py-2">
            <Input
              placeholder="分组名称"
              value={newGroupName}
              onChange={(e) => setNewGroupName(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") handleCreateGroupSubmit(); }}
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowCreateGroupDialog(false)}>取消</Button>
            <Button onClick={handleCreateGroupSubmit} disabled={!newGroupName.trim()}>创建</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
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
}) {
  const { setNodeRef } = useDroppable({ id: `group:${groupId}` });

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "rounded-md transition-colors",
        isDropTarget && "bg-primary/10 ring-1 ring-primary/40",
      )}
    >
      <ContextMenu>
        <ContextMenuTrigger>
          <div
            className="flex h-7 cursor-pointer items-center gap-1 rounded-md px-1 text-xs font-medium text-muted-foreground hover:bg-accent"
            onClick={onToggle}
          >
            <ChevronRight className={cn("h-3 w-3 transition-transform", !collapsed && "rotate-90")} />
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
              />
            ) : (
              <span className="min-w-0 flex-1 truncate uppercase">{name}</span>
            )}
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuItem onClick={onStartRename}>重命名</ContextMenuItem>
          <ContextMenuItem className="text-destructive" onClick={onDelete}>删除分组</ContextMenuItem>
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
            />
          ))}
          {channels.length === 0 && isDragging && (
            <div
              className={cn(
                "mx-1 mb-0.5 ml-5 h-8 rounded-md border border-dashed transition-colors",
                isDropTarget ? "border-primary/50" : "border-border/60",
              )}
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
}: {
  channels: ChannelSummary[];
  isDragging: boolean;
  isDropTarget: boolean;
  activeChannelId: string | null;
  onSelect: (id: string) => void;
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
}: {
  channel: ChannelSummary;
  isActive: boolean;
  onSelect: (id: string) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: channel.id,
  });
  const style = { transform: CSS.Transform.toString(transform), transition };

  return (
    <div
      ref={setNodeRef}
      style={style}
      {...attributes}
      {...listeners}
      className={cn(isDragging && "opacity-50")}
    >
      <button
        onClick={() => onSelect(channel.id)}
        className={cn(
          "flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-sm hover:bg-accent",
          isActive && "bg-accent font-medium",
          channel.group_id && "pl-5",
        )}
      >
        {channel.access_mode === "invite" ? (
          <Lock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <Hash className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="min-w-0 flex-1 truncate">{channel.name}</span>
        {channel.has_unread && <span className="h-2 w-2 shrink-0 rounded-full bg-primary" />}
      </button>
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
}) {
  const editorRef = useRef<ContentEditorRef>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const nav = useNavigation();
  const paths = useWorkspacePaths();
  const { uploadWithToast } = useFileUpload(api, (err) => toast.error(err.message));
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => editorRef.current?.uploadFiles(files),
  });
  const [isEmpty, setIsEmpty] = useState(true);
  const prevMsgCount = useRef(messages.length);

  useEffect(() => {
    if (messages.length > prevMsgCount.current) {
      const isNewMessage = messages.length - prevMsgCount.current <= 2;
      if (isNewMessage) {
        bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
      }
    } else if (prevMsgCount.current === 0 && messages.length > 0) {
      bottomRef.current?.scrollIntoView({ block: "end" });
    }
    prevMsgCount.current = messages.length;
  }, [messages.length]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" });
  }, [channelId]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el || !hasMore || loadingMore) return;
    if (el.scrollTop < 80) {
      loadMore?.();
    }
  }, [hasMore, loadMore, loadingMore]);

  const sendMutation = useMutation({
    mutationFn: (content: string) => api.sendChannelMessage(channelId, { content }),
    onSuccess: () => {
      editorRef.current?.clearContent();
      setIsEmpty(true);
      qc.invalidateQueries({ queryKey: channelKeys.channelMessages(wsId, channelId) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
    onError: () => toast.error("发送失败"),
  });

  const convertMutation = useMutation({
    mutationFn: (messageId: string) => api.convertMessageToIssue(channelId, messageId),
    onSuccess: (data) => {
      toast.success(`已创建 Issue #${data.issue_number}`);
      nav.push(paths.issueDetail(data.issue_id));
    },
    onError: () => toast.error("转换失败"),
  });

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
      <div ref={scrollRef} onScroll={handleScroll} className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {loadingMore && (
          <div className="flex justify-center py-2">
            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
          </div>
        )}
        {messages.length === 0 ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            还没有消息，发第一条吧
          </div>
        ) : (
          <ul className="space-y-3">
            {messages.map((msg) => (
              <MessageRow
                key={msg.id}
                message={msg}
                onOpenReplies={onOpenReplies}
                onConvert={(id) => convertMutation.mutate(id)}
                converting={convertMutation.isPending}
              />
            ))}
            <div ref={bottomRef} />
          </ul>
        )}
      </div>
      <div className="border-t px-4 py-3">
        <div
          {...dropZoneProps}
          className="relative flex min-h-16 max-h-44 flex-col rounded-lg border bg-card pb-9 focus-within:border-brand"
        >
          <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
            <ContentEditor
              ref={editorRef}
              placeholder="输入消息，支持 Markdown、图片和 @提及"
              onUpdate={(md) => setIsEmpty(!md.trim())}
              onSubmit={handleSend}
              onUploadFile={(file) => uploadWithToast(file)}
              debounceMs={100}
              submitOnEnter
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
          </div>
          {isDragOver && <FileDropOverlay />}
        </div>
      </div>
    </>
  );
}

function MessageRow({
  message,
  onOpenReplies,
  onConvert,
  converting,
}: {
  message: ChannelMessage;
  onOpenReplies: (id: string) => void;
  onConvert: (id: string) => void;
  converting: boolean;
}) {
  const isSystem = message.author_type === "system" || !message.author_id;
  const authorId = message.author_id ?? "";
  return (
    <ContextMenu>
      <ContextMenuTrigger className="block select-text">
        <li className={cn(
          "group flex items-start gap-3 rounded-md p-2 hover:bg-muted/50",
          isSystem && "bg-muted/25",
        )}>
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
                className="mt-1 text-xs text-primary hover:underline"
              >
                {message.reply_count} 条回复
              </button>
            )}
            <ChannelAgentTaskStrip tasks={message.agent_tasks ?? []} />
          </div>
        </li>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onClick={() => onOpenReplies(message.id)}>
          <MessageCircleReply className="mr-2 h-4 w-4" />
          回复
        </ContextMenuItem>
        <ContextMenuItem onClick={() => onConvert(message.id)} disabled={converting}>
          <FileText className="mr-2 h-4 w-4" />
          转换为 Issue
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

function ChannelAgentTaskStrip({ tasks }: { tasks: ChannelMessage["agent_tasks"] }) {
  if (!tasks || tasks.length === 0) return null;
  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5">
      {tasks.map((task) => {
        const cfg = taskStatusConfig[task.status] ?? taskStatusConfig.queued!;
        const Icon = cfg.icon;
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
                title="查看执行历史"
              />
            )}
          </div>
        );
      })}
    </div>
  );
}

function channelTaskStatusLabel(status: string): string {
  switch (status) {
    case "queued":
      return "排队中";
    case "dispatched":
      return "已派发";
    case "waiting_local_directory":
      return "等待目录";
    case "running":
      return "运行中";
    case "completed":
      return "已完成";
    case "failed":
      return "失败";
    case "cancelled":
      return "已取消";
    default:
      return status;
  }
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
}: {
  channelId: string;
  messageId: string;
  data: MessageThreadResponse | undefined;
  loading: boolean;
  wsId: string;
  memberIds: string[];
  qc: ReturnType<typeof useQueryClient>;
  onClose: () => void;
}) {
  const editorRef = useRef<ContentEditorRef>(null);
  const { uploadWithToast } = useFileUpload(api, (err) => toast.error(err.message));
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => editorRef.current?.uploadFiles(files),
  });
  const [isEmpty, setIsEmpty] = useState(true);

  const replyMutation = useMutation({
    mutationFn: (content: string) => api.replyToMessage(channelId, messageId, { content }),
    onSuccess: () => {
      editorRef.current?.clearContent();
      setIsEmpty(true);
      qc.invalidateQueries({ queryKey: channelKeys.messageThread(wsId, channelId, messageId) });
      qc.invalidateQueries({ queryKey: channelKeys.channelMessages(wsId, channelId) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
    onError: () => toast.error("回复失败"),
  });

  const handleReply = useCallback(() => {
    const content = editorRef.current?.getMarkdown().trim();
    if (!content || editorRef.current?.hasActiveUploads()) return;
    replyMutation.mutate(content);
  }, [replyMutation]);

  return (
    <div className="flex h-full min-w-0 flex-col border-l bg-background shadow-sm">
      <PanelHeader title="回复" onClose={onClose} />
      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : data ? (
          <div className="space-y-4">
            <PanelMessage message={data.root_message} framed />
            <div className="space-y-3">
              {data.replies.length > 0 ? (
                data.replies.map((reply) => <PanelMessage key={reply.id} message={reply} />)
              ) : (
                <p className="py-4 text-center text-xs text-muted-foreground">还没有回复</p>
              )}
            </div>
          </div>
        ) : (
          <p className="py-4 text-center text-xs text-muted-foreground">还没有回复</p>
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
              placeholder="回复..."
              onUpdate={(md) => setIsEmpty(!md.trim())}
              onSubmit={handleReply}
              onUploadFile={(file) => uploadWithToast(file)}
              debounceMs={100}
              submitOnEnter
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
}: {
  message?: ChannelMessage;
  framed?: boolean;
}) {
  if (!message) return null;
  const isSystem = message.author_type === "system" || !message.author_id;
  const authorId = message.author_id ?? "";
  return (
    <div className={cn("flex items-start gap-2", framed && "rounded-md border bg-muted/30 p-2")}>
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
      </div>
    </div>
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
  const [query, setQuery] = useState("");
  const memberUserIds = useMemo(() => new Set(members.map((m) => m.user_id)), [members]);
  const ownerCount = members.filter((m) => m.role === "owner").length;

  const addMutation = useMutation({
    mutationFn: (userId: string) => api.addChannelMember(channel.id, { user_id: userId }),
    onSuccess: () => {
      toast.success("成员已添加");
      qc.invalidateQueries({ queryKey: channelKeys.members(wsId, channel.id) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
    onError: () => toast.error("添加失败"),
  });

  const removeMutation = useMutation({
    mutationFn: (userId: string) => api.removeChannelMember(channel.id, userId),
    onSuccess: () => {
      toast.success("成员已移除");
      qc.invalidateQueries({ queryKey: channelKeys.members(wsId, channel.id) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
    onError: () => toast.error("移除失败"),
  });

  const updateMutation = useMutation({
    mutationFn: (accessMode: "open" | "invite") =>
      api.updateChannel(channel.id, { access_mode: accessMode }),
    onSuccess: () => {
      toast.success("频道设置已更新");
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: channelKeys.detail(wsId, channel.id) });
    },
    onError: () => toast.error("更新失败"),
  });

  const candidates = useMemo(
    () =>
      workspaceMembers
        .filter((member) => matchesMember(member, query))
        .sort((a, b) => a.name.localeCompare(b.name))
        .slice(0, 80),
    [query, workspaceMembers],
  );

  return (
    <div className="flex h-full min-w-0 flex-col border-l bg-background shadow-sm">
      <PanelHeader title="成员与设置" onClose={onClose} />
      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        <section className="space-y-2">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h3 className="text-sm font-medium">频道性质</h3>
              <p className="text-xs text-muted-foreground">控制成员是否需要被添加后才能进入。</p>
            </div>
            <Select
              value={channel.access_mode}
              onValueChange={(value) => updateMutation.mutate(value as "open" | "invite")}
              disabled={!canManage || updateMutation.isPending}
            >
              <SelectTrigger size="sm" className="w-24">
                <SelectValue>{channel.access_mode === "open" ? "公开" : "邀请"}</SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value="open">公开</SelectItem>
                <SelectItem value="invite">邀请</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </section>

        <div className="my-4 h-px bg-border" />

        <section className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium">当前成员</h3>
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
                  {disableRemove ? (
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <span>
                            <Button variant="ghost" size="sm" className="h-7 text-xs" disabled>
                              移除
                            </Button>
                          </span>
                        }
                      />
                      <TooltipContent side="left">
                        {!canManage ? "只有管理员或频道 Owner 可以移除成员" : "不能移除唯一 Owner"}
                      </TooltipContent>
                    </Tooltip>
                  ) : (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 text-xs text-destructive"
                      onClick={() => removeMutation.mutate(member.user_id)}
                    >
                      移除
                    </Button>
                  )}
                </li>
              );
            })}
          </ul>
        </section>

        <div className="my-4 h-px bg-border" />

        <section className="space-y-2">
          <div>
            <h3 className="text-sm font-medium">添加成员</h3>
            <p className="text-xs text-muted-foreground">按姓名、邮箱或拼音搜索 Workspace 成员。</p>
          </div>
          <div className="relative">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="搜索成员"
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
                    {alreadyInChannel ? "已添加" : "添加"}
                  </Button>
                </li>
              );
            })}
            {candidates.length === 0 && (
              <li className="py-6 text-center text-xs text-muted-foreground">没有匹配成员</li>
            )}
          </ul>
        </section>
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
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [access, setAccess] = useState<"open" | "invite">("open");
  const nav = useNavigation();
  const paths = useWorkspacePaths();

  const createMutation = useMutation({
    mutationFn: () => api.createChannel({ name, description: desc || undefined, access_mode: access }),
    onSuccess: (ch) => {
      toast.success("频道已创建");
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      onClose();
      setName("");
      setDesc("");
      setAccess("open");
      nav.push(paths.channelDetail(ch.id));
    },
    onError: () => toast.error("创建失败"),
  });

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>创建频道</DialogTitle>
          <DialogDescription>创建一个新的频道来讨论话题</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <Input placeholder="频道名称" value={name} onChange={(e) => setName(e.target.value)} />
          <Input placeholder="描述（可选）" value={desc} onChange={(e) => setDesc(e.target.value)} />
          <div className="flex items-center gap-4 text-sm">
            <label className="flex items-center gap-1.5">
              <input type="radio" checked={access === "open"} onChange={() => setAccess("open")} />
              公开
            </label>
            <label className="flex items-center gap-1.5">
              <input type="radio" checked={access === "invite"} onChange={() => setAccess("invite")} />
              邀请制
            </label>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={() => createMutation.mutate()} disabled={!name.trim() || createMutation.isPending}>
            {createMutation.isPending ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : null}
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
