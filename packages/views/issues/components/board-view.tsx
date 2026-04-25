"use client";

import { useState, useCallback, useMemo, useEffect, useRef } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  pointerWithin,
  closestCenter,
  type CollisionDetection,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from "@dnd-kit/core";
import { arrayMove } from "@dnd-kit/sortable";
import { Eye, MoreHorizontal } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useColumnConfigs } from "@multica/core/issues";
import { useCurrentWorkspace } from "@multica/core/paths";
import type { Issue, IssueStatus, WorkspaceColumnConfig } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useLoadMoreByStatus } from "@multica/core/issues/mutations";
import type { MyIssuesFilter } from "@multica/core/issues/queries";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { getStatusConfig } from "@multica/core/issues/config";
import { useViewStoreApi, useViewStore } from "@multica/core/issues/stores/view-store-context";
import type { SortField, SortDirection } from "@multica/core/issues/stores/view-store";
import { sortIssues } from "../utils/sort";
import { isAutoHidden, DEFAULT_AUTO_HIDE_DAYS } from "../utils/auto-hide";
import { StatusIcon } from "./status-icon";
import { BoardColumn } from "./board-column";
import { BoardCardContent } from "./board-card";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";
import type { ChildProgress } from "./list-row";

function makeKanbanCollision(columnIds: Set<string>): CollisionDetection {
  return (args) => {
    const pointer = pointerWithin(args);
    if (pointer.length > 0) {
      // Prefer card collisions over column collisions so that
      // dragging down within a column finds the target card
      // instead of the column droppable.
      const cards = pointer.filter((c) => !columnIds.has(c.id as string));
      if (cards.length > 0) return cards;
    }
    // Fallback: closestCenter finds the nearest card even when
    // the pointer is in a gap between cards (common when dragging down).
    return closestCenter(args);
  };
}

/** Build column ID arrays from TQ issue data, respecting current sort and auto-hide. */
function buildColumns(
  issues: Issue[],
  visibleStatuses: string[],
  sortBy: SortField,
  sortDirection: SortDirection,
  showHiddenPerStatus: Record<string, boolean> = {},
  autoHideDays = DEFAULT_AUTO_HIDE_DAYS,
): Record<string, string[]> {
  const cols = {} as Record<string, string[]>;
  for (const status of visibleStatuses) {
    const showHidden = showHiddenPerStatus[status] ?? false;
    const sorted = sortIssues(
      issues.filter((i) => i.status === status && (showHidden || !isAutoHidden(i, autoHideDays))),
      sortBy,
      sortDirection,
    );
    cols[status] = sorted.map((i) => i.id);
  }
  return cols;
}

/** Count auto-hidden issues per status. */
function countHiddenByStatus(
  issues: Issue[],
  visibleStatuses: string[],
  autoHideDays = DEFAULT_AUTO_HIDE_DAYS,
): Record<string, number> {
  const counts = {} as Record<string, number>;
  for (const status of visibleStatuses) {
    counts[status] = issues.filter((i) => i.status === status && isAutoHidden(i, autoHideDays)).length;
  }
  return counts;
}

/** Compute a float position for `activeId` based on its neighbors in `ids`. */
function computePosition(ids: string[], activeId: string, issueMap: Map<string, Issue>): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => issueMap.get(id)?.position ?? 0;
  if (ids.length === 1) return issueMap.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

/** Find which column (status) contains a given ID (issue or column droppable). */
function findColumn(
  columns: Record<string, string[]>,
  id: string,
  visibleStatuses: string[],
): string | null {
  if (visibleStatuses.includes(id)) return id;
  for (const [status, ids] of Object.entries(columns)) {
    if (ids.includes(id)) return status;
  }
  return null;
}

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();

