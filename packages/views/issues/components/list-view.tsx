"use client";

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
import type { Issue, IssueStatus, Project } from "@multica/core/types";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { StatusHeading } from "./status-heading";
import { ListRow, DraggableListRow, type ChildProgress } from "./list-row";
import { useDragSettle } from "./use-drag-settle";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";
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
import { VirtuosoSeed, VIRTUOSO_SEED_COUNT } from "../../common/virtuoso-seed";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { useRestoredScrollRef } from "../../platform";

// List rows are a fixed 36px (h-9). Sharing the estimate between the seed's
// trailing spacer and Virtuoso's defaultItemHeight keeps the shared
// scroller's height truthful from the first frame — which both stops the
// scrollbar from re-drawing across the seed → Virtuoso handoff and lets the
// restored scrollTop assignment stick at ref-attach (MUL-4741).
const LIST_ROW_ESTIMATED_HEIGHT = 36;

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();
const EMPTY_IDS: string[] = [];
// Passed to <Virtuoso components> when there is no Footer. Must be a STABLE
// object, never `undefined`: react-virtuoso seeds `components` with an internal
// `{}` default, and an explicit `undefined` prop overwrites that default, so
// its startup destructure of `EmptyPlaceholder`/`Footer` throws (MUL-4474).
const EMPTY_VIRTUOSO_COMPONENTS = {};

function buildListGroups(visibleStatuses: IssueStatus[]): BoardColumnGroup[] {
  return visibleStatuses.map((status) => ({
    id: statusGroupId(status),
    title: status,
    status,
    createData: { status },
  }));
}

