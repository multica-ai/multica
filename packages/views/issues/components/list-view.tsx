"use client";

import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useWorkspaceId } from "@multica/core/hooks";

import { memo, useState, useCallback, useMemo, useEffect, useRef } from "react";
import { ChevronRight, Plus } from "lucide-react";
import { Accordion } from "@base-ui/react/accordion";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useDroppable,
  useSensor,
  useSensors,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy, arrayMove } from "@dnd-kit/sortable";
import { Virtuoso } from "react-virtuoso";
import { Button } from "@multica/ui/components/ui/button";
import type { Issue, IssueWorkflowStatusNode, IssueStatus, Project } from "@multica/core/types";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { StatusIcon } from "./status-icon";
import { statusCategoryOfKey } from "@multica/core/issues";
import { StatusHeading } from "./status-heading";
import { ListRow, DraggableListRow, type ChildProgress } from "./list-row";
import { useDragSettle } from "./use-drag-settle";
import { ListLoadMoreFooter } from "./list-load-more-footer";
import { useT } from "../../i18n";
import {
  type DragMoveUpdates,
  makeKanbanCollision,
  statusGroupId,
  buildColumns,
  computePosition,
  findColumn,
  getMoveAnchors,
  insertIdByPosition,
  issueMatchesGroup,
  getMoveUpdates,
} from "../utils/drag-utils";
import type { BoardColumnGroup } from "./board-column";
import { useIssueSurfaceSelection } from "../surface/selection-context";
import type { IssueCreateDefaults } from "../surface/types";
import type {
  IssueStatusPageState,
  IssueStatusPagination,
} from "../surface/use-issue-status-branches";
import type { IssueGroupBranches } from "../surface/use-issue-group-branches";
import { buildWorkflowStatusGroups } from "../utils/workflow-status-groups";
import { VirtuosoSeed, VIRTUOSO_SEED_COUNT } from "../../common/virtuoso-seed";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { useRestoredScrollRef } from "../../platform";
import { HiddenColumnsPanel, HiddenColumnRow } from "./hidden-columns-panel";
import { toast } from "sonner";

// The seed must use the same CSS height as ListRow before scroll restoration
// runs at ref-attach. Virtuoso receives the seed's resolved pixel height below.
const LIST_ROW_HEIGHT = "var(--issue-row-height)";

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();
const EMPTY_IDS: string[] = [];
const EMPTY_PAGE: IssueStatusPageState = {
  total: 0,
  loaded: 0,
  hasMore: false,
  isLoading: false,
  isFetching: false,
  isError: false,
  loadMore: () => {},
  retry: () => {},
};

function buildListGroups(visibleStatuses: IssueStatus[]): BoardColumnGroup[] {
  return visibleStatuses.map((status) => ({
    id: statusGroupId(status),
    title: status,
    status,
    createData: { status: status },
  }));
}

