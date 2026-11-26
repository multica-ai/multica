"use client";

import { useCallback, useEffect, useId, useMemo, useRef, useSyncExternalStore, type MouseEvent, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DndContext,
  PointerSensor,
  closestCenter,
  pointerWithin,
  useDroppable,
  useSensor,
  useSensors,
  type CollisionDetection,
  type DragEndEvent,
} from "@dnd-kit/core";
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { restrictToFirstScrollableAncestor, restrictToVerticalAxis } from "@dnd-kit/modifiers";
import { CSS } from "@dnd-kit/utilities";
import { Folder, FolderKanban, FolderMinus, FolderOpen, Plus } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueKeys, issueTableFacetsOptions, sidebarIssuesOptions } from "@multica/core/issues/queries";
import { filterDependencyState, statusCatalogSource } from "@multica/core/issues/surface/filter-dependency";
import {
  NO_PROJECT_FACET_KEY,
  NO_PROJECT_ORDER_ID,
  NO_PROJECT_SIDEBAR_SOURCE,
  SIDEBAR_FIRST_PAGE,
  noProjectCountRequest,
  noProjectSidebarRequest,
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
import {
  projectFolderTreeKey,
  sidebarProjectRowIds,
  sidebarProjectRows,
  type SidebarProjectRow,
} from "@multica/core/projects/folder-tree";
import type { SidebarFolder } from "@multica/core/projects/folder-tree";
import type { Issue, ListIssuesResponse, Project } from "@multica/core/types";
import { useT } from "../../i18n";
import {
  PANEL_ROW_CLASS,
  PanelCount,
  PanelDisclosure,
  PanelLink,
  PanelList,
  PanelSection,
  isPathWithin,
  panelRowStateClass,
} from "../panel-nav";
import { ProjectIcon } from "../../projects/components/project-icon";
import { ProjectMenu } from "../../projects/components/project-row-actions";
import { getProjectFoldersExtension, useProjectFolders, type ProjectFolders } from "../../extensions";
import { shouldOpenRowMenu, useRowMenu } from "../../common/point-menu";
import { CollectionRowContext } from "../collection-list/row-context";
import { useDragClickGuard } from "../use-drag-click-guard";
import { ProjectsSectionSettings, SECTION_SETTINGS_REVEAL_CLASS } from "./sidebar-settings";
import { PrivateProjectLock, SidebarIssueList, TreeRow, TreeToggleRow } from "./sidebar-tree";

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

export interface IssueInTree {
  /** `null` when no expanded row lists the issue. */
  issueId: string | null;
  /** The sidebar sources of the expanded rows that list or keep it. */
  sources: readonly string[];
}

const NOT_IN_TREE: IssueInTree = { issueId: null, sources: [] };

function hasPrefix(key: readonly unknown[], prefix: readonly unknown[]): boolean {
  return prefix.every((part, i) => key[i] === part);
}

/**
 * Whether a cache entry can change `useIssueInTree`'s answer: a sidebar list,
 * or the detail of an issue a list keeps.
 */
function readsIntoTree(wsId: string, key: readonly unknown[]): boolean {
  if (hasPrefix(key, issueKeys.sidebarAll(wsId))) return true;
  const detail = issueKeys.detail(wsId, "");
  if (key.length !== detail.length || !hasPrefix(key, detail.slice(0, -1))) return false;
  const id = key[detail.length - 1];
  return Object.values(useSidebarKeptStore.getState().held).some((held) => held.id === id);
}

/**
 * Where the expanded rows list or keep the issue under `ref` — its UUID, or
 * an identifier like `MUL-123` matched case-insensitively as the server does.
 */
export function useIssueInTree(wsId: string, ref: string | null): IssueInTree {
  const qc = useQueryClient();
  const cache = qc.getQueryCache();
  const subscribe = useCallback(
    (onChange: () => void) => {
      // Only events on the entries the answer reads. Opening an issue mounts
      // hundreds of observers elsewhere in the cache, and rescanning the
      // whole cache for each of them blocked the main thread for seconds,
      // more the longer the session had filled the cache (MUL-9375).
      const unsubscribeCache = cache.subscribe((event) => {
        if (readsIntoTree(wsId, event.query.queryKey)) onChange();
      });
      const unsubscribeKept = useSidebarKeptStore.subscribe(onChange);
      return () => {
        unsubscribeCache();
        unsubscribeKept();
      };
    },
    [cache, wsId],
  );
  // The snapshot is a string so an unchanged answer compares equal.
  const snapshot = useSyncExternalStore(subscribe, () => {
    if (!ref) return "";
    const identifier = ref.toUpperCase();
    const matches = (issue: Issue) => issue.id === ref || issue.identifier.toUpperCase() === identifier;
    const sourceAt = issueKeys.sidebarAll(wsId).length;
    const shown = (queryKey: readonly unknown[]) =>
      cache.findAll({ queryKey }).filter((query) => query.getObserversCount() > 0);
    let issueId: string | null = null;
    const sources = new Set<string>();
    for (const query of shown(issueKeys.sidebarAll(wsId))) {
      const issue = (query.state.data as ListIssuesResponse | undefined)?.issues?.find(matches);
      if (!issue) continue;
      issueId = issue.id;
      sources.add(String(query.queryKey[sourceAt]));
    }
    // A shown list keeps the open issue its query dropped (`useKeptActiveItem`).
    for (const [list, held] of Object.entries(useSidebarKeptStore.getState().held)) {
      const issue = qc.getQueryData<Issue>(issueKeys.detail(wsId, held.id));
      if (!issue || !matches(issue) || shown([...issueKeys.sidebarAll(wsId), list]).length === 0) continue;
      issueId = issue.id;
      sources.add(list);
    }
    return issueId ? [issueId, ...sources].join("\n") : "";
  });
  return useMemo(() => {
    if (!snapshot) return NOT_IN_TREE;
    const [issueId, ...sources] = snapshot.split("\n");
    return { issueId: issueId!, sources };
  }, [snapshot]);
}

interface ProjectsSectionProps {
  wsId: string;
  userId: string | undefined;
  pathname: string;
  /** Already filtered and ordered by the section's settings. */
  projects: readonly Project[];
  totalProjects: number;
  plainRowsLit: boolean;
  activeIssueId: string | null;
  /** The one list that lights the open issue. */
  litSource: string | null;
  /** The row, a project's or "No project", that marks the open issue no list shows. */
  locatedSource: string | null;
}

export function ProjectsSection({
  wsId,
  userId,
  pathname,
  projects,
  totalProjects,
  plainRowsLit,
  activeIssueId,
  litSource,
  locatedSource,
}: ProjectsSectionProps) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const collapsed = useSidebarTreeStore((s) => s.projectsCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setProjectsCollapsed);
  const setExpanded = useSidebarTreeStore((s) => s.setExpanded);
  const locate = useSidebarTreeStore((s) => s.locate);
  const { projectSettings, issueSettings, projectOrder } = useSidebarProjectPrefs(wsId);
  const updatePrefs = useUpdateSidebarProjectPrefs(wsId);
  const folders = useProjectFolders(wsId);
  const foldersExtension = getProjectFoldersExtension();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const { arm: armClickGuard, release: releaseClickGuard } = useDragClickGuard();
  const rows = useMemo(
    () => sidebarProjectRows(projects, folders.folders, folders.folderOf, projectSettings, projectOrder),
    [projects, folders.folders, folders.folderOf, projectSettings, projectOrder],
  );
  const topIds = useMemo(() => rows.map((row) => row.id), [rows]);
  const placement = useMemo(() => projectRowPlacement(rows), [rows]);
  const collisionDetection = useMemo(
    () => projectCollisionDetection(placement, folders.canFile),
    [placement, folders.canFile],
  );
  const activeProjectId = plainRowsLit
    ? (projects.find((project) => isPathWithin(pathname, p.projectDetail(project.id)))?.id ?? null)
    : null;

  // "Show in sidebar" on an issue opens the folder its project sits in.
  useEffect(() => {
    if (!locate) return;
    const folder = rows.find(
      (row) => row.kind === "folder" && row.projects.some((project) => projectSidebarSource(project.id) === locate.source),
    );
    if (folder) setExpanded(projectFolderTreeKey(folder.id), true);
  }, [locate, rows, setExpanded]);

  const { canFile, moveProject } = folders;

  const onDragEnd = useCallback(
    ({ active, over }: DragEndEvent) => {
      releaseClickGuard();
      if (!over) return;
      const activeId = String(active.id);
      const overId = String(over.id);
      const from = placement.get(activeId);
      if (!from) return;
      if (overId.startsWith(FOLDER_DROP_PREFIX)) {
        const folderId = overId.slice(FOLDER_DROP_PREFIX.length);
        if (canFile && from.kind === "project" && from.container !== folderId) moveProject(activeId, folderId);
        return;
      }
      if (activeId === overId) return;
      const to = placement.get(overId);
      if (!to) return;
      let order: string[];
      if (from.container === to.container) {
        const ids =
          from.container === TOP_CONTAINER
            ? topIds
            : (rows.find((row) => row.id === from.container) as FolderRow | undefined)?.projects.map((project) => project.id) ?? [];
        const moved = arrayMove(ids, ids.indexOf(activeId), ids.indexOf(overId));
        order =
          from.container === TOP_CONTAINER
            ? sidebarProjectRowIds(moved.map((id) => rows.find((row) => row.id === id)!))
            : rows.flatMap((row) =>
                row.id === from.container
                  ? [row.id, ...moved]
                  : row.kind === "folder"
                    ? [row.id, ...row.projects.map((project) => project.id)]
                    : [row.id],
              );
      } else {
        if (from.kind !== "project" || (!canFile && to.container !== TOP_CONTAINER)) return;
        moveProject(activeId, to.container === TOP_CONTAINER ? null : to.container);
        order = sidebarProjectRowIds(rows).filter((id) => id !== activeId);
        order.splice(order.indexOf(overId), 0, activeId);
      }
      updatePrefs({
        // Rows the settings hide keep their relative place after the shown ones.
        projectOrder: [...order, ...projectOrder.filter((id) => !order.includes(id))],
        projectSettings: { ordering: "manual" },
      });
    },
    [rows, topIds, placement, projectOrder, releaseClickGuard, updatePrefs, canFile, moveProject],
  );

  const renderProject = (project: Project) => (
    <ProjectTreeItem
      key={project.id}
      wsId={wsId}
      userId={userId}
      project={project}
      active={activeProjectId === project.id}
      showCount={projectSettings.showCount}
      minimalIcon={projectSettings.minimalIcons}
      issueSettings={issueSettings}
      activeIssueId={litSource === projectSidebarSource(project.id) ? activeIssueId : null}
      openIssueId={activeIssueId}
      located={locatedSource === projectSidebarSource(project.id)}
    />
  );

  return (
    <PanelSection
      label={t(($) => $.nav.projects)}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      action={
        <>
          <ProjectsSectionSettings wsId={wsId} className={SECTION_SETTINGS_REVEAL_CLASS} />
          {foldersExtension ? (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <IconButton
                    label={t(($) => $.panels.work.new_project)}
                    size="icon-xs"
                    className="text-muted-foreground"
                  >
                    <Plus />
                  </IconButton>
                }
              />
              <DropdownMenuContent align="end" className="w-auto min-w-44">
                <DropdownMenuItem onClick={() => useModalStore.getState().open("create-project")}>
                  <FolderKanban className="size-3.5" />
                  {t(($) => $.panels.work.new_project)}
                </DropdownMenuItem>
                <foldersExtension.AddMenuItems />
              </DropdownMenuContent>
            </DropdownMenu>
          ) : (
            <IconButton
              label={t(($) => $.panels.work.new_project)}
              size="icon-xs"
              className="text-muted-foreground"
              onClick={() => useModalStore.getState().open("create-project")}
            >
              <Plus />
            </IconButton>
          )}
        </>
      }
    >
      <DndContext
        sensors={sensors}
        collisionDetection={collisionDetection}
        modifiers={DRAG_MODIFIERS}
        onDragStart={armClickGuard}
        onDragEnd={onDragEnd}
        onDragCancel={releaseClickGuard}
      >
        <SortableContext items={topIds} strategy={verticalListSortingStrategy}>
          <PanelList>
            {rows.map((row) =>
              row.kind === "no_project" ? (
                <NoProjectTreeItem
                  key={row.id}
                  wsId={wsId}
                  userId={userId}
                  showCount={projectSettings.showCount}
                  issueSettings={issueSettings}
                  activeIssueId={litSource === NO_PROJECT_SIDEBAR_SOURCE ? activeIssueId : null}
                  openIssueId={activeIssueId}
                  located={locatedSource === NO_PROJECT_SIDEBAR_SOURCE}
                />
              ) : row.kind === "folder" ? (
                <ProjectFolderTreeItem
                  key={row.id}
                  folder={row.folder}
                  folders={folders}
                  projects={row.projects}
                  holdsOpenItem={row.projects.some(
                    (project) =>
                      project.id === activeProjectId || locatedSource === projectSidebarSource(project.id),
                  )}
                  renderProject={renderProject}
                />
              ) : (
                renderProject(row.project)
              ),
            )}
          </PanelList>
        </SortableContext>
      </DndContext>
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

/** The sortable list a row sits in: the top level, or a folder's id. */
const TOP_CONTAINER = "top";
/** A folder row's drop target for filing a project in it, apart from its sortable id. */
const FOLDER_DROP_PREFIX = "folder-drop:";

interface ProjectRowPlace {
  kind: SidebarProjectRow<Project>["kind"];
  container: string;
}

function projectRowPlacement(rows: readonly SidebarProjectRow<Project>[]): Map<string, ProjectRowPlace> {
  const placement = new Map<string, ProjectRowPlace>();
  for (const row of rows) {
    placement.set(row.id, { kind: row.kind, container: TOP_CONTAINER });
    if (row.kind !== "folder") continue;
    for (const project of row.projects) placement.set(project.id, { kind: "project", container: row.id });
  }
  return placement;
}

type FolderRow = Extract<SidebarProjectRow<Project>, { kind: "folder" }>;

/**
 * A project held over a folder row files into that folder, when filing is
 * allowed. Otherwise rows
 * sort among their neighbours, and a project dropped among another list's
 * rows moves to that list. Folders and "No project" stay at the top level,
 * and a project sorts within its own folder without its folder row in the
 * way.
 */
const projectCollisionDetection = (
  placement: ReadonlyMap<string, ProjectRowPlace>,
  canFile: boolean,
): CollisionDetection => (args) => {
  const from = placement.get(String(args.active.id));
  const isProject = from?.kind === "project";
  if (isProject && canFile) {
    const into = pointerWithin({
      ...args,
      droppableContainers: args.droppableContainers.filter(
        (container) =>
          String(container.id).startsWith(FOLDER_DROP_PREFIX) &&
          String(container.id) !== FOLDER_DROP_PREFIX + from.container,
      ),
    });
    if (into.length > 0) return into;
  }
  return closestCenter({
    ...args,
    droppableContainers: args.droppableContainers.filter((container) => {
      if (String(container.id).startsWith(FOLDER_DROP_PREFIX)) return false;
      if (!isProject) return placement.get(String(container.id))?.container === TOP_CONTAINER;
      return container.id !== from.container;
    }),
  });
};

/**
 * A folder of projects: it only expands, and its projects sit under it on
 * the same connector line as an expanded project's issues.
 */
function ProjectFolderTreeItem({
  folder,
  folders,
  projects,
  holdsOpenItem,
  renderProject,
}: {
  folder: SidebarFolder;
  folders: ProjectFolders;
  projects: readonly Project[];
  /** The open project, or the row marking the open issue, is in this folder. */
  holdsOpenItem: boolean;
  renderProject: (project: Project) => ReactNode;
}) {
  const FolderMenu = getProjectFoldersExtension()?.FolderMenu;
  const listId = useId();
  const rowRef = useRef<HTMLButtonElement>(null);
  const [expanded, setExpanded] = useExpanded(projectFolderTreeKey(folder.id));
  const { listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: folder.id });
  const { setNodeRef: setDropRef, isOver } = useDroppable({ id: FOLDER_DROP_PREFIX + folder.id });
  const setRowRef = useCallback(
    (node: HTMLDivElement | null) => {
      setActivatorNodeRef(node);
      setDropRef(node);
    },
    [setActivatorNodeRef, setDropRef],
  );
  const rowMenu = useRowMenu();
  const rowMenuContext = useMemo(
    () => ({ menu: rowMenu.menu, setMenu: rowMenu.setMenu }),
    [rowMenu.menu, rowMenu.setMenu],
  );
  const projectIds = useMemo(() => projects.map((project) => project.id), [projects]);

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(isDragging && SORTABLE_ROW_DRAGGING_CLASS)}
    >
      <TreeToggleRow
        ref={rowRef}
        rowRef={setRowRef}
        {...listeners}
        label={folder.name}
        icon={expanded ? <FolderOpen className="size-4 shrink-0" /> : <Folder className="size-4 shrink-0" />}
        located={!expanded && holdsOpenItem}
        expanded={expanded}
        onExpandedChange={setExpanded}
        listId={listId}
        className={cn(isOver && "bg-nav-item-hover text-nav-item-hover-foreground ring-1 ring-surface-border")}
        onContextMenu={(event: MouseEvent) => {
          if (shouldOpenRowMenu(event)) rowMenu.openAtPointer(event);
        }}
        trailing={
          folders.canFile && (
            <span className="relative flex h-5 min-w-5 items-center justify-end">
              <IconButton
                label={folders.labels.newProjectIn(folder.name)}
                size="icon-xs"
                className="absolute -right-2 z-10 text-muted-foreground opacity-0 transition-opacity group-focus-within/row:opacity-100 group-hover/row:opacity-100 focus-visible:opacity-100 [@media(pointer:coarse)]:opacity-100"
                onPointerDown={(event) => event.stopPropagation()}
                onClick={() => useModalStore.getState().open("create-project", { folderId: folder.id })}
              >
                <Plus />
              </IconButton>
            </span>
          )
        }
      />
      {rowMenu.menu.point && FolderMenu && (
        <CollectionRowContext.Provider value={rowMenuContext}>
          <FolderMenu folder={folder} />
        </CollectionRowContext.Provider>
      )}
      <PanelDisclosure open={expanded}>
        {/* Same connector as an expanded project's issues, so the projects'
            icons sit on the folder's label column: 1px border + 5px + the
            row's 12px reaches the folder row's 12px + 16px icon + 10px gap. */}
        <SortableContext items={projectIds} strategy={verticalListSortingStrategy}>
          <ul id={listId} className="ml-5 space-y-px border-l border-surface-border pl-[5px]">
            {projects.length === 0 ? (
              <li className="flex h-7 items-center px-3 text-caption text-faint-foreground">
                {folders.labels.empty}
              </li>
            ) : (
              projects.map(renderProject)
            )}
          </ul>
        </SortableContext>
      </PanelDisclosure>
    </li>
  );
}

