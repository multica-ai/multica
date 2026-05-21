"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCenter,
  pointerWithin,
  useDroppable,
  useSensor,
  useSensors,
  type CollisionDetection,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { SortableContext, arrayMove, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { GitBranch, Link2Off } from "lucide-react";
import type { Issue, IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import type { SortDirection, SortField } from "@multica/core/issues/stores/view-store";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { sortIssues } from "../utils/sort";
import {
  UNPARENTED_SWIMLANE_ID,
  buildIssueSwimlanes,
  type IssueSwimlane,
} from "../utils/issue-hierarchy";
import { BoardCardContent, DraggableBoardCard } from "./board-card";
import { PriorityIcon } from "./priority-icon";
import { ProgressRing } from "./progress-ring";
import { StatusHeading } from "./status-heading";
import type { ChildProgress } from "./list-row";
import { useT } from "../../i18n";

type SwimlaneMoveUpdates = Pick<
  UpdateIssueRequest,
  "status" | "parent_issue_id" | "position"
>;

const CELL_PREFIX = "swimlane-cell:";
const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();

function swimlaneCellId(laneId: string, status: IssueStatus) {
  return `${CELL_PREFIX}${laneId}:${status}`;
}

function parseSwimlaneCellId(id: string) {
  if (!id.startsWith(CELL_PREFIX)) return null;
  const body = id.slice(CELL_PREFIX.length);
  const statusSeparator = body.lastIndexOf(":");
  if (statusSeparator === -1) return null;
  return {
    laneId: body.slice(0, statusSeparator),
    status: body.slice(statusSeparator + 1) as IssueStatus,
  };
}

function parentIdForLane(laneId: string): string | null {
  if (laneId === UNPARENTED_SWIMLANE_ID) return null;
  if (laneId.startsWith("parent:")) return laneId.slice("parent:".length);
  return null;
}

function makeSwimlaneCollision(cellIds: Set<string>): CollisionDetection {
  return (args) => {
    const pointer = pointerWithin(args);
    if (pointer.length > 0) {
      const cards = pointer.filter((collision) => !cellIds.has(collision.id as string));
      if (cards.length > 0) return cards;
    }
    return closestCenter(args);
  };
}

function issueMap(issues: Issue[]) {
  return new Map(issues.map((issue) => [issue.id, issue]));
}

function buildColumns(
  lanes: IssueSwimlane[],
  visibleStatuses: IssueStatus[],
  map: Map<string, Issue>,
  sortBy: SortField,
  sortDirection: SortDirection,
): Record<string, string[]> {
  const cols: Record<string, string[]> = {};
  for (const lane of lanes) {
    for (const status of visibleStatuses) {
      const ids = lane.issueIdsByStatus[status] ?? [];
      cols[swimlaneCellId(lane.id, status)] = sortIssues(
        ids.flatMap((id) => {
          const issue = map.get(id);
          return issue ? [issue] : [];
        }),
        sortBy,
        sortDirection,
      ).map((issue) => issue.id);
    }
  }
  return cols;
}

function findCell(
  columns: Record<string, string[]>,
  id: string,
  cellIds: Set<string>,
): string | null {
  if (cellIds.has(id)) return id;
  for (const [cellId, ids] of Object.entries(columns)) {
    if (ids.includes(id)) return cellId;
  }
  return null;
}

function computePosition(ids: string[], activeId: string, map: Map<string, Issue>): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => map.get(id)?.position ?? 0;
  if (ids.length === 1) return map.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

function issueMatchesCell(issue: Issue, cellId: string) {
  const parsed = parseSwimlaneCellId(cellId);
  if (!parsed) return false;
  return (
    issue.status === parsed.status &&
    (issue.parent_issue_id ?? null) === parentIdForLane(parsed.laneId)
  );
}

function getMoveUpdates(cellId: string, position: number): SwimlaneMoveUpdates | null {
  const parsed = parseSwimlaneCellId(cellId);
  if (!parsed) return null;
  return {
    status: parsed.status,
    parent_issue_id: parentIdForLane(parsed.laneId),
    position,
  };
}

export function SwimlaneBoardView({
  issues,
  allIssues,
  visibleStatuses,
  onMoveIssue,
  childProgressMap = EMPTY_PROGRESS_MAP,
}: {
  issues: Issue[];
  allIssues: Issue[];
  visibleStatuses: IssueStatus[];
  onMoveIssue: (issueId: string, updates: SwimlaneMoveUpdates) => void;
  childProgressMap?: Map<string, ChildProgress>;
}) {
  const { t } = useT("issues");
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const lanes = useMemo(
    () => buildIssueSwimlanes(issues, allIssues, visibleStatuses),
    [allIssues, issues, visibleStatuses],
  );
  const map = useMemo(() => issueMap(issues), [issues]);
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null);
  const isDraggingRef = useRef(false);
  const mapRef = useRef(map);
  if (!isDraggingRef.current) mapRef.current = map;

  const [columns, setColumns] = useState<Record<string, string[]>>(() =>
    buildColumns(lanes, visibleStatuses, map, sortBy, sortDirection),
  );
  const columnsRef = useRef(columns);
  columnsRef.current = columns;

  useEffect(() => {
    if (!isDraggingRef.current) {
      setColumns(buildColumns(lanes, visibleStatuses, map, sortBy, sortDirection));
    }
  }, [lanes, map, sortBy, sortDirection, visibleStatuses]);

  const cellIds = useMemo(
    () =>
      new Set(
        lanes.flatMap((lane) =>
          visibleStatuses.map((status) => swimlaneCellId(lane.id, status)),
        ),
      ),
    [lanes, visibleStatuses],
  );
  const collisionDetection = useMemo(
    () => makeSwimlaneCollision(cellIds),
    [cellIds],
  );
  const statusCounts = useMemo(() => {
    const counts = new Map<IssueStatus, number>();
    for (const status of visibleStatuses) counts.set(status, 0);
    for (const ids of Object.values(columns)) {
      for (const id of ids) {
        const issue = mapRef.current.get(id);
        if (!issue) continue;
        counts.set(issue.status, (counts.get(issue.status) ?? 0) + 1);
      }
    }
    return counts;
  }, [columns, visibleStatuses]);

  const recentlyMovedRef = useRef(false);
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      recentlyMovedRef.current = false;
    });
    return () => cancelAnimationFrame(id);
  }, [columns]);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    }),
  );

  const resetColumns = useCallback(() => {
    setColumns(buildColumns(lanes, visibleStatuses, map, sortBy, sortDirection));
  }, [lanes, map, sortBy, sortDirection, visibleStatuses]);

  const handleDragStart = useCallback((event: DragStartEvent) => {
    isDraggingRef.current = true;
    setActiveIssue(mapRef.current.get(event.active.id as string) ?? null);
  }, []);

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      const { active, over } = event;
      if (!over || recentlyMovedRef.current) return;

      const activeId = active.id as string;
      const overId = over.id as string;
      setColumns((prev) => {
        const activeCell = findCell(prev, activeId, cellIds);
        const overCell = findCell(prev, overId, cellIds);
        if (!activeCell || !overCell || activeCell === overCell) return prev;

        recentlyMovedRef.current = true;
        const oldIds = prev[activeCell]!.filter((id) => id !== activeId);
        const newIds = [...prev[overCell]!];
        const overIndex = newIds.indexOf(overId);
        const insertIndex = overIndex >= 0 ? overIndex : newIds.length;
        newIds.splice(insertIndex, 0, activeId);
        return { ...prev, [activeCell]: oldIds, [overCell]: newIds };
      });
    },
    [cellIds],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      isDraggingRef.current = false;
      setActiveIssue(null);

      if (!over) {
        resetColumns();
        return;
      }

      const activeId = active.id as string;
      const overId = over.id as string;
      const cols = columnsRef.current;
      const activeCell = findCell(cols, activeId, cellIds);
      const overCell = findCell(cols, overId, cellIds);
      if (!activeCell || !overCell) {
        resetColumns();
        return;
      }

      let finalColumns = cols;
      if (activeCell === overCell) {
        const ids = cols[activeCell]!;
        const oldIndex = ids.indexOf(activeId);
        const newIndex = ids.indexOf(overId);
        if (oldIndex !== -1 && newIndex !== -1 && oldIndex !== newIndex) {
          const reordered = arrayMove(ids, oldIndex, newIndex);
          finalColumns = { ...cols, [activeCell]: reordered };
          setColumns(finalColumns);
        }
      }

      const finalCell = findCell(finalColumns, activeId, cellIds);
      if (!finalCell) {
        resetColumns();
        return;
      }

      const finalIds = finalColumns[finalCell]!;
      const newPosition = computePosition(finalIds, activeId, mapRef.current);
      const currentIssue = mapRef.current.get(activeId);

      if (
        currentIssue &&
        issueMatchesCell(currentIssue, finalCell) &&
        currentIssue.position === newPosition
      ) {
        return;
      }

      const updates = getMoveUpdates(finalCell, newPosition);
      if (!updates) {
        resetColumns();
        return;
      }
      onMoveIssue(activeId, updates);
    },
    [cellIds, onMoveIssue, resetColumns],
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <div className="flex-1 min-h-0 overflow-auto p-4">
        {lanes.length === 0 ? (
          <div className="flex min-h-full min-w-full items-center justify-center text-sm text-muted-foreground">
            {t(($) => $.board.empty_grouping)}
          </div>
        ) : (
          <div
            className="grid min-w-max gap-2"
            style={{
              gridTemplateColumns: `256px repeat(${visibleStatuses.length}, 280px)`,
            }}
          >
            <div className="sticky top-0 z-20 bg-background/95 backdrop-blur" />
            {visibleStatuses.map((status) => (
              <div
                key={status}
                className="sticky top-0 z-20 rounded-lg border bg-background/95 px-3 py-2 backdrop-blur"
              >
                <StatusHeading status={status} count={statusCounts.get(status) ?? 0} />
              </div>
            ))}
            {lanes.map((lane) => (
              <SwimlaneRow
                key={lane.id}
                lane={lane}
                visibleStatuses={visibleStatuses}
                columns={columns}
                issueMap={mapRef.current}
                childProgressMap={childProgressMap}
              />
            ))}
          </div>
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

function SwimlaneRow({
  lane,
  visibleStatuses,
  columns,
  issueMap,
  childProgressMap,
}: {
  lane: IssueSwimlane;
  visibleStatuses: IssueStatus[];
  columns: Record<string, string[]>;
  issueMap: Map<string, Issue>;
  childProgressMap: Map<string, ChildProgress>;
}) {
  return (
    <>
      <SwimlaneHeader lane={lane} childProgress={lane.parentIssue ? childProgressMap.get(lane.parentIssue.id) : undefined} />
      {visibleStatuses.map((status) => {
        const cellId = swimlaneCellId(lane.id, status);
        return (
          <SwimlaneCell
            key={cellId}
            id={cellId}
            issueIds={columns[cellId] ?? []}
            issueMap={issueMap}
            childProgressMap={childProgressMap}
          />
        );
      })}
    </>
  );
}

function SwimlaneHeader({
  lane,
  childProgress,
}: {
  lane: IssueSwimlane;
  childProgress?: ChildProgress;
}) {
  const { t } = useT("issues");
  const p = useWorkspacePaths();

  if (lane.parentIssue) {
    const parent = lane.parentIssue;
    const content = (
      <>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <GitBranch className="size-3.5" />
          <span>{parent.identifier}</span>
          <PriorityIcon priority={parent.priority} />
        </div>
        <div className="mt-1 line-clamp-3 text-sm font-medium leading-snug">
          {parent.title}
        </div>
        {childProgress && (
          <div className="mt-2 inline-flex items-center gap-1 rounded-full bg-muted/70 px-1.5 py-0.5">
            <ProgressRing done={childProgress.done} total={childProgress.total} size={14} />
            <span className="text-[11px] text-muted-foreground tabular-nums font-medium">
              {childProgress.done}/{childProgress.total}
            </span>
          </div>
        )}
      </>
    );

    return (
      <AppLink
        href={p.issueDetail(parent.id)}
        className="sticky left-0 z-10 min-h-[132px] rounded-lg border bg-card p-3 shadow-sm transition-colors hover:bg-accent/60"
      >
        {content}
      </AppLink>
    );
  }

  return (
    <div className="sticky left-0 z-10 flex min-h-[132px] flex-col justify-center rounded-lg border bg-card p-3 shadow-sm">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Link2Off className="size-3.5" />
        <span>
          {lane.isUnparented
            ? t(($) => $.board.unparented_lane)
            : t(($) => $.board.missing_parent_lane)}
        </span>
      </div>
      {lane.missingParentId && (
        <div className="mt-1 truncate text-xs text-muted-foreground/80">
          {lane.missingParentId}
        </div>
      )}
    </div>
  );
}

function SwimlaneCell({
  id,
  issueIds,
  issueMap,
  childProgressMap,
}: {
  id: string;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap: Map<string, ChildProgress>;
}) {
  const { setNodeRef, isOver } = useDroppable({ id });
  const resolvedIssues = useMemo(
    () =>
      issueIds.flatMap((issueId) => {
        const issue = issueMap.get(issueId);
        return issue ? [issue] : [];
      }),
    [issueIds, issueMap],
  );

  return (
    <div
      ref={setNodeRef}
      className={cn(
        "min-h-[132px] rounded-lg border border-dashed bg-muted/25 p-1.5 transition-colors",
        isOver && "border-primary/50 bg-accent/60",
      )}
    >
      <SortableContext items={issueIds} strategy={verticalListSortingStrategy}>
        <div className="space-y-2">
          {resolvedIssues.map((issue) => (
            <DraggableBoardCard
              key={issue.id}
              issue={issue}
              childProgress={childProgressMap.get(issue.id)}
            />
          ))}
        </div>
      </SortableContext>
    </div>
  );
}
