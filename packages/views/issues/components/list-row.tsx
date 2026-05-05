"use client";

// CEREBRO-PATCH(list-row-cerebro): cerebro modification of upstream file

import { memo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight } from "lucide-react";
import { AppLink } from "../../navigation";
import type { Issue } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { projectListOptions } from "@multica/core/projects/queries";
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
  childIssues,
  indent = false,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  childIssues?: Issue[];
  indent?: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  const selected = useIssueSelectionStore((s) => s.selectedIds.has(issue.id));
  const toggle = useIssueSelectionStore((s) => s.toggle);
  const p = useWorkspacePaths();
  const storeProperties = useViewStore((s) => s.cardProperties);
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery({
    ...projectListOptions(wsId),
    enabled: storeProperties.project && !!issue.project_id,
  });
  const project = issue.project_id ? projects.find((pr) => pr.id === issue.project_id) : undefined;

  const showProject = storeProperties.project && project;
  const showChildProgress = storeProperties.childProgress && childProgress;
  const showAssignee = storeProperties.assignee && issue.assignee_type && issue.assignee_id;
  const showDueDate = storeProperties.dueDate && issue.due_date;
  const hasChildren = childIssues && childIssues.length > 0;

  return (
    <>
      <div
        className={`group/row flex h-9 items-center gap-2 text-sm transition-colors hover:bg-accent/50 ${
          selected ? "bg-accent/30" : ""
        } ${indent ? "pr-4" : "px-4"}`}
      >
        {hasChildren && (
          <button
            type="button"
            className="flex shrink-0 items-center justify-center w-4 h-4 text-muted-foreground hover:text-foreground"
            onClick={() => setExpanded(!expanded)}
          >
            <ChevronRight className={`size-3.5 transition-transform ${expanded ? "rotate-90" : ""}`} />
          </button>
        )}
        <div className={`relative flex shrink-0 items-center justify-center w-4 h-4 ${!hasChildren && !indent ? "" : ""}`}>
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
            {showChildProgress && (
              <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5">
                <ProgressRing done={childProgress!.done} total={childProgress!.total} size={14} />
                <span className="text-[11px] text-muted-foreground tabular-nums font-medium">
                  {childProgress!.done}/{childProgress!.total}
                </span>
              </span>
            )}
          </span>
          {showProject && (
            <span className="inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground max-w-[140px]">
              <span aria-hidden="true" className="shrink-0">{project!.icon || "📁"}</span>
              <span className="truncate">{project!.title}</span>
            </span>
          )}
          {showDueDate && (
            <span className="shrink-0 text-xs text-muted-foreground">
              {formatDate(issue.due_date!)}
            </span>
          )}
          {showAssignee && (
            <ActorAvatar
              actorType={issue.assignee_type!}
              actorId={issue.assignee_id!}
              size={20}
            />
          )}
        </AppLink>
      </div>
      {hasChildren && expanded && (
        <div className="relative ml-[22px]">
          {/* Vertical tree line */}
          <div className="absolute left-0 top-0 bottom-[18px] w-px bg-border" />
          {childIssues!.map((child) => (
            <div key={child.id} className="relative">
              {/* Horizontal branch line */}
              <div className="absolute left-0 top-[18px] w-3 h-px bg-border" />
              <div className="pl-5">
                <ListRow issue={child} indent />
              </div>
            </div>
          ))}
        </div>
      )}
    </>
  );
});
