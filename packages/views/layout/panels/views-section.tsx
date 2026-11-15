"use client";

import { useCallback, useId, useRef, useState } from "react";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { restrictToFirstScrollableAncestor, restrictToVerticalAxis } from "@dnd-kit/modifiers";
import { CSS } from "@dnd-kit/utilities";
import { Copy, EyeOff, Filter, Layers, Link2, MoreHorizontal, Pencil, Plus, Trash2 } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import type { IssueView } from "@multica/core/api/schemas";
import type { IssueTableFacetsRequest } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueTableFacetsOptions, sidebarIssuesOptions } from "@multica/core/issues/queries";
import { workspaceWorkingAgentsOptions } from "@multica/core/agents";
import {
  SIDEBAR_FIRST_PAGE,
  builtinSidebarRequest,
  builtinSidebarSource,
  sidebarCountRequest,
  viewSidebarNeedsUnread,
  viewSidebarRequest,
  viewSidebarSource,
  type SidebarIssuesRequest,
} from "@multica/core/issues/sidebar-list";
import { todayDateOnly } from "@multica/core/issues/date";
import { useIssueUnreadIds } from "@multica/core/issues/unread";
import { filterDependencyState, statusCatalogSource } from "@multica/core/issues/surface/filter-dependency";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useSidebarProjectPrefs } from "@multica/core/issues/sidebar-project-prefs";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import {
  issueViewContainerKey,
  useActiveIssueViewStore,
} from "@multica/core/issue-views/active-view-store";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { issueViewSeedDefinition } from "@multica/core/issue-views/definition";
import { useIssueViewRenameStore } from "@multica/core/issue-views/rename-store";
import {
  BUILTIN_VIEW_KEYS,
  builtinViewItemId,
  type BuiltinViewKey,
  type SidebarViewItem,
} from "@multica/core/issue-views/sidebar-views";
import { issueViewTarget } from "@multica/core/issue-views/view-target";
import type { IssueViewActions } from "../../issue-views/use-view-actions";
import { DeleteViewDialog } from "../../issue-views/delete-view-dialog";
import { ViewNameInput } from "../../issue-views/view-name-input";
import { BUILTIN_VIEW_ICON, useBuiltinViewLabel, useBuiltinViewNavigation } from "../../issue-views/builtin-views";
import { shouldOpenRowMenu, useRowMenu } from "../../common/point-menu";
import { useT } from "../../i18n";
import { PANEL_ROW_CLASS, PanelCount, PanelLink, PanelList, PanelSection, isPathWithin, panelRowAnchor, panelRowStateClass } from "../panel-nav";
import { useDragClickGuard } from "../use-drag-click-guard";
import { SECTION_SETTINGS_REVEAL_CLASS, ViewsSectionSettings } from "./sidebar-settings";
import { SidebarIssueList, TREE_ROW_BUTTON_CLASS, TreeRow } from "./sidebar-tree";

const DRAG_MODIFIERS = [restrictToVerticalAxis, restrictToFirstScrollableAncestor];
const EMPTY_VIEWS: IssueView[] = [];
const WORKSPACE_SCOPE = { scope_type: "workspace" } as const;
const ROW_MENU_CLASS = cn(
  TREE_ROW_BUTTON_CLASS,
  "text-muted-foreground opacity-0 group-hover/row:opacity-100 data-[popup-open]:opacity-100 [@media(pointer:coarse)]:opacity-100",
);
const EMPTY_COUNT_REQUEST: IssueTableFacetsRequest = {
  query: { scope: { kind: "workspace" }, filters: {}, sort: { field: "position", direction: "asc" } },
  facets: [{ kind: "priority" }],
  include_total: true,
};
const ROW_COUNT_CLASS =
  "transition-opacity group-focus-within/row:opacity-0 group-hover/row:opacity-0 group-has-[[data-popup-open]]/row:opacity-0 [@media(pointer:coarse)]:opacity-0";

