"use client";

import { memo } from "react";
import type { Issue } from "@/shared/types";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import {
  formatIssueSchedule,
  isIssueScheduleOverdue,
} from "@/features/issues/utils/workbench-view";
import { Link } from "@/shared/router";
import { PriorityIcon } from "./priority-icon";
import { IssueTaskStatusBadge } from "./issue-task-status-badge";

export const ListRow = memo(function ListRow({ issue }: { issue: Issue }) {
  const selected = useIssueSelectionStore((s) => s.selectedIds.has(issue.id));
  const toggle = useIssueSelectionStore((s) => s.toggle);
  const scheduleLabel = formatIssueSchedule(issue);
  const isOverdue = isIssueScheduleOverdue(issue);

  return (
    <div
      className={`group/row flex min-h-9 items-center gap-2 px-4 py-1 text-sm transition-colors hover:bg-accent/50 ${
        selected ? "bg-accent/30" : ""
      }`}
    >
      <div className="relative flex shrink-0 items-center justify-center w-4 h-4">
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
        className="flex flex-1 items-center gap-2 min-w-0"
      >
        <span className="w-16 shrink-0 text-xs text-muted-foreground">
          {issue.identifier}
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate">{issue.title}</div>
          <div className="mt-0.5 flex items-center gap-2">
            <IssueTaskStatusBadge issue={issue} variant="list" />
          </div>
        </div>
        {scheduleLabel && (
          <span className={`shrink-0 text-xs ${isOverdue ? "text-destructive" : "text-muted-foreground"}`}>
            {scheduleLabel}
          </span>
        )}
        {issue.assignee_type && issue.assignee_id && (
          <ActorAvatar
            actorType={issue.assignee_type}
            actorId={issue.assignee_id}
            size={20}
          />
        )}
      </Link>
    </div>
  );
});
