"use client";

import { useCallback, useEffect, useId, useRef, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { restrictToFirstScrollableAncestor, restrictToVerticalAxis } from "@dnd-kit/modifiers";
import { CSS } from "@dnd-kit/utilities";
import { ListChevronsDownUp, Folder, Plus, Search, X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueKeys, sidebarIssuesOptions } from "@multica/core/issues/queries";
import { filterDependencyState, statusCatalogSource } from "@multica/core/issues/surface/filter-dependency";
import {
  SIDEBAR_FIRST_PAGE,
  projectSidebarRequest,
  projectSidebarSource,
  type SidebarIssuesRequest,
} from "@multica/core/issues/sidebar-list";
import { useSidebarProjectPrefs, useUpdateSidebarProjectPrefs } from "@multica/core/issues/sidebar-project-prefs";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores/create-mode-store";
import { useSidebarKeptStore } from "@multica/core/issues/stores/sidebar-kept-store";
import { useSidebarTreeStore, type SidebarIssueSettings } from "@multica/core/issues/stores/sidebar-tree-store";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Issue, ListIssuesResponse, Project } from "@multica/core/types";
import { useT } from "../../i18n";
import {
  PANEL_ROW_CLASS,
  PanelCount,
  PanelLink,
  PanelList,
  PanelSection,
  isPathWithin,
  panelRowStateClass,
} from "../panel-nav";
import { ProjectsSectionSettings } from "./sidebar-settings";
import { SidebarIssueList, TreeRow } from "./sidebar-tree";

const DRAG_MODIFIERS = [restrictToVerticalAxis, restrictToFirstScrollableAncestor];

function useExpanded(key: string) {
  const expanded = useSidebarTreeStore((s) => s.expanded.includes(key));
  const setExpanded = useSidebarTreeStore((s) => s.setExpanded);
  return [expanded, (next: boolean) => setExpanded(key, next)] as const;
}

function usePrefetch(wsId: string, source: string, request: SidebarIssuesRequest | null | undefined) {
  const qc = useQueryClient();
  return () => {
    if (request) void qc.prefetchQuery(sidebarIssuesOptions(wsId, source, request, SIDEBAR_FIRST_PAGE));
  };
}

/**
 * The id of the issue an expanded row lists or keeps under `ref` — its UUID,
 * or an identifier like `MUL-123` matched case-insensitively as the server
 * does — or `null` when no expanded row lists it.
 */
export function useIssueIdInTree(wsId: string, ref: string | null): string | null {
  const qc = useQueryClient();
  const cache = qc.getQueryCache();
  const subscribe = useCallback(
    (onChange: () => void) => {
      const unsubscribeCache = cache.subscribe(onChange);
      const unsubscribeKept = useSidebarKeptStore.subscribe(onChange);
      return () => {
        unsubscribeCache();
        unsubscribeKept();
      };
    },
    [cache],
  );
  return useSyncExternalStore(subscribe, () => {
    if (!ref) return null;
    const identifier = ref.toUpperCase();
    const matches = (issue: Issue) => issue.id === ref || issue.identifier.toUpperCase() === identifier;
    const shown = (queryKey: readonly unknown[]) =>
      cache.findAll({ queryKey }).filter((query) => query.getObserversCount() > 0);
    for (const query of shown(issueKeys.sidebarAll(wsId))) {
      const issue = (query.state.data as ListIssuesResponse | undefined)?.issues?.find(matches);
      if (issue) return issue.id;
    }
    // A shown list keeps the open issue its query dropped (`useKeptActiveItem`).
    for (const [list, held] of Object.entries(useSidebarKeptStore.getState().held)) {
      const issue = qc.getQueryData<Issue>(issueKeys.detail(wsId, held.id));
      if (issue && matches(issue) && shown([...issueKeys.sidebarAll(wsId), list]).length > 0) return issue.id;
    }
    return null;
  });
}

/** Filtering the section by name; the owner keeps the query. */
export interface ProjectsSearch {
  query: string;
  onQueryChange: (query: string) => void;
  /** Projects matching the query that the section's filter leaves out. */
  hiddenMatches: number;
  /** Whether the query also covers the projects the filter leaves out. */
  searchAll: boolean;
  onSearchAllChange: (searchAll: boolean) => void;
}

interface ProjectsSectionProps {
  wsId: string;
  userId: string | undefined;
  pathname: string;
  /** Already filtered, searched and ordered by the section's settings. */
  projects: Project[];
  totalProjects: number;
  plainRowsLit: boolean;
  activeIssueId: string | null;
  /** The project holding the open issue its row does not list. */
  hintedProjectId?: string | null;
  search?: ProjectsSearch;
}

const PROJECT_PREFIX = projectSidebarSource("");

