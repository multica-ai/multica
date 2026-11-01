"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useStore } from "zustand";
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { restrictToFirstScrollableAncestor, restrictToVerticalAxis } from "@dnd-kit/modifiers";
import { CSS } from "@dnd-kit/utilities";
import { Bot, CircleUser, Copy, EyeOff, Layers, Link2, ListTodo, MoreHorizontal, Pencil, Plus, Trash2 } from "@multica/ui/icons";
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
import { isMyAgentsSearch, useWorkspacePaths } from "@multica/core/paths";
import { sidebarIssuesOptions } from "@multica/core/issues/queries";
import {
  SIDEBAR_FIRST_PAGE,
  builtinSidebarRequest,
  builtinSidebarSource,
  viewSidebarRequest,
  viewSidebarSource,
  type SidebarIssuesRequest,
} from "@multica/core/issues/sidebar-list";
import { todayDateOnly } from "@multica/core/issues/date";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useSidebarProjectPrefs } from "@multica/core/issues/sidebar-project-prefs";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import {
  myIssuesRelationFromScope,
  myIssuesScopeFor,
  myIssuesViewStore,
} from "@multica/core/issues/stores/my-issues-view-store";
import {
  issueViewContainerKey,
  useActiveIssueViewStore,
} from "@multica/core/issue-views/active-view-store";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { issueViewSeedDefinition } from "@multica/core/issue-views/definition";
import { useIssueViewRenameStore } from "@multica/core/issue-views/rename-store";
import { builtinViewItemId, type BuiltinViewKey, type SidebarViewItem } from "@multica/core/issue-views/sidebar-views";
import { issueViewTarget } from "@multica/core/issue-views/view-target";
import { useIssueViewActions, type IssueViewActions } from "../../issue-views/use-view-actions";
import { DeleteViewDialog } from "../../issue-views/delete-view-dialog";
import { ViewNameInput } from "../../issue-views/view-name-input";
import { useBuiltinViewNavigation } from "../../issue-views/builtin-views";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { PANEL_ROW_CLASS, PanelCount, PanelLink, PanelList, PanelSection, isPathWithin, panelRowStateClass } from "../panel-nav";
import { SidebarIssueList, TREE_ROW_BUTTON_CLASS, TreeRow } from "./sidebar-tree";

const DRAG_MODIFIERS = [restrictToVerticalAxis, restrictToFirstScrollableAncestor];
const EMPTY_VIEWS: IssueView[] = [];
const WORKSPACE_SCOPE = { scope_type: "workspace" } as const;
const MY_SCOPE = { scope_type: "my" } as const;
const NO_SEARCH = new URLSearchParams();
const ROW_MENU_CLASS = cn(
  TREE_ROW_BUTTON_CLASS,
  "text-muted-foreground opacity-0 group-hover/row:opacity-100 data-[popup-open]:opacity-100 [@media(pointer:coarse)]:opacity-100",
);

interface ViewsSectionProps {
  wsId: string;
  pathname: string;
  plainRowsLit: boolean;
  /** An expanded tree row lists the current issue, so "All issues" stays unlit. */
  issueLitInTree: boolean;
  activeIssueId: string | null;
}

/**
 * The user's starred views, built-in and saved alike, in their own order,
 * closed by the way to every other view.
 */
