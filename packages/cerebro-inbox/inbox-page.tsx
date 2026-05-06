"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  useDraggable,
  useDroppable,
  DragOverlay,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  inboxListOptions,
  inboxArchivedListOptions,
  inboxListInFolderOptions,
  activeIssueTasksOptions,
  deduplicateInboxItems,
} from "@multica/core/inbox/queries";
import {
  useMarkInboxRead,
  useArchiveInbox,
  useMarkAllInboxRead,
  useArchiveAllInbox,
  useArchiveAllReadInbox,
  useArchiveCompletedInbox,
} from "@multica/core/inbox/mutations";
import {
  inboxFolderListOptions,
  inboxFolderMembershipsOptions,
  useCreateInboxFolder,
  useRenameInboxFolder,
  useDeleteInboxFolder,
  useAddInboxFolderItem,
  useSetInboxFolderParent,
} from "./core/folders";
import { IssueDetail } from "@multica/views/issues/components";
import { useNavigation } from "@multica/views/navigation";
import { SidebarTrigger } from "@multica/ui/components/ui/sidebar";
import { toast } from "sonner";
import {
  MoreHorizontal,
  Inbox,
  CheckCheck,
  Archive,
  BookCheck,
  ListChecks,
  ArrowLeft,
  Plus,
  Bot,
  Folder,
  FolderOpen,
  Trash2,
  Pencil,
  ChevronRight,
  ChevronDown,
  FolderPlus,
} from "lucide-react";
import type { InboxItem, InboxItemType, InboxFolder, InboxFolderItemType } from "@multica/core/types";
import {
  chatSessionsOptions,
  chatSessionsInFolderOptions,
  archivedChatSessionsOptions,
  allChatSessionsOptions,
  pendingChatTasksOptions,
} from "@multica/core/chat/queries";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { PageHeader } from "@multica/views/layout/page-header";
import { InboxListItem, timeAgo } from "@multica/views/inbox/components/inbox-list-item";
import { typeLabels } from "@multica/views/inbox/components/inbox-detail-label";
import { InboxChatPanel } from "./views/components/inbox-chat-panel";

type ViewMode =
  | { kind: "inbox" }
  | { kind: "archived" }
  | { kind: "folder"; id: string };

type GroupByMode = "none" | "project" | "agent" | "type";

const GROUP_BY_OPTIONS: { value: GroupByMode; label: string }[] = [
  { value: "none", label: "None" },
  { value: "project", label: "Project" },
  { value: "agent", label: "Agent" },
  { value: "type", label: "Type" },
];

function isGroupByMode(s: string | null): s is GroupByMode {
  return s === "none" || s === "project" || s === "agent" || s === "type";
}

