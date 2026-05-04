"use client";

import { useMemo } from "react";
import { ChevronRight, Plus } from "lucide-react";
import { Accordion } from "@base-ui/react/accordion";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import type { Issue, IssueStatus } from "@multica/core/types";
import { useLoadMoreByStatus } from "@multica/core/issues/mutations";
import type { MyIssuesFilter } from "@multica/core/issues/queries";
import { useModalStore } from "@multica/core/modals";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { sortIssues } from "../utils/sort";
import { StatusHeading } from "./status-heading";
import { ListRow, type ChildProgress } from "./list-row";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();

export function ListView({
  issues,
  visibleStatuses,
  childProgressMap = EMPTY_PROGRESS_MAP,
  myIssuesScope,
  myIssuesFilter,
}: {
  issues: Issue[];
  visibleStatuses: IssueStatus[];
  childProgressMap?: Map<string, ChildProgress>;
  /** When set, per-status load-more targets the scoped cache instead of the workspace one. */
  myIssuesScope?: string;
  myIssuesFilter?: MyIssuesFilter;
}) {
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const listCollapsedStatuses = useViewStore(
    (s) => s.listCollapsedStatuses
  );
  const toggleListCollapsed = useViewStore(
    (s) => s.toggleListCollapsed
  );
  const collapsedParentIds = useViewStore((s) => s.collapsedParentIds);
  const toggleParentCollapsed = useViewStore((s) => s.toggleParentCollapsed);

  // Build a map from parent_issue_id → child issues, scoped to the issues
  // currently visible (post-filter, post-scope). A child whose parent is
  // also in `issues` is treated as nested; a child whose parent has been
  // filtered out becomes an "orphan" and renders at the top level so it
  // isn't silently hidden when a status filter excludes the parent.
  const { issuesByStatus, childrenByParent, orphanedChildIds } = useMemo(() => {
    const parentIds = new Set<string>();
    for (const i of issues) parentIds.add(i.id);

    const childrenMap = new Map<string, Issue[]>();
    const orphans = new Set<string>();
    for (const i of issues) {
      if (i.parent_issue_id && parentIds.has(i.parent_issue_id)) {
        const arr = childrenMap.get(i.parent_issue_id);
        if (arr) arr.push(i);
        else childrenMap.set(i.parent_issue_id, [i]);
      } else if (i.parent_issue_id) {
        orphans.add(i.id);
      }
    }

    // Sort children by the same view-level sort (so order is consistent
    // with siblings at the top level).
    for (const [pid, arr] of childrenMap) {
      childrenMap.set(pid, sortIssues(arr, sortBy, sortDirection));
    }

    // Top-level issues per status: any issue with no parent OR with a
    // parent that isn't in the visible set (orphans surface here).
    const byStatus = new Map<IssueStatus, Issue[]>();
    for (const status of visibleStatuses) {
      const filtered = issues.filter(
        (i) =>
          i.status === status &&
          (!i.parent_issue_id || !parentIds.has(i.parent_issue_id)),
      );
      byStatus.set(status, sortIssues(filtered, sortBy, sortDirection));
    }

    return {
      issuesByStatus: byStatus,
      childrenByParent: childrenMap,
      orphanedChildIds: orphans,
    };
  }, [issues, visibleStatuses, sortBy, sortDirection]);

  const collapsedSet = useMemo(
    () => new Set(collapsedParentIds),
    [collapsedParentIds],
  );

  const expandedStatuses = useMemo(
    () =>
      visibleStatuses.filter(
        (s) => !listCollapsedStatuses.includes(s)
      ),
    [visibleStatuses, listCollapsedStatuses]
  );

  const myIssuesOpts = myIssuesScope
    ? { scope: myIssuesScope, filter: myIssuesFilter ?? {} }
    : undefined;

  return (
    <div className="flex-1 min-h-0 overflow-y-auto p-2">
      <Accordion.Root
        multiple
        className="space-y-1"
        value={expandedStatuses}
        onValueChange={(value: string[]) => {
          for (const status of visibleStatuses) {
            const wasExpanded = expandedStatuses.includes(status);
            const isExpanded = value.includes(status);
            if (wasExpanded !== isExpanded) {
              toggleListCollapsed(status as IssueStatus);
            }
          }
        }}
      >
        {visibleStatuses.map((status) => (
          <StatusAccordionItem
            key={status}
            status={status}
            issues={issuesByStatus.get(status) ?? []}
            childrenByParent={childrenByParent}
            collapsedParentIds={collapsedSet}
            onToggleParent={toggleParentCollapsed}
            childProgressMap={childProgressMap}
            orphanedChildIds={orphanedChildIds}
            myIssuesOpts={myIssuesOpts}
          />
        ))}
      </Accordion.Root>
    </div>
  );
}