interface ViewsSectionProps {
  wsId: string;
  pathname: string;
  plainRowsLit: boolean;
  /** No other row marks the open issue and it has no project, so "All issues" lights for it. */
  issueInAllIssues: boolean;
  activeIssueId: string | null;
  /** The one list that lights the open issue. */
  litSource: string | null;
  actions: IssueViewActions;
}

/** The sidebar source of a starred view's issue list. */
export function sidebarViewItemSource(item: SidebarViewItem): string {
  return item.kind === "view" ? viewSidebarSource(item.view.id) : builtinSidebarSource(item.key);
}

/**
 * The user's starred views, built-in and saved alike, in their own order,
 * closed by the way to every other view.
 */
export function ViewsSection({
  wsId,
  pathname,
  plainRowsLit,
  issueInAllIssues,
  activeIssueId,
  litSource,
  actions,
}: ViewsSectionProps) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const { items, reorder } = actions.sidebar;
  const { data: allViews = EMPTY_VIEWS } = useQuery(issueViewEverywhereOptions(wsId));
  const [deleteTarget, setDeleteTarget] = useState<IssueView | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const collapsed = useSidebarTreeStore((s) => s.viewsCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setViewsCollapsed);
  const showCount = useSidebarTreeStore((s) => s.viewsShowCount);
  const { arm: armClickGuard, release: releaseClickGuard } = useDragClickGuard();

  const onDragEnd = useCallback(
    ({ active, over }: DragEndEvent) => {
      releaseClickGuard();
      if (!over || active.id === over.id) return;
      const ids = items.map((item) => item.id);
      const from = ids.indexOf(String(active.id));
      const to = ids.indexOf(String(over.id));
      if (from === -1 || to === -1) return;
      reorder(arrayMove(ids, from, to));
    },
    [items, releaseClickGuard, reorder],
  );

  return (
    <PanelSection
      label={t(($) => $.panels.work.views)}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      action={
        <>
          <ViewsSectionSettings sidebar={actions.sidebar} className={SECTION_SETTINGS_REVEAL_CLASS} />
          <IconButton
            label={t(($) => $.panels.work.new_view)}
            size="icon-xs"
            className="text-muted-foreground"
            onClick={() => {
              // The new view lands in this list, so it has to be open. It is
              // named in the page header, next to where its filters are set.
              setCollapsed(false);
              actions.create({
                scope: WORKSPACE_SCOPE,
                scopeVariant: null,
                definition: issueViewSeedDefinition({ query: {}, display: {} }),
                renameIn: "header",
              });
            }}
          >
            <Plus />
          </IconButton>
        </>
      }
    >
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        modifiers={DRAG_MODIFIERS}
        onDragStart={armClickGuard}
        onDragEnd={onDragEnd}
        onDragCancel={releaseClickGuard}
      >
        <SortableContext items={items.map((item) => item.id)} strategy={verticalListSortingStrategy}>
          <PanelList>
            {items.map((item) => (
              <SortableViewRow key={item.id} id={item.id}>
                {(drag) =>
                  item.kind === "view" ? (
                    <SavedViewRow
                      wsId={wsId}
                      item={item}
                      pathname={pathname}
                      plainRowsLit={plainRowsLit}
                      activeIssueId={litSource === sidebarViewItemSource(item) ? activeIssueId : null}
                      actions={actions}
                      onDelete={setDeleteTarget}
                      drag={drag}
                    />
                  ) : (
                    <BuiltinViewRow
                      wsId={wsId}
                      builtin={item.key}
                      pathname={pathname}
                      plainRowsLit={plainRowsLit}
                      issueInAllIssues={issueInAllIssues}
                      activeIssueId={litSource === sidebarViewItemSource(item) ? activeIssueId : null}
                      actions={actions}
                      drag={drag}
                    />
                  )
                }
              </SortableViewRow>
            ))}
          </PanelList>
        </SortableContext>
      </DndContext>
      <PanelList className="mt-px">
        <PanelLink
          href={p.views()}
          icon={<span className="size-4" aria-hidden />}
          label={t(($) => $.panels.work.all_views)}
          active={pathname === p.views()}
          trailing={showCount && <PanelCount value={allViews.length + BUILTIN_VIEW_KEYS.length} />}
          className="text-caption"
        />
      </PanelList>
      <DeleteViewDialog
        view={deleteTarget}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        onConfirm={actions.remove}
      />
    </PanelSection>
  );
}

