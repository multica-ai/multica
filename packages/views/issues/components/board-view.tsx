"use client";

import { useState, useCallback, useMemo, useEffect, useRef, memo } from "react";
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
import type { QueryKey } from "@tanstack/react-query";
import { arrayMove } from "@dnd-kit/sortable";
import type { Issue, IssueAssigneeGroup, IssueAssigneeType, IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { useLoadMoreByAssigneeGroup, useLoadMoreByStatus } from "@multica/core/issues/mutations";
import type { AssigneeGroupedIssuesFilter, MyIssuesFilter } from "@multica/core/issues/queries";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { useViewStoreApi, useViewStore } from "@multica/core/issues/stores/view-store-context";
import type { IssueGrouping } from "@multica/core/issues/stores/view-store";
import { useActorName } from "@multica/core/workspace/hooks";
import { StatusIcon } from "./status-icon";
import { BoardColumn, BOARD_CARD_WIDTH, type BoardColumnGroup } from "./board-column";
import { BoardCardContent } from "./board-card";
import { HiddenColumnsPanel, HiddenColumnRow } from "./hidden-columns-panel";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";
import type { ChildProgress } from "./list-row";
import { useT } from "../../i18n";

type BoardMoveUpdates = Pick<
  UpdateIssueRequest,
  "status" | "assignee_type" | "assignee_id" | "position"
>;

const UNASSIGNED_GROUP_ID = "assignee:unassigned";

function makeKanbanCollision(columnIds: Set<string>): CollisionDetection {
  return (args) => {
    const pointer = pointerWithin(args);
    if (pointer.length > 0) {
      const cards = pointer.filter((c) => !columnIds.has(c.id as string));
      if (cards.length > 0) return cards;
      return pointer;
    }
    return closestCenter(args);
  };
}

function statusGroupId(status: IssueStatus): string {
  return `status:${status}`;
}

function assigneeGroupId(
  type: IssueAssigneeType | null,
  id: string | null,
): string {
  return type && id ? `assignee:${type}:${id}` : UNASSIGNED_GROUP_ID;
}

function getIssueGroupId(issue: Issue, grouping: IssueGrouping): string {
  if (grouping === "status") return statusGroupId(issue.status);
  return assigneeGroupId(issue.assignee_type, issue.assignee_id);
}

function isStatusGroup(
  group: BoardColumnGroup,
): group is BoardColumnGroup & { status: IssueStatus } {
  return group.status !== undefined;
}

function buildGroups(
  issues: Issue[],
  visibleStatuses: IssueStatus[],
  grouping: IssueGrouping,
  getActorName: (type: string, id: string) => string,
  noAssigneeLabel: string,
): BoardColumnGroup[] {
  if (grouping === "status") {
    return visibleStatuses.map((status) => ({
      id: statusGroupId(status),
      title: status,
      status,
      createData: { status },
    }));
  }

  const groups = new Map<string, BoardColumnGroup>();
  for (const issue of issues) {
    const id = assigneeGroupId(issue.assignee_type, issue.assignee_id);
    if (groups.has(id)) continue;

    if (issue.assignee_type && issue.assignee_id) {
      groups.set(id, {
        id,
        title: getActorName(issue.assignee_type, issue.assignee_id),
        assigneeType: issue.assignee_type,
        assigneeId: issue.assignee_id,
        createData: {
          assignee_type: issue.assignee_type,
          assignee_id: issue.assignee_id,
        },
      });
      continue;
    }

    groups.set(id, {
      id,
      title: noAssigneeLabel,
      assigneeType: null,
      assigneeId: null,
      createData: {
        assignee_type: null,
        assignee_id: null,
      },
    });
  }

  const order: Record<string, number> = {
    member: 0,
    agent: 1,
    squad: 2,
    none: 3,
  };

  return [...groups.values()].sort((a, b) => {
    const aOrder = order[a.assigneeType ?? "none"] ?? 99;
    const bOrder = order[b.assigneeType ?? "none"] ?? 99;
    if (aOrder !== bOrder) return aOrder - bOrder;
    return a.title.localeCompare(b.title);
  });
}

/** Build column ID arrays from TQ issue data (server-sorted). */
function buildColumns(
  issues: Issue[],
  groups: BoardColumnGroup[],
  grouping: IssueGrouping,
): Record<string, string[]> {
  const cols: Record<string, string[]> = {};
  for (const group of groups) cols[group.id] = [];
  for (const issue of issues) {
    const gid = getIssueGroupId(issue, grouping);
    if (cols[gid]) cols[gid].push(issue.id);
  }
  return cols;
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

/** Find which column contains a given ID (issue or column droppable). */
function findColumn(
  columns: Record<string, string[]>,
  id: string,
  columnIds: Set<string>,
): string | null {
  if (columnIds.has(id)) return id;
  for (const [columnId, ids] of Object.entries(columns)) {
    if (ids.includes(id)) return columnId;
  }
  return null;
}

function issueMatchesGroup(issue: Issue, group: BoardColumnGroup): boolean {
  if (group.status) return issue.status === group.status;
  return (
    (issue.assignee_type ?? null) === (group.assigneeType ?? null) &&
    (issue.assignee_id ?? null) === (group.assigneeId ?? null)
  );
}

function getMoveUpdates(
  group: BoardColumnGroup,
  position: number,
): BoardMoveUpdates {
  if (group.status) return { status: group.status, position };
  return {
    assignee_type: group.assigneeType ?? null,
    assignee_id: group.assigneeId ?? null,
    position,
  };
}

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();
const EMPTY_IDS: string[] = [];

export function BoardView({
  issues,
  assigneeGroups,
  assigneeGroupQueryKey,
  assigneeGroupFilter,
  visibleStatuses,
  hiddenStatuses,
  onMoveIssue,
  childProgressMap = EMPTY_PROGRESS_MAP,
  myIssuesScope,
  myIssuesFilter,
  projectId,
}: {
  issues: Issue[];
  assigneeGroups?: IssueAssigneeGroup[];
  assigneeGroupQueryKey?: QueryKey;
  assigneeGroupFilter?: AssigneeGroupedIssuesFilter;
  visibleStatuses: IssueStatus[];
  hiddenStatuses: IssueStatus[];
  onMoveIssue: (issueId: string, updates: BoardMoveUpdates, onSettled?: () => void) => void;
  childProgressMap?: Map<string, ChildProgress>;
  /** When set, per-status load-more targets the scoped cache instead of the workspace one. */
  myIssuesScope?: string;
  myIssuesFilter?: MyIssuesFilter;
  /** When set, the per-column "+" pre-fills the project on the create form. */
  projectId?: string;
}) {
  const { t } = useT("issues");
  const grouping = useViewStore((s) => s.grouping);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortFieldKey = sortBy === "created_at" ? "created" : sortBy;
  const sortLabel = sortBy !== "position"
    ? t(($) => $.board.ordered_by, { field: t(($) => $.display[`sort_${sortFieldKey}` as keyof typeof $.display]) })
    : null;
  const { getActorName } = useActorName();
  const myIssuesOpts = myIssuesScope
    ? { scope: myIssuesScope, filter: myIssuesFilter ?? {} }
    : undefined;
  const groupedIssues = useMemo(
    () =>
      grouping === "assignee" && assigneeGroups
        ? assigneeGroups.flatMap((group) => group.issues)
        : issues,
    [assigneeGroups, grouping, issues],
  );
  const hydratedAssigneeGroups = useMemo(() => {
    if (grouping !== "assignee" || !assigneeGroups) return undefined;
    const order: Record<string, number> = {
      member: 0,
      agent: 1,
      squad: 2,
      none: 3,
    };
    return assigneeGroups
      .map((group) => ({
        id: group.id,
        title:
          group.assignee_type && group.assignee_id
            ? getActorName(group.assignee_type, group.assignee_id)
            : t(($) => $.filters.no_assignee),
        assigneeType: group.assignee_type,
        assigneeId: group.assignee_id,
        totalCount: group.total,
        createData: {
          assignee_type: group.assignee_type,
          assignee_id: group.assignee_id,
        },
      }))
      .sort((a, b) => {
        const aOrder = order[a.assigneeType ?? "none"] ?? 99;
        const bOrder = order[b.assigneeType ?? "none"] ?? 99;
        if (aOrder !== bOrder) return aOrder - bOrder;
        return a.title.localeCompare(b.title);
      });
  }, [assigneeGroups, getActorName, grouping, t]);
  const groups = useMemo(
    () =>
      hydratedAssigneeGroups ??
      buildGroups(
        issues,
        visibleStatuses,
        grouping,
        getActorName,
        t(($) => $.filters.no_assignee),
      ),
    [hydratedAssigneeGroups, issues, visibleStatuses, grouping, getActorName, t],
  );
  const groupIds = useMemo(
    () => new Set(groups.map((group) => group.id)),
    [groups],
  );
  const groupMap = useMemo(
    () => new Map(groups.map((group) => [group.id, group])),
    [groups],
  );
  const collisionDetection = useMemo(
    () => makeKanbanCollision(groupIds),
    [groupIds],
  );

  // --- Drag state ---
  const [activeIssue, setActiveIssue] = useState<Issue | null>(null);
  const isDraggingRef = useRef(false);
  const isSettlingRef = useRef(false);
  const [settleVersion, setSettleVersion] = useState(0);

  // --- Local columns state ---
  // Between drags: follows TQ via useEffect.
  // During drag: local-only, driven by onDragOver/onDragEnd.
  const [columns, setColumns] = useState<Record<string, string[]>>(() =>
    buildColumns(groupedIssues, groups, grouping),
  );
  const columnsRef = useRef(columns);
  columnsRef.current = columns;

  useEffect(() => {
    if (!isDraggingRef.current && !isSettlingRef.current) {
      setColumns(buildColumns(groupedIssues, groups, grouping));
    }
  }, [groupedIssues, groups, grouping, settleVersion]);

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
    for (const issue of groupedIssues) map.set(issue.id, issue);
    return map;
  }, [groupedIssues]);

  const issueMapRef = useRef(issueMap);
  if (!isDraggingRef.current && !isSettlingRef.current) {
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
    [groupIds, sortBy],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      isDraggingRef.current = false;
      setActiveIssue(null);

      const resetColumns = () =>
        setColumns(buildColumns(groupedIssues, groups, grouping));

      if (!over) {
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

      // Same-column reorder (manual sort only)
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
        // Cross-column: only update group (status/assignee), keep original position.
        const currentIssue = map.get(activeId);
        if (!currentIssue || issueMatchesGroup(currentIssue, finalGroup)) {
          resetColumns();
          return;
        }
        isSettlingRef.current = true;
        onMoveIssue(activeId, getMoveUpdates(finalGroup, currentIssue.position), () => {
          isSettlingRef.current = false;
          setSettleVersion((v) => v + 1);
        });
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

      isSettlingRef.current = true;
      onMoveIssue(activeId, getMoveUpdates(finalGroup, newPosition), () => {
        isSettlingRef.current = false;
      });
    },
    [groupedIssues, groups, grouping, onMoveIssue, groupIds, groupMap, sortBy],
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
        {groups.length === 0 ? (
          <div className="flex min-w-full flex-1 items-center justify-center text-sm text-muted-foreground">
            {t(($) => $.board.empty_grouping)}
          </div>
        ) : (
          groups.map((group) =>
            isStatusGroup(group) ? (
              <PaginatedBoardColumn
                key={group.id}
                group={group}
                issueIds={columns[group.id] ?? EMPTY_IDS}
                issueMap={issueMapRef.current}
                childProgressMap={childProgressMap}
                myIssuesOpts={myIssuesOpts}
                projectId={projectId}
                sortLabel={sortLabel}
              />
            ) : (
              assigneeGroupQueryKey && assigneeGroupFilter ? (
                <PaginatedAssigneeBoardColumn
                  key={group.id}
                  group={group}
                  issueIds={columns[group.id] ?? EMPTY_IDS}
                  issueMap={issueMapRef.current}
                  childProgressMap={childProgressMap}
                  queryKey={assigneeGroupQueryKey}
                  filter={assigneeGroupFilter}
                  projectId={projectId}
                  sortLabel={sortLabel}
                />
              ) : (
                <BoardColumn
                  key={group.id}
                  group={group}
                  issueIds={columns[group.id] ?? EMPTY_IDS}
                  issueMap={issueMapRef.current}
                  childProgressMap={childProgressMap}
                  projectId={projectId}
                  totalCount={group.totalCount}
                  sortLabel={sortLabel}
                />
              )
            ),
          )
        )}

        {grouping === "status" && hiddenStatuses.length > 0 && (
          <BoardHiddenColumnsPanel
            hiddenStatuses={hiddenStatuses}
            myIssuesOpts={myIssuesOpts}
          />
        )}
      </div>

      <DragOverlay dropAnimation={null}>
        {activeIssue ? (
          <div style={{ width: BOARD_CARD_WIDTH }} className="rotate-1 cursor-grabbing opacity-90 shadow-lg shadow-black/10">
            <BoardCardContent issue={activeIssue} childProgress={childProgressMap.get(activeIssue.id)} />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

const PaginatedAssigneeBoardColumn = memo(function PaginatedAssigneeBoardColumn({
  group,
  issueIds,
  issueMap,
  childProgressMap,
  queryKey,
  filter,
  projectId,
  sortLabel,
}: {
  group: BoardColumnGroup;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  queryKey: QueryKey;
  filter: AssigneeGroupedIssuesFilter;
  projectId?: string;
  sortLabel?: string | null;
}) {
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByAssigneeGroup(
    {
      id: group.id,
      assignee_type: group.assigneeType ?? null,
      assignee_id: group.assigneeId ?? null,
    },
    queryKey,
    filter,
  );
  return (
    <BoardColumn
      group={group}
      issueIds={issueIds}
      issueMap={issueMap}
      childProgressMap={childProgressMap}
      totalCount={total}
      projectId={projectId}
      sortLabel={sortLabel}
      footer={
        hasMore ? (
          <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />
        ) : undefined
      }
    />
  );
});

const PaginatedBoardColumn = memo(function PaginatedBoardColumn({
  group,
  issueIds,
  issueMap,
  childProgressMap,
  myIssuesOpts,
  projectId,
  sortLabel,
}: {
  group: BoardColumnGroup & { status: IssueStatus };
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
  projectId?: string;
  sortLabel?: string | null;
}) {
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByStatus(
    group.status,
    myIssuesOpts,
  );
  return (
    <BoardColumn
      group={group}
      issueIds={issueIds}
      issueMap={issueMap}
      childProgressMap={childProgressMap}
      totalCount={total}
      projectId={projectId}
      sortLabel={sortLabel}
      footer={
        hasMore ? (
          <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />
        ) : undefined
      }
    />
  );
});

/**
 * Board-view-specific row that pulls the server-aggregated total from
 * `useLoadMoreByStatus` and hands it to the shared {@link HiddenColumnRow}.
 * Lives here (not in `hidden-columns-panel.tsx`) so the shared panel stays
 * free of `useLoadMoreByStatus` / `myIssuesOpts` coupling — the swimlane
 * uses an in-memory total instead.
 */
function BoardHiddenColumnRow({
  status,
  myIssuesOpts,
}: {
  status: IssueStatus;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  const { total } = useLoadMoreByStatus(status, myIssuesOpts);
  return <HiddenColumnRow status={status} total={total} />;
}

function BoardHiddenColumnsPanel({
  hiddenStatuses,
  myIssuesOpts,
}: {
  hiddenStatuses: IssueStatus[];
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  return (
    <HiddenColumnsPanel
      hiddenStatuses={hiddenStatuses}
      renderRow={(status) => (
        <BoardHiddenColumnRow
          key={status}
          status={status}
          myIssuesOpts={myIssuesOpts}
        />
      )}
    />
  );
}
