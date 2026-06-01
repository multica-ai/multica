"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  viewListOptions,
  useCreateView,
  useUpdateView,
  useDeleteView,
  useReorderViews,
} from "@multica/core/views";
import { useWorkspaceId } from "@multica/core/hooks";
import type { SavedView, ViewFilters, ViewPage } from "@multica/core/types";
import {
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  closestCenter,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  horizontalListSortingStrategy,
  useSortable,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { MoreHorizontal, Pencil, Plus, Trash2, Users, Lock } from "lucide-react";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { useSeedDefaultViews } from "./use-seed-default-views";
import { ViewFormDialog, type ViewFormValues } from "./view-form-dialog";

type SeedablePage = Exclude<ViewPage, "project">;

/**
 * Shared saved-view tab strip for the Issues and My Issues headers. ONE
 * component for both pages — store coupling is injected via props
 * (`currentViewId` + `onSelectView`) so the component stays store-agnostic.
 *
 * Behaviour:
 *   - Reads saved views from the views query cache (viewListOptions).
 *   - Lazily seeds the page's default views on first visit (see
 *     useSeedDefaultViews); the page passes `resolveDefaultName` so each page
 *     resolves its own `views.defaults` i18n namespace statically (avoids a
 *     union-namespace dynamic lookup here).
 *   - URL is the source of truth for the active view: `?view=<id>` is read on
 *     mount → onSelectView, and written on tab click. Falls back to the first
 *     view (the seeded "All") when no ?view= is present, so a refresh restores
 *     the active tab without component useState.
 *   - Management (when `currentFilters` is provided): a "+" button saves the
 *     live filters as a new view; non-default views get a ⋯ menu to rename,
 *     toggle workspace sharing, or delete; tabs drag-reorder via dnd-kit.
 */
export function ViewTabs({
  page,
  projectId,
  currentViewId,
  onSelectView,
  resolveDefaultName,
  currentFilters,
}: {
  page: SeedablePage;
  projectId?: string;
  currentViewId: string | null;
  onSelectView: (view: SavedView | null) => void;
  /** Maps a default view's i18n key to a display name (page-owned namespace). */
  resolveDefaultName: (nameKey: string) => string;
  /** The page's live effective filters. When set, the management UI is enabled. */
  currentFilters?: ViewFilters;
}) {
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const { t } = useT("common");

  const { data: views, isLoading } = useQuery(viewListOptions(wsId, page, projectId));

  useSeedDefaultViews(wsId, page, views, isLoading, resolveDefaultName, projectId);

  const createView = useCreateView(page, projectId);
  const updateView = useUpdateView(page, projectId);
  const deleteView = useDeleteView(page, projectId);
  const reorderViews = useReorderViews(page, projectId);

  const [createOpen, setCreateOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const urlViewId = navigation.searchParams.get("view");
  const sorted = useMemo(
    () => (views ? [...views].sort((a, b) => a.position - b.position) : []),
    [views],
  );

  const activeView = useMemo(
    () => sorted.find((v) => v.id === currentViewId),
    [sorted, currentViewId],
  );

  const selectView = useCallback(
    (view: SavedView) => {
      onSelectView(view);
      writeViewParam(navigation, view.id);
    },
    [navigation, onSelectView],
  );

  // Resolve the active view: URL param wins, else the pinned store id, else the
  // first (default) view. Sync the store + URL to it on mount / when views
  // load. Guarded by a ref so we only auto-select once per resolved id.
  const lastSyncedRef = useRef<string | null>(null);
  useEffect(() => {
    if (sorted.length === 0) return;
    const target =
      sorted.find((v) => v.id === urlViewId) ??
      sorted.find((v) => v.id === currentViewId) ??
      sorted[0]!;
    if (lastSyncedRef.current === target.id && currentViewId === target.id) return;
    lastSyncedRef.current = target.id;
    if (currentViewId !== target.id) onSelectView(target);
    if (urlViewId !== target.id) writeViewParam(navigation, target.id);
    // navigation is stable enough; onSelectView identity comes from the store.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sorted, urlViewId, currentViewId]);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const ids = sorted.map((v) => v.id);
      const oldIndex = ids.indexOf(String(active.id));
      const newIndex = ids.indexOf(String(over.id));
      if (oldIndex === -1 || newIndex === -1) return;
      reorderViews.mutate(arrayMove(ids, oldIndex, newIndex));
    },
    [sorted, reorderViews],
  );

  const handleCreate = useCallback(
    async ({ name, shared }: ViewFormValues) => {
      const position = sorted.length
        ? Math.max(...sorted.map((v) => v.position)) + 1
        : 0;
      const view = await createView.mutateAsync({
        name,
        page,
        project_id: projectId ?? null,
        filters: currentFilters ?? {},
        shared,
        position,
      });
      setCreateOpen(false);
      selectView(view);
    },
    [createView, currentFilters, page, projectId, selectView, sorted],
  );

  const handleRename = useCallback(
    ({ name }: ViewFormValues) => {
      if (!activeView) return;
      updateView.mutate({ id: activeView.id, name });
      setRenameOpen(false);
    },
    [activeView, updateView],
  );

  const toggleShare = useCallback(() => {
    if (!activeView) return;
    updateView.mutate({ id: activeView.id, shared: !activeView.shared });
  }, [activeView, updateView]);

  const handleDelete = useCallback(() => {
    if (!activeView) return;
    deleteView.mutate(activeView.id);
    setDeleteOpen(false);
  }, [activeView, deleteView]);

  if (sorted.length === 0) return null;

  const manageable = currentFilters !== undefined;
  // Default views are scaffolding seeded for every workspace — renaming or
  // deleting them is meaningless (and the backend rejects deletes), so the ⋯
  // menu only appears for a user-created view that is currently active.
  const showMenu = manageable && activeView !== undefined && !activeView.is_default;

  return (
    <Tabs value={currentViewId ?? sorted[0]!.id}>
      <div className="flex items-center gap-1">
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
        >
          <SortableContext
            items={sorted.map((v) => v.id)}
            strategy={horizontalListSortingStrategy}
          >
            <TabsList variant="line">
              {sorted.map((view) => (
                <SortableViewTab key={view.id} view={view} onSelect={selectView} />
              ))}
            </TabsList>
          </SortableContext>
        </DndContext>

        {manageable && (
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => setCreateOpen(true)}
            aria-label={t(($) => $.views.save_view)}
            title={t(($) => $.views.save_view)}
          >
            <Plus className="h-4 w-4 text-muted-foreground" />
          </Button>
        )}

        {showMenu && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.views.options)}>
                  <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
                </Button>
              }
            />
            <DropdownMenuContent align="start" className="w-auto">
              <DropdownMenuItem onClick={() => setRenameOpen(true)}>
                <Pencil className="h-3.5 w-3.5" />
                {t(($) => $.views.rename)}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={toggleShare}>
                {activeView!.shared ? (
                  <>
                    <Lock className="h-3.5 w-3.5" />
                    {t(($) => $.views.unshare)}
                  </>
                ) : (
                  <>
                    <Users className="h-3.5 w-3.5" />
                    {t(($) => $.views.share)}
                  </>
                )}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onClick={() => setDeleteOpen(true)}>
                <Trash2 className="h-3.5 w-3.5" />
                {t(($) => $.views.delete_view)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>

      {manageable && (
        <ViewFormDialog
          open={createOpen}
          mode="create"
          busy={createView.isPending}
          onSubmit={handleCreate}
          onOpenChange={setCreateOpen}
        />
      )}

      {showMenu && (
        <>
          <ViewFormDialog
            open={renameOpen}
            mode="rename"
            initialName={activeView!.name}
            busy={updateView.isPending}
            onSubmit={handleRename}
            onOpenChange={setRenameOpen}
          />
          <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>{t(($) => $.views.delete_confirm_title)}</AlertDialogTitle>
                <AlertDialogDescription>
                  {t(($) => $.views.delete_confirm_body)}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>{t(($) => $.cancel)}</AlertDialogCancel>
                <AlertDialogAction onClick={handleDelete}>
                  {t(($) => $.delete)}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </>
      )}
    </Tabs>
  );
}

/** A single draggable saved-view tab. Drag listeners live on a wrapper so the
 *  inner TabsTrigger keeps its click-to-select; a `wasDragged` guard suppresses
 *  the click that fires at the end of a drag. */
function SortableViewTab({
  view,
  onSelect,
}: {
  view: SavedView;
  onSelect: (view: SavedView) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: view.id });
  const wasDragged = useRef(false);

  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);

  const style = { transform: CSS.Transform.toString(transform), transition };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={cn("touch-none", isDragging && "opacity-40")}
      {...attributes}
      {...listeners}
    >
      <TabsTrigger
        value={view.id}
        onClick={(event) => {
          if (wasDragged.current) {
            wasDragged.current = false;
            event.preventDefault();
            return;
          }
          onSelect(view);
        }}
      >
        {view.name}
      </TabsTrigger>
    </div>
  );
}

/** Replace the `?view=` query param without touching the rest of the URL. */
function writeViewParam(
  navigation: ReturnType<typeof useNavigation>,
  viewId: string,
) {
  const params = new URLSearchParams(navigation.searchParams);
  params.set("view", viewId);
  navigation.replace(`${navigation.pathname}?${params.toString()}`);
}
