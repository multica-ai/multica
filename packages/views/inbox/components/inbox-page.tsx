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
  inboxListInFolderOptions,
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
} from "@multica/core/inbox/folders";
import { IssueDetail } from "../../issues/components";
import { useNavigation } from "../../navigation";
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
} from "lucide-react";
import type { InboxItem, InboxFolder, InboxFolderItemType } from "@multica/core/types";
import {
  chatSessionsOptions,
  chatSessionsInFolderOptions,
} from "@multica/core/chat/queries";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { agentListOptions } from "@multica/core/workspace/queries";
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
import { PageHeader } from "../../layout/page-header";
import { InboxListItem, timeAgo } from "./inbox-list-item";
import { typeLabels } from "./inbox-detail-label";
import { InboxChatPanel } from "./inbox-chat-panel";

type ViewMode = { kind: "inbox" } | { kind: "folder"; id: string };

export function InboxPage() {
  const { searchParams, replace } = useNavigation();
  const urlIssue = searchParams.get("issue") ?? "";
  const wsPaths = useWorkspacePaths();

  const [selectedKey, setSelectedKeyState] = useState(() => urlIssue);
  const [viewMode, setViewMode] = useState<ViewMode>({ kind: "inbox" });

  useEffect(() => {
    setSelectedKeyState(urlIssue);
  }, [urlIssue]);

  const wsId = useWorkspaceId();
  const isFolderView = viewMode.kind === "folder";
  const folderId = isFolderView ? viewMode.id : "";

  // Query both shapes; only one is enabled at a time. Doing it this way keeps
  // hook order stable and lets each query keep its own queryKey type.
  const inboxDefaultQuery = useQuery({
    ...inboxListOptions(wsId),
    enabled: !isFolderView,
  });
  const inboxInFolderQuery = useQuery({
    ...inboxListInFolderOptions(wsId, folderId),
    enabled: isFolderView && !!folderId,
  });
  const rawItems = isFolderView
    ? inboxInFolderQuery.data ?? []
    : inboxDefaultQuery.data ?? [];
  const loading = isFolderView ? inboxInFolderQuery.isLoading : inboxDefaultQuery.isLoading;
  const items = useMemo(
    () =>
      isFolderView
        ? rawItems.filter((i) => !i.archived)
        : deduplicateInboxItems(rawItems),
    [rawItems, isFolderView],
  );

  const chatDefaultQuery = useQuery({
    ...chatSessionsOptions(wsId),
    enabled: !isFolderView,
  });
  const chatInFolderQuery = useQuery({
    ...chatSessionsInFolderOptions(wsId, folderId),
    enabled: isFolderView && !!folderId,
  });
  const chatSessions = isFolderView
    ? chatInFolderQuery.data ?? []
    : chatDefaultQuery.data ?? [];

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const agentMap = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);

  const { data: folders = [] } = useQuery(inboxFolderListOptions(wsId));
  useQuery(inboxFolderMembershipsOptions(wsId)); // primed for cache; not read here

  const createFolder = useCreateInboxFolder();
  const renameFolder = useRenameInboxFolder();
  const deleteFolder = useDeleteInboxFolder();
  const addItemToFolder = useAddInboxFolderItem();

  const selectedChatSession = chatSessions.find((s) => s.id === selectedKey) ?? null;
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
    replace(wsPaths.issueDetail(selectedKey));
  }, [loading, selectedKey, selected, selectedChatSession, replace, wsPaths]);

  useEffect(() => {
    if (pendingChatIdRef.current && chatSessions.some((s) => s.id === pendingChatIdRef.current)) {
      pendingChatIdRef.current = null;
    }
  }, [chatSessions]);

  const setSelectedKey = useCallback((key: string) => {
    setSelectedKeyState(key);
    const inboxPath = wsPaths.inbox();
    const url = key ? `${inboxPath}?issue=${key}` : inboxPath;
    replace(url);
  }, [replace, wsPaths]);

  const handleArchiveChat = useCallback(async (sessionId: string) => {
    if (sessionId === selectedKey) setSelectedKey("");
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
    setSelectedKey(item.issue_id ?? item.id);
    if (!item.read) {
      markReadMutation.mutate(item.id, {
        onError: () => toast.error("Failed to mark as read"),
      });
    }
  };

  const handleArchive = (id: string) => {
    const archived = items.find((i) => i.id === id);
    if (archived && (archived.issue_id ?? archived.id) === selectedKey) setSelectedKey("");
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
    setSelectedKey("");
    archiveAllMutation.mutate(undefined, {
      onError: () => toast.error("Failed to archive all"),
    });
  };

  const handleArchiveAllRead = () => {
    const readKeys = items.filter((i) => i.read).map((i) => i.issue_id ?? i.id);
    if (readKeys.includes(selectedKey)) setSelectedKey("");
    archiveAllReadMutation.mutate(undefined, {
      onError: () => toast.error("Failed to archive read items"),
    });
  };

  const handleArchiveCompleted = () => {
    setSelectedKey("");
    archiveCompletedMutation.mutate(undefined, {
      onError: () => toast.error("Failed to archive completed"),
    });
  };

  const handleNewChat = () => {
    setSelectedKey("new-chat");
  };

  // -- Drag and drop ---------------------------------------------------------

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const [activeDrag, setActiveDrag] = useState<{
    type: InboxFolderItemType;
    id: string;
    label: string;
  } | null>(null);

  const handleDragStart = useCallback(
    (event: DragStartEvent) => {
      const id = String(event.active.id);
      const [type, itemId] = id.split(":");
      if (type === "notification") {
        const item = items.find((i) => i.id === itemId);
        if (item) setActiveDrag({ type: "notification", id: itemId ?? "", label: item.title });
      } else if (type === "chat_session") {
        const session = chatSessions.find((s) => s.id === itemId);
        if (session) {
          const agent = agentMap.get(session.agent_id);
          setActiveDrag({
            type: "chat_session",
            id: itemId ?? "",
            label: session.title || agent?.name || "Chat",
          });
        }
      }
    },
    [items, chatSessions, agentMap],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      setActiveDrag(null);
      const { active, over } = event;
      if (!over) return;
      const overId = String(over.id);
      const activeId = String(active.id);
      if (!overId.startsWith("folder:")) return;
      const targetFolderId = overId.slice("folder:".length);
      const [type, itemId] = activeId.split(":");
      if (!itemId) return;
      if (type !== "notification" && type !== "chat_session") return;
      addItemToFolder.mutate(
        { folderId: targetFolderId, itemType: type, itemId },
        { onError: () => toast.error("Failed to move item") },
      );
      if ((type === "notification" && selectedKey === itemId) || (type === "chat_session" && selectedKey === itemId)) {
        // Item is being moved out of view — clear selection.
        setSelectedKey("");
      }
    },
    [addItemToFolder, selectedKey, setSelectedKey],
  );

  const currentFolder = isFolderView ? folders.find((f) => f.id === folderId) : null;

  const listHeader = (
    <PageHeader className="justify-between">
      <div className="flex min-w-0 items-center gap-2">
        {isFolderView && (
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
        <h1 className="truncate text-sm font-semibold">
          {isFolderView ? currentFolder?.name ?? "Folder" : "Inbox"}
        </h1>
        {!isFolderView && unreadCount > 0 && (
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
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </PageHeader>
  );

  const mergedEntries = useMemo(() => {
    type Entry =
      | { kind: "chat"; id: string; time: number; session: typeof chatSessions[number] }
      | { kind: "notif"; id: string; time: number; item: InboxItem };
    const entries: Entry[] = [];
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

  const listBody = (
    <div>
      {mergedEntries.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
          <Inbox className="mb-3 h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm">
            {isFolderView ? "Empty folder" : "No notifications"}
          </p>
        </div>
      ) : (
        mergedEntries.map((entry) => {
          if (entry.kind === "chat") {
            const session = entry.session;
            const agent = agentMap.get(session.agent_id);
            return (
              <DraggableRow
                key={`chat:${session.id}`}
                draggableId={`chat_session:${session.id}`}
              >
                <div
                  className={`group/chat flex items-center gap-3 px-4 py-2.5 cursor-pointer transition-colors hover:bg-accent/50 ${
                    session.id === selectedKey ? "bg-accent" : ""
                  }`}
                  onClick={() => setSelectedKey(session.id)}
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
                    <span className={`text-xs ${session.has_unread ? "text-muted-foreground" : "text-muted-foreground/60"}`}>
                      {agent?.name} · {timeAgo(session.updated_at)}
                    </span>
                  </div>
                  <button
                    type="button"
                    className="hidden size-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground hover:bg-accent group-hover/chat:flex"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleArchiveChat(session.id);
                    }}
                    title="Archive"
                  >
                    <Archive className="size-3" />
                  </button>
                </div>
              </DraggableRow>
            );
          }
          const item = entry.item;
          return (
            <DraggableRow
              key={`notif:${item.id}`}
              draggableId={`notification:${item.id}`}
            >
              <InboxListItem
                item={item}
                isSelected={(item.issue_id ?? item.id) === selectedKey}
                onClick={() => handleSelect(item)}
                onArchive={() => handleArchive(item.id)}
              />
            </DraggableRow>
          );
        })
      )}
    </div>
  );

  const folderSection = (
    <FolderSection
      folders={folders}
      selectedFolderId={isFolderView ? folderId : null}
      onSelect={(id) => setViewMode(id ? { kind: "folder", id } : { kind: "inbox" })}
      onCreate={(name) =>
        createFolder.mutate(name, {
          onError: () => toast.error("Failed to create folder"),
        })
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
        setSelectedKey(id);
      }}
    />
  ) : selected?.issue_id ? (
    <IssueDetail
      key={selected.id}
      issueId={selected.issue_id}
      defaultSidebarOpen={false}
      layoutId="multica_inbox_issue_detail_layout"
      highlightCommentId={selected.details?.comment_id ?? undefined}
      onDelete={() => {
        handleArchive(selected.id);
      }}
    />
  ) : selected ? (
    <div className="p-6">
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
          <div className="flex h-12 shrink-0 items-center border-b px-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setSelectedKey("")}
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
  onCreate: (name: string) => void;
  onRename: (id: string, name: string) => void;
  onDelete: (id: string) => void;
}) {
  const [creating, setCreating] = useState(false);
  const [createName, setCreateName] = useState("");
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");

  const submitCreate = () => {
    const name = createName.trim();
    if (name) onCreate(name);
    setCreateName("");
    setCreating(false);
  };

  const submitRename = (id: string) => {
    const name = renameValue.trim();
    if (name) onRename(id, name);
    setRenamingId(null);
    setRenameValue("");
  };

  return (
    <div className="border-t bg-background">
      <div className="flex items-center justify-between px-4 py-2">
        <span className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Folders
        </span>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground"
          onClick={() => setCreating(true)}
          title="New folder"
        >
          <Plus className="h-3.5 w-3.5" />
        </Button>
      </div>
      <div className="pb-2">
        {folders.map((folder) => (
          <FolderRow
            key={folder.id}
            folder={folder}
            isSelected={selectedFolderId === folder.id}
            isRenaming={renamingId === folder.id}
            renameValue={renameValue}
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
            onDelete={() => onDelete(folder.id)}
          />
        ))}
        {creating && (
          <div className="flex items-center gap-2 px-4 py-2">
            <Folder className="size-4 shrink-0 text-muted-foreground" />
            <input
              autoFocus
              className="flex-1 bg-transparent text-sm outline-none"
              placeholder="Folder name"
              value={createName}
              onChange={(e) => setCreateName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") submitCreate();
                if (e.key === "Escape") {
                  setCreating(false);
                  setCreateName("");
                }
              }}
              onBlur={submitCreate}
            />
          </div>
        )}
        {folders.length === 0 && !creating && (
          <p className="px-4 py-1 text-xs text-muted-foreground">
            Drop chats and notifications here to organize them.
          </p>
        )}
      </div>
    </div>
  );
}