export function ViewsSection({
  wsId,
  pathname,
  plainRowsLit,
  issueLitInTree,
  activeIssueId,
}: ViewsSectionProps) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const actions = useIssueViewActions();
  const { items, reorder } = actions.sidebar;
  const { data: allViews = EMPTY_VIEWS } = useQuery(issueViewEverywhereOptions(wsId));
  const [deleteTarget, setDeleteTarget] = useState<IssueView | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const collapsed = useSidebarTreeStore((s) => s.viewsCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setViewsCollapsed);

  const onDragEnd = useCallback(
    ({ active, over }: DragEndEvent) => {
      if (!over || active.id === over.id) return;
      const ids = items.map((item) => item.id);
      const from = ids.indexOf(String(active.id));
      const to = ids.indexOf(String(over.id));
      if (from === -1 || to === -1) return;
      reorder(arrayMove(ids, from, to));
    },
    [items, reorder],
  );

  return (
    <PanelSection
      label={t(($) => $.panels.work.views)}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      action={
        <IconButton
          label={t(($) => $.panels.work.new_view)}
          size="icon-xs"
          className="text-muted-foreground"
          onClick={() => {
            // The new view is named inline in this list, so it has to be open.
            setCollapsed(false);
            actions.create({
              scope: WORKSPACE_SCOPE,
              scopeVariant: null,
              definition: issueViewSeedDefinition({ query: {}, display: {} }),
              renameIn: "sidebar",
            });
          }}
        >
          <Plus />
        </IconButton>
      }
    >
      <DndContext sensors={sensors} collisionDetection={closestCenter} modifiers={DRAG_MODIFIERS} onDragEnd={onDragEnd}>
        <SortableContext items={items.map((item) => item.id)} strategy={verticalListSortingStrategy}>
          <PanelList>
            {items.map((item) => (
              <SortableViewRow key={item.id} id={item.id}>
                {(drag) =>
                  item.kind === "builtin" ? (
                    <BuiltinViewRow
                      wsId={wsId}
                      builtin={item.key}
                      pathname={pathname}
                      plainRowsLit={plainRowsLit}
                      issueLitInTree={issueLitInTree}
                      activeIssueId={activeIssueId}
                      actions={actions}
                      drag={drag}
                    />
                  ) : (
                    <SavedViewRow
                      wsId={wsId}
                      item={item}
                      pathname={pathname}
                      plainRowsLit={plainRowsLit}
                      activeIssueId={activeIssueId}
                      actions={actions}
                      onDelete={setDeleteTarget}
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
          trailing={<PanelCount value={allViews.length + 3} />}
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

function usePrefetch(wsId: string, source: string, request: SidebarIssuesRequest | null | undefined) {
  const qc = useQueryClient();
  return () => {
    if (request) void qc.prefetchQuery(sidebarIssuesOptions(wsId, source, request, SIDEBAR_FIRST_PAGE));
  };
}

interface DragBindings {
  rowRef: (node: HTMLElement | null) => void;
  listeners: ReturnType<typeof useSortable>["listeners"];
  /** True right after a drag, so the release does not also navigate. */
  consumeDrag: () => boolean;
}

function SortableViewRow({ id, children }: { id: string; children: (drag: DragBindings) => React.ReactNode }) {
  const { listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id });
  const wasDragged = useRef(false);
  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);
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
        consumeDrag: () => {
          if (!wasDragged.current) return false;
          wasDragged.current = false;
          return true;
        },
      })}
    </li>
  );
}

/** "…" on hover, and the same menu on right-click. */
function RowMenu({
  label,
  open,
  onOpenChange,
  children,
}: {
  label: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: React.ReactNode;
}) {
  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={label}
            onPointerDown={(event) => event.stopPropagation()}
            className={ROW_MENU_CLASS}
          />
        }
      >
        <MoreHorizontal className="size-3.5" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="right" className="w-auto min-w-48">
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

const BUILTIN_ICON: Record<BuiltinViewKey, React.ReactNode> = {
  my_issues: <CircleUser className="size-4 shrink-0" />,
  my_agents: <Bot className="size-4 shrink-0" />,
  all_issues: <ListTodo className="size-4 shrink-0" />,
};