function ListViewImpl({
  issues,
  visibleStatuses,
  childProgressMap = EMPTY_PROGRESS_MAP,
  projectMap,
  statusPagination,
  projectId,
  onMoveIssue,
  onCreateIssue,
}: {
  issues: Issue[];
  visibleStatuses: IssueStatus[];
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  statusPagination: IssueStatusPagination;
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

  const sortFieldKey = sortBy === "created_at" ? "created" : sortBy;
  const sortLabel = sortBy !== "position"
    ? t(($) => $.board.ordered_by, { field: t(($) => $.display[`sort_${sortFieldKey}` as keyof typeof $.display]) })
    : null;

  const expandedStatuses = useMemo(
    () =>
      visibleStatuses.filter(
        (s) => !listCollapsedStatuses.includes(s)
      ),
    [visibleStatuses, listCollapsedStatuses]
  );

  const dragEnabled = !!onMoveIssue;

  const groups = useMemo(
    () => buildListGroups(visibleStatuses),
    [visibleStatuses],
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
  } = useDragSettle(() => buildColumns(issues, groups, "status"));

  useEffect(() => {
    if (!isDraggingRef.current && !isSettlingRef.current) {
      setColumns(buildColumns(issues, groups, "status"));
    }
  }, [issues, groups, settleVersion, setColumns, isDraggingRef, isSettlingRef]);

  const issueMap = useMemo(() => {
    const map = new Map<string, Issue>();
    for (const issue of issues) map.set(issue.id, issue);
    return map;
  }, [issues]);

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
    [groupIds, sortBy, recentlyMovedRef, setColumns],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      isDraggingRef.current = false;
      setActiveIssue(null);

      const resetColumns = () =>
        setColumns(buildColumns(issues, groups, "status"));

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
      if (!finalGroup) {
        resetColumns();
        return;
      }

      const map = issueMapRef.current;

      if (sortBy !== "position") {
        const currentIssue = map.get(activeId);
        if (!currentIssue || issueMatchesGroup(currentIssue, finalGroup)) {
          resetColumns();
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
            ...getMoveUpdates(finalGroup, currentIssue.position),
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
          ...getMoveUpdates(finalGroup, newPosition),
          ...getMoveAnchors(finalIds, activeId),
        },
        beginSettle(),
      );
    },
    [issues, groups, onMoveIssue, groupIds, groupMap, sortBy, beginSettle, setColumns, columnsRef, isDraggingRef],
  );

  // The single scroll container is shared by every status panel's Virtuoso as
  // its customScrollParent, so a callback ref hands the element to the panels
  // once it mounts. Keeping one scroller (rather than one per panel) preserves
  // the current sticky-header + cross-section scroll behavior; only the rows
  // inside each expanded panel virtualize.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  // Pull-based scroll restoration (MUL-4741): assign the saved offset when
  // the shared scroller attaches — the per-status seeds plus their estimate
  // spacers give it a truthful height on the first commit.
  const restoreScrollRef = useRestoredScrollRef("list");
  const attachScroller = useCallback(
    (el: HTMLDivElement | null) => {
      setScrollEl(el);
      restoreScrollRef(el);
    },
    [restoreScrollRef],
  );

  const content = (
    <Accordion.Root
      multiple
      className="space-y-1"
      value={expandedStatuses}
      onValueChange={(value: string[]) => {
        if (isDraggingRef.current) return;
        for (const status of visibleStatuses) {
          const wasExpanded = expandedStatuses.includes(status);
          const isExpanded = value.includes(status);
          if (wasExpanded !== isExpanded) {
            toggleListCollapsed(status as IssueStatus);
          }
        }
      }}
    >
      {visibleStatuses.map((status) => {
        const isExpanded = expandedStatuses.includes(status);
        return (
          <StatusAccordionItem
            key={status}
            status={status}
            issueIds={columns[statusGroupId(status)] ?? EMPTY_IDS}
            issueMap={issueMapRef.current}
            childProgressMap={childProgressMap}
            projectMap={projectMap}
            page={statusPagination[status]}
            projectId={projectId}
            onCreateIssue={onCreateIssue}
            dragEnabled={dragEnabled}
            isExpanded={isExpanded}
            sortLabel={sortLabel}
            scrollParent={scrollEl}
          />
        );
      })}
    </Accordion.Root>
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
    >
      <div ref={attachScroller} data-tab-scroll-root="list" className="flex-1 min-h-0 overflow-y-auto p-2 pt-0">
        {content}
      </div>

      <DragOverlay dropAnimation={null}>
        {activeIssue ? (
          <div className="max-w-2xl rotate-1 cursor-grabbing opacity-90 shadow-lg shadow-black/10 rounded-md border border-border bg-card px-4 py-2">
            <span className="text-xs text-muted-foreground mr-2">{activeIssue.identifier}</span>
            <span className="text-sm">{activeIssue.title}</span>
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

function StatusAccordionItem({
  status,
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
}: {
  status: IssueStatus;
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

  const { setNodeRef: setDroppableRef, isOver } = useDroppable({
    id: statusGroupId(status),
    disabled: !dragEnabled,
  });

  const disableSorting = !!sortLabel;

  // The infinite-scroll sentinel rides Virtuoso's Footer so it sits at the true
  // end of the virtualized rows and still fires loadMore when scrolled to it.
  const listComponents = useMemo(
    () => {
      if (page.isError) {
        return {
          Footer: () => (
            <button
              type="button"
              className="w-full py-2 text-xs text-destructive hover:underline"
              onClick={page.retry}
            >
              {t(($) => $.table.load_more_failed_retry)}
            </button>
          ),
        };
      }
      if (page.hasMore) {
        return {
          Footer: () => (
            <InfiniteScrollSentinel
              onVisible={page.loadMore}
              loading={page.isLoading || page.isFetching}
            />
          ),
        };
      }
      return EMPTY_VIRTUOSO_COMPONENTS;
    },
    [page, t],
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
          defaultItemHeight={LIST_ROW_ESTIMATED_HEIGHT}
          increaseViewportBy={{ top: 400, bottom: 400 }}
          components={listComponents}
          itemContent={itemContent}
        />
      ) : (
        <VirtuosoSeed
          data={issues}
          itemContent={itemContent}
          computeItemKey={computeItemKey}
          estimatedItemHeight={LIST_ROW_ESTIMATED_HEIGHT}
        />
      )
    ) : null;

  return (
    <Accordion.Item value={status} ref={dragEnabled ? setDroppableRef : undefined}>
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
          <StatusHeading status={status} count={page.total} />
        </Accordion.Trigger>
        {onCreateIssue && (
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
                      status,
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
          <p className="py-6 text-center text-xs text-muted-foreground">
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
