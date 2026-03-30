"use client";

import { useMemo } from "react";
import { ChevronRight, Plus } from "lucide-react";
import { Accordion } from "@base-ui/react/accordion";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import type { Issue, IssueStatus } from "@/shared/types";
import { STATUS_CONFIG } from "@/features/issues/config";
import { useModalStore } from "@/features/modals";
import { useIssueViewStore } from "@/features/issues/stores/view-store";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { sortIssues } from "@/features/issues/utils/sort";
import { StatusIcon } from "./status-icon";
import { ListRow } from "./list-row";

export function ListView({
  issues,
  visibleStatuses,
}: {
  issues: Issue[];
  visibleStatuses: IssueStatus[];
}) {
  const sortBy = useIssueViewStore((s) => s.sortBy);
  const sortDirection = useIssueViewStore((s) => s.sortDirection);
  const listCollapsedStatuses = useIssueViewStore(
    (s) => s.listCollapsedStatuses
  );
  const toggleListCollapsed = useIssueViewStore(
    (s) => s.toggleListCollapsed
  );
  const selectedIds = useIssueSelectionStore((s) => s.selectedIds);
  const select = useIssueSelectionStore((s) => s.select);
  const deselect = useIssueSelectionStore((s) => s.deselect);

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
        {visibleStatuses.map((status) => {
          const cfg = STATUS_CONFIG[status];
          const statusIssues = issuesByStatus.get(status) ?? [];
          const statusIssueIds = statusIssues.map((i) => i.id);
          const selectedCount = statusIssueIds.filter((id) => selectedIds.has(id)).length;
          const allSelected = statusIssues.length > 0 && selectedCount === statusIssues.length;
          const someSelected = selectedCount > 0;

          return (
            <Accordion.Item key={status} value={status}>
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
                        deselect(statusIssueIds);
                      } else {
                        select(statusIssueIds);
                      }
                    }}
                    className="cursor-pointer accent-primary"
                  />
                </div>
                <Accordion.Trigger className="group/trigger flex flex-1 items-center gap-2 px-2 h-full text-left outline-none">
                  <ChevronRight className="size-3.5 shrink-0 text-muted-foreground transition-transform group-aria-expanded/trigger:rotate-90" />
                  <StatusIcon status={status} className="h-3.5 w-3.5" />
                  <span className="text-sm font-medium">{cfg.label}</span>
                  <span className="flex h-5 min-w-5 items-center justify-center rounded-full bg-muted px-1.5 text-xs text-muted-foreground">
                    {statusIssues.length}
                  </span>
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
              <Accordion.Panel>
                {statusIssues.length > 0 ? (
                  statusIssues.map((issue) => (
                    <ListRow key={issue.id} issue={issue} />
                  ))
                ) : (
                  <p className="py-6 text-center text-xs text-muted-foreground">
                    No issues
                  </p>
                )}
              </Accordion.Panel>
            </Accordion.Item>
          );
        })}
      </Accordion.Root>
    </div>
  );
}