function FolderRow({
  folder,
  isSelected,
  isRenaming,
  renameValue,
  onSelect,
  onStartRename,
  onChangeRename,
  onSubmitRename,
  onCancelRename,
  onDelete,
}: {
  folder: InboxFolder;
  isSelected: boolean;
  isRenaming: boolean;
  renameValue: string;
  onSelect: () => void;
  onStartRename: () => void;
  onChangeRename: (value: string) => void;
  onSubmitRename: () => void;
  onCancelRename: () => void;
  onDelete: () => void;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: `folder:${folder.id}` });
  const Icon = isSelected ? FolderOpen : Folder;
  return (
    <div
      ref={setNodeRef}
      className={`group/folder flex items-center gap-2 px-4 py-1.5 cursor-pointer text-sm transition-colors ${
        isSelected ? "bg-accent" : ""
      } ${isOver ? "bg-accent/70 ring-1 ring-inset ring-brand" : "hover:bg-accent/50"}`}
      onClick={isRenaming ? undefined : onSelect}
    >
      <Icon className="size-4 shrink-0 text-muted-foreground" />
      {isRenaming ? (
        <input
          autoFocus
          className="flex-1 bg-transparent outline-none"
          value={renameValue}
          onChange={(e) => onChangeRename(e.target.value)}
          onClick={(e) => e.stopPropagation()}
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
              />
            }
          >
            <MoreHorizontal className="size-3" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-auto">
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