export function InboxPage() {
  const { searchParams, replace } = useNavigation();
  // ?chat=<id> selects a chat session (preferred). ?issue=<id> selects a
  // notification or issue. Both are also accepted as legacy aliases for
  // each other so old bookmarks of `?issue=<chat-id>` keep landing on the
  // chat — the lookup further down decides which one matches.
  const urlChat = searchParams.get("chat") ?? "";
  const urlIssue = searchParams.get("issue") ?? "";
  const urlSelected = urlChat || urlIssue;
  const wsPaths = useWorkspacePaths();

  const [selectedKey, setSelectedKeyState] = useState(() => urlSelected);
  const [viewMode, setViewMode] = useState<ViewMode>({ kind: "inbox" });

  useEffect(() => {
    setSelectedKeyState(urlSelected);
  }, [urlSelected]);

  const wsId = useWorkspaceId();
  const isFolderView = viewMode.kind === "folder";
  const isArchivedView = viewMode.kind === "archived";
  const folderId = isFolderView ? viewMode.id : "";

  // Query each shape; only one is enabled at a time. Doing it this way keeps
  // hook order stable and lets each query keep its own queryKey type.
  const inboxDefaultQuery = useQuery({
    ...inboxListOptions(wsId),
    enabled: !isFolderView && !isArchivedView,
  });
  const inboxInFolderQuery = useQuery({
    ...inboxListInFolderOptions(wsId, folderId),
    enabled: isFolderView && !!folderId,
  });
  const inboxArchivedQuery = useQuery({
    ...inboxArchivedListOptions(wsId),
    enabled: isArchivedView,
  });
  const rawItems = isFolderView
    ? inboxInFolderQuery.data ?? []
    : isArchivedView
      ? inboxArchivedQuery.data ?? []
      : inboxDefaultQuery.data ?? [];
  const loading = isFolderView
    ? inboxInFolderQuery.isLoading
    : isArchivedView
      ? inboxArchivedQuery.isLoading
      : inboxDefaultQuery.isLoading;
  const items = useMemo(
    () =>
      isFolderView
        ? rawItems.filter((i) => !i.archived)
        : isArchivedView
          ? rawItems
          : deduplicateInboxItems(rawItems),
    [rawItems, isFolderView, isArchivedView],
  );

  const chatDefaultQuery = useQuery({
    ...chatSessionsOptions(wsId),
    enabled: !isFolderView && !isArchivedView,
  });
  const chatInFolderQuery = useQuery({
    ...chatSessionsInFolderOptions(wsId, folderId),
    enabled: isFolderView && !!folderId,
  });
  const chatArchivedQuery = useQuery({
    ...archivedChatSessionsOptions(wsId),
    enabled: isArchivedView,
  });
  const chatSessions = isFolderView
    ? chatInFolderQuery.data ?? []
    : isArchivedView
      ? chatArchivedQuery.data ?? []
      : chatDefaultQuery.data ?? [];

  // Workspace-wide "agent is working" signals: which issues and chat sessions
  // currently have an in-flight task. Used to render the small live indicator
  // on each row in the inbox list.
  const { data: activeIssueTasksData } = useQuery(activeIssueTasksOptions(wsId));
  const activeIssueIds = useMemo(
    () => new Set(activeIssueTasksData?.issue_ids ?? []),
    [activeIssueTasksData],
  );
  const { data: pendingChatTasksData } = useQuery(pendingChatTasksOptions(wsId));
  const activeChatSessionIds = useMemo(
    () => new Set((pendingChatTasksData?.tasks ?? []).map((t) => t.chat_session_id)),
    [pendingChatTasksData],
  );

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const agentMap = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const projectMap = useMemo(() => new Map(projects.map((p) => [p.id, p])), [projects]);

  const userId = useAuthStore((s) => s.user?.id ?? "");
  const groupByStorageKey = `multica_inbox_groupby_${wsId}_${userId}`;
  const [groupBy, setGroupBy] = useState<{ primary: GroupByMode; secondary: GroupByMode }>(() => {
    if (typeof window === "undefined") return { primary: "none", secondary: "none" };
    const saved = window.localStorage.getItem(groupByStorageKey);
    if (!saved) return { primary: "none", secondary: "none" };
    const [p, s] = saved.split(",");
    return {
      primary: isGroupByMode(p ?? null) ? (p as GroupByMode) : "none",
      secondary: isGroupByMode(s ?? null) ? (s as GroupByMode) : "none",
    };
  });
  useEffect(() => {
    if (typeof window === "undefined") return;
    window.localStorage.setItem(groupByStorageKey, `${groupBy.primary},${groupBy.secondary}`);
  }, [groupBy, groupByStorageKey]);
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({});
  const toggleGroup = useCallback((key: string) => {
    setCollapsedGroups((c) => ({ ...c, [key]: !c[key] }));
  }, []);

  const { data: folders = [] } = useQuery(inboxFolderListOptions(wsId));
  useQuery(inboxFolderMembershipsOptions(wsId)); // primed for cache; not read here

  // All sessions (active + archived). Used purely for URL-driven lookup so a
  // pasted/bookmarked chat URL still resolves when the active list doesn't
  // contain that session — e.g. an archived chat opened via deep link.
  const { data: allSessions = [] } = useQuery(allChatSessionsOptions(wsId));

  const createFolder = useCreateInboxFolder();
  const renameFolder = useRenameInboxFolder();
  const deleteFolder = useDeleteInboxFolder();
  const addItemToFolder = useAddInboxFolderItem();
  const setFolderParent = useSetInboxFolderParent();

  const selectedChatSession =
    chatSessions.find((s) => s.id === selectedKey) ??
    allSessions.find((s) => s.id === selectedKey) ??
    null;
  const selected = selectedChatSession
    ? null
    : items.find((i) => (i.issue_id ?? i.id) === selectedKey) ?? null;
  const qc = useQueryClient();

  const pendingChatIdRef = useRef<string | null>(null);

  useEffect(() => {
    if (loading) return;
    if (!selectedKey) return;
    if (selected) return;
    if (selectedChatSession || selectedKey === "new-chat") return;
    if (pendingChatIdRef.current === selectedKey) return;
    // Don't redirect a `?chat=<id>` URL to the issue page — if the chat
    // genuinely doesn't exist, leave the panel empty rather than sending
    // the user to a "this issue does not exist" screen.
    if (urlChat && !urlIssue) return;
    replace(wsPaths.issueDetail(selectedKey));
  }, [loading, selectedKey, selected, selectedChatSession, replace, wsPaths, urlChat, urlIssue]);

  useEffect(() => {
    if (pendingChatIdRef.current && chatSessions.some((s) => s.id === pendingChatIdRef.current)) {
      pendingChatIdRef.current = null;
    }
  }, [chatSessions]);

  // `kind` decides which query param the URL uses: `?chat=` for chat
  // sessions (incl. the new-chat sentinel), `?issue=` for inbox items.
  // Picking the right one is what makes shared URLs land on the right
  // panel without the inbox redirecting chats into "issue does not exist".
  const setSelectedKey = useCallback((kind: "chat" | "issue" | null, key: string) => {
    setSelectedKeyState(key);
    const inboxPath = wsPaths.inbox();
    const url = !key
      ? inboxPath
      : kind === "chat"
        ? `${inboxPath}?chat=${key}`
        : `${inboxPath}?issue=${key}`;
    replace(url);
  }, [replace, wsPaths]);

  const handleArchiveChat = useCallback(async (sessionId: string) => {
    if (sessionId === selectedKey) setSelectedKey(null, "");
    await api.archiveChatSession(sessionId);
    qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
  }, [selectedKey, setSelectedKey, wsId, qc]);

  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_inbox_layout",
  });

  const isMobile = useIsMobile();
  const unreadCount = items.filter((i) => !i.read).length;

  const markReadMutation = useMarkInboxRead();
  const archiveMutation = useArchiveInbox();
  const markAllReadMutation = useMarkAllInboxRead();
  const archiveAllMutation = useArchiveAllInbox();
  const archiveAllReadMutation = useArchiveAllReadInbox();
  const archiveCompletedMutation = useArchiveCompletedInbox();

  const handleSelect = (item: InboxItem) => {
    setSelectedKey("issue", item.issue_id ?? item.id);
    if (!item.read) {
      markReadMutation.mutate(item.id, {
        onError: () => toast.error("Failed to mark as read"),
      });
    }
  };

  const handleArchive = (id: string) => {
    const archived = items.find((i) => i.id === id);
    if (archived && (archived.issue_id ?? archived.id) === selectedKey) setSelectedKey(null, "");
    archiveMutation.mutate(id, {
      onError: () => toast.error("Failed to archive"),
    });
  };

  const handleMarkAllRead = () => {
    markAllReadMutation.mutate(undefined, {
      onError: () => toast.error("Failed to mark all as read"),
    });
  };

  const handleArchiveAll = () => {
    setSelectedKey(null, "");
    archiveAllMutation.mutate(undefined, {
      onError: () => toast.error("Failed to archive all"),
    });
  };

  const handleArchiveAllRead = () => {
    const readKeys = items.filter((i) => i.read).map((i) => i.issue_id ?? i.id);
    if (readKeys.includes(selectedKey)) setSelectedKey(null, "");
    archiveAllReadMutation.mutate(undefined, {
      onError: () => toast.error("Failed to archive read items"),
    });
  };

  const handleArchiveCompleted = () => {
    setSelectedKey(null, "");
    archiveCompletedMutation.mutate(undefined, {
      onError: () => toast.error("Failed to archive completed"),
    });
  };

  const handleNewChat = () => {
    setSelectedKey("chat", "new-chat");
  };

  // -- Drag and drop ---------------------------------------------------------

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const [activeDrag, setActiveDrag] = useState<
    | { kind: "item"; type: InboxFolderItemType; id: string; label: string }
    | { kind: "folder"; id: string; label: string }
    | null
  >(null);

  // Map of folder.id → set of descendant ids (for client-side cycle blocking).
  const folderDescendants = useMemo(() => {
    const childrenByParent = new Map<string | null, InboxFolder[]>();
    for (const f of folders) {
      const key = f.parent_id;
      const list = childrenByParent.get(key) ?? [];
      list.push(f);
      childrenByParent.set(key, list);
    }
    const collect = (rootId: string): Set<string> => {
      const result = new Set<string>([rootId]);
      const stack = [rootId];
      while (stack.length) {
        const cur = stack.pop()!;
        for (const child of childrenByParent.get(cur) ?? []) {
          if (!result.has(child.id)) {
            result.add(child.id);
            stack.push(child.id);
          }
        }
      }
      return result;
    };
    const map = new Map<string, Set<string>>();
    for (const f of folders) map.set(f.id, collect(f.id));
    return map;
  }, [folders]);

  const handleDragStart = useCallback(
    (event: DragStartEvent) => {
      const id = String(event.active.id);
      if (id.startsWith("folder-move:")) {
        const folderId = id.slice("folder-move:".length);
        const folder = folders.find((f) => f.id === folderId);
        if (folder) setActiveDrag({ kind: "folder", id: folderId, label: folder.name });
        return;
      }
      const [type, itemId] = id.split(":");
      if (type === "notification") {
        const item = items.find((i) => i.id === itemId);
        if (item)
          setActiveDrag({ kind: "item", type: "notification", id: itemId ?? "", label: item.title });
      } else if (type === "chat_session") {
        const session = chatSessions.find((s) => s.id === itemId);
        if (session) {
          const agent = agentMap.get(session.agent_id);
          setActiveDrag({
            kind: "item",
            type: "chat_session",
            id: itemId ?? "",
            label: session.title || agent?.name || "Chat",
          });
        }
      }
    },
    [items, chatSessions, agentMap, folders],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      setActiveDrag(null);
      const { active, over } = event;
      if (!over) return;
      const overId = String(over.id);
      const activeId = String(active.id);

      // Folder reparent.
      if (activeId.startsWith("folder-move:")) {
        const folderId = activeId.slice("folder-move:".length);
        let newParent: string | null = null;
        if (overId === "folder-root") {
          newParent = null;
        } else if (overId.startsWith("folder:")) {
          newParent = overId.slice("folder:".length);
        } else {
          return;
        }
        if (newParent === folderId) return;
        // Reject if target is a descendant of the dragged folder (would cycle).
        if (newParent && (folderDescendants.get(folderId)?.has(newParent) ?? false)) {
          toast.error("Cannot move folder into its own subtree");
          return;
        }
        // Skip no-op (already this parent).
        const folder = folders.find((f) => f.id === folderId);
        if (folder && (folder.parent_id ?? null) === newParent) return;
        setFolderParent.mutate(
          { id: folderId, parentId: newParent },
          { onError: () => toast.error("Failed to move folder") },
        );
        return;
      }

      // Item drop on folder.
      if (!overId.startsWith("folder:")) return;
      const targetFolderId = overId.slice("folder:".length);
      const [type, itemId] = activeId.split(":");
      if (!itemId) return;
      if (type !== "notification" && type !== "chat_session") return;
      addItemToFolder.mutate(
        { folderId: targetFolderId, itemType: type, itemId },
        { onError: () => toast.error("Failed to move item") },
      );
      if (selectedKey === itemId) {
        setSelectedKey(null, "");
      }
    },
    [addItemToFolder, setFolderParent, folders, folderDescendants, selectedKey, setSelectedKey],
  );

  const currentFolder = isFolderView ? folders.find((f) => f.id === folderId) : null;
  const headerTitle = isFolderView
    ? currentFolder?.name ?? "Folder"
    : isArchivedView
      ? "Archived"
      : "Inbox";
  const showBack = isFolderView || isArchivedView;

  const listHeader = (
    <PageHeader className="justify-between">
      <div className="flex min-w-0 items-center gap-2">
        {showBack && (
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground"
            onClick={() => setViewMode({ kind: "inbox" })}
            title="Back to Inbox"
          >
            <ArrowLeft className="h-4 w-4" />
          </Button>
        )}
        <h1 className="truncate text-sm font-semibold">{headerTitle}</h1>
        {!isFolderView && !isArchivedView && unreadCount > 0 && (
          <span className="text-xs text-muted-foreground">{unreadCount}</span>
        )}
      </div>
      <div className="flex items-center gap-1">
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          onClick={handleNewChat}
          title="New message"
        >
          <Plus className="h-4 w-4" />
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground"
              />
            }
          >
            <MoreHorizontal className="h-4 w-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuItem disabled>Group by</DropdownMenuItem>
            {GROUP_BY_OPTIONS.map((opt) => (
              <DropdownMenuItem
                key={`p-${opt.value}`}
                onClick={() =>
                  setGroupBy((g) => ({
                    primary: opt.value,
                    // Drop secondary when it would equal new primary or when primary is none.
                    secondary:
                      opt.value === "none" || g.secondary === opt.value ? "none" : g.secondary,
                  }))
                }
              >
                <span className="w-4 text-muted-foreground">
                  {groupBy.primary === opt.value ? "✓" : ""}
                </span>
                {opt.label}
              </DropdownMenuItem>
            ))}
            {groupBy.primary !== "none" && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem disabled>Then by</DropdownMenuItem>
                {GROUP_BY_OPTIONS.filter((opt) => opt.value !== groupBy.primary).map((opt) => (
                  <DropdownMenuItem
                    key={`s-${opt.value}`}
                    onClick={() =>
                      setGroupBy((g) => ({ ...g, secondary: opt.value }))
                    }
                  >
                    <span className="w-4 text-muted-foreground">
                      {groupBy.secondary === opt.value ? "✓" : ""}
                    </span>
                    {opt.label}
                  </DropdownMenuItem>
                ))}
              </>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() => {
                setSelectedKey(null, "");
                setViewMode(isArchivedView ? { kind: "inbox" } : { kind: "archived" });
              }}
            >
              <Archive className="h-4 w-4" />
              {isArchivedView ? "Show active" : "Show archived"}
            </DropdownMenuItem>
            {!isArchivedView && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={handleMarkAllRead}>
                  <CheckCheck className="h-4 w-4" />
                  Mark all as read
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={handleArchiveAll}>
                  <Archive className="h-4 w-4" />
                  Archive all
                </DropdownMenuItem>
                <DropdownMenuItem onClick={handleArchiveAllRead}>
                  <BookCheck className="h-4 w-4" />
                  Archive all read
                </DropdownMenuItem>
                <DropdownMenuItem onClick={handleArchiveCompleted}>
                  <ListChecks className="h-4 w-4" />
                  Archive completed
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </PageHeader>
  );

  type MergedEntry =
    | { kind: "chat"; id: string; time: number; session: typeof chatSessions[number] }
    | { kind: "notif"; id: string; time: number; item: InboxItem };

  const mergedEntries = useMemo<MergedEntry[]>(() => {
    const entries: MergedEntry[] = [];
    for (const session of chatSessions) {
      entries.push({
        kind: "chat",
        id: session.id,
        time: new Date(session.updated_at).getTime(),
        session,
      });
    }
    for (const item of items) {
      entries.push({
        kind: "notif",
        id: item.id,
        time: new Date(item.created_at).getTime(),
        item,
      });
    }
    entries.sort((a, b) => b.time - a.time);
    return entries;
  }, [chatSessions, items]);

  const renderEntry = (entry: MergedEntry) => {
    if (entry.kind === "chat") {
      const session = entry.session;
      const agent = agentMap.get(session.agent_id);
      const isAgentActive = activeChatSessionIds.has(session.id);
      return (
        <DraggableRow
          key={`chat:${session.id}`}
          draggableId={`chat_session:${session.id}`}
        >
          <div
            className={`group/chat flex items-center gap-3 px-4 py-2.5 cursor-pointer transition-colors hover:bg-accent/50 ${
              session.id === selectedKey ? "bg-accent" : ""
            }`}
            onClick={() => setSelectedKey("chat", session.id)}
          >
            <Avatar className="size-7 shrink-0">
              {agent?.avatar_url && <AvatarImage src={agent.avatar_url} />}
              <AvatarFallback className="bg-purple-100 text-purple-700 text-xs">
                <Bot className="size-3.5" />
              </AvatarFallback>
            </Avatar>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-1.5">
                {session.has_unread && (
                  <span className="size-1.5 shrink-0 rounded-full bg-brand" />
                )}
                <span
                  className={`truncate text-sm ${session.has_unread ? "font-medium" : "text-muted-foreground"}`}
                >
                  {session.title || agent?.name || "Chat"}
                </span>
              </div>
              <span className={`flex items-center gap-1.5 text-xs ${session.has_unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}>
                {isAgentActive && <AgentRunningPip />}
                {agent?.name} · {timeAgo(session.updated_at)}
              </span>
            </div>
            <button
              type="button"
              className="flex h-9 w-9 sm:h-6 sm:w-6 sm:hidden shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-accent sm:group-hover/chat:flex"
              onClick={(e) => {
                e.stopPropagation();
                handleArchiveChat(session.id);
              }}
              title="Archive"
              aria-label="Archive chat"
            >
              <Archive className="size-4 sm:size-3" />
            </button>
          </div>
        </DraggableRow>
      );
    }
    const item = entry.item;
    const isAgentActive = !!item.issue_id && activeIssueIds.has(item.issue_id);
    return (
      <DraggableRow
        key={`notif:${item.id}`}
        draggableId={`notification:${item.id}`}
      >
        <InboxListItem
          item={item}
          isSelected={(item.issue_id ?? item.id) === selectedKey}
          isAgentActive={isAgentActive}
          onClick={() => handleSelect(item)}
          onArchive={() => handleArchive(item.id)}
        />
      </DraggableRow>
    );
  };

  const bucketize = useCallback(
    (mode: GroupByMode, entry: MergedEntry): { key: string; label: string; isFallback: boolean } => {
      if (mode === "project") {
        if (entry.kind === "notif" && entry.item.project_id) {
          const proj = projectMap.get(entry.item.project_id);
          return {
            key: entry.item.project_id,
            label: proj?.title ?? "Unknown project",
            isFallback: false,
          };
        }
        return { key: "__no_project__", label: "No project", isFallback: true };
      }
      if (mode === "agent") {
        if (entry.kind === "chat") {
          const agent = agentMap.get(entry.session.agent_id);
          return {
            key: entry.session.agent_id,
            label: agent?.name ?? "Unknown agent",
            isFallback: false,
          };
        }
        if (entry.item.actor_type === "agent" && entry.item.actor_id) {
          const agent = agentMap.get(entry.item.actor_id);
          return {
            key: entry.item.actor_id,
            label: agent?.name ?? "Unknown agent",
            isFallback: false,
          };
        }
        return { key: "__no_agent__", label: "No agent", isFallback: true };
      }
      if (mode === "type") {
        if (entry.kind === "chat") {
          return { key: "__chat__", label: "Chats", isFallback: true };
        }
        const t = entry.item.type;
        return {
          key: t,
          label: typeLabels[t as InboxItemType] ?? t,
          isFallback: false,
        };
      }
      return { key: "__none__", label: "", isFallback: true };
    },
    [projectMap, agentMap],
  );

  type Group = {
    key: string;
    label: string;
    isFallback: boolean;
    entries: MergedEntry[];
    children?: Group[];
  };

  const sortGroups = (groups: Group[]) => {
    groups.sort((a, b) => {
      if (a.isFallback !== b.isFallback) return a.isFallback ? 1 : -1;
      return a.label.localeCompare(b.label);
    });
  };

  const groupedEntries = useMemo<Group[] | null>(() => {
    if (groupBy.primary === "none") return null;
    const primaryMap = new Map<string, Group>();
    for (const entry of mergedEntries) {
      const p = bucketize(groupBy.primary, entry);
      let pg = primaryMap.get(p.key);
      if (!pg) {
        pg = { ...p, entries: [], children: groupBy.secondary !== "none" ? [] : undefined };
        primaryMap.set(p.key, pg);
      }
      pg.entries.push(entry);
    }

    if (groupBy.secondary !== "none") {
      for (const pg of primaryMap.values()) {
        const subMap = new Map<string, Group>();
        for (const entry of pg.entries) {
          const s = bucketize(groupBy.secondary, entry);
          let sg = subMap.get(s.key);
          if (!sg) {
            sg = { ...s, entries: [] };
            subMap.set(s.key, sg);
          }
          sg.entries.push(entry);
        }
        const subGroups = Array.from(subMap.values());
        sortGroups(subGroups);
        pg.children = subGroups;
      }
    }

    const top = Array.from(primaryMap.values());
    sortGroups(top);
    return top;
  }, [mergedEntries, groupBy, bucketize]);

  const renderGroup = (
    group: Group,
    depth: number,
    parentKey: string,
  ): React.ReactNode => {
    const fullKey = `${parentKey}/${group.key}`;
    const collapsed = collapsedGroups[fullKey] ?? false;
    return (
      <div key={fullKey}>
        <button
          type="button"
          onClick={() => toggleGroup(fullKey)}
          className="flex w-full items-center gap-1.5 py-1.5 pr-4 text-left text-xs font-medium uppercase tracking-wide text-muted-foreground hover:bg-accent/50"
          style={{ paddingLeft: 16 + depth * 12 }}
        >
          {collapsed ? (
            <ChevronRight className="size-3" />
          ) : (
            <ChevronDown className="size-3" />
          )}
          <span className="truncate">{group.label}</span>
          <span className="text-muted-foreground/60">{group.entries.length}</span>
        </button>
        {!collapsed && (
          group.children
            ? group.children.map((child) => renderGroup(child, depth + 1, fullKey))
            : group.entries.map(renderEntry)
        )}
      </div>
    );
  };

  const emptyMessage = isFolderView
    ? "Empty folder"
    : isArchivedView
      ? "No archived items"
      : "No notifications";

  const listBody = (
    <div>
      {mergedEntries.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
          <Inbox className="mb-3 h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm">{emptyMessage}</p>
        </div>
      ) : groupedEntries ? (
        groupedEntries.map((group) => renderGroup(group, 0, ""))
      ) : (
        mergedEntries.map(renderEntry)
      )}
    </div>
  );

  const folderSection = (
    <FolderSection
      folders={folders}
      selectedFolderId={isFolderView ? folderId : null}
      onSelect={(id) => {
        setSelectedKey(null, "");
        setViewMode(id ? { kind: "folder", id } : { kind: "inbox" });
      }}
      onCreate={(name, parentId) =>
        createFolder.mutate(
          { name, parentId },
          { onError: () => toast.error("Failed to create folder") },
        )
      }
      onRename={(id, name) =>
        renameFolder.mutate(
          { id, name },
          { onError: () => toast.error("Failed to rename") },
        )
      }
      onDelete={(id) => {
        if (isFolderView && folderId === id) setViewMode({ kind: "inbox" });
        deleteFolder.mutate(id, {
          onError: () => toast.error("Failed to delete folder"),
        });
      }}
    />
  );

  const detailContent = selectedChatSession || selectedKey === "new-chat" ? (
    <InboxChatPanel
      key={selectedChatSession?.id ?? "new"}
      sessionId={selectedChatSession?.id ?? null}
      onSessionCreated={(id) => {
        pendingChatIdRef.current = id;
        setSelectedKey("chat", id);
      }}
    />
  ) : selected?.issue_id ? (
    <IssueDetail
      key={selected.id}
      issueId={selected.issue_id}
      defaultSidebarOpen={false}
      layoutId="multica_inbox_issue_detail_layout"
      highlightCommentId={selected.details?.comment_id ?? undefined}
      linkSelfInBreadcrumb
      onDelete={() => {
        handleArchive(selected.id);
      }}
    />
  ) : selected ? (
    <div className="p-3 sm:p-6">
      <h2 className="text-lg font-semibold">{selected.title}</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        {typeLabels[selected.type]} · {timeAgo(selected.created_at)}
      </p>
      {selected.body && (
        <div className="mt-4 whitespace-pre-wrap text-sm leading-relaxed text-foreground/80">
          {selected.body}
        </div>
      )}
      <div className="mt-4">
        <Button
          variant="outline"
          size="sm"
          onClick={() => handleArchive(selected.id)}
        >
          <Archive className="mr-1.5 h-3.5 w-3.5" />
          Archive
        </Button>
      </div>
    </div>
  ) : null;

  if (isMobile) {
    if (loading) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center border-b px-4">
            <Skeleton className="h-5 w-16" />
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto space-y-1 p-2">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-2.5">
                <Skeleton className="h-7 w-7 shrink-0 rounded-full" />
                <div className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-3/4" />
                  <Skeleton className="h-3 w-1/2" />
                </div>
              </div>
            ))}
          </div>
        </div>
      );
    }

    if (selected) {
      return (
        <div className="flex flex-1 flex-col min-h-0">
          <div className="flex h-12 shrink-0 items-center border-b px-2 gap-1">
            <SidebarTrigger />
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setSelectedKey(null, "")}
              className="gap-1.5 text-muted-foreground"
            >
              <ArrowLeft className="h-4 w-4" />
              Inbox
            </Button>
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto">
            {detailContent}
          </div>
        </div>
      );
    }

    return (
      <DndContext sensors={sensors} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
        <div className="flex flex-1 flex-col min-h-0">
          {listHeader}
          <div className="flex-1 min-h-0 overflow-y-auto">
            {listBody}
          </div>
          {folderSection}
        </div>
        <DragOverlay>
          {activeDrag && (
            <div className="rounded border bg-background px-3 py-2 text-sm shadow-md">
              {activeDrag.label}
            </div>
          )}
        </DragOverlay>
      </DndContext>
    );
  }

  if (loading) {
    return (
      <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
        <ResizablePanel id="list" defaultSize={320} minSize={240} maxSize={480} groupResizeBehavior="preserve-pixel-size">
          <div className="flex flex-col border-r h-full">
            <div className="flex h-12 shrink-0 items-center border-b px-4">
              <Skeleton className="h-5 w-16" />
            </div>
            <div className="flex-1 min-h-0 overflow-y-auto space-y-1 p-2">
              {Array.from({ length: 5 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 px-4 py-2.5">
                  <Skeleton className="h-7 w-7 shrink-0 rounded-full" />
                  <div className="flex-1 space-y-2">
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-3 w-1/2" />
                  </div>
                </div>
              ))}
            </div>
          </div>
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel id="detail" minSize="40%">
          <div className="p-6">
            <Skeleton className="h-6 w-48" />
            <Skeleton className="mt-4 h-4 w-32" />
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>
    );
  }

  return (
    <DndContext sensors={sensors} onDragStart={handleDragStart} onDragEnd={handleDragEnd}>
      <ResizablePanelGroup orientation="horizontal" className="flex-1 min-h-0" defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
        <ResizablePanel id="list" defaultSize={320} minSize={240} maxSize={480} groupResizeBehavior="preserve-pixel-size">
          <div className="flex flex-col border-r h-full">
            {listHeader}
            <div className="flex-1 min-h-0 overflow-y-auto">
              {listBody}
            </div>
            {folderSection}
          </div>
        </ResizablePanel>
        <ResizableHandle />
        <ResizablePanel id="detail" minSize="40%">
          <div className="flex flex-col min-h-0 h-full">
            {detailContent ?? (
              <div className="flex h-full flex-col items-center justify-center text-muted-foreground">
                <Inbox className="mb-3 h-10 w-10 text-muted-foreground/30" />
                <p className="text-sm">
                  {items.length === 0
                    ? "Your inbox is empty"
                    : "Select a notification to view details"}
                </p>
              </div>
            )}
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>
      <DragOverlay>
        {activeDrag && (
          <div className="rounded border bg-background px-3 py-2 text-sm shadow-md">
            {activeDrag.label}
          </div>
        )}
      </DragOverlay>
    </DndContext>
  );
}

// -----------------------------------------------------------------------------
// Active agent indicator — tiny pulsing dot
// -----------------------------------------------------------------------------

function AgentRunningPip() {
  return (
    <span
      title="Agent is working"
      className="relative inline-flex size-2 shrink-0 items-center justify-center"
    >
      <span className="absolute inline-flex size-2 animate-ping rounded-full bg-emerald-500/60" />
      <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
    </span>
  );
}

// -----------------------------------------------------------------------------
// Draggable wrapper
// -----------------------------------------------------------------------------

function DraggableRow({
  draggableId,
  children,
}: {
  draggableId: string;
  children: React.ReactNode;
}) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: draggableId,
  });
  return (
    <div
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      className={isDragging ? "opacity-30" : ""}
    >
      {children}
    </div>
  );
}

// -----------------------------------------------------------------------------
// Folder section: list of user folders + create / rename / delete
// -----------------------------------------------------------------------------

function FolderSection({
  folders,
  selectedFolderId,
  onSelect,
  onCreate,
  onRename,
  onDelete,
}: {
  folders: InboxFolder[];
  selectedFolderId: string | null;
  onSelect: (id: string | null) => void;
  onCreate: (name: string, parentId: string | null) => void;
  onRename: (id: string, name: string) => void;
  onDelete: (id: string) => void;
}) {
  // Top-level (parent_id = null) inline create. Subfolder create is per-row
  // and stored as { underParentId, name } so only one row at a time edits.
  const [topCreating, setTopCreating] = useState(false);
  const [createName, setCreateName] = useState("");
  const [createUnderParent, setCreateUnderParent] = useState<string | null>(null);
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const childrenByParent = useMemo(() => {
    const map = new Map<string | null, InboxFolder[]>();
    const sorted = [...folders].sort(
      (a, b) =>
        a.position - b.position ||
        new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
    );
    for (const f of sorted) {
      const key = f.parent_id;
      const list = map.get(key) ?? [];
      list.push(f);
      map.set(key, list);
    }
    return map;
  }, [folders]);

  const submitCreate = () => {
    const name = createName.trim();
    if (name) onCreate(name, createUnderParent);
    setCreateName("");
    setTopCreating(false);
    setCreateUnderParent(null);
    if (createUnderParent) {
      // Auto-expand the parent so the new folder is visible.
      setExpanded((e) => ({ ...e, [createUnderParent]: true }));
    }
  };

  const submitRename = (id: string) => {
    const name = renameValue.trim();
    if (name) onRename(id, name);
    setRenamingId(null);
    setRenameValue("");
  };

  const { setNodeRef: setRootRef, isOver: isOverRoot } = useDroppable({ id: "folder-root" });

  const renderTree = (parentId: string | null, depth: number): React.ReactNode => {
    const list = childrenByParent.get(parentId) ?? [];
    return list.map((folder) => (
      <div key={folder.id}>
        <FolderRow
          folder={folder}
          depth={depth}
          isSelected={selectedFolderId === folder.id}
          isExpanded={expanded[folder.id] ?? false}
          hasChildren={(childrenByParent.get(folder.id) ?? []).length > 0}
          isRenaming={renamingId === folder.id}
          renameValue={renameValue}
          onToggleExpand={() =>
            setExpanded((e) => ({ ...e, [folder.id]: !(e[folder.id] ?? false) }))
          }
          onSelect={() => onSelect(folder.id === selectedFolderId ? null : folder.id)}
          onStartRename={() => {
            setRenamingId(folder.id);
            setRenameValue(folder.name);
          }}
          onChangeRename={setRenameValue}
          onSubmitRename={() => submitRename(folder.id)}
          onCancelRename={() => {
            setRenamingId(null);
            setRenameValue("");
          }}
          onCreateChild={() => {
            setCreateUnderParent(folder.id);
            setCreateName("");
            setExpanded((e) => ({ ...e, [folder.id]: true }));
          }}
          onDelete={() => onDelete(folder.id)}
        />
        {(expanded[folder.id] ?? false) && (
          <>
            {renderTree(folder.id, depth + 1)}
            {createUnderParent === folder.id && (
              <FolderCreateRow
                depth={depth + 1}
                value={createName}
                onChange={setCreateName}
                onSubmit={submitCreate}
                onCancel={() => {
                  setCreateUnderParent(null);
                  setCreateName("");
                }}
              />
            )}
          </>
        )}
      </div>
    ));
  };

  return (
    <div
      ref={setRootRef}
      className={`border-t bg-background ${isOverRoot ? "ring-1 ring-inset ring-brand" : ""}`}
    >
      <div className="flex items-center justify-between px-4 py-2">
        <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Folders
        </span>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          onClick={() => {
            setCreateUnderParent(null);
            setCreateName("");
            setTopCreating(true);
          }}
          title="New folder"
        >
          <Plus className="h-3.5 w-3.5" />
        </Button>
      </div>
      <div className="pb-2">
        {renderTree(null, 0)}
        {topCreating && createUnderParent === null && (
          <FolderCreateRow
            depth={0}
            value={createName}
            onChange={setCreateName}
            onSubmit={submitCreate}
            onCancel={() => {
              setTopCreating(false);
              setCreateName("");
            }}
          />
        )}
        {folders.length === 0 && !topCreating && (
          <p className="px-4 py-1 text-xs text-muted-foreground">
            Drop chats and notifications here to organize them.
          </p>
        )}
      </div>
    </div>
  );
}

function FolderCreateRow({
  depth,
  value,
  onChange,
  onSubmit,
  onCancel,
}: {
  depth: number;
  value: string;
  onChange: (value: string) => void;
  onSubmit: () => void;
  onCancel: () => void;
}) {
  return (
    <div
      className="flex items-center gap-2 py-1.5 pr-4"
      style={{ paddingLeft: 16 + depth * 16 }}
    >
      <Folder className="size-4 shrink-0 text-muted-foreground" />
      <input
        autoFocus
        className="flex-1 bg-transparent text-sm outline-none"
        placeholder="Folder name"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") onSubmit();
          if (e.key === "Escape") onCancel();
        }}
        onBlur={onSubmit}
      />
    </div>
  );
}

function FolderRow({
  folder,
  depth,
  isSelected,
  isExpanded,
  hasChildren,
  isRenaming,
  renameValue,
  onToggleExpand,
  onSelect,
  onStartRename,
  onChangeRename,
  onSubmitRename,
  onCancelRename,
  onCreateChild,
  onDelete,
}: {
  folder: InboxFolder;
  depth: number;
  isSelected: boolean;
  isExpanded: boolean;
  hasChildren: boolean;
  isRenaming: boolean;
  renameValue: string;
  onToggleExpand: () => void;
  onSelect: () => void;
  onStartRename: () => void;
  onChangeRename: (value: string) => void;
  onSubmitRename: () => void;
  onCancelRename: () => void;
  onCreateChild: () => void;
  onDelete: () => void;
}) {
  const { setNodeRef: setDropRef, isOver } = useDroppable({ id: `folder:${folder.id}` });
  const {
    attributes,
    listeners,
    setNodeRef: setDragRef,
    isDragging,
  } = useDraggable({ id: `folder-move:${folder.id}` });
  const Icon = isSelected ? FolderOpen : Folder;

  // Combine drag + drop refs onto a single element.
  const setRefs = useCallback(
    (node: HTMLElement | null) => {
      setDropRef(node);
      setDragRef(node);
    },
    [setDropRef, setDragRef],
  );

  return (
    <div
      ref={setRefs}
      {...attributes}
      {...listeners}
      className={`group/folder flex items-center gap-1 py-1.5 pr-2 cursor-pointer text-sm transition-colors ${
        isSelected ? "bg-accent" : ""
      } ${isOver ? "bg-accent/70 ring-1 ring-inset ring-brand" : "hover:bg-accent/50"} ${
        isDragging ? "opacity-30" : ""
      }`}
      style={{ paddingLeft: 8 + depth * 16 }}
      onClick={isRenaming ? undefined : onSelect}
    >
      {hasChildren ? (
        <button
          type="button"
          className="flex size-4 shrink-0 items-center justify-center text-muted-foreground hover:text-foreground"
          onClick={(e) => {
            e.stopPropagation();
            onToggleExpand();
          }}
          onPointerDown={(e) => e.stopPropagation()}
        >
          {isExpanded ? (
            <ChevronDown className="size-3" />
          ) : (
            <ChevronRight className="size-3" />
          )}
        </button>
      ) : (
        <span className="size-4 shrink-0" />
      )}
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      {isRenaming ? (
        <input
          autoFocus
          className="flex-1 bg-transparent outline-none"
          value={renameValue}
          onChange={(e) => onChangeRename(e.target.value)}
          onClick={(e) => e.stopPropagation()}
          onPointerDown={(e) => e.stopPropagation()}
          onKeyDown={(e) => {
            if (e.key === "Enter") onSubmitRename();
            if (e.key === "Escape") onCancelRename();
          }}
          onBlur={onSubmitRename}
        />
      ) : (
        <span className="flex-1 truncate">{folder.name}</span>
      )}
      {!isRenaming && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <button
                type="button"
                className="hidden size-5 shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-accent group-hover/folder:flex"
                onClick={(e) => e.stopPropagation()}
                onPointerDown={(e) => e.stopPropagation()}
              />
            }
          >
            <MoreHorizontal className="size-3" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuItem
              onClick={(e) => {
                e.stopPropagation();
                onCreateChild();
              }}
            >
              <FolderPlus className="size-3.5" />
              New subfolder
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={(e) => {
                e.stopPropagation();
                onStartRename();
              }}
            >
              <Pencil className="size-3.5" />
              Rename
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
            >
              <Trash2 className="size-3.5" />
              Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}