function ListViewImpl({
  issues,
  visibleStatuses,
  hiddenStatuses = [],
  childProgressMap = EMPTY_PROGRESS_MAP,
  projectMap,
  statusPagination,
  groupBranches,
  workflowStatuses,
  projectId,
  onMoveIssue,
  onCreateIssue,
}: {
  issues: Issue[];
  visibleStatuses: IssueStatus[];
  hiddenStatuses?: IssueStatus[];
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  statusPagination?: IssueStatusPagination;
  groupBranches?: IssueGroupBranches;
  workflowStatuses?: IssueWorkflowStatusNode[];
  projectId?: string;
  onMoveIssue?: (issueId: string, updates: DragMoveUpdates, onSettled?: () => void) => void;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
}) {
  const listCollapsedStatuses = useViewStore(
    (s) => s.listCollapsedStatuses
  );
  const toggleListCollapsed = useViewStore(
    (s) => s.toggleListCollapsed
  );
  const sortBy = useViewStore((s) => s.sortBy);
  const { t } = useT("issues");
  const hiddenKeys = useViewStore((s) => s.hiddenStatuses);
  const selectedKeys = useViewStore((s) => s.statusFilters);
  const showStatus = useViewStore((s) => s.showStatus);
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);

  const sortFieldKey = sortBy === "created_at" ? "created" : sortBy;
  const sortLabel = sortBy !== "position"
    ? t(($) => $.board.ordered_by, { field: t(($) => $.display[`sort_${sortFieldKey}` as keyof typeof $.display]) })
    : null;

  const dragEnabled = !!onMoveIssue;

  const allGroups = useMemo(
    () =>
      workflowStatuses === undefined
        ? buildListGroups(visibleStatuses)
        : buildWorkflowStatusGroups(
            workflowStatuses,
            groupBranches?.descriptors ?? [],
            true,
          ),
    [groupBranches?.descriptors, workflowStatuses, visibleStatuses],
  );
  const hiddenGroups = useMemo(() => allGroups.filter((group) => {
    if (workflowStatuses === undefined) return false;
    const keys = [group.workflowStatusId, group.workflowStatusLegacyKey].filter((key): key is string => !!key);
    return keys.some((key) => hiddenKeys.includes(key)) && !keys.some((key) => selectedKeys.includes(key));
  }), [allGroups, workflowStatuses, hiddenKeys, selectedKeys]);
  const groups = useMemo(() => allGroups.filter((group) => !hiddenGroups.includes(group)), [allGroups, hiddenGroups]);
  const groupedIssues = useMemo(
    () => (groupBranches?.enabled ? groupBranches.issues : issues),
    [groupBranches, issues],
  );
  const expandedGroupIds = useMemo(
    () =>
      groups
        .filter((group) =>
          workflowStatuses === undefined
            ? group.status != null && !listCollapsedStatuses.includes(group.status)
            : ![group.workflowStatusId, group.workflowStatusLegacyKey].some((key) => !!key && listCollapsedStatuses.includes(key)),
        )
        .map((group) => group.id),
    [groups, workflowStatuses, listCollapsedStatuses],
  );
  const groupIds = useMemo(
    () => new Set(groups.map((g) => g.id)),
    [groups],
  );
  const groupMap = useMemo(
    () => new Map(groups.map((g) => [g.id, g])),
    [groups],
  );

  // --- Drag state ---
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null);
  // Shared drag/settle primitive (see use-drag-settle) — same machine as
  // board-view, so the two surfaces can't drift apart.
  const {
    columns,
    setColumns,
    columnsRef,
    isDraggingRef,
    isSettlingRef,
    recentlyMovedRef,
    settleVersion,
    beginSettle,
  } = useDragSettle(() => buildColumns(groupedIssues, groups, "status"));

  useEffect(() => {
    if (!isDraggingRef.current && !isSettlingRef.current) {
      setColumns(buildColumns(groupedIssues, groups, "status"));
    }
  }, [groupedIssues, groups, settleVersion, setColumns, isDraggingRef, isSettlingRef]);

  const issueMap = useMemo(() => {
    const map = new Map<string, Issue>();
    for (const issue of groupedIssues) map.set(issue.id, issue);
    return map;
  }, [groupedIssues]);

  const issueMapRef = useRef(issueMap);
  if (!isDraggingRef.current && !isSettlingRef.current) {
    issueMapRef.current = issueMap;
  }

  const collisionDetection = useMemo(
    () => makeKanbanCollision(groupIds),
    [groupIds],
  );

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    })
  );

  const handleDragStart = useCallback(
    (event: DragStartEvent) => {
      isDraggingRef.current = true;
      const issue = issueMapRef.current.get(event.active.id as string) ?? null;
      setActiveIssue(issue);
    },
    [isDraggingRef],
  );

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      const { active, over } = event;
      if (!over || recentlyMovedRef.current) return;

      const activeId = active.id as string;
      const overId = over.id as string;

      setColumns((prev) => {
        const activeCol = findColumn(prev, activeId, groupIds);
        const overCol = findColumn(prev, overId, groupIds);
        if (!activeCol || !overCol || activeCol === overCol) return prev;
        const target = groups.find((group) => group.id === overCol);
        if (target?.workflowStatusArchived || (target?.workflowId && target.workflowId !== issueMapRef.current.get(activeId)?.workflow_id)) return prev;
        const targetStatus = target?.status;
        if (targetStatus && catalog.entryOf(targetStatus)?.archived_at) return prev;

        if (sortBy !== "position") return prev;

        recentlyMovedRef.current = true;
        const oldIds = prev[activeCol]!.filter((id) => id !== activeId);
        const newIds = [...prev[overCol]!];
        const overIndex = newIds.indexOf(overId);
        const insertIndex = overIndex >= 0 ? overIndex : newIds.length;
        newIds.splice(insertIndex, 0, activeId);
        return { ...prev, [activeCol]: oldIds, [overCol]: newIds };
      });
    },
    [groupIds, groups, catalog, sortBy, recentlyMovedRef, setColumns],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      isDraggingRef.current = false;
      setActiveIssue(null);

      const resetColumns = () =>
        setColumns(buildColumns(groupedIssues, groups, "status"));

      if (!over || !onMoveIssue) {
        resetColumns();
        return;
      }

      const activeId = active.id as string;
      const overId = over.id as string;

      const cols = columnsRef.current;
      const activeCol = findColumn(cols, activeId, groupIds);
      const overCol = findColumn(cols, overId, groupIds);
      if (!activeCol || !overCol) {
        resetColumns();
        return;
      }

      const targetGroup = groupMap.get(overCol);
      if (targetGroup?.workflowStatusArchived || (targetGroup?.workflowId && targetGroup.workflowId !== issueMapRef.current.get(activeId)?.workflow_id)) {
        resetColumns();
        return;
      }

      let finalColumns = cols;
      if (activeCol === overCol && sortBy === "position") {
        const ids = cols[activeCol]!;
        const oldIndex = ids.indexOf(activeId);
        const newIndex = ids.indexOf(overId);
        if (oldIndex !== -1 && newIndex !== -1 && oldIndex !== newIndex) {
          const reordered = arrayMove(ids, oldIndex, newIndex);
          finalColumns = { ...cols, [activeCol]: reordered };
          setColumns(finalColumns);
        }
      }

      const finalCol = sortBy === "position"
        ? findColumn(finalColumns, activeId, groupIds)
        : overCol;
      if (!finalCol) {
        resetColumns();
        return;
      }
      const finalGroup = groupMap.get(finalCol);
      if (!finalGroup || finalGroup.workflowStatusArchived || (finalGroup.workflowId && finalGroup.workflowId !== issueMapRef.current.get(activeId)?.workflow_id)) {
        resetColumns();
        return;
      }

      const map = issueMapRef.current;
      if (finalGroup.status && map.get(activeId)?.status !== finalGroup.status && catalog.entryOf(finalGroup.status)?.archived_at) {
        resetColumns();
        return;
      }

      if (sortBy !== "position") {
        const currentIssue = map.get(activeId);
        if (!currentIssue || issueMatchesGroup(currentIssue, finalGroup)) {
          resetColumns();
          if (activeId !== overId) {
            toast.info(t(($) => $.board.manual_reorder_hint), {
              id: "issue-manual-reorder-hint",
            });
          }
          return;
        }
        // Optimistically move the row into the target group *now*. Without this
        // the sortBy != "position" branch never touched local columns on drop,
        // so the row sat in its origin group for the whole request and only
        // jumped across when the mutation settled — the same "snaps back, then
        // moves" glitch the board view had. Placement mirrors the cache
        // (insertIdByPosition) so the settle rebuild is a visual no-op.
        const targetIds = insertIdByPosition(
          (cols[finalCol] ?? []).filter((id) => id !== activeId),
          activeId,
          currentIssue.position,
          map,
        );
        setColumns((prev) => {
          const fromIds = (prev[activeCol] ?? []).filter((cid) => cid !== activeId);
          return { ...prev, [activeCol]: fromIds, [finalCol]: targetIds };
        });
        onMoveIssue(
          activeId,
          {
            ...getMoveUpdates(finalGroup, currentIssue.position, currentIssue),
            ...getMoveAnchors(targetIds, activeId),
          },
          beginSettle(),
        );
        return;
      }

      const finalIds = finalColumns[finalCol]!;
      const newPosition = computePosition(finalIds, activeId, map);
      const currentIssue = map.get(activeId);

      if (
        currentIssue &&
        issueMatchesGroup(currentIssue, finalGroup) &&
        currentIssue.position === newPosition
      ) {
        return;
      }

      // beginSettle() also bumps settleVersion on settle (board-view did, this
      // branch did not) so a failed position move reverts instead of stranding
      // the row at the drop target.
      onMoveIssue(
        activeId,
        {
          ...getMoveUpdates(finalGroup, newPosition, currentIssue),
          ...getMoveAnchors(finalIds, activeId),
        },
        beginSettle(),
      );
    },
    [groupedIssues, groups, onMoveIssue, groupIds, groupMap, sortBy, beginSettle, setColumns, columnsRef, isDraggingRef, catalog, t],
  );

  // dnd-kit fires onDragCancel — never onDragEnd — when an active drag is
  // aborted: pointercancel, window resize, tab hide, or Escape. Touch browsers
  // hit that path constantly, because a scroll gesture that starts on a row
  // moves past the 5px activation distance and *then* the browser takes the
  // gesture over for native scrolling and cancels the pointer. Without this
  // handler `isDraggingRef` stayed true for the rest of the session, which
  // froze the column mirror against cache updates and — because the accordion's
  // onValueChange is guarded by the same ref — made tapping a status header a
  // no-op, so groups could no longer be collapsed at all (MUL-6240).
  const handleDragCancel = useCallback(() => {
    isDraggingRef.current = false;
    setActiveIssue(null);
    setColumns(buildColumns(groupedIssues, groups, "status"));
  }, [groupedIssues, groups, setColumns, isDraggingRef]);

  // The single scroll container is shared by every status panel's Virtuoso as
  // its customScrollParent, so a callback ref hands the element to the panels
  // once it mounts. Keeping one scroller (rather than one per panel) preserves
  // the current sticky-header + cross-section scroll behavior; only the rows
  // inside each expanded panel virtualize.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const [rowHeight, setRowHeight] = useState<number>();
  // Pull-based scroll restoration (MUL-4741): assign the saved offset when
  // the shared scroller attaches — the per-status seeds plus their estimate
  // spacers give it a truthful height on the first commit.
  const restoreScrollRef = useRestoredScrollRef("list");
  const attachScroller = useCallback(
    (el: HTMLDivElement | null) => {
      const row = el?.querySelector<HTMLElement>('[data-slot="issue-list-row"]');
      // Read the rendered height, not parseFloat of the custom property: the
      // source token may be a rem/calc length. With no seed rows, let Virtuoso
      // measure its first item when a group is expanded or data arrives.
      const height = row ? Number.parseFloat(getComputedStyle(row).height) : 0;
      setRowHeight(height > 0 ? height : undefined);
      setScrollEl(el);
      restoreScrollRef(el);
    },
    [restoreScrollRef],
  );

  const content = (
    <>
      <Accordion.Root
        multiple
        className="space-y-1"
        value={expandedGroupIds}
        onValueChange={(value: string[]) => {
          if (isDraggingRef.current) return;
          for (const group of groups) {
            const wasExpanded = expandedGroupIds.includes(group.id);
            const isExpanded = value.includes(group.id);
            if (wasExpanded !== isExpanded) {
              const key = workflowStatuses === undefined ? group.status : group.workflowStatusId ?? group.workflowStatusLegacyKey;
              if (isExpanded) {
                for (const collapsedKey of new Set([key, group.workflowStatusLegacyKey])) {
                  if (collapsedKey && listCollapsedStatuses.includes(collapsedKey)) toggleListCollapsed(collapsedKey);
                }
              } else if (key) toggleListCollapsed(key);
            }
          }
        }}
      >
        {groups.map((group) => {
          const isExpanded = expandedGroupIds.includes(group.id);
          const page = group.workflowStatusId !== undefined
            ? groupBranches?.pagination[group.id]
            : group.status
              ? statusPagination?.[group.status]
              : undefined;
          return (
            <StatusAccordionItem
              key={group.id}
              group={group}
              issueIds={columns[group.id] ?? EMPTY_IDS}
              issueMap={issueMapRef.current}
              childProgressMap={childProgressMap}
              projectMap={projectMap}
              page={page ?? { ...EMPTY_PAGE, total: group.totalCount ?? 0 }}
              projectId={projectId}
              onCreateIssue={onCreateIssue}
              dragEnabled={dragEnabled}
              isExpanded={isExpanded}
              sortLabel={sortLabel}
              scrollParent={scrollEl}
              rowHeight={rowHeight}
            />
          );
        })}
      </Accordion.Root>
      {hiddenGroups.length > 0 && (
        <div className="mt-4 px-1 pb-4">
          {hiddenGroups.map((group) => <HiddenColumnRow key={group.id} status={group.workflowStatusId ?? group.workflowStatusLegacyKey ?? ""}
            label={group.title} total={group.totalCount} onShow={() => {
              if (group.workflowStatusId) showStatus(group.workflowStatusId);
              if (group.workflowStatusLegacyKey) showStatus(group.workflowStatusLegacyKey);
            }} />)}
        </div>
      )}
      {workflowStatuses === undefined && hiddenStatuses.length > 0 && (
        <div className="mt-4 px-1 pb-4">
          <HiddenColumnsPanel hiddenStatuses={hiddenStatuses} renderRow={(status) => (
            <HiddenColumnRow key={status} status={status} total={statusPagination?.[status]?.total} />
          )} />
        </div>
      )}
    </>
  );

  if (!dragEnabled) {
    return (
      <div ref={attachScroller} data-tab-scroll-root="list" className="flex-1 min-h-0 overflow-y-auto p-2 pt-0">
        {content}
      </div>
    );
  }

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
      onDragCancel={handleDragCancel}
    >
      <div ref={attachScroller} data-tab-scroll-root="list" className="flex-1 min-h-0 overflow-y-auto p-2 pt-0">
        {content}
      </div>

      <DragOverlay dropAnimation={null}>
        {activeIssue ? (
          <div className="max-w-2xl rotate-1 cursor-grabbing opacity-90 shadow-lg shadow-black/10 rounded-md border border-border bg-card px-4 py-2">
            <span className="text-caption text-muted-foreground mr-2">{activeIssue.identifier}</span>
            <span className="text-body">{activeIssue.title}</span>
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

function StatusAccordionItem({
  group,
  issueIds,
  issueMap,
  childProgressMap,
  projectMap,
  page,
  projectId,
  onCreateIssue,
  dragEnabled,
  isExpanded,
  sortLabel,
  scrollParent,
  rowHeight,
}: {
  group: BoardColumnGroup;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  page: IssueStatusPageState;
  projectId?: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
  dragEnabled: boolean;
  isExpanded: boolean;
  sortLabel: string | null;
  scrollParent: HTMLElement | null;
  rowHeight: number | undefined;
}) {
  const { t } = useT("issues");
  const selection = useIssueSurfaceSelection();
  const selectedIds = selection.selectedIds;
  const select = selection.select;
  const deselect = selection.deselect;

  const issues = useMemo(
    () => issueIds.flatMap((id) => {
      const issue = issueMap.get(id);
      return issue ? [issue] : [];
    }),
    [issueIds, issueMap],
  );

  const selectedCount = issueIds.filter((id) => selectedIds.has(id)).length;
  const allSelected = issues.length > 0 && selectedCount === issues.length;
  const someSelected = selectedCount > 0;

  const statusWsId = useWorkspaceId();
  const statusCatalog = useIssueStatuses(statusWsId);
  const { setNodeRef: setDroppableRef, isOver: droppableIsOver } = useDroppable({
    id: group.id,
    disabled: !dragEnabled,
  });
  const isOver = droppableIsOver && !statusCatalog.entryOf(group.status ?? "")?.archived_at;

  const disableSorting = !!sortLabel;

  // The infinite-scroll sentinel rides Virtuoso's Footer so it sits at the true
  // end of the virtualized rows and still fires loadMore when scrolled to it.
  const listComponents = useMemo(
    () => ({
      // Always a non-undefined object: react-virtuoso throws if `components`
      // is ever undefined (MUL-4474). The footer itself renders null for a
      // short, non-paginated section.
      Footer: () => (
        <ListLoadMoreFooter
          hasMore={page.hasMore}
          isLoading={page.isLoading || page.isFetching}
          total={page.total}
          onLoadMore={page.loadMore}
          isError={page.isError}
          onRetry={page.retry}
        />
      ),
    }),
    [page],
  );

  const computeItemKey = (_index: number, issue: Issue) => issue.id;
  const itemContent = (_index: number, issue: Issue) =>
    dragEnabled ? (
      <DraggableListRow
        issue={issue}
        childProgress={childProgressMap.get(issue.id)}
        project={
          issue.project_id ? projectMap?.get(issue.project_id) : undefined
        }
        disableSorting={disableSorting}
      />
    ) : (
      <ListRow
        issue={issue}
        childProgress={childProgressMap.get(issue.id)}
        project={
          issue.project_id ? projectMap?.get(issue.project_id) : undefined
        }
      />
    );

  // Rows virtualize into the page's shared scroll parent. Only render when the
  // section is expanded and non-empty — a Virtuoso in a collapsed (0-height /
  // hidden) panel has no viewport to measure. While the shared scroll parent
  // is still null (callback ref not settled after a route-return remount),
  // seed a bounded slice of real rows so the first painted frame isn't blank;
  // once it's set, mount the Virtuoso with a matching `initialItemCount` so the
  // measurement frame keeps those rows instead of flashing empty (MUL-4750).
  // The droppable, SortableContext, sticky header, and collapse are unchanged;
  // virtualization only decides whether an off-screen row is in the DOM.
  const rows =
    isExpanded && issues.length > 0 ? (
      scrollParent ? (
        <Virtuoso
          customScrollParent={scrollParent}
          data={issues}
          computeItemKey={computeItemKey}
          initialItemCount={Math.min(issues.length, VIRTUOSO_SEED_COUNT)}
          defaultItemHeight={rowHeight}
          increaseViewportBy={{ top: 400, bottom: 400 }}
          components={listComponents}
          itemContent={itemContent}
        />
      ) : (
        <VirtuosoSeed
          data={issues}
          itemContent={itemContent}
          computeItemKey={computeItemKey}
          estimatedItemHeight={LIST_ROW_HEIGHT}
        />
      )
    ) : null;

  return (
    <Accordion.Item value={group.id} ref={dragEnabled ? setDroppableRef : undefined}>
      <Accordion.Header
        className={`group/header sticky top-0 z-10 flex h-10 items-center rounded-lg bg-muted transition-colors hover:bg-accent ${
          isOver && !isExpanded
            ? "ring-2 ring-brand/25 bg-accent/15"
            : ""
        }`}
      >
        <div className="pl-3 flex items-center">
          <input
            type="checkbox"
            checked={allSelected}
            ref={(el) => {
              if (el) el.indeterminate = someSelected && !allSelected;
            }}
            onChange={() => {
              if (allSelected) {
                deselect(issueIds);
              } else {
                select(issueIds);
              }
            }}
            className="cursor-pointer accent-primary"
          />
        </div>
        <Accordion.Trigger className="group/trigger flex flex-1 items-center gap-2 px-2 h-full text-left outline-none cursor-pointer">
          <ChevronRight className="size-3.5 shrink-0 text-muted-foreground transition-transform group-aria-expanded/trigger:rotate-90" />
          {group.workflowStatusId !== undefined ? (
            <div className="flex min-w-0 items-center gap-2">
              <StatusIcon
          status={group.workflowStatusLegacyKey ?? group.id}
          category={statusCategoryOfKey(group.workflowStatusPhase ?? "unstarted")}
          color={group.workflowStatusColor}
          icon={group.workflowStatusIcon}
          className="size-3"
        />
              <span className="truncate text-body font-medium" title={group.title}>
                {group.title}
              </span>
              <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-micro font-medium tabular-nums text-muted-foreground">
                {page.total}
              </span>
            </div>
          ) : group.status ? (
            <StatusHeading status={group.status} count={page.total} />
          ) : null}
        </Accordion.Trigger>
        {onCreateIssue && !statusCatalog.entryOf(group.status ?? "")?.archived_at &&
          (group.workflowStatusId === undefined ||
            group.createData !== undefined) && (
            <div className="pr-2">
              {/* Lazy-mounted tooltip machinery — see DeferredTooltip. */}
              <DeferredTooltip
                content={t(($) => $.list.add_issue_tooltip)}
                trigger={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="rounded-full text-muted-foreground opacity-0 group-hover/header:opacity-100 transition-opacity"
                    onClick={() => {
                      const defaults = {
                        ...(group.createData ?? {}),
                        ...(group.workflowId ? { required_workflow_id: group.workflowId, require_project_choice: !projectId, project_id: projectId ?? null } : {}),
                        ...(projectId ? { project_id: projectId } : {}),
                      };
                      onCreateIssue(defaults);
                    }}
                  >
                    <Plus className="size-3.5" />
                  </Button>
                }
              />
            </div>
          )}
      </Accordion.Header>
      <Accordion.Panel>
        {issues.length > 0 ? (
          dragEnabled ? (
            <SortableContext items={issueIds} strategy={verticalListSortingStrategy}>
              {rows}
            </SortableContext>
          ) : (
            rows
          )
        ) : (
          <p className="py-6 text-center text-caption text-muted-foreground">
            {t(($) => $.list.empty_status)}
          </p>
        )}
      </Accordion.Panel>
    </Accordion.Item>
  );
}

/**
 * Memoized: the surface controller re-renders on loading-flag flips (e.g. a
 * query enabling when the view changes) — without memo every such flip
 * re-rendered this entire view tree (hundreds of ms). All props are
 * referentially stable useMemo/useCallback outputs from the controller.
 */
export const ListView = memo(ListViewImpl);