const SORTABLE_ROW_DRAGGING_CLASS =
  "relative z-20 rounded-lg bg-surface-raised shadow-[var(--menu-shadow)] ring-1 ring-surface-border";

/**
 * Issues outside every project, listed like a project's. No page shows them,
 * so the row only expands.
 */
function NoProjectTreeItem({
  wsId,
  userId,
  showCount,
  issueSettings,
  activeIssueId,
  openIssueId,
  located,
}: {
  wsId: string;
  userId: string | undefined;
  showCount: boolean;
  issueSettings: SidebarIssueSettings;
  activeIssueId: string | null;
  openIssueId: string | null;
  located: boolean;
}) {
  const { t } = useT("layout");
  const listId = useId();
  const rowRef = useRef<HTMLButtonElement>(null);
  const [expanded, setExpanded] = useExpanded(NO_PROJECT_SIDEBAR_SOURCE);
  const statusCatalog = useIssueStatuses(wsId);
  const catalogDependency = filterDependencyState(true, [statusCatalogSource(statusCatalog)]);
  const params = catalogDependency.unresolved
    ? undefined
    : noProjectSidebarRequest(issueSettings, statusCatalog.statuses, userId);
  const prefetch = usePrefetch(wsId, NO_PROJECT_SIDEBAR_SOURCE, params);
  const { data: count = 0 } = useQuery({
    ...issueTableFacetsOptions(wsId, noProjectCountRequest(statusCatalog.statuses)),
    enabled: showCount && !catalogDependency.unresolved,
    select: (data) =>
      data.facets[0]?.values.find((value) => value.key === NO_PROJECT_FACET_KEY)?.count ?? 0,
  });
  const label = t(($) => $.panels.work.no_project);
  const { listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: NO_PROJECT_ORDER_ID,
  });

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(isDragging && SORTABLE_ROW_DRAGGING_CLASS)}
    >
      <TreeToggleRow
        ref={rowRef}
        rowRef={setActivatorNodeRef}
        {...listeners}
        label={label}
        icon={<FolderMinus className="size-4 shrink-0" />}
        located={located}
        expanded={expanded}
        onExpandedChange={setExpanded}
        listId={listId}
        onPrefetch={prefetch}
        trailing={
          <span className="relative flex h-5 min-w-5 items-center justify-end">
            {showCount && (
              <PanelCount
                value={count}
                className="transition-opacity group-focus-within/row:opacity-0 group-hover/row:opacity-0 [@media(pointer:coarse)]:opacity-0"
              />
            )}
            <IconButton
              label={t(($) => $.panels.work.new_issue_no_project)}
              size="icon-xs"
              className="absolute -right-2 z-10 text-muted-foreground opacity-0 transition-opacity group-focus-within/row:opacity-100 group-hover/row:opacity-100 focus-visible:opacity-100 [@media(pointer:coarse)]:opacity-100"
              onPointerDown={(event) => event.stopPropagation()}
              // Explicit: an unfinished draft's project must not fill in.
              onClick={() => openCreateIssueWithPreference({ project_id: null })}
            >
              <Plus />
            </IconButton>
          </span>
        }
      />
      <PanelDisclosure open={expanded}>
        <SidebarIssueList
          id={listId}
          wsId={wsId}
          source={NO_PROJECT_SIDEBAR_SOURCE}
          params={params}
          paramsError={catalogDependency.error}
          onRetry={catalogDependency.retry}
          activeIssueId={activeIssueId}
          openIssueId={openIssueId}
          ownsOpen={(issue) => !issue.project_id}
          onCollapse={() => {
            setExpanded(false);
            rowRef.current?.focus();
          }}
        />
      </PanelDisclosure>
    </li>
  );
}