function StatusAccordionItem({
  status,
  issues,
  childrenByParent,
  collapsedParentIds,
  onToggleParent,
  childProgressMap,
  orphanedChildIds,
  myIssuesOpts,
}: {
  status: IssueStatus;
  issues: Issue[];
  childrenByParent: Map<string, Issue[]>;
  collapsedParentIds: Set<string>;
  onToggleParent: (parentId: string) => void;
  childProgressMap: Map<string, ChildProgress>;
  orphanedChildIds: Set<string>;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  const selectedIds = useIssueSelectionStore((s) => s.selectedIds);
  const select = useIssueSelectionStore((s) => s.select);
  const deselect = useIssueSelectionStore((s) => s.deselect);
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByStatus(
    status,
    myIssuesOpts,
  );

  // Selection at the status level covers ALL rows that will render
  // under this status — including currently-visible children — so
  // shift-click and "select all" behave the same with or without nesting.
  const visibleRowIds = useMemo(() => {
    const ids: string[] = [];
    for (const issue of issues) {
      ids.push(issue.id);
      const children = childrenByParent.get(issue.id);
      if (children && !collapsedParentIds.has(issue.id)) {
        for (const c of children) ids.push(c.id);
      }
    }
    return ids;
  }, [issues, childrenByParent, collapsedParentIds]);

  const selectedCount = visibleRowIds.filter((id) => selectedIds.has(id)).length;
  const allSelected = visibleRowIds.length > 0 && selectedCount === visibleRowIds.length;
  const someSelected = selectedCount > 0;

  return (
    <Accordion.Item value={status}>
      <Accordion.Header className="group/header flex h-10 items-center rounded-lg bg-muted/40 transition-colors hover:bg-accent/30">
        <div className="pl-3 flex items-center">
          <input
            type="checkbox"
            checked={allSelected}
            ref={(el) => {
              if (el) el.indeterminate = someSelected && !allSelected;
            }}
            onChange={() => {
              if (allSelected) {
                deselect(visibleRowIds);
              } else {
                select(visibleRowIds);
              }
            }}
            className="cursor-pointer accent-primary"
          />
        </div>
        <Accordion.Trigger className="group/trigger flex flex-1 items-center gap-2 px-2 h-full text-left outline-none">
          <ChevronRight className="size-3.5 shrink-0 text-muted-foreground transition-transform group-aria-expanded/trigger:rotate-90" />
          <StatusHeading status={status} count={total} />
        </Accordion.Trigger>
        <div className="pr-2">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-full text-muted-foreground opacity-0 group-hover/header:opacity-100 transition-opacity"
                  onClick={() =>
                    useModalStore
                      .getState()
                      .open("create-issue", { status })
                  }
                />
              }
            >
              <Plus className="size-3.5" />
            </TooltipTrigger>
            <TooltipContent>Add issue</TooltipContent>
          </Tooltip>
        </div>
      </Accordion.Header>
      <Accordion.Panel className="pt-1">
        {issues.length > 0 ? (
          <>
            {issues.map((issue) => {
              const children = childrenByParent.get(issue.id);
              const hasChildren = !!children && children.length > 0;
              const collapsed = collapsedParentIds.has(issue.id);
              return (
                <div key={issue.id}>
                  <ListRow
                    issue={issue}
                    childProgress={childProgressMap.get(issue.id)}
                    hasChildren={hasChildren}
                    collapsed={collapsed}
                    onToggleCollapsed={
                      hasChildren ? () => onToggleParent(issue.id) : undefined
                    }
                    isOrphan={orphanedChildIds.has(issue.id)}
                  />
                  {hasChildren && !collapsed && (
                    <div role="group" aria-label={`Sub-issues of ${issue.identifier}`}>
                      {children!.map((child) => (
                        <ListRow
                          key={child.id}
                          issue={child}
                          childProgress={childProgressMap.get(child.id)}
                          indentLevel={1}
                        />
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
            {hasMore && (
              <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />
            )}
          </>
        ) : (
          <p className="py-6 text-center text-xs text-muted-foreground">
            No issues
          </p>
        )}
      </Accordion.Panel>
    </Accordion.Item>
  );
}