/**
 * How many issues the row lists when expanded. Kept off the sidebar list
 * cache: a list query with an observer reads as a list showing the open issue.
 */
function useListedCount(wsId: string, request: SidebarIssuesRequest | null | undefined, enabled: boolean): number {
  const { data = 0 } = useQuery({
    ...issueTableFacetsOptions(wsId, request ? sidebarCountRequest(request) : EMPTY_COUNT_REQUEST),
    enabled: enabled && !!request,
    placeholderData: keepPreviousData,
    select: (response) => response.total,
  });
  return enabled && request ? data : 0;
}

function usePrefetch(wsId: string, source: string, request: SidebarIssuesRequest | null | undefined) {
  const qc = useQueryClient();
  return () => {
    if (request) void qc.prefetchQuery(sidebarIssuesOptions(wsId, source, request, SIDEBAR_FIRST_PAGE));
  };
}

interface DragBindings {
  rowRef: (node: HTMLElement | null) => void;
  listeners: ReturnType<typeof useSortable>["listeners"];
}

function SortableViewRow({ id, children }: { id: string; children: (drag: DragBindings) => React.ReactNode }) {
  const { listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id });
  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(
        isDragging && "relative z-20 rounded-lg bg-surface-raised shadow-[var(--menu-shadow)] ring-1 ring-surface-border",
      )}
    >
      {children({
        rowRef: setActivatorNodeRef,
        listeners,
      })}
    </li>
  );
}