function ProjectTreeItem({
  wsId,
  userId,
  project,
  active,
  showCount,
  minimalIcon,
  issueSettings,
  activeIssueId,
  openIssueId,
  located,
}: {
  wsId: string;
  userId: string | undefined;
  project: Project;
  active: boolean;
  showCount: boolean;
  minimalIcon: boolean;
  issueSettings: SidebarIssueSettings;
  activeIssueId: string | null;
  openIssueId: string | null;
  located: boolean;
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

  const { listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: project.id });
  // Right-click opens the projects page's row menu at the cursor. It mounts on
  // the first right-click, so idle rows hold no menu or its queries.
  const rowMenu = useRowMenu();
  const rowMenuContext = useMemo(
    () => ({ menu: rowMenu.menu, setMenu: rowMenu.setMenu }),
    [rowMenu.menu, rowMenu.setMenu],
  );

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={cn(isDragging && SORTABLE_ROW_DRAGGING_CLASS)}
    >
      <TreeRow
        ref={linkRef}
        rowRef={setActivatorNodeRef}
        {...listeners}
        href={p.projectDetail(project.id)}
        label={project.title}
        icon={
          project.icon && !minimalIcon ? (
            <ProjectIcon project={project} size="md" />
          ) : (
            <FolderKanban className="size-4 shrink-0" />
          )
        }
        active={active}
        located={located}
        expanded={expanded}
        onExpandedChange={setExpanded}
        listId={listId}
        toggleLabel={t(($) => $.panels.work.project_issues, { name: project.title })}
        onPrefetch={prefetch}
        labelSuffix={project.visibility === "private" && <PrivateProjectLock />}
        onContextMenu={(event: MouseEvent) => {
          if (shouldOpenRowMenu(event)) rowMenu.openAtPointer(event);
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
      {rowMenu.menu.point && (
        <CollectionRowContext.Provider value={rowMenuContext}>
          <ProjectMenu
            project={project}
            triggerHidden
            leading={
              <DropdownMenuItem onClick={() => openCreateIssueWithPreference({ project_id: project.id })}>
                <Plus />
                {t(($) => $.panels.work.new_issue_here)}
              </DropdownMenuItem>
            }
          />
        </CollectionRowContext.Provider>
      )}
      <PanelDisclosure open={expanded}>
        <SidebarIssueList
          id={listId}
          wsId={wsId}
          source={source}
          params={params}
          paramsError={catalogDependency.error}
          onRetry={catalogDependency.retry}
          activeIssueId={activeIssueId}
          openIssueId={openIssueId}
          ownsOpen={(issue) => issue.project_id === project.id}
          onCollapse={() => {
            setExpanded(false);
            linkRef.current?.focus();
          }}
        />
      </PanelDisclosure>
    </li>
  );
}