export function BoardView({
  issues,
  visibleStatuses,
  hiddenStatuses,
  columnLabels,
  columnTerminals,
  activePipelineId,
  onMoveIssue,
  childProgressMap = EMPTY_PROGRESS_MAP,
  myIssuesScope,
  myIssuesFilter,
  myIssuesStatuses,
}: {
  issues: Issue[];
  visibleStatuses: string[];
  hiddenStatuses: string[];
  columnLabels?: Record<string, string>;
  columnTerminals?: Record<string, boolean>;
  activePipelineId?: string | null;
  onMoveIssue: (
    issueId: string,
    newStatus: string,
    newPosition?: number
  ) => void;
  childProgressMap?: Map<string, ChildProgress>;
  /** When set, per-status load-more targets the scoped cache instead of the workspace one. */
  myIssuesScope?: string;
  myIssuesFilter?: MyIssuesFilter;
  myIssuesStatuses?: readonly string[];
}) {
  const wsId = useWorkspaceId();
  const { data: columnConfigs = [] } = useColumnConfigs(wsId);
  const workspace = useCurrentWorkspace();
  const autoHideDays = (workspace?.settings?.auto_hide_days ?? DEFAULT_AUTO_HIDE_DAYS);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const showHiddenPerStatus = useViewStore((s) => s.showHiddenPerStatus);
  const viewStoreApi = useViewStoreApi();
  const columnIdSet = useMemo(() => new Set<string>(visibleStatuses), [visibleStatuses]);
  const kanbanCollision = useMemo(() => makeKanbanCollision(columnIdSet), [columnIdSet]);
  const myIssuesOpts = myIssuesScope
    ? {
        scope: myIssuesScope,
        filter: myIssuesFilter ?? {},
        ...(myIssuesStatuses ? { statuses: myIssuesStatuses } : {}),
      }
    : undefined;
  const columnConfigMap = useMemo(
    () => new Map(columnConfigs.map((config) => [config.status, config])),
    [columnConfigs],
  );

  // --- Drag state ---
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null);
  const isDraggingRef = useRef(false);

  // --- Auto-hide counts ---
  const hiddenCounts = useMemo(
    () => countHiddenByStatus(issues, visibleStatuses, autoHideDays),
    [issues, visibleStatuses, autoHideDays],
  );

  // --- Local columns state ---
  // Between drags: follows TQ via useEffect.
  // During drag: local-only, driven by onDragOver/onDragEnd.
  const [columns, setColumns] = useState<Record<string, string[]>>(() =>
    buildColumns(issues, visibleStatuses, sortBy, sortDirection, showHiddenPerStatus, autoHideDays),
  );
  const columnsRef = useRef(columns);
  columnsRef.current = columns;

  useEffect(() => {
    if (!isDraggingRef.current) {
      setColumns(buildColumns(issues, visibleStatuses, sortBy, sortDirection, showHiddenPerStatus, autoHideDays));
    }
  }, [issues, visibleStatuses, sortBy, sortDirection, showHiddenPerStatus, autoHideDays]);

  // After a cross-column move, lock for one animation frame so dnd-kit's
  // collision detection can stabilize before processing the next move.
  // Without this, collision oscillates: A→B→A→B… until React bails out.
  const recentlyMovedRef = useRef(false);
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      recentlyMovedRef.current = false;
    });
    return () => cancelAnimationFrame(id);
  }, [columns]);

  // --- Issue map ---
  // Frozen during drag so BoardColumn/DraggableBoardCard props stay
  // referentially stable even if a TQ refetch lands mid-drag.
  const issueMap = useMemo(() => {
    const map = new Map<string, Issue>();
    for (const issue of issues) map.set(issue.id, issue);
    return map;
  }, [issues]);

  const issueMapRef = useRef(issueMap);
  if (!isDraggingRef.current) {
    issueMapRef.current = issueMap;
  }

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
    [],
  );

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      const { active, over } = event;
      if (!over || recentlyMovedRef.current) return;

      const activeId = active.id as string;
      const overId = over.id as string;

      setColumns((prev) => {
        const activeCol = findColumn(prev, activeId, visibleStatuses);
        const overCol = findColumn(prev, overId, visibleStatuses);
        if (!activeCol || !overCol || activeCol === overCol) return prev;

        recentlyMovedRef.current = true;
        const oldIds = prev[activeCol]!.filter((id) => id !== activeId);
        const newIds = [...prev[overCol]!];
        const overIndex = newIds.indexOf(overId);
        const insertIndex = overIndex >= 0 ? overIndex : newIds.length;
        newIds.splice(insertIndex, 0, activeId);
        return { ...prev, [activeCol]: oldIds, [overCol]: newIds };
      });
    },
    [visibleStatuses],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      isDraggingRef.current = false;
      setActiveIssue(null);

      const resetColumns = () =>
        setColumns(buildColumns(issues, visibleStatuses, sortBy, sortDirection, showHiddenPerStatus, autoHideDays));

      if (!over) {
        resetColumns();
        return;
      }

      const activeId = active.id as string;
      const overId = over.id as string;

      const cols = columnsRef.current;
      const activeCol = findColumn(cols, activeId, visibleStatuses);
      const overCol = findColumn(cols, overId, visibleStatuses);
      if (!activeCol || !overCol) {
        resetColumns();
        return;
      }

      // Same-column reorder
      let finalColumns = cols;
      if (activeCol === overCol) {
        const ids = cols[activeCol]!;
        const oldIndex = ids.indexOf(activeId);
        const newIndex = ids.indexOf(overId);
        if (oldIndex !== -1 && newIndex !== -1 && oldIndex !== newIndex) {
          const reordered = arrayMove(ids, oldIndex, newIndex);
          finalColumns = { ...cols, [activeCol]: reordered };
          setColumns(finalColumns);
        }
      }

      const finalCol = findColumn(finalColumns, activeId, visibleStatuses);
      if (!finalCol) {
        resetColumns();
        return;
      }

      const map = issueMapRef.current;
      const finalIds = finalColumns[finalCol]!;
      const newPosition = computePosition(finalIds, activeId, map);
      const currentIssue = map.get(activeId);

      if (
        currentIssue &&
        currentIssue.status === finalCol &&
        currentIssue.position === newPosition
      ) {
        return;
      }

      onMoveIssue(activeId, finalCol, newPosition);
    },
    [issues, visibleStatuses, sortBy, sortDirection, showHiddenPerStatus, autoHideDays, onMoveIssue],
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={kanbanCollision}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
        {visibleStatuses.map((status) => (
          <PaginatedBoardColumn
            key={status}
            status={status}
            isTerminal={columnTerminals?.[status]}
            label={columnLabels?.[status]}
            pipelineId={activePipelineId}
            issueIds={columns[status] ?? []}
            issueMap={issueMapRef.current}
            columnConfig={columnConfigMap.get(status as import("@multica/core/types").IssueStatus)}
            childProgressMap={childProgressMap}
            myIssuesOpts={myIssuesOpts}
            hiddenCount={hiddenCounts[status] ?? 0}
            showHidden={showHiddenPerStatus[status] ?? false}
            onToggleShowHidden={() => viewStoreApi.getState().toggleShowHidden(status)}
          />
        ))}

        {hiddenStatuses.length > 0 && (
          <HiddenColumnsPanel
            hiddenStatuses={hiddenStatuses}
            myIssuesOpts={myIssuesOpts}
          />
        )}
      </div>

      <DragOverlay dropAnimation={null}>
        {activeIssue ? (
          <div className="w-[280px] rotate-2 scale-105 cursor-grabbing opacity-90 shadow-lg shadow-black/10">
            <BoardCardContent issue={activeIssue} childProgress={childProgressMap.get(activeIssue.id)} />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

function PaginatedBoardColumn({
  status,
  isTerminal,
  label,
  pipelineId,
  issueIds,
  issueMap,
  childProgressMap,
  columnConfig,
  myIssuesOpts,
  hiddenCount,
  showHidden,
  onToggleShowHidden,
}: {
  status: string;
  isTerminal?: boolean;
  label?: string;
  pipelineId?: string | null;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  columnConfig?: WorkspaceColumnConfig;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
  hiddenCount?: number;
  showHidden?: boolean;
  onToggleShowHidden?: () => void;
}) {
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByStatus(
    status,
    myIssuesOpts,
  );
  return (
    <BoardColumn
      status={status}
      isTerminal={isTerminal}
      label={label}
      pipelineId={pipelineId}
      issueIds={issueIds}
      issueMap={issueMap}
      childProgressMap={childProgressMap}
      columnConfig={columnConfig}
      totalCount={total}
      footer={
        hasMore ? (
          <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />
        ) : undefined
      }
      hiddenCount={hiddenCount}
      showHidden={showHidden}
      onToggleShowHidden={onToggleShowHidden}
    />
  );
}

function HiddenColumnsPanel({
  hiddenStatuses,
  myIssuesOpts,
}: {
  hiddenStatuses: string[];
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  return (
    <div className="flex w-[240px] shrink-0 flex-col">
      <div className="mb-2 flex items-center gap-2 px-1">
        <span className="text-sm font-medium text-muted-foreground">
          Hidden columns
        </span>
      </div>
      <div className="flex-1 space-y-0.5">
        {hiddenStatuses.map((status) => (
          <HiddenColumnRow
            key={status}
            status={status}
            myIssuesOpts={myIssuesOpts}
          />
        ))}
      </div>
    </div>
  );
}

function HiddenColumnRow({
  status,
  myIssuesOpts,
}: {
  status: string;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  const cfg = getStatusConfig(status);
  const viewStoreApi = useViewStoreApi();
  const { total } = useLoadMoreByStatus(status, myIssuesOpts);
  return (
    <div className="flex items-center justify-between rounded-lg px-2.5 py-2 hover:bg-muted/50">
      <div className="flex items-center gap-2">
        <StatusIcon status={status} className="h-3.5 w-3.5" />
        <span className="text-sm">{cfg.label}</span>
      </div>
      <div className="flex items-center gap-1.5">
        <span className="text-xs text-muted-foreground">{total}</span>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="rounded-full text-muted-foreground"
              >
                <MoreHorizontal className="size-3.5" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onClick={() => viewStoreApi.getState().showStatus(status as IssueStatus)}
            >
              <Eye className="size-3.5" />
              Show column
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
