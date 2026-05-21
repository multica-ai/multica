"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  BookOpenText,
  ChevronRight,
  FileText,
  MoreHorizontal,
  Plus,
  Trash2,
  History,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { api } from "@multica/core/api";
import type { ListWikiPagesResponse, WikiPageSummary, WikiPageActivity } from "@multica/core/types";
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
  const canEdit = currentMember?.role === "owner" || currentMember?.role === "admin";
  const tree = useMemo(() => buildWikiTree(pages), [pages]);
  const flatPages = useMemo(() => flattenWikiTree(tree), [tree]);
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

  const createPage = useCallback(async (parentId: string | null = null) => {
    if (!canEdit) return;
    const ok = await saveCurrentPage();
    if (!ok) return;
    try {
      const page = await api.createWikiPage({ title: "新页面", parent_id: parentId });
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
      nav.push(paths.wikiPage(page.id));
      setRenamingId(page.id);
      setRenameValue(page.title);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create wiki page");
    }
  }, [canEdit, nav, paths, queryClient, saveCurrentPage, wsId]);

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
    if (!deleteTarget || !canEdit) return;
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
  }, [canEdit, deleteTarget, flatPages, nav, paths, queryClient, wsId]);

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
          {canEdit ? "Owner/Admin can edit" : "Read-only"}
        </span>
      </PageHeader>

      <div className="flex min-h-0 flex-1">
        <aside className="flex w-72 shrink-0 flex-col border-r bg-muted/20">
          <div className="flex h-12 items-center gap-2 border-b px-3">
            <span className="text-sm font-medium">Pages</span>
            {canEdit && (
              <Button size="icon-sm" variant="ghost" className="ml-auto" onClick={() => void createPage(null)}>
                <Plus className="size-4" />
              </Button>
            )}
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {tree.length ? (
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
                members={members}
              />
            ) : (
              <div className="px-3 py-10 text-center">
                <FileText className="mx-auto size-8 text-muted-foreground" />
                <p className="mt-3 text-sm font-medium">No wiki pages yet</p>
                <p className="mt-1 text-xs text-muted-foreground">Create the first page to start documenting this workspace.</p>
                {canEdit && (
                  <Button size="sm" className="mt-4" onClick={() => void createPage(null)}>
                    <Plus className="size-3.5" />
                    New page
                  </Button>
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
                  <Button className="mt-5" onClick={() => void createPage(null)}>
                    <Plus className="size-4" />
                    New page
                  </Button>
                )}
              </div>
            )}
          </div>
        </main>
      </div>

      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete page{deleteTarget ? ` "${deleteTarget.title}"` : ""}?</AlertDialogTitle>
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
  members,
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
  members: Array<{ user_id: string; name: string; avatar_url: string | null }>;
  depth?: number;
}) {
  return (
    <div className={depth === 0 ? "space-y-0.5" : "space-y-0.5"}>
      {nodes.map((node) => {
        const active = node.id === selectedPageId;
        const renaming = node.id === renamingId;
        const creator = node.created_by ? members.find((m) => m.user_id === node.created_by) : undefined;
        return (
          <div key={node.id}>
            <div
              className={cn(
                "group flex h-8 items-center gap-1 rounded-md pr-1 text-sm",
                active ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
              )}
              style={{ paddingLeft: 6 + depth * 14 }}
            >
              {node.children.length ? (
                <ChevronRight className="size-3.5 rotate-90 text-muted-foreground" />
              ) : (
                <span className="size-3.5" />
              )}
              {renaming ? (
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
              {creator && !renaming && (
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
              {canEdit && !renaming && (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={(
                      <Button size="icon-sm" variant="ghost" className="opacity-0 group-hover:opacity-100">
                        <MoreHorizontal className="size-3.5" />
                      </Button>
                    )}
                  />
                  <DropdownMenuContent align="end" className="w-40">
                    <DropdownMenuItem onClick={() => onCreateChild(node.id)}>
                      <Plus className="size-3.5" />
                      New child page
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={() => onStartRename(node)}>
                      Rename
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem variant="destructive" onClick={() => onDelete(node)}>
                      <Trash2 className="size-3.5" />
                      Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </div>
            {node.children.length > 0 && (
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
                members={members}
                depth={depth + 1}
              />
            )}
          </div>
        );
      })}
    </div>
  );
}

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