function BuiltinViewRow({
  wsId,
  builtin,
  pathname,
  plainRowsLit,
  issueLitInTree,
  activeIssueId,
  actions,
  drag,
}: {
  wsId: string;
  builtin: BuiltinViewKey;
  pathname: string;
  plainRowsLit: boolean;
  issueLitInTree: boolean;
  activeIssueId: string | null;
  actions: IssueViewActions;
  drag: DragBindings;
}) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const listId = useId();
  const linkRef = useRef<HTMLAnchorElement>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const builtinNav = useBuiltinViewNavigation();
  const { searchParams } = useNavigation();
  const myScope = useStore(myIssuesViewStore, (s) => myIssuesScopeFor(NO_SEARCH, s.scope));
  const workspaceKey = issueViewContainerKey(wsId, WORKSPACE_SCOPE);
  const myKey = issueViewContainerKey(wsId, MY_SCOPE);
  const activeWorkspaceView = useActiveIssueViewStore((s) => s.active[workspaceKey] ?? null);
  const activeMyView = useActiveIssueViewStore((s) => s.active[myKey] ?? null);

  const onMyIssues = pathname === p.myIssues();
  const href = builtinNav.hrefFor(builtin);
  const label =
    builtin === "my_issues"
      ? t(($) => $.nav.my_issues)
      : builtin === "my_agents"
        ? t(($) => $.panels.work.my_agents)
        : t(($) => $.panels.work.all_issues);
  const active =
    plainRowsLit &&
    (builtin === "all_issues"
      ? pathname === p.issues()
        ? activeWorkspaceView === null
        : !issueLitInTree && isPathWithin(pathname, p.issues())
      : onMyIssues && activeMyView === null && (builtin === "my_agents") === isMyAgentsSearch(searchParams));

  const source = builtinSidebarSource(builtin);
  const expanded = useSidebarTreeStore((s) => s.expanded.includes(source));
  const setExpanded = useSidebarTreeStore((s) => s.setExpanded);
  const statusCatalog = useIssueStatuses(wsId);
  const { issueSettings } = useSidebarProjectPrefs(wsId);
  const request = statusCatalog.isLoaded
    ? builtinSidebarRequest(builtin, myIssuesRelationFromScope(myScope), issueSettings, statusCatalog.statuses)
    : undefined;
  const prefetch = usePrefetch(wsId, source, request);

  return (
    <>
      <TreeRow
        ref={linkRef}
        rowRef={drag.rowRef}
        {...drag.listeners}
        href={href}
        label={label}
        icon={BUILTIN_ICON[builtin]}
        active={active}
        expanded={expanded}
        onExpandedChange={(next) => setExpanded(source, next)}
        listId={listId}
        toggleLabel={t(($) => $.panels.work.project_issues, { name: label })}
        onPrefetch={prefetch}
        onContextMenu={(event) => {
          event.preventDefault();
          setMenuOpen(true);
        }}
        onNavigate={(event) => {
          if (drag.consumeDrag()) event.preventDefault();
        }}
        onNavigateInPlace={() => builtinNav.prepare(builtin)}
        trailing={
          <RowMenu label={t(($) => $.panels.work.view_actions, { name: label })} open={menuOpen} onOpenChange={setMenuOpen}>
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
        }
      />
      {expanded && (
        <SidebarIssueList
          id={listId}
          wsId={wsId}
          source={source}
          params={request}
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
  const [menuOpen, setMenuOpen] = useState(false);
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
  const request = viewSidebarRequest(view, todayDateOnly());
  const prefetch = usePrefetch(wsId, source, request);
  const canManage = actions.canManage(view);

  if (renaming) {
    return (
      <div className={cn(PANEL_ROW_CLASS, panelRowStateClass(active), "py-1")}>
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
          event.preventDefault();
          setMenuOpen(true);
        }}
        onNavigate={(event) => {
          if (drag.consumeDrag()) event.preventDefault();
        }}
        onNavigateInPlace={() => setActive(target.containerKey, view.id)}
        trailing={
          <RowMenu label={t(($) => $.panels.work.view_actions, { name: view.name })} open={menuOpen} onOpenChange={setMenuOpen}>
            {canManage && (
              <DropdownMenuItem onClick={() => startRename(view.id, "sidebar")}>
                <Pencil className="size-3.5" />
                {t(($) => $.panels.work.view_rename)}
              </DropdownMenuItem>
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
        }
      />
      {expanded && (
        <SidebarIssueList
          id={listId}
          wsId={wsId}
          source={source}
          params={request}
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
