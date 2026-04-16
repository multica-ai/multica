"use client";

import { memo } from "react";
import { AppLink } from "../../navigation";
import type { Issue } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { useWorkspacePaths } from "@multica/core/paths";
import { PriorityIcon } from "./priority-icon";
import { ProgressRing } from "./progress-ring";

export interface ChildProgress {
  done: number;
  total: number;
}

function formatDate(date: string): string {
  return new Date(date).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

export const ListRow = memo(function ListRow({
  issue,
  childProgress,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
}) {
  const selected = useIssueSelectionStore((s) => s.selectedIds.has(issue.id));
  const toggle = useIssueSelectionStore((s) => s.toggle);
  const p = useWorkspacePaths();

  return (
    <div
      className={`group/row flex h-9 items-center gap-2 px-4 text-sm transition-colors hover:bg-accent/50 ${
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
      <AppLink
        href={p.issueDetail(issue.id)}
        className="flex flex-1 items-center gap-2 min-w-0"
      >
        <span className="w-16 shrink-0 text-xs text-muted-foreground">
          {issue.identifier}
        </span>
        <span className="flex min-w-0 flex-1 items-center gap-1.5">
          <span className="truncate">{issue.title}</span>
          {childProgress && (
            <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5">
              <ProgressRing done={childProgress.done} total={childProgress.total} size={14} />
              <span className="text-[11px] text-muted-foreground tabular-nums font-medium">
                {childProgress.done}/{childProgress.total}
              </span>
            </span>
          )}
        </span>
        {issue.due_date && (
          <span className="shrink-0 text-xs text-muted-foreground">
            {formatDate(issue.due_date)}
          </span>
        )}
        {issue.assignee_type && issue.assignee_id && (
          <ActorAvatar
            actorType={issue.assignee_type}
            actorId={issue.assignee_id}
            size={20}
          />
        )}
      </AppLink>
    </div>
  );
});