export function ProjectsSection({
  wsId,
  userId,
  pathname,
  projects,
  totalProjects,
  plainRowsLit,
  activeIssueId,
  hintedProjectId = null,
  search,
}: ProjectsSectionProps) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const collapsed = useSidebarTreeStore((s) => s.projectsCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setProjectsCollapsed);
  const anyExpanded = useSidebarTreeStore((s) => s.expanded.some((key) => key.startsWith(PROJECT_PREFIX)));
  const collapseAll = useSidebarTreeStore((s) => s.collapseAll);
  const { projectSettings, issueSettings, projectOrder } = useSidebarProjectPrefs(wsId);
  const updatePrefs = useUpdateSidebarProjectPrefs(wsId);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const [searchOpen, setSearchOpen] = useState(false);
  const searching = !!search && search.query.trim() !== "";
  const closeSearch = () => {
    search?.onQueryChange("");
    setSearchOpen(false);
  };

  const onDragEnd = useCallback(
    ({ active, over }: DragEndEvent) => {
      if (searching || !over || active.id === over.id) return;
      const ids = projects.map((project) => project.id);
      const from = ids.indexOf(String(active.id));
      const to = ids.indexOf(String(over.id));
      if (from === -1 || to === -1) return;
      const moved = arrayMove(ids, from, to);
      updatePrefs({
        // Projects the filter hides keep their relative place after the shown ones.
        projectOrder: [...moved, ...projectOrder.filter((id) => !moved.includes(id))],
        projectSettings: { ordering: "manual" },
      });
    },
    [projects, projectOrder, updatePrefs, searching],
  );

  return (
    <PanelSection
      label={t(($) => $.nav.projects)}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      action={
        <>
          {anyExpanded && (
            <IconButton
              label={t(($) => $.panels.work.collapse_projects)}
              size="icon-xs"
              className="text-muted-foreground opacity-0 group-hover/section:opacity-100 group-focus-within/section:opacity-100 [@media(pointer:coarse)]:opacity-100"
              onClick={() => collapseAll(PROJECT_PREFIX)}
            >
              <ListChevronsDownUp />
            </IconButton>
          )}
          <ProjectsSectionSettings wsId={wsId} className="opacity-0 group-hover/section:opacity-100 group-focus-within/section:opacity-100 [@media(pointer:coarse)]:opacity-100" />
          {search && totalProjects > 0 && (
            <IconButton
              label={t(($) => $.panels.work.search_projects)}
              size="icon-xs"
              aria-expanded={searchOpen && !collapsed}
              className={cn("text-muted-foreground", searchOpen && !collapsed && "bg-surface-selected text-foreground")}
              onClick={() => {
                if (searchOpen) return closeSearch();
                setCollapsed(false);
                setSearchOpen(true);
              }}
            >
              <Search />
            </IconButton>
          )}
          <IconButton
            label={t(($) => $.panels.work.new_project)}
            size="icon-xs"
            className="text-muted-foreground"
            onClick={() => useModalStore.getState().open("create-project")}
          >
            <Plus />
          </IconButton>
        </>
      }
    >
      {search && searchOpen && <ProjectSearchField search={search} onClose={closeSearch} />}
      <DndContext sensors={sensors} collisionDetection={closestCenter} modifiers={DRAG_MODIFIERS} onDragEnd={onDragEnd}>
        <SortableContext items={projects.map((project) => project.id)} strategy={verticalListSortingStrategy}>
          <PanelList>
            {projects.map((project) => (
              <ProjectTreeItem
                key={project.id}
                wsId={wsId}
                userId={userId}
                project={project}
                active={plainRowsLit && isPathWithin(pathname, p.projectDetail(project.id))}
                hinted={plainRowsLit && project.id === hintedProjectId}
                showCount={projectSettings.showCount}
                issueSettings={issueSettings}
                activeIssueId={activeIssueId}
                // A drop in a searched list would move projects the search hides.
                dragDisabled={searching}
              />
            ))}
          </PanelList>
        </SortableContext>
      </DndContext>
      {search && searching && (
        <ProjectSearchStatus search={search} empty={projects.length === 0} />
      )}
      <PanelList className="mt-px">
        {totalProjects === 0 ? (
          <li>
            <button
              type="button"
              className={cn(
                PANEL_ROW_CLASS,
                panelRowStateClass(false),
                "w-full text-caption text-muted-foreground [&_svg]:size-4 [&_svg]:shrink-0",
              )}
              onClick={() => useModalStore.getState().open("create-project")}
            >
              <Plus aria-hidden="true" />
              <span className="min-w-0 flex-1 truncate text-left">
                {t(($) => $.panels.work.new_project)}
              </span>
            </button>
          </li>
        ) : (
          <PanelLink
            href={p.projects()}
            icon={<span className="size-4" aria-hidden />}
            label={t(($) => $.panels.work.all_projects, { count: totalProjects })}
            active={pathname === p.projects()}
            className="text-caption"
          />
        )}
      </PanelList>
    </PanelSection>
  );
}

