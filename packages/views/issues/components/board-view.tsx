"use client";

import { useState, useCallback, useMemo, useEffect, useRef, memo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from "@dnd-kit/core";
import type { QueryKey } from "@tanstack/react-query";
import { arrayMove } from "@dnd-kit/sortable";
import { toast } from "sonner";
import type {
  Issue,
  IssueAssigneeGroup,
  IssueAssigneeType,
  IssueStatus,
  Project,
  IssueProperty,
} from "@multica/core/types";
import { useLoadMoreByAssigneeGroup, useLoadMoreByStatus } from "@multica/core/issues/mutations";
import type { AssigneeGroupedIssuesFilter, IssueSortParam, MyIssuesFilter } from "@multica/core/issues/queries";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import { propertyListOptions, useSetIssueProperty, useUnsetIssueProperty } from "@multica/core/properties";
import { useWorkspaceId } from "@multica/core/hooks";
import type { IssueGrouping } from "@multica/core/issues/stores/view-store";
import { useActorName } from "@multica/core/workspace/hooks";
import { BoardColumn, BOARD_CARD_WIDTH, type BoardColumnGroup } from "./board-column";
import { BoardCardContent } from "./board-card";
import { HiddenColumnsPanel, HiddenColumnRow } from "./hidden-columns-panel";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";
import type { ChildProgress } from "./list-row";
import type { IssueCreateDefaults } from "../surface/types";
import type {
  IssueStatusPageState,
  IssueStatusPagination,
} from "../surface/use-issue-status-branches";
import type {
  IssueGroupBranches,
  IssueGroupPageState,
} from "../surface/use-issue-group-branches";
import { useDragSettle } from "./use-drag-settle";
import { useT } from "../../i18n";
import {
  type DragMoveUpdates,
  makeKanbanCollision,
  statusGroupId,
  assigneeGroupId,
  buildColumns,
  computePosition,
  findColumn,
  getMoveAnchors,
  insertIdByPosition,
  issueMatchesGroup,
  getMoveUpdates,
  propertyGroupId,
} from "../utils/drag-utils";

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
  groupingProperty: IssueProperty | null,
  noValueLabel: string,
): BoardColumnGroup[] {
  if (grouping === "status") {
    return visibleStatuses.map((status) => ({
      id: statusGroupId(status),
      title: status,
      status,
      createData: { status },
    }));
  }

  // Select-property board: one column per option (definition order) plus a
  // trailing "No value" column. Empty columns stay visible — they are drop
  // targets for assigning the value.
  if (groupingProperty) {
    const columns: BoardColumnGroup[] = (groupingProperty.config.options ?? []).map(
      (option) => ({
        id: propertyGroupId(groupingProperty.id, option.id),
        title: option.name,
        propertyId: groupingProperty.id,
        propertyOptionId: option.id,
        propertyOptionColor: option.color,
      }),
    );
    columns.push({
      id: propertyGroupId(groupingProperty.id, null),
      title: noValueLabel,
      propertyId: groupingProperty.id,
      propertyOptionId: null,
    });
    return columns;
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

  return Array.from(groups.values()).toSorted((a, b) => {
    const aOrder = order[a.assigneeType ?? "none"] ?? 99;
    const bOrder = order[b.assigneeType ?? "none"] ?? 99;
    if (aOrder !== bOrder) return aOrder - bOrder;
    return a.title.localeCompare(b.title);
  });
}

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();
const EMPTY_IDS: string[] = [];

function BoardViewImpl({
  issues,
  assigneeGroups,
  assigneeGroupQueryKey,
  assigneeGroupFilter,
  visibleStatuses,
  hiddenStatuses,
  onMoveIssue,
  childProgressMap = EMPTY_PROGRESS_MAP,
  projectMap,
  myIssuesScope,
  myIssuesFilter,
  sort,
  projectId,
  onCreateIssue,
  statusPagination,
  groupBranches,
}: {
  issues: Issue[];
  assigneeGroups?: IssueAssigneeGroup[];
  assigneeGroupQueryKey?: QueryKey;
  assigneeGroupFilter?: AssigneeGroupedIssuesFilter;
  visibleStatuses: IssueStatus[];
  hiddenStatuses: IssueStatus[];
  onMoveIssue: (issueId: string, updates: DragMoveUpdates, onSettled?: () => void) => void;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  /** When set, per-status load-more targets the scoped cache instead of the workspace one. */
  myIssuesScope?: string;
  myIssuesFilter?: MyIssuesFilter;
  /** Must match the sort the page queried with — embedded in the cache key. */
  sort?: IssueSortParam;
  /** When set, the per-column "+" pre-fills the project on the create form. */
  projectId?: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
  statusPagination?: IssueStatusPagination;
  groupBranches?: IssueGroupBranches;
}) {
  const { t } = useT("issues");
  const storeGrouping = useViewStore((s) => s.grouping);
  const sortBy = useViewStore((s) => s.sortBy);
  const boardWsId = useWorkspaceId();
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(boardWsId));
  const groupingPropertyId = propertyIdFromViewKey(storeGrouping);
  const groupingProperty = groupingPropertyId
    ? workspaceProperties.find((p) => p.id === groupingPropertyId && p.type === "select") ?? null
    : null;
  // A persisted `property:<id>` grouping whose definition is gone (archived,
  // deleted, other workspace) falls back to status columns.
  const grouping: IssueGrouping =
    groupingPropertyId && !groupingProperty ? "status" : storeGrouping;
  const groupingOptionIds = useMemo(
    () =>
      groupingProperty
        ? new Set((groupingProperty.config.options ?? []).map((option) => option.id))
        : undefined,
    [groupingProperty],
  );
  const setIssuePropertyMutation = useSetIssueProperty();
  const unsetIssuePropertyMutation = useUnsetIssueProperty();
  const applyPropertyGroupValue = useCallback(
    (group: BoardColumnGroup, issueId: string) => {
      if (group.propertyId === undefined) return;
      // Surface failures like status/assignee drags do (use-issue-surface-
      // actions): the mutation rolls the card back, but without a toast the
      // snap-back reads as a UI glitch instead of a rejected write.
      const onError = (err: unknown) => {
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.page.move_failed),
        );
      };
      if (group.propertyOptionId === null) {
        unsetIssuePropertyMutation.mutate(
          { issueId, propertyId: group.propertyId },
          { onError },
        );
      } else if (group.propertyOptionId !== undefined) {
        setIssuePropertyMutation.mutate(
          {
            issueId,
            propertyId: group.propertyId,
            value: group.propertyOptionId,
          },
          { onError },
        );
      }
    },
    [setIssuePropertyMutation, t, unsetIssuePropertyMutation],
  );
  const sortFieldKey = sortBy === "created_at" ? "created" : sortBy;
  const sortPropertyId = propertyIdFromViewKey(sortBy);
  const sortLabel = sortBy !== "position"
    ? t(($) => $.board.ordered_by, {
        field: sortPropertyId
          ? workspaceProperties.find((p) => p.id === sortPropertyId)?.name ?? ""
          : t(($) => $.display[`sort_${sortFieldKey}` as keyof typeof $.display]),
      })
    : null;
  const { getActorName } = useActorName();
  const myIssuesOpts = myIssuesScope
    ? { scope: myIssuesScope, filter: myIssuesFilter ?? {} }
    : undefined;
  const groupedIssues = useMemo(
    () =>
      groupBranches?.enabled
        ? groupBranches.issues
        : grouping === "assignee" && assigneeGroups
        ? assigneeGroups.flatMap((group) => group.issues)
        : issues,
    [assigneeGroups, groupBranches, grouping, issues],
  );
  const hydratedAssigneeGroups = useMemo<BoardColumnGroup[] | undefined>(() => {
    if (grouping === "assignee" && groupBranches?.enabled) {
      return groupBranches.descriptors.flatMap((descriptor): BoardColumnGroup[] => {
        if (descriptor.value.kind !== "assignee") return [];
        const actorRef = descriptor.value.actor;
        const actor: { type: IssueAssigneeType; id: string } | null =
          actorRef &&
          (actorRef.type === "member" ||
            actorRef.type === "agent" ||
            actorRef.type === "squad")
            ? { type: actorRef.type, id: actorRef.id }
            : null;
        return [{
          id: descriptor.key,
          title: actor
            ? getActorName(actor.type, actor.id)
            : t(($) => $.filters.no_assignee),
          assigneeType: actor?.type ?? null,
          assigneeId: actor?.id ?? null,
          totalCount: descriptor.count,
          createData: {
            assignee_type: actor?.type ?? null,
            assignee_id: actor?.id ?? null,
          },
        }];
      });
    }
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
  }, [assigneeGroups, getActorName, groupBranches, grouping, t]);
  const groupPagination = useMemo(() => {
    if (!groupBranches?.enabled) return undefined;
    const grouped = new Map<string, IssueGroupPageState[]>();
    for (const descriptor of groupBranches.descriptors) {
      const page = groupBranches.pagination[descriptor.key];
      if (!page) continue;
      let id = descriptor.key;
      if (descriptor.value.kind === "property") {
        const value = descriptor.value;
        id =
          value.value_state === "value" && typeof value.value === "string"
            ? propertyGroupId(value.property_id, value.value)
            : propertyGroupId(value.property_id, null);
      }
      const pages = grouped.get(id) ?? [];
      pages.push(page);
      grouped.set(id, pages);
    }
    return Object.fromEntries(
      Array.from(grouped, ([id, pages]) => [
        id,
        {
          total: pages.reduce((sum, page) => sum + page.total, 0),
          loaded: pages.reduce((sum, page) => sum + page.loaded, 0),
          hasMore: pages.some((page) => page.hasMore),
          isLoading: pages.some((page) => page.isLoading),
          isFetching: pages.some((page) => page.isFetching),
          isError: pages.some((page) => page.isError),
          loadMore: () => {
            for (const page of pages) {
              if (page.hasMore) page.loadMore();
            }
          },
          retry: () => {
            for (const page of pages) {
              if (page.isError) page.retry();
            }
          },
        },
      ]),
    ) as Record<string, IssueGroupPageState>;
  }, [groupBranches]);
  const groups = useMemo(
    () => {
      const built =
        hydratedAssigneeGroups ??
        buildGroups(
        issues,
        visibleStatuses,
        grouping,
        getActorName,
        t(($) => $.filters.no_assignee),
        groupingProperty,
        t(($) => $.board.no_value),
        );
      return built.map((group) => ({
        ...group,
        totalCount: groupPagination?.[group.id]?.total ?? group.totalCount,
      }));
    },
    [hydratedAssigneeGroups, issues, visibleStatuses, grouping, getActorName, groupingProperty, groupPagination, t],
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
  // Shared drag/settle primitive: owns the local column mirror, the
  // dragging/settling locks, the post-move animation-frame throttle, and the
  // settle callback. Shared with list-view (and swimlane) so the surfaces
  // can't drift apart. Local columns follow TQ between drags via the resync
  // effect below; during a drag/settle they are frozen by the locks.
  const {
    columns,
    setColumns,
    columnsRef,
    isDraggingRef,
    isSettlingRef,
    recentlyMovedRef,
    settleVersion,
    beginSettle,
  } = useDragSettle(() => buildColumns(groupedIssues, groups, grouping, groupingOptionIds));

  useEffect(() => {
    if (!isDraggingRef.current && !isSettlingRef.current) {
      setColumns(buildColumns(groupedIssues, groups, grouping, groupingOptionIds));
    }
  }, [groupedIssues, groups, grouping, groupingOptionIds, settleVersion, setColumns, isDraggingRef, isSettlingRef]);

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
        setColumns(buildColumns(groupedIssues, groups, grouping, groupingOptionIds));

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
        // Optimistically move the card into the target column *now*. Without
        // this, the sortBy != "position" path never touches local columns on
        // drop, so onDragOver having been a no-op leaves the card in its origin
        // column for the whole request — it only jumps across when the mutation
        // settles. That is the "snaps back to origin, then moves" glitch.
        // Placement mirrors the cache (insertByPosition) so the settle rebuild
        // from TanStack Query is a visual no-op.
        const targetIds = insertIdByPosition(
          (cols[overCol] ?? []).filter((id) => id !== activeId),
          activeId,
          currentIssue.position,
          map,
        );
        setColumns((prev) => {
          const fromIds = (prev[activeCol] ?? []).filter((cid) => cid !== activeId);
          return { ...prev, [activeCol]: fromIds, [overCol]: targetIds };
        });
        onMoveIssue(
          activeId,
          {
            ...getMoveUpdates(finalGroup, currentIssue.position),
            ...getMoveAnchors(targetIds, activeId),
          },
          beginSettle(),
        );
        applyPropertyGroupValue(finalGroup, activeId);
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

      // beginSettle() holds the lock and returns the onSettled callback that
      // releases it and resyncs local columns from the cache: a no-op on
      // success (onSuccess already patched the moved card in place), the revert
      // on error (onError restored the snapshot). Without it a failed move would
      // strand the card at the drop target, since onSettled no longer refetches.
      onMoveIssue(
        activeId,
        {
          ...getMoveUpdates(finalGroup, newPosition),
          ...getMoveAnchors(finalIds, activeId),
        },
        beginSettle(),
      );
      applyPropertyGroupValue(finalGroup, activeId);
    },
    [groupedIssues, groups, grouping, groupingOptionIds, onMoveIssue, groupIds, groupMap, sortBy, beginSettle, columnsRef, isDraggingRef, setColumns, applyPropertyGroupValue],
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-2">
        {groups.length === 0 ? (
          groupBranches?.isError ? (
            <button
              type="button"
              className="flex min-w-full flex-1 items-center justify-center text-sm text-destructive hover:underline"
              onClick={groupBranches.retryGroups}
            >
              {t(($) => $.table.load_more_failed_retry)}
            </button>
          ) : (
            <div className="flex min-w-full flex-1 items-center justify-center text-sm text-muted-foreground">
              {t(($) => $.board.empty_grouping)}
            </div>
          )
        ) : (
          groups.map((group) =>
            isStatusGroup(group) ? (
              statusPagination ? (
                <ServerPaginatedBoardColumn
                  key={group.id}
                  group={group}
                  issueIds={columns[group.id] ?? EMPTY_IDS}
                  issueMap={issueMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  page={statusPagination[group.status]}
                  projectId={projectId}
                  onCreateIssue={onCreateIssue}
                  sortLabel={sortLabel}
                />
              ) : (
                <PaginatedBoardColumn
                  key={group.id}
                  group={group}
                  issueIds={columns[group.id] ?? EMPTY_IDS}
                  issueMap={issueMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  myIssuesOpts={myIssuesOpts}
                  sort={sort}
                  projectId={projectId}
                  onCreateIssue={onCreateIssue}
                  sortLabel={sortLabel}
                />
              )
            ) : (
              groupPagination?.[group.id] ? (
                <ServerPaginatedBoardColumn
                  key={group.id}
                  group={group}
                  issueIds={columns[group.id] ?? EMPTY_IDS}
                  issueMap={issueMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  page={groupPagination[group.id]!}
                  projectId={projectId}
                  onCreateIssue={onCreateIssue}
                  sortLabel={sortLabel}
                />
              ) : assigneeGroupQueryKey && assigneeGroupFilter ? (
                <PaginatedAssigneeBoardColumn
                  key={group.id}
                  group={group}
                  issueIds={columns[group.id] ?? EMPTY_IDS}
                  issueMap={issueMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  queryKey={assigneeGroupQueryKey}
                  filter={assigneeGroupFilter}
                  sort={sort}
                  projectId={projectId}
                  onCreateIssue={onCreateIssue}
                  sortLabel={sortLabel}
                />
              ) : (
                <BoardColumn
                  key={group.id}
                  group={group}
                  issueIds={columns[group.id] ?? EMPTY_IDS}
                  issueMap={issueMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  projectId={projectId}
                  onCreateIssue={onCreateIssue}
                  totalCount={group.totalCount}
                  sortLabel={sortLabel}
                />
              )
            ),
          )
        )}
        {groupBranches?.hasMoreGroups && (
          <div className="flex w-8 shrink-0 items-center justify-center">
            <InfiniteScrollSentinel
              onVisible={groupBranches.loadMoreGroups}
              loading={groupBranches.isLoadingMoreGroups}
            />
          </div>
        )}

        {groupingProperty && !groupBranches?.enabled && (
          <PropertyBoardPoolLoader
            statuses={visibleStatuses}
            myIssuesOpts={myIssuesOpts}
            sort={sort}
          />
        )}

        {grouping === "status" && hiddenStatuses.length > 0 && (
          <BoardHiddenColumnsPanel
            hiddenStatuses={hiddenStatuses}
            myIssuesOpts={myIssuesOpts}
            sort={sort}
            statusPagination={statusPagination}
          />
        )}
      </div>

      <DragOverlay dropAnimation={null}>
        {activeIssue ? (
          <div style={{ width: BOARD_CARD_WIDTH }} className="rotate-1 cursor-grabbing opacity-90 shadow-lg shadow-black/10">
            <BoardCardContent
              issue={activeIssue}
              childProgress={childProgressMap.get(activeIssue.id)}
              project={
                activeIssue.project_id
                  ? projectMap?.get(activeIssue.project_id)
                  : undefined
              }
            />
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
  projectMap,
  queryKey,
  filter,
  sort,
  projectId,
  onCreateIssue,
  sortLabel,
}: {
  group: BoardColumnGroup;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  queryKey: QueryKey;
  filter: AssigneeGroupedIssuesFilter;
  sort?: IssueSortParam;
  projectId?: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
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
    sort,
  );
  return (
    <BoardColumn
      group={group}
      issueIds={issueIds}
      issueMap={issueMap}
      childProgressMap={childProgressMap}
      projectMap={projectMap}
      totalCount={total}
      projectId={projectId}
      onCreateIssue={onCreateIssue}
      sortLabel={sortLabel}
      footer={
        hasMore ? (
          <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />
        ) : undefined
      }
    />
  );
});

const ServerPaginatedBoardColumn = memo(function ServerPaginatedBoardColumn({
  group,
  issueIds,
  issueMap,
  childProgressMap,
  projectMap,
  page,
  projectId,
  onCreateIssue,
  sortLabel,
}: {
  group: BoardColumnGroup;
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  page: IssueStatusPageState | IssueGroupPageState;
  projectId?: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
  sortLabel?: string | null;
}) {
  const { t } = useT("issues");
  const footer = page.isError ? (
    <button
      type="button"
      className="w-full py-2 text-xs text-destructive hover:underline"
      onClick={page.retry}
    >
      {t(($) => $.table.load_more_failed_retry)}
    </button>
  ) : page.hasMore ? (
    <InfiniteScrollSentinel
      onVisible={page.loadMore}
      loading={page.isLoading || page.isFetching}
    />
  ) : undefined;
  return (
    <BoardColumn
      group={group}
      issueIds={issueIds}
      issueMap={issueMap}
      childProgressMap={childProgressMap}
      projectMap={projectMap}
      totalCount={page.total}
      projectId={projectId}
      onCreateIssue={onCreateIssue}
      sortLabel={sortLabel}
      footer={footer}
    />
  );
});

const PaginatedBoardColumn = memo(function PaginatedBoardColumn({
  group,
  issueIds,
  issueMap,
  childProgressMap,
  projectMap,
  myIssuesOpts,
  sort,
  projectId,
  onCreateIssue,
  sortLabel,
}: {
  group: BoardColumnGroup & { status: IssueStatus };
  issueIds: string[];
  issueMap: Map<string, Issue>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
  sort?: IssueSortParam;
  projectId?: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
  sortLabel?: string | null;
}) {
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByStatus(
    group.status,
    myIssuesOpts,
    sort,
  );
  return (
    <BoardColumn
      group={group}
      issueIds={issueIds}
      issueMap={issueMap}
      childProgressMap={childProgressMap}
      projectMap={projectMap}
      totalCount={total}
      projectId={projectId}
      onCreateIssue={onCreateIssue}
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
/**
 * The property-grouped board derives its columns from the status-bucketed
 * pool, which pages per status. Property columns have no per-column
 * pagination yet (tracked in MUL-4493), so this strip keeps every issue
 * REACHABLE: one sentinel per status that still has server rows loads the
 * pool further and the property columns re-derive. Without it, rows beyond
 * a status's loaded page silently never join any column (review round 3).
 */
function PropertyBoardPoolLoader({
  statuses,
  myIssuesOpts,
  sort,
}: {
  statuses: IssueStatus[];
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
  sort?: IssueSortParam;
}) {
  return (
    <div className="col-span-full flex justify-center py-1">
      {statuses.map((status) => (
        <PropertyBoardPoolSentinel key={status} status={status} myIssuesOpts={myIssuesOpts} sort={sort} />
      ))}
    </div>
  );
}

function PropertyBoardPoolSentinel({
  status,
  myIssuesOpts,
  sort,
}: {
  status: IssueStatus;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
  sort?: IssueSortParam;
}) {
  const { loadMore, hasMore, isLoading } = useLoadMoreByStatus(status, myIssuesOpts, sort);
  if (!hasMore) return null;
  return <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />;
}

function BoardHiddenColumnRow({
  status,
  myIssuesOpts,
  sort,
}: {
  status: IssueStatus;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
  sort?: IssueSortParam;
}) {
  const { total } = useLoadMoreByStatus(status, myIssuesOpts, sort);
  return <HiddenColumnRow status={status} total={total} />;
}

function BoardHiddenColumnsPanel({
  hiddenStatuses,
  myIssuesOpts,
  sort,
  statusPagination,
}: {
  hiddenStatuses: IssueStatus[];
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
  sort?: IssueSortParam;
  statusPagination?: IssueStatusPagination;
}) {
  return (
    <HiddenColumnsPanel
      hiddenStatuses={hiddenStatuses}
      renderRow={(status) => (
        statusPagination ? (
          <HiddenColumnRow
            key={status}
            status={status}
            total={statusPagination[status].total}
          />
        ) : (
          <BoardHiddenColumnRow
            key={status}
            status={status}
            myIssuesOpts={myIssuesOpts}
            sort={sort}
          />
        )
      )}
    />
  );
}

/**
 * Memoized: the surface controller re-renders on loading-flag flips (e.g. a
 * query enabling when the view changes) — without memo every such flip
 * re-rendered this entire view tree (hundreds of ms). All props are
 * referentially stable useMemo/useCallback outputs from the controller.
 */
export const BoardView = memo(BoardViewImpl);
