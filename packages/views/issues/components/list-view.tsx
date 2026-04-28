"use client";

import { useMemo } from "react";
import { ChevronRight, Plus } from "lucide-react";
import { Accordion } from "@base-ui/react/accordion";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import type { Issue, IssueExecutionSummary, IssueStatus } from "@multica/core/types";
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
  executionSummaryMap,
  onOpenIssue,
  myIssuesScope,
  myIssuesFilter,
}: {
  issues: Issue[];
  visibleStatuses: IssueStatus[];
  childProgressMap?: Map<string, ChildProgress>;
  executionSummaryMap?: Map<string, IssueExecutionSummary>;
  onOpenIssue?: (issue: Issue) => void;
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

  const issuesByStatus = useMemo(() => {
    const map = new Map<IssueStatus, Issue[]>();
    for (const status of visibleStatuses) {
      const filtered = issues.filter((i) => i.status === status);
      map.set(status, sortIssues(filtered, sortBy, sortDirection));
    }
    return map;
  }, [issues, visibleStatuses, sortBy, sortDirection]);

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
            childProgressMap={childProgressMap}
            executionSummaryMap={executionSummaryMap}
            onOpenIssue={onOpenIssue}
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
  childProgressMap,
  executionSummaryMap,
  onOpenIssue,
  myIssuesOpts,
}: {
  status: IssueStatus;
  issues: Issue[];
  childProgressMap: Map<string, ChildProgress>;
  executionSummaryMap?: Map<string, IssueExecutionSummary>;
  onOpenIssue?: (issue: Issue) => void;
  myIssuesOpts?: { scope: string; filter: MyIssuesFilter };
}) {
  const selectedIds = useIssueSelectionStore((s) => s.selectedIds);
  const select = useIssueSelectionStore((s) => s.select);
  const deselect = useIssueSelectionStore((s) => s.deselect);
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByStatus(
    status,
    myIssuesOpts,
  );

  const issueIds = issues.map((i) => i.id);
  const selectedCount = issueIds.filter((id) => selectedIds.has(id)).length;
  const allSelected = issues.length > 0 && selectedCount === issues.length;
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
                deselect(issueIds);
              } else {
                select(issueIds);
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
            {issues.map((issue) => (
              <ListRow
                key={issue.id}
                issue={issue}
                childProgress={childProgressMap.get(issue.id)}
                executionSummary={executionSummaryMap?.get(issue.id)}
                onOpenIssue={onOpenIssue}
              />
            ))}
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
