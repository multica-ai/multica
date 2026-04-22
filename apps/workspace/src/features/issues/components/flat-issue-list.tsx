"use client";

import { memo, useMemo } from "react";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { useViewStore } from "@/features/issues/stores/view-store-context";
import { STATUS_CONFIG, PRIORITY_CONFIG } from "@/features/issues/config";
import { sortIssues } from "@/features/issues/utils/sort";
import {
  formatIssueSchedule,
  isIssueScheduleOverdue,
} from "@/features/issues/utils/workbench-view";
import { Link } from "@/shared/router";
import type { Issue } from "@/shared/types";
import { PriorityIcon } from "./priority-icon";
import { StatusIcon } from "./status-icon";
import { IssueTaskStatusBadge } from "./issue-task-status-badge";

const FlatIssueRow = memo(function FlatIssueRow({ issue }: { issue: Issue }) {
  const cardProperties = useViewStore((state) => state.cardProperties);
  const selected = useIssueSelectionStore((state) => state.selectedIds.has(issue.id));
  const toggle = useIssueSelectionStore((state) => state.toggle);

  const statusConfig = STATUS_CONFIG[issue.status];
  const priorityConfig = PRIORITY_CONFIG[issue.priority];
  const scheduleLabel = cardProperties.dueDate ? formatIssueSchedule(issue) : null;
  const showDescription = cardProperties.description && Boolean(issue.description);
  const showAssignee = cardProperties.assignee && issue.assignee_type && issue.assignee_id;
  const showPriority = cardProperties.priority;

  return (
    <div
      className={`group/row flex items-start gap-3 px-4 py-2.5 text-sm transition-colors hover:bg-accent/30 ${
        selected ? "bg-accent/20" : ""
      }`}
    >
      <div className="relative mt-1 flex h-4 w-4 shrink-0 items-center justify-center">
        <PriorityIcon
          priority={issue.priority}
          className={selected ? "hidden" : "group-hover/row:hidden"}
        />
        <input
          type="checkbox"
          checked={selected}
          onChange={() => toggle(issue.id)}
          className={`absolute inset-0 cursor-pointer accent-primary ${
            selected ? "" : "hidden group-hover/row:block"
          }`}
        />
      </div>

      <Link
        href={`/issues/${issue.id}`}
        className="flex min-w-0 flex-1 items-start justify-between gap-4"
      >
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="shrink-0 text-xs text-muted-foreground">
              {issue.identifier}
            </span>
            <span
              className={`inline-flex items-center gap-1.5 rounded px-2 py-0.5 text-xs font-semibold ${statusConfig.badgeBg} ${statusConfig.badgeText}`}
            >
              <StatusIcon status={issue.status} className="h-3 w-3" inheritColor />
              {statusConfig.label}
            </span>
            {showPriority ? (
              <span
                className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${priorityConfig.badgeBg} ${priorityConfig.badgeText}`}
              >
                <PriorityIcon priority={issue.priority} className="h-3 w-3" inheritColor />
                {priorityConfig.label}
              </span>
            ) : null}
          </div>

          <div className="mt-1 truncate font-medium text-foreground">
            {issue.title}
          </div>

          {(showDescription || issue.assignee_type === "agent") ? (
            <div className="mt-1 flex flex-wrap items-center gap-2">
              {showDescription ? (
                <p className="min-w-0 flex-1 truncate text-xs text-muted-foreground">
                  {issue.description}
                </p>
              ) : null}
              <IssueTaskStatusBadge issue={issue} variant="list" />
            </div>
          ) : null}
        </div>

        <div className="flex shrink-0 items-center gap-3 self-center">
          {scheduleLabel ? (
            <span
              className={`shrink-0 text-xs ${
                isIssueScheduleOverdue(issue)
                  ? "text-destructive"
                  : "text-muted-foreground"
              }`}
            >
              {scheduleLabel}
            </span>
          ) : null}
          {showAssignee ? (
            <ActorAvatar
              actorType={issue.assignee_type!}
              actorId={issue.assignee_id!}
              size={20}
            />
          ) : null}
        </div>
      </Link>
    </div>
  );
});

export function FlatIssueList({ issues }: { issues: Issue[] }) {
  const sortBy = useViewStore((state) => state.sortBy);
  const sortDirection = useViewStore((state) => state.sortDirection);

  const sortedIssues = useMemo(
    () => sortIssues(issues, sortBy, sortDirection),
    [issues, sortBy, sortDirection],
  );

  return (
    <div className="flex-1 min-h-0 overflow-y-auto">
      <div className="divide-y">
        {sortedIssues.map((issue) => (
          <FlatIssueRow key={issue.id} issue={issue} />
        ))}
      </div>
    </div>
  );
}