"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  BookOpenText,
  ChevronRight,
  FileText,
  Folder,
  FolderOpen,
  MoreHorizontal,
  Plus,
  Trash2,
  History,
  GripVertical,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  DragOverlay,
  closestCenter,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { api } from "@multica/core/api";
import type { ListWikiPagesResponse, WikiPageSummary, WikiPageActivity, WikiPageType } from "@multica/core/types";
import { wikiKeys, wikiPageDetailOptions, wikiPageListOptions, wikiPageActivityOptions } from "@multica/core/wiki";
import { memberListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
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
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { useNavigation } from "../../navigation";
import {
  ContentEditor,
  FileDropOverlay,
  type ContentEditorRef,
  ReadonlyContent,
  useFileDropZone,
} from "../../editor";
import { PageHeader } from "../../layout/page-header";
import { buildWikiTree, flattenWikiTree, type WikiPageTreeNode } from "../lib/tree";

// ---------------------------------------------------------------------------
// Position computation (same lexorank pattern as Issues)
// ---------------------------------------------------------------------------
function computePosition(ids: string[], activeId: string, pageMap: Map<string, WikiPageSummary>): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => pageMap.get(id)?.position ?? 0;
  if (ids.length === 1) return pageMap.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

interface WikiPageProps {
  pageId?: string;
}

export function WikiPage({ pageId }: WikiPageProps) {
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const paths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const nav = useNavigation();
  const queryClient = useQueryClient();
  const editorRef = useRef<ContentEditorRef>(null);
  const lastSavedContentRef = useRef<string>("");
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<WikiPageTreeNode | null>(null);
  const [isSwitching, setIsSwitching] = useState(false);

  // DnD state
  const [activeId, setActiveId] = useState<string | null>(null);
  const [overFolderId, setOverFolderId] = useState<string | null>(null);

  const { data: members = [], isLoading: membersLoading } = useQuery(memberListOptions(wsId));
  const { data: pages = [], isLoading: pagesLoading } = useQuery(wikiPageListOptions(wsId));
  const selectedPageId = pageId && pages.some((page) => page.id === pageId)
    ? pageId
    : pages[0]?.id ?? null;
  const { data: selectedPage, isLoading: pageLoading } = useQuery(wikiPageDetailOptions(wsId, selectedPageId));
  const { data: activities = [] } = useQuery(wikiPageActivityOptions(wsId, selectedPageId));
  const { uploadWithToast, uploading } = useFileUpload(api, (err) => {
    toast.error(err.message);
  });

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canEdit =
    currentMember?.role === "owner" ||
    currentMember?.role === "admin" ||
    currentMember?.role === "member";
  // Deletion is ownership-gated for members: they may only delete pages they
  // created, while owners/admins may delete any page.
  const canDeletePage = useCallback(
    (page: { created_by: string | null }) =>
      currentMember?.role === "owner" ||
      currentMember?.role === "admin" ||
      (currentMember?.role === "member" && page.created_by === user?.id),
    [currentMember?.role, user?.id],
  );
  const tree = useMemo(() => buildWikiTree(pages), [pages]);
  const flatPages = useMemo(() => flattenWikiTree(tree), [tree]);
  const pageMap = useMemo(() => {
    const map = new Map<string, WikiPageSummary>();
    for (const page of pages) map.set(page.id, page);
    return map;
  }, [pages]);
  const childCountById = useMemo(() => {
    const counts = new Map<string, number>();
    const count = (node: WikiPageTreeNode): number => {
      const total = node.children.reduce((sum, child) => sum + 1 + count(child), 0);
      counts.set(node.id, total);
      return total;
    };
    tree.forEach(count);
    return counts;
  }, [tree]);

  const getMemberByUserId = useCallback(
    (userId: string | null) => (userId ? members.find((m) => m.user_id === userId) : undefined),
    [members],
  );

  useEffect(() => {
    if (selectedPageId && pageId !== selectedPageId) {
      nav.replace(paths.wikiPage(selectedPageId));
    }
  }, [nav, pageId, paths, selectedPageId]);

  useEffect(() => {
    lastSavedContentRef.current = selectedPage?.content ?? "";
  }, [selectedPage?.id, selectedPage?.content]);

  const saveCurrentPage = useCallback(async () => {
    if (!canEdit || !selectedPageId) return true;
    const latestContent = editorRef.current?.getMarkdown();
    if (latestContent == null || latestContent === lastSavedContentRef.current) return true;
    try {
      const updated = await api.updateWikiPage(selectedPageId, { content: latestContent });
      lastSavedContentRef.current = updated.content;
      queryClient.setQueryData(wikiKeys.detail(wsId, selectedPageId), updated);
      queryClient.invalidateQueries({ queryKey: wikiKeys.list(wsId) });
      return true;
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save wiki page");
      return false;
    }
  }, [canEdit, queryClient, selectedPageId, wsId]);

  const selectPage = useCallback(async (targetId: string) => {
    if (targetId === selectedPageId || isSwitching) return;
    setIsSwitching(true);
    const ok = await saveCurrentPage();
    setIsSwitching(false);
    if (ok) nav.push(paths.wikiPage(targetId));
  }, [isSwitching, nav, paths, saveCurrentPage, selectedPageId]);

  const createPage = useCallback(async (parentId: string | null = null, type: WikiPageType = "page") => {
    if (!canEdit) return;
    const ok = await saveCurrentPage();
    if (!ok) return;
    const defaultTitle = type === "folder" ? "新文件夹" : "新页面";
    try {
      const page = await api.createWikiPage({ title: defaultTitle, parent_id: parentId, type });
      queryClient.setQueryData<ListWikiPagesResponse>(wikiKeys.list(wsId), (old) => {
        if (!old || old.pages.some((existing) => existing.id === page.id)) return old;
        const { content: _content, ...summary } = page;
        return {
          ...old,
          pages: [...old.pages, summary],
          total: old.total + 1,
        };
      });
      queryClient.setQueryData(wikiKeys.detail(wsId, page.id), page);
      queryClient.invalidateQueries({ queryKey: wikiKeys.list(wsId) });
      if (type === "page") {
        nav.push(paths.wikiPage(page.id));
      }
      setRenamingId(page.id);
      setRenameValue(page.title);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : `Failed to create wiki ${type}`);
    }
  }, [canEdit, nav, paths, queryClient, saveCurrentPage, wsId]);

  const createFolder = useCallback(async () => {
    await createPage(null, "folder");
  }, [createPage]);

  const renamePage = useCallback(async (page: WikiPageSummary, title: string) => {
    const nextTitle = title.trim();
    setRenamingId(null);
    if (!canEdit || !nextTitle || nextTitle === page.title) return;
    try {
      const updated = await api.updateWikiPage(page.id, { title: nextTitle });
      queryClient.setQueryData(wikiKeys.detail(wsId, page.id), updated);
      queryClient.invalidateQueries({ queryKey: wikiKeys.list(wsId) });
      queryClient.invalidateQueries({ queryKey: wikiKeys.activity(wsId, page.id) });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to rename wiki page");
    }
  }, [canEdit, queryClient, wsId]);

  const deletePage = useCallback(async () => {
    if (!deleteTarget || !canDeletePage(deleteTarget)) return;
    try {
      await api.deleteWikiPage(deleteTarget.id);
      const deletedIds = new Set<string>();
      const collectDeletedIds = (node: WikiPageTreeNode) => {
        deletedIds.add(node.id);
        node.children.forEach(collectDeletedIds);
      };
      collectDeletedIds(deleteTarget);
      const remaining = flatPages.filter((page) => !deletedIds.has(page.id));
      const fallback = remaining.find((page) => page.parent_id === deleteTarget.parent_id && page.position > deleteTarget.position)
        ?? [...remaining].reverse().find((page) => page.parent_id === deleteTarget.parent_id && page.position < deleteTarget.position)
        ?? remaining.find((page) => page.id === deleteTarget.parent_id)
        ?? remaining[0]
        ?? null;
      setDeleteTarget(null);
      queryClient.invalidateQueries({ queryKey: wikiKeys.list(wsId) });
      for (const id of deletedIds) {
        queryClient.removeQueries({ queryKey: wikiKeys.detail(wsId, id) });
      }
      nav.replace(fallback ? paths.wikiPage(fallback.id) : paths.wiki());
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete wiki page");
    }
  }, [canDeletePage, deleteTarget, flatPages, nav, paths, queryClient, wsId]);

  const handleUpdate = useCallback(async (content: string) => {
    if (!selectedPageId || !canEdit) return;
    if (content === lastSavedContentRef.current) return;
    try {
      const updated = await api.updateWikiPage(selectedPageId, { content });
      lastSavedContentRef.current = updated.content;
      queryClient.setQueryData(wikiKeys.detail(wsId, selectedPageId), updated);
      queryClient.invalidateQueries({ queryKey: wikiKeys.list(wsId) });
      queryClient.invalidateQueries({ queryKey: wikiKeys.activity(wsId, selectedPageId) });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to save wiki page");
    }
  }, [canEdit, queryClient, selectedPageId, wsId]);

  const handleBlur = useCallback(() => {
    void saveCurrentPage();
  }, [saveCurrentPage]);

  const handleUpload = useCallback(
    (file: File) => uploadWithToast(file),
    [uploadWithToast],
  );

  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => editorRef.current?.uploadFiles(files),
    enabled: canEdit && !!selectedPage,
  });

  // ---- DnD handlers ----
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  // Build ordered ID lists for each "container" (top-level + each folder)
  const containerIds = useMemo(() => {
    const map = new Map<string | null, string[]>();
    // Top-level items (parent_id = null)
    const topIds: string[] = [];
    for (const node of tree) topIds.push(node.id);
    map.set(null, topIds);
    // Per-folder items
    for (const node of tree) {
      if (node.type === "folder" && node.children.length > 0) {
        map.set(node.id, node.children.map((c) => c.id));
      } else if (node.type === "folder") {
        map.set(node.id, []);
      }
    }
    // Also include folders that are children (shouldn't happen, but just in case)
    for (const page of pages) {
      if (page.type === "folder" && !map.has(page.id)) {
        map.set(page.id, []);
      }
    }
    return map;
  }, [tree, pages]);

  const handleDragStart = useCallback((event: DragStartEvent) => {
    setActiveId(String(event.active.id));
  }, []);

  const handleDragOver = useCallback((event: DragOverEvent) => {
    const { over } = event;
    if (!over) {
      setOverFolderId(null);
      return;
    }
    const overId = String(over.id);
    const overPage = pageMap.get(overId);
    // Highlight folder when hovering over it
    if (overPage?.type === "folder") {
      setOverFolderId(overId);
    } else {
      setOverFolderId(null);
    }
  }, [pageMap]);

  const handleDragEnd = useCallback(async (event: DragEndEvent) => {
    const { active, over } = event;
    setActiveId(null);
    setOverFolderId(null);

    if (!over || active.id === over.id || !canEdit) return;

    const activeItemId = String(active.id);
    const overItemId = String(over.id);
    const activePage = pageMap.get(activeItemId);
    const overPage = pageMap.get(overItemId);
    if (!activePage) return;

    // Determine target folder: if dropping on a folder, move into it
    // If dropping on a page, keep in the same container as the over item
    let targetParentId: string | null;
    if (overPage?.type === "folder") {
      // Dropping onto a folder → move INTO that folder (only for pages, not folders)
      if (activePage.type === "folder") {
        // Folders cannot be nested - snap back
        toast.error("文件夹不能嵌套到其他文件夹中");
        return;
      }
      targetParentId = overItemId;
      // Place at the end of the folder
      const folderChildren = containerIds.get(overItemId) ?? [];
      const newPosition = folderChildren.length > 0
        ? (pageMap.get(folderChildren[folderChildren.length - 1]!)?.position ?? 0) + 1
        : 0;

      try {
        const result = await api.reorderWikiPages({
          pages: [{ id: activeItemId, position: newPosition, parent_id: targetParentId }],
        });
        queryClient.setQueryData<ListWikiPagesResponse>(wikiKeys.list(wsId), (old) => {
          if (!old) return old;
          return { pages: result.pages, total: result.total };
        });
        toast.success(`已移入文件夹「${overPage.title}」`);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "移动失败");
        queryClient.invalidateQueries({ queryKey: wikiKeys.list(wsId) });
      }
      return;
    }

    // Dropping on a page → reorder within the same container
    targetParentId = overPage?.parent_id ?? null;
    const activeParentId = activePage.parent_id;

    // Prevent folder from being dragged into another folder
    if (activePage.type === "folder" && targetParentId !== null) {
      // This shouldn't happen since folders are always top-level, but guard anyway
      toast.error("文件夹不能嵌套到其他文件夹中");
      return;
    }

    // If moving between containers (different parent), handle the cross-folder move
    if (activeParentId !== targetParentId && activePage.type === "page") {
      const container = containerIds.get(targetParentId) ?? [];
      const newContainer = activeParentId
        ? [...container, activeItemId]  // adding to new container
        : [...container, activeItemId];

      // Compute position based on where it was dropped
      const overIdx = newContainer.indexOf(overItemId);
      const reordered = arrayMove(newContainer, newContainer.indexOf(activeItemId), overIdx >= 0 ? overIdx : newContainer.length - 1);
      const newPosition = computePosition(reordered, activeItemId, pageMap);

      try {
        const result = await api.reorderWikiPages({
          // Use "" to signal "clear parent_id" when moving to top level
          pages: [{ id: activeItemId, position: newPosition, parent_id: targetParentId ?? "" }],
        });
        queryClient.setQueryData<ListWikiPagesResponse>(wikiKeys.list(wsId), (old) => {
          if (!old) return old;
          return { pages: result.pages, total: result.total };
        });
        toast.success("移动完成");
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "移动失败");
        queryClient.invalidateQueries({ queryKey: wikiKeys.list(wsId) });
      }
      return;
    }

    // Same container reorder
    const container = containerIds.get(targetParentId) ?? [];
    const activeIdx = container.indexOf(activeItemId);
    const overIdx = container.indexOf(overItemId);
    if (activeIdx === -1 || overIdx === -1) return;

    const reordered = arrayMove(container, activeIdx, overIdx);
    const newPosition = computePosition(reordered, activeItemId, pageMap);

    try {
      const result = await api.reorderWikiPages({
        pages: [{ id: activeItemId, position: newPosition }],
      });
      queryClient.setQueryData<ListWikiPagesResponse>(wikiKeys.list(wsId), (old) => {
        if (!old) return old;
        return { pages: result.pages, total: result.total };
      });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "排序失败");
      queryClient.invalidateQueries({ queryKey: wikiKeys.list(wsId) });
    }
  }, [canEdit, containerIds, pageMap, queryClient, wsId]);

  const activePage = activeId ? pageMap.get(activeId) : null;

  if (!workspace || membersLoading || pagesLoading) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <PageHeader>
          <Skeleton className="h-5 w-5 rounded" />
          <Skeleton className="ml-2 h-4 w-20" />
        </PageHeader>
        <div className="flex min-h-0 flex-1">
          <div className="w-72 border-r p-4">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="mt-4 h-32 w-full" />
          </div>
          <div className="flex-1 p-8">
            <Skeleton className="h-8 w-48" />
            <Skeleton className="mt-6 h-40 w-full" />
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader>
        <div className="flex items-center gap-2">
          <BookOpenText className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Wiki</h1>
        </div>
        <span className="ml-auto text-xs text-muted-foreground">
          {canEdit ? "All members can edit" : "Read-only"}
        </span>
      </PageHeader>

      <div className="flex min-h-0 flex-1">
        <aside className="flex w-72 shrink-0 flex-col border-r bg-muted/20">
          <div className="flex h-12 items-center gap-2 border-b px-3">
            <span className="text-sm font-medium">Pages</span>
            {canEdit && (
              <div className="ml-auto flex items-center gap-1">
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button size="icon-sm" variant="ghost" onClick={() => void createFolder()}>
                        <Folder className="size-4" />
                      </Button>
                    }
                  />
                  <TooltipContent>新建文件夹</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <Button size="icon-sm" variant="ghost" onClick={() => void createPage(null)}>
                        <Plus className="size-4" />
                      </Button>
                    }
                  />
                  <TooltipContent>新建页面</TooltipContent>
                </Tooltip>
              </div>
            )}
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {tree.length ? (
              <DndContext
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragStart={handleDragStart}
                onDragOver={handleDragOver}
                onDragEnd={handleDragEnd}
              >
                <SortableContext
                  items={containerIds.get(null) ?? []}
                  strategy={verticalListSortingStrategy}
                >
                  <WikiTree
                    nodes={tree}
                    selectedPageId={selectedPageId}
                    canEdit={canEdit}
                    renamingId={renamingId}
                    renameValue={renameValue}
                    onRenameValueChange={setRenameValue}
                    onStartRename={(page) => {
                      setRenamingId(page.id);
                      setRenameValue(page.title);
                    }}
                    onCommitRename={renamePage}
                    onSelect={(id) => void selectPage(id)}
                    onCreateChild={(id) => void createPage(id)}
                    onDelete={setDeleteTarget}
                    canDeletePage={canDeletePage}
                    members={members}
                    overFolderId={overFolderId}
                  />
                </SortableContext>
                <DragOverlay>
                  {activeId && activePage ? (
                    <div className="flex h-8 items-center gap-1.5 rounded-md border bg-background px-2 text-sm shadow-lg">
                      {activePage.type === "folder" ? (
                        <Folder className="size-3.5 text-muted-foreground" />
                      ) : (
                        <FileText className="size-3.5 text-muted-foreground" />
                      )}
                      <span className="truncate">{activePage.title}</span>
                    </div>
                  ) : null}
                </DragOverlay>
              </DndContext>
            ) : (
              <div className="px-3 py-10 text-center">
                <FileText className="mx-auto size-8 text-muted-foreground" />
                <p className="mt-3 text-sm font-medium">No wiki pages yet</p>
                <p className="mt-1 text-xs text-muted-foreground">Create the first page to start documenting this workspace.</p>
                {canEdit && (
                  <div className="mt-4 flex items-center justify-center gap-2">
                    <Button size="sm" onClick={() => void createPage(null)}>
                      <Plus className="size-3.5" />
                      New page
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => void createFolder()}>
                      <Folder className="size-3.5" />
                      New folder
                    </Button>
                  </div>
                )}
              </div>
            )}
          </div>
        </aside>

        <main className="min-w-0 flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-4xl px-8 py-8">
            {selectedPage ? (
              <>
                <h2 className="text-2xl font-bold leading-snug tracking-tight">{selectedPage.title}</h2>
                <WikiPageMeta
                  createdBy={selectedPage.created_by}
                  updatedBy={selectedPage.updated_by}
                  createdAt={selectedPage.created_at}
                  updatedAt={selectedPage.updated_at}
                  getMemberByUserId={getMemberByUserId}
                />
                <div
                  {...(canEdit ? dropZoneProps : {})}
                  className={cn("relative mt-6", canEdit && "rounded-lg")}
                >
                  {canEdit ? (
                    <>
                      <ContentEditor
                        ref={editorRef}
                        key={selectedPage.id}
                        defaultValue={selectedPage.content}
                        placeholder="Add wiki content..."
                        onUpdate={handleUpdate}
                        onBlur={handleBlur}
                        onUploadFile={handleUpload}
                        debounceMs={1500}
                      />
                      <div className="mt-3 flex items-center gap-1">
                        <FileUploadButton
                          size="sm"
                          disabled={uploading}
                          multiple
                          onSelect={(file) => editorRef.current?.uploadFile(file)}
                          onSelectMany={(files) => editorRef.current?.uploadFiles(files)}
                        />
                      </div>
                      {isDragOver && <FileDropOverlay />}
                    </>
                  ) : selectedPage.content.trim() ? (
                    <ReadonlyContent content={selectedPage.content} />
                  ) : (
                    <p className="text-sm text-muted-foreground">No wiki content yet.</p>
                  )}
                </div>
                {activities.length > 0 && (
                  <WikiPageActivityList activities={activities} getMemberByUserId={getMemberByUserId} />
                )}
              </>
            ) : pageLoading ? (
              <>
                <Skeleton className="h-8 w-48" />
                <Skeleton className="mt-6 h-40 w-full" />
              </>
            ) : (
              <div className="py-24 text-center">
                <BookOpenText className="mx-auto size-10 text-muted-foreground" />
                <h2 className="mt-4 text-lg font-semibold">No wiki page selected</h2>
                <p className="mt-1 text-sm text-muted-foreground">Select a page from the tree or create a new one.</p>
                {canEdit && (
                  <div className="mt-5 flex items-center justify-center gap-2">
                    <Button onClick={() => void createPage(null)}>
                      <Plus className="size-4" />
                      New page
                    </Button>
                    <Button variant="outline" onClick={() => void createFolder()}>
                      <Folder className="size-4" />
                      New folder
                    </Button>
                  </div>
                )}
              </div>
            )}
          </div>
        </main>
      </div>

      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Delete {deleteTarget?.type === "folder" ? "folder" : "page"}{deleteTarget ? ` "${deleteTarget.title}"` : ""}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget && (childCountById.get(deleteTarget.id) ?? 0) > 0
                ? `This will also delete ${childCountById.get(deleteTarget.id)} child page(s). This action cannot be undone.`
                : "This action cannot be undone."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={() => void deletePage()}>
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Sortable tree item component
// ---------------------------------------------------------------------------
function SortableWikiItem({
  node,
  selectedPageId,
  canEdit,
  renamingId,
  renameValue,
  onRenameValueChange,
  onStartRename,
  onCommitRename,
  onSelect,
  onCreateChild,
  onDelete,
  canDeletePage,
  members,
  overFolderId,
  depth = 0,
}: {
  node: WikiPageTreeNode;
  selectedPageId: string | null;
  canEdit: boolean;
  renamingId: string | null;
  renameValue: string;
  onRenameValueChange: (value: string) => void;
  onStartRename: (page: WikiPageSummary) => void;
  onCommitRename: (page: WikiPageSummary, value: string) => void | Promise<void>;
  onSelect: (id: string) => void;
  onCreateChild: (id: string) => void;
  onDelete: (page: WikiPageTreeNode) => void;
  canDeletePage: (page: { created_by: string | null }) => boolean;
  members: Array<{ user_id: string; name: string; avatar_url: string | null }>;
  overFolderId: string | null;
  depth?: number;
}) {
  const isFolder = node.type === "folder";
  const isRenaming = node.id === renamingId;
  const isActive = node.id === selectedPageId;
  const isDragOverFolder = node.id === overFolderId;
  const [expanded, setExpanded] = useState(true);
  const creator = node.created_by ? members.find((m) => m.user_id === node.created_by) : undefined;

  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: node.id });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.5 : 1,
  };

  return (
    <div ref={setNodeRef} style={style}>
      <div
        className={cn(
          "group flex h-8 items-center gap-1 rounded-md pr-1 text-sm",
          isActive ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
          isDragOverFolder && isFolder && "ring-2 ring-primary/50 bg-primary/10",
        )}
        style={{ paddingLeft: 6 + depth * 14 }}
      >
        {canEdit && (
          <button
            type="button"
            className="cursor-grab shrink-0 opacity-0 group-hover:opacity-100 transition-opacity"
            {...attributes}
            {...listeners}
          >
            <GripVertical className="size-3.5 text-muted-foreground" />
          </button>
        )}
        {isFolder ? (
          <button
            type="button"
            className="shrink-0"
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? (
              <ChevronRight className="size-3.5 rotate-90 text-muted-foreground" />
            ) : (
              <ChevronRight className="size-3.5 text-muted-foreground" />
            )}
          </button>
        ) : node.children.length > 0 ? (
          <ChevronRight className="size-3.5 rotate-90 text-muted-foreground" />
        ) : (
          <span className="size-3.5" />
        )}
        {isFolder ? (
          expanded ? (
            <FolderOpen className="size-3.5 shrink-0 text-muted-foreground" />
          ) : (
            <Folder className="size-3.5 shrink-0 text-muted-foreground" />
          )
        ) : (
          <FileText className="size-3.5 shrink-0 text-muted-foreground" />
        )}
        {isRenaming ? (
          <Input
            autoFocus
            value={renameValue}
            onChange={(event) => onRenameValueChange(event.target.value)}
            onBlur={() => void onCommitRename(node, renameValue)}
            onKeyDown={(event) => {
              if (event.key === "Enter") event.currentTarget.blur();
              if (event.key === "Escape") onCommitRename(node, node.title);
            }}
            className="h-6 flex-1 px-1.5 text-sm"
          />
        ) : (
          <button
            type="button"
            onClick={() => onSelect(node.id)}
            className="min-w-0 flex-1 truncate text-left"
          >
            {node.title}
          </button>
        )}
        {creator && !isRenaming && (
          <Tooltip>
            <TooltipTrigger
              render={
                <span className="shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
                  <ActorAvatar actorType="member" actorId={creator.user_id} size={14} />
                </span>
              }
            />
            <TooltipContent side="right">{creator.name}</TooltipContent>
          </Tooltip>
        )}
        {canEdit && !isRenaming && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={(
                <Button size="icon-sm" variant="ghost" className="opacity-0 group-hover:opacity-100">
                  <MoreHorizontal className="size-3.5" />
                </Button>
              )}
            />
            <DropdownMenuContent align="end" className="w-40">
              {isFolder && (
                <DropdownMenuItem onClick={() => onCreateChild(node.id)}>
                  <Plus className="size-3.5" />
                  添加页面
                </DropdownMenuItem>
              )}
              {!isFolder && (
                <DropdownMenuItem onClick={() => onCreateChild(node.id)}>
                  <Plus className="size-3.5" />
                  New child page
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onClick={() => onStartRename(node)}>
                Rename
              </DropdownMenuItem>
              {canDeletePage(node) && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onClick={() => onDelete(node)}>
                    <Trash2 className="size-3.5" />
                    Delete
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      {isFolder && expanded && node.children.length > 0 && (
        <SortableContext
          items={node.children.map((c) => c.id)}
          strategy={verticalListSortingStrategy}
        >
          <WikiTree
            nodes={node.children}
            selectedPageId={selectedPageId}
            canEdit={canEdit}
            renamingId={renamingId}
            renameValue={renameValue}
            onRenameValueChange={onRenameValueChange}
            onStartRename={onStartRename}
            onCommitRename={onCommitRename}
            onSelect={onSelect}
            onCreateChild={onCreateChild}
            onDelete={onDelete}
            canDeletePage={canDeletePage}
            members={members}
            overFolderId={overFolderId}
            depth={depth + 1}
          />
        </SortableContext>
      )}
      {!isFolder && node.children.length > 0 && (
        <WikiTree
          nodes={node.children}
          selectedPageId={selectedPageId}
          canEdit={canEdit}
          renamingId={renamingId}
          renameValue={renameValue}
          onRenameValueChange={onRenameValueChange}
          onStartRename={onStartRename}
          onCommitRename={onCommitRename}
          onSelect={onSelect}
          onCreateChild={onCreateChild}
          onDelete={onDelete}
          canDeletePage={canDeletePage}
          members={members}
          overFolderId={overFolderId}
          depth={depth + 1}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// WikiTree (renders a list of sortable items)
// ---------------------------------------------------------------------------
function WikiTree({
  nodes,
  selectedPageId,
  canEdit,
  renamingId,
  renameValue,
  onRenameValueChange,
  onStartRename,
  onCommitRename,
  onSelect,
  onCreateChild,
  onDelete,
  canDeletePage,
  members,
  overFolderId,
  depth = 0,
}: {
  nodes: WikiPageTreeNode[];
  selectedPageId: string | null;
  canEdit: boolean;
  renamingId: string | null;
  renameValue: string;
  onRenameValueChange: (value: string) => void;
  onStartRename: (page: WikiPageSummary) => void;
  onCommitRename: (page: WikiPageSummary, value: string) => void | Promise<void>;
  onSelect: (id: string) => void;
  onCreateChild: (id: string) => void;
  onDelete: (page: WikiPageTreeNode) => void;
  canDeletePage: (page: { created_by: string | null }) => boolean;
  members: Array<{ user_id: string; name: string; avatar_url: string | null }>;
  overFolderId: string | null;
  depth?: number;
}) {
  return (
    <div className="space-y-0.5">
      {nodes.map((node) => (
        <SortableWikiItem
          key={node.id}
          node={node}
          selectedPageId={selectedPageId}
          canEdit={canEdit}
          renamingId={renamingId}
          renameValue={renameValue}
          onRenameValueChange={onRenameValueChange}
          onStartRename={onStartRename}
          onCommitRename={onCommitRename}
          onSelect={onSelect}
          onCreateChild={onCreateChild}
          onDelete={onDelete}
          canDeletePage={canDeletePage}
          members={members}
          overFolderId={overFolderId}
          depth={depth}
        />
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Helper sub-components (unchanged from original)
// ---------------------------------------------------------------------------
function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  if (diffMin < 1) return "just now";
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHours = Math.floor(diffMin / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  if (diffDays < 30) return `${diffDays}d ago`;
  return date.toLocaleDateString();
}

function WikiPageMeta({
  createdBy,
  updatedBy,
  createdAt,
  updatedAt,
  getMemberByUserId,
}: {
  createdBy: string | null;
  updatedBy: string | null;
  createdAt: string;
  updatedAt: string;
  getMemberByUserId: (userId: string | null) => { user_id: string; name: string; avatar_url: string | null } | undefined;
}) {
  const creator = getMemberByUserId(createdBy);
  const updater = getMemberByUserId(updatedBy);
  const isUpdated = createdAt !== updatedAt;

  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
      <span className="inline-flex items-center gap-1.5">
        {creator ? (
          <>
            <ActorAvatar actorType="member" actorId={creator.user_id} size={16} />
            <span>{creator.name}</span>
          </>
        ) : (
          <span className="italic">Unknown author</span>
        )}
        <span>·</span>
        <Tooltip>
          <TooltipTrigger
            render={<time dateTime={createdAt}>{formatRelativeTime(createdAt)}</time>}
          />
          <TooltipContent>{new Date(createdAt).toLocaleString()}</TooltipContent>
        </Tooltip>
      </span>
      {isUpdated && (
        <span className="inline-flex items-center gap-1.5">
          <span>Updated by</span>
          {updater ? (
            <>
              <ActorAvatar actorType="member" actorId={updater.user_id} size={16} />
              <span>{updater.name}</span>
            </>
          ) : (
            <span className="italic">unknown</span>
          )}
          <span>·</span>
          <Tooltip>
            <TooltipTrigger
              render={<time dateTime={updatedAt}>{formatRelativeTime(updatedAt)}</time>}
            />
            <TooltipContent>{new Date(updatedAt).toLocaleString()}</TooltipContent>
          </Tooltip>
        </span>
      )}
    </div>
  );
}

const activityLabels: Record<string, string> = {
  created: "created this page",
  updated: "updated this page",
  title_updated: "renamed this page",
  content_updated: "edited content",
  deleted: "deleted this page",
};

function WikiPageActivityList({
  activities,
  getMemberByUserId,
}: {
  activities: WikiPageActivity[];
  getMemberByUserId: (userId: string | null) => { user_id: string; name: string; avatar_url: string | null } | undefined;
}) {
  const [expanded, setExpanded] = useState(false);
  const visibleActivities = expanded ? activities : activities.slice(0, 5);

  return (
    <div className="mt-8 border-t pt-6">
      <h3 className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
        <History className="size-4" />
        Activity
      </h3>
      <div className="mt-3 space-y-2">
        {visibleActivities.map((activity) => {
          const member = getMemberByUserId(activity.actor_id);
          return (
            <div key={activity.id} className="flex items-center gap-2 text-xs text-muted-foreground">
              {member ? (
                <ActorAvatar actorType="member" actorId={member.user_id} size={18} />
              ) : (
                <span className="inline-flex size-[18px] items-center justify-center rounded-full bg-muted text-[10px]">?</span>
              )}
              <span className="font-medium text-foreground">{member?.name ?? "Unknown"}</span>
              <span>{activityLabels[activity.action] ?? activity.action}</span>
              {"title" in activity.details && (
                <span className="truncate italic">&quot;{String(activity.details.title)}&quot;</span>
              )}
              <Tooltip>
                <TooltipTrigger
                  render={
                    <time className="ml-auto shrink-0" dateTime={activity.created_at}>
                      {formatRelativeTime(activity.created_at)}
                    </time>
                  }
                />
                <TooltipContent>{new Date(activity.created_at).toLocaleString()}</TooltipContent>
              </Tooltip>
            </div>
          );
        })}
      </div>
      {activities.length > 5 && !expanded && (
        <Button variant="ghost" size="sm" className="mt-2 text-xs" onClick={() => setExpanded(true)}>
          Show all {activities.length} activities
        </Button>
      )}
    </div>
  );
}