/** "…" on hover, and the same menu on right-click. */
/** A view row's ⋯ menu. A right-click on the row opens it at the cursor. */
function RowMenu({
  label,
  state,
  className,
  children,
}: {
  label: string;
  state: ReturnType<typeof useRowMenu>;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <DropdownMenu open={state.menu.open} onOpenChange={state.onOpenChange}>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={label}
            onPointerDown={(event) => event.stopPropagation()}
            className={cn(ROW_MENU_CLASS, className)}
          />
        }
      >
        <MoreHorizontal className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        side={state.menu.point ? "bottom" : "right"}
        anchor={state.anchor}
        className="w-auto min-w-48"
      >
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function BuiltinViewRow({
  wsId,
  builtin,
  pathname,
  plainRowsLit,
  issueInAllIssues,
  activeIssueId,
  actions,
  drag,
}: {
  wsId: string;
  builtin: BuiltinViewKey;
  pathname: string;
  plainRowsLit: boolean;
  issueInAllIssues: boolean;
  activeIssueId: string | null;
  actions: IssueViewActions;
  drag: DragBindings;
}) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const listId = useId();
  const linkRef = useRef<HTMLAnchorElement>(null);
  const rowMenu = useRowMenu();
  const builtinNav = useBuiltinViewNavigation();
  const workspaceKey = issueViewContainerKey(wsId, WORKSPACE_SCOPE);
  const activeWorkspaceView = useActiveIssueViewStore((s) => s.active[workspaceKey] ?? null);
  const showCount = useSidebarTreeStore((s) => s.viewsShowCount);

  const href = builtinNav.hrefFor(builtin);
  const label = useBuiltinViewLabel()(builtin);
  const active =
    plainRowsLit &&
    (builtin === "all_issues"
      ? pathname === p.issues()
        ? activeWorkspaceView === null
        : issueInAllIssues && isPathWithin(pathname, p.issues())
      : pathname === p.issues() && activeWorkspaceView === builtin);

  const source = builtinSidebarSource(builtin);
  const expanded = useSidebarTreeStore((s) => s.expanded.includes(source));
  const setExpanded = useSidebarTreeStore((s) => s.setExpanded);
  const statusCatalog = useIssueStatuses(wsId);
  const { issueSettings } = useSidebarProjectPrefs(wsId);
  // Working lists, and counts, the issues agents are running on right now.
  const working = useQuery({
    ...workspaceWorkingAgentsOptions(wsId, "issue"),
    enabled: builtin === "working",
    select: workingIssueIds,
  });
  const dependency = filterDependencyState(true, [
    statusCatalogSource(statusCatalog),
    ...(builtin === "working" ? [working] : []),
  ]);
  const request = dependency.unresolved
    ? undefined
    : builtinSidebarRequest(builtin, issueSettings, statusCatalog.statuses, working.data);
  const prefetch = usePrefetch(wsId, source, request);
  const listedCount = useListedCount(wsId, request, showCount && builtin !== "working");
  const count = !showCount ? 0 : builtin === "working" ? (working.data?.length ?? 0) : listedCount;

  return (
    <>
      <TreeRow
        ref={linkRef}
        rowRef={drag.rowRef}
        {...drag.listeners}
        href={href}
        label={label}
        icon={BUILTIN_VIEW_ICON[builtin]}
        active={active}
        expanded={expanded}
        onExpandedChange={(next) => setExpanded(source, next)}
        listId={listId}
        toggleLabel={t(($) => $.panels.work.project_issues, { name: label })}
        onPrefetch={prefetch}
        onContextMenu={(event) => {
          if (shouldOpenRowMenu(event)) rowMenu.openAtPointer(event);
        }}
        onNavigateInPlace={() => builtinNav.prepare(builtin)}
        trailing={
          // The menu button takes the count's place, so the row never reflows.
          <span className="relative flex h-5 min-w-5 items-center justify-end">
            <PanelCount value={count} className={ROW_COUNT_CLASS} />
            <RowMenu
              label={t(($) => $.panels.work.view_actions, { name: label })}
              state={rowMenu}
              className="absolute right-0"
            >
              <DropdownMenuItem onClick={() => actions.copyLink(href)}>
                <Link2 className="size-3.5" />
                {t(($) => $.panels.work.view_copy_link)}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => actions.sidebar.unstar(builtinViewItemId(builtin))}>
                <EyeOff className="size-3.5" />
                {t(($) => $.panels.work.view_remove)}
              </DropdownMenuItem>
            </RowMenu>
          </span>
        }
      />
      {expanded && (
        <SidebarIssueList
          id={listId}
          wsId={wsId}
          source={source}
          params={request}
          paramsError={dependency.error}
          onRetry={dependency.retry}
          activeIssueId={activeIssueId}
          onCollapse={() => {
            setExpanded(source, false);
            linkRef.current?.focus();
          }}
        />
      )}
    </>
  );
}

function workingIssueIds(agents: readonly { issue_ids: readonly string[] }[]): string[] {
  return [...new Set(agents.flatMap((agent) => agent.issue_ids))].sort();
}

