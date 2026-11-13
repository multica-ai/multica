"use client";

import { useCallback, useId, useMemo, useRef, useSyncExternalStore, type MouseEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { restrictToFirstScrollableAncestor, restrictToVerticalAxis } from "@dnd-kit/modifiers";
import { CSS } from "@dnd-kit/utilities";
import { Folder, Plus } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
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
import { ProjectIcon } from "../../projects/components/project-icon";
import { ProjectMenu } from "../../projects/components/project-row-actions";
import { shouldOpenRowMenu, useRowMenu } from "../../common/point-menu";
import { CollectionRowContext } from "../collection-list/row-context";
import { useDragClickGuard } from "../use-drag-click-guard";
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

export interface IssueInTree {
  /** `null` when no expanded row lists the issue. */
  issueId: string | null;
  /** The sidebar sources of the expanded rows that list or keep it. */
  sources: readonly string[];
}

const NOT_IN_TREE: IssueInTree = { issueId: null, sources: [] };

/**
 * Where the expanded rows list or keep the issue under `ref` — its UUID, or
 * an identifier like `MUL-123` matched case-insensitively as the server does.
 */
export function useIssueInTree(wsId: string, ref: string | null): IssueInTree {
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
  /** The project whose row marks the open issue no list shows. */
  locatedProjectId: string | null;
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
  locatedProjectId,
}: ProjectsSectionProps) {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const collapsed = useSidebarTreeStore((s) => s.projectsCollapsed);
  const setCollapsed = useSidebarTreeStore((s) => s.setProjectsCollapsed);
  const { projectSettings, issueSettings, projectOrder } = useSidebarProjectPrefs(wsId);
  const updatePrefs = useUpdateSidebarProjectPrefs(wsId);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }));
  const { arm: armClickGuard, release: releaseClickGuard } = useDragClickGuard();

  const onDragEnd = useCallback(
    ({ active, over }: DragEndEvent) => {
      releaseClickGuard();
      if (!over || active.id === over.id) return;
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
    [projects, projectOrder, releaseClickGuard, updatePrefs],
  );

  return (
    <PanelSection
      label={t(($) => $.nav.projects)}
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      action={
        <>
          <ProjectsSectionSettings wsId={wsId} className="opacity-0 group-hover/section:opacity-100 group-focus-within/section:opacity-100 [@media(pointer:coarse)]:opacity-100" />
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
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        modifiers={DRAG_MODIFIERS}
        onDragStart={armClickGuard}
        onDragEnd={onDragEnd}
        onDragCancel={releaseClickGuard}
      >
        <SortableContext items={projects.map((project) => project.id)} strategy={verticalListSortingStrategy}>
          <PanelList>
            {projects.map((project) => (
              <ProjectTreeItem
                key={project.id}
                wsId={wsId}
                userId={userId}
                project={project}
                active={plainRowsLit && isPathWithin(pathname, p.projectDetail(project.id))}
                showCount={projectSettings.showCount}
                issueSettings={issueSettings}
                activeIssueId={litSource === projectSidebarSource(project.id) ? activeIssueId : null}
                openIssueId={activeIssueId}
                located={locatedProjectId === project.id}
              />
            ))}
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

function ProjectTreeItem({
  wsId,
  userId,
  project,
  active,
  showCount,
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
        icon={
          project.icon ? <ProjectIcon project={project} size="md" /> : <Folder className="size-4 shrink-0" />
        }
        active={active}
        located={located}
        expanded={expanded}
        onExpandedChange={setExpanded}
        listId={listId}
        toggleLabel={t(($) => $.panels.work.project_issues, { name: project.title })}
        onPrefetch={prefetch}
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
      {expanded && (
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
      )}
    </li>
  );
}