function ProjectSearchField({ search, onClose }: { search: ProjectsSearch; onClose: () => void }) {
  const { t } = useT("layout");
  return (
    <div className="relative mx-1 mb-1 flex h-7 items-center rounded-md bg-surface-hover pl-2 pr-1 focus-within:outline-2 focus-within:outline-ring">
      <Search aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
      <input
        autoFocus
        type="search"
        value={search.query}
        aria-label={t(($) => $.panels.work.search_projects)}
        placeholder={t(($) => $.panels.work.search_projects_placeholder)}
        onChange={(event) => search.onQueryChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.stopPropagation();
            onClose();
          }
        }}
        className="min-w-0 flex-1 bg-transparent px-1.5 text-caption text-foreground outline-none placeholder:text-faint-foreground [&::-webkit-search-cancel-button]:hidden"
      />
      {search.query && (
        <IconButton
          label={t(($) => $.panels.work.clear_search)}
          size="icon-xs"
          className="text-muted-foreground"
          onClick={() => search.onQueryChange("")}
        >
          <X />
        </IconButton>
      )}
    </div>
  );
}

/** Under a searched list: what it found, and the projects the filter keeps out. */
function ProjectSearchStatus({ search, empty }: { search: ProjectsSearch; empty: boolean }) {
  const { t } = useT("layout");
  const toggle = search.searchAll
    ? t(($) => $.panels.work.search_shown_only)
    : search.hiddenMatches > 0
      ? t(($) => $.panels.work.search_include_hidden, { count: search.hiddenMatches })
      : null;
  return (
    <div className="space-y-0.5 px-3 py-1 text-caption text-faint-foreground">
      {empty && <p>{t(($) => $.panels.work.search_no_projects)}</p>}
      {toggle && (
        <button
          type="button"
          className="rounded-sm text-left text-muted-foreground underline-offset-2 outline-none hover:text-foreground hover:underline focus-visible:outline-2 focus-visible:outline-ring"
          onClick={() => search.onSearchAllChange(!search.searchAll)}
        >
          {toggle}
        </button>
      )}
    </div>
  );
}

function ProjectTreeItem({
  wsId,
  userId,
  project,
  active,
  hinted,
  showCount,
  issueSettings,
  activeIssueId,
  dragDisabled,
}: {
  wsId: string;
  userId: string | undefined;
  project: Project;
  active: boolean;
  hinted: boolean;
  showCount: boolean;
  issueSettings: SidebarIssueSettings;
  activeIssueId: string | null;
  dragDisabled: boolean;
}) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const listId = useId();
  const linkRef = useRef<HTMLAnchorElement>(null);
  const source = projectSidebarSource(project.id);
  const [expanded, setExpanded] = useExpanded(source);
  const statusCatalog = useIssueStatuses(wsId);
  const catalogDependency = filterDependencyState(true, [statusCatalogSource(statusCatalog)]);
  const params = catalogDependency.unresolved
    ? undefined
    : projectSidebarRequest(project.id, issueSettings, statusCatalog.statuses, userId);
  const prefetch = usePrefetch(wsId, source, params);

  const { listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: project.id,
    disabled: dragDisabled,
  });
  const wasDragged = useRef(false);
  useEffect(() => {
    if (isDragging) wasDragged.current = true;
  }, [isDragging]);

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(
        isDragging &&
          "relative z-20 rounded-lg bg-surface-raised shadow-[var(--menu-shadow)] ring-1 ring-surface-border",
      )}
    >
      <TreeRow
        ref={linkRef}
        rowRef={setActivatorNodeRef}
        {...listeners}
        href={p.projectDetail(project.id)}
        label={project.title}
        icon={<Folder className="size-4 shrink-0" />}
        active={active}
        hint={hinted ? t(($) => $.panels.work.project_has_open_issue) : undefined}
        expanded={expanded}
        onExpandedChange={setExpanded}
        listId={listId}
        toggleLabel={t(($) => $.panels.work.project_issues, { name: project.title })}
        onPrefetch={prefetch}
        onNavigate={(event) => {
          if (!wasDragged.current) return;
          wasDragged.current = false;
          event.preventDefault();
        }}
        trailing={
          // The button takes the count's place on hover, so the row never reflows.
          <span className="relative flex h-5 min-w-5 items-center justify-end">
            {showCount && (
              <PanelCount
                value={project.issue_count - project.done_count}
                className="transition-opacity group-focus-within/row:opacity-0 group-hover/row:opacity-0 [@media(pointer:coarse)]:opacity-0"
              />
            )}
            <IconButton
              label={t(($) => $.panels.work.new_issue_in_project, { name: project.title })}
              size="icon-xs"
              className="absolute -right-2 z-10 text-muted-foreground opacity-0 transition-opacity group-focus-within/row:opacity-100 group-hover/row:opacity-100 focus-visible:opacity-100 [@media(pointer:coarse)]:opacity-100"
              onPointerDown={(event) => event.stopPropagation()}
              onClick={() => openCreateIssueWithPreference({ project_id: project.id })}
            >
              <Plus />
            </IconButton>
          </span>
        }
      />
      {expanded && (
        <SidebarIssueList
          id={listId}
          wsId={wsId}
          source={source}
          params={params}
          paramsError={catalogDependency.error}
          onRetry={catalogDependency.retry}
          activeIssueId={activeIssueId}
          onCollapse={() => {
            setExpanded(false);
            linkRef.current?.focus();
          }}
        />
      )}
    </li>
  );
}