function SavedViewRow({
  wsId,
  item,
  pathname,
  plainRowsLit,
  activeIssueId,
  actions,
  onDelete,
  drag,
}: {
  wsId: string;
  item: Extract<SidebarViewItem, { kind: "view" }>;
  pathname: string;
  plainRowsLit: boolean;
  activeIssueId: string | null;
  actions: IssueViewActions;
  onDelete: (view: IssueView) => void;
  drag: DragBindings;
}) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const { view } = item;
  const listId = useId();
  const linkRef = useRef<HTMLAnchorElement>(null);
  const rowMenu = useRowMenu();
  const target = issueViewTarget(wsId, view, p);
  const active = useActiveIssueViewStore(
    (s) => plainRowsLit && pathname === target.path && s.active[target.containerKey] === view.id,
  );
  const setActive = useActiveIssueViewStore((s) => s.setActive);
  const renaming = useIssueViewRenameStore(
    (s) => s.target?.surface === "sidebar" && s.target.viewId === view.id,
  );
  const startRename = useIssueViewRenameStore((s) => s.start);
  const stopRename = useIssueViewRenameStore((s) => s.stop);
  const source = viewSidebarSource(view.id);
  const expanded = useSidebarTreeStore((s) => s.expanded.includes(source));
  const setExpanded = useSidebarTreeStore((s) => s.setExpanded);
  const needsUnread = viewSidebarNeedsUnread(view);
  const unread = useIssueUnreadIds(wsId, needsUnread);
  const unreadDependency = filterDependencyState(needsUnread, [unread]);
  const request = unreadDependency.unresolved
    ? undefined
    : viewSidebarRequest(view, todayDateOnly(), unread.data);
  const prefetch = usePrefetch(wsId, source, request);
  const showCount = useSidebarTreeStore((s) => s.viewsShowCount);
  const count = useListedCount(wsId, request, showCount);
  const canManage = actions.canManage(view);

  if (renaming) {
    return (
      <div {...panelRowAnchor(active)} className={cn(PANEL_ROW_CLASS, panelRowStateClass(active), "py-1")}>
        <Layers className="size-4 shrink-0" />
        <ViewNameInput
          initial={view.name}
          onCommit={(name) => actions.rename(view, name)}
          onDone={stopRename}
          className="w-full"
        />
      </div>
    );
  }

  return (
    <>
      <TreeRow
        ref={linkRef}
        rowRef={drag.rowRef}
        {...drag.listeners}
        href={target.href}
        label={view.name}
        icon={<Layers className="size-4 shrink-0" />}
        active={active}
        expanded={expanded}
        onExpandedChange={(next) => setExpanded(source, next)}
        listId={listId}
        toggleLabel={t(($) => $.panels.work.project_issues, { name: view.name })}
        onPrefetch={prefetch}
        onContextMenu={(event) => {
          if (shouldOpenRowMenu(event)) rowMenu.openAtPointer(event);
        }}
        onNavigateInPlace={() => setActive(target.containerKey, view.id)}
        trailing={
          <span className="relative flex h-5 min-w-5 items-center justify-end">
            <PanelCount value={count} className={ROW_COUNT_CLASS} />
            <RowMenu
              label={t(($) => $.panels.work.view_actions, { name: view.name })}
              state={rowMenu}
              className="absolute right-0"
            >
              {canManage && (
                <>
                  <DropdownMenuItem onClick={() => actions.edit(view)}>
                    <Filter className="size-3.5" />
                    {t(($) => $.panels.work.view_edit)}
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => startRename(view.id, "sidebar")}>
                    <Pencil className="size-3.5" />
                    {t(($) => $.panels.work.view_rename)}
                  </DropdownMenuItem>
                </>
              )}
              <DropdownMenuItem onClick={() => actions.duplicate(view, "sidebar")}>
                <Copy className="size-3.5" />
                {t(($) => $.panels.work.view_duplicate)}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => actions.copyLink(target.href)}>
                <Link2 className="size-3.5" />
                {t(($) => $.panels.work.view_copy_link)}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => actions.sidebar.unstar(item.id)}>
                <EyeOff className="size-3.5" />
                {t(($) => $.panels.work.view_remove)}
              </DropdownMenuItem>
              {canManage && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem variant="destructive" onClick={() => onDelete(view)}>
                    <Trash2 className="size-3.5" />
                    {t(($) => $.panels.work.view_delete)}
                  </DropdownMenuItem>
                </>
              )}
            </RowMenu>
          </span>
        }
      />
      {expanded && (
        <SidebarIssueList
          id={listId}
          wsId={wsId}
          source={source}
          params={request}
          paramsError={unreadDependency.error}
          onRetry={unreadDependency.retry}
          activeIssueId={activeIssueId}
          onCollapse={() => {
            setExpanded(source, false);
            linkRef.current?.focus();
          }}
        />
      )}
    </>
  );
}
