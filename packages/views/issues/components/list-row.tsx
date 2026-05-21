"use client";

import { memo } from "react";
import { useQuery } from "@tanstack/react-query";
import { AppLink } from "../../navigation";
import type { Issue } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useIssueSelectionStore } from "@multica/core/issues/stores/selection-store";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { projectListOptions } from "@multica/core/projects/queries";
import { ProjectIcon } from "../../projects/components/project-icon";
import { PriorityIcon } from "./priority-icon";
import { StatusIcon } from "./status-icon";
import { ProgressRing } from "./progress-ring";
import { IssueActionsContextMenu } from "../actions";
import { LabelChip } from "../../labels/label-chip";

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
  const storeProperties = useViewStore((s) => s.cardProperties);
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery({
    ...projectListOptions(wsId),
    enabled: storeProperties.project && !!issue.project_id,
  });
  const project = issue.project_id ? projects.find((pr) => pr.id === issue.project_id) : undefined;
  const labels = issue.labels ?? [];

  const showProject = storeProperties.project && project;
  const showChildProgress = storeProperties.childProgress && childProgress;
  const showAssignee = storeProperties.assignee && issue.assignee_type && issue.assignee_id;
  const showStartDate = storeProperties.startDate && issue.start_date;
  const showDueDate = storeProperties.dueDate && issue.due_date;
  const showLabels = storeProperties.labels && labels.length > 0;

  return (
    <IssueActionsContextMenu issue={issue}>
      <div
        className={`group/row flex items-center gap-2 px-3 md:px-4 text-sm transition-colors hover:not-data-[popup-open]:bg-accent/60 data-[popup-open]:bg-accent min-h-[60px] md:min-h-0 md:h-9 ${
          selected ? "bg-accent/30" : ""
        }`}
      >
        {/* Mobile: show status icon as the leading visual cue (more useful
            than priority — most issues are "no priority", almost all rows
            looked identical). Desktop: keep the priority/checkbox swap. */}
        <StatusIcon
          status={issue.status}
          className="md:hidden h-[18px] w-[18px] shrink-0"
        />
        <div className="hidden md:relative md:flex shrink-0 items-center justify-center w-4 h-4">
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
          className="flex flex-1 items-center gap-2 min-w-0 py-2 md:py-0"
        >
          {/* Desktop: identifier sits to the left of the title in its own
              fixed-width column. Mobile: identifier is hidden here and
              re-rendered as a small line under the title (see below). */}
          <span className="hidden md:inline-block w-16 shrink-0 text-xs text-muted-foreground">
            {issue.identifier}
          </span>
          <span className="flex min-w-0 flex-1 flex-col md:flex-row md:items-center gap-0.5 md:gap-1.5">
            <span className="flex min-w-0 items-center gap-1.5">
              {/* Title — desktop truncates on a single line; mobile allows
                  a 2-line clamp so longer titles stay readable. */}
              <span className="truncate md:truncate text-[15px] md:text-sm leading-snug line-clamp-2 md:line-clamp-none">
                {issue.title}
              </span>
              {showChildProgress && (
                <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5">
                  <ProgressRing done={childProgress!.done} total={childProgress!.total} size={14} />
                  <span className="text-[11px] text-muted-foreground tabular-nums font-medium">
                    {childProgress!.done}/{childProgress!.total}
                  </span>
                </span>
              )}
              {showLabels && (
                <span className="ml-1.5 hidden md:inline-flex shrink-0 items-center gap-1 max-w-[260px] overflow-hidden">
                  {labels.slice(0, 3).map((label) => (
                    <LabelChip key={label.id} label={label} />
                  ))}
                  {labels.length > 3 && (
                    <span className="text-[11px] text-muted-foreground">
                      +{labels.length - 3}
                    </span>
                  )}
                </span>
              )}
            </span>
            {/* Mobile-only secondary line: identifier + small icons that
                give scanability without crowding the title. */}
            <span className="md:hidden flex items-center gap-1.5 text-[11px] text-muted-foreground tabular-nums">
              <PriorityIcon priority={issue.priority} className="h-3 w-3 shrink-0" />
              <span className="shrink-0">{issue.identifier}</span>
              {showDueDate && (
                <span className="shrink-0">· {formatDate(issue.due_date!)}</span>
              )}
              {showProject && (
                <span className="inline-flex shrink-0 items-center gap-1 max-w-[160px]">
                  <span>·</span>
                  <ProjectIcon project={project} size="sm" />
                  <span className="truncate">{project!.title}</span>
                </span>
              )}
            </span>
          </span>
          {/* Desktop-only meta blocks (already had md:hidden equivalents above). */}
          {showProject && (
            <span className="hidden md:inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground max-w-[140px]">
              <ProjectIcon project={project} size="sm" />
              <span className="truncate">{project!.title}</span>
            </span>
          )}
          {showStartDate && (
            <span className="shrink-0 text-xs text-muted-foreground">
              {formatDate(issue.start_date!)}
            </span>
          )}
          {showDueDate && (
            <span className="hidden md:inline shrink-0 text-xs text-muted-foreground">
              {formatDate(issue.due_date!)}
            </span>
          )}
          {showAssignee && (
            <>
              {/* Larger tap target on mobile (24px), unchanged on desktop. */}
              <span className="md:hidden">
                <ActorAvatar
                  actorType={issue.assignee_type!}
                  actorId={issue.assignee_id!}
                  size={24}
                  enableHoverCard
                />
              </span>
              <span className="hidden md:inline">
                <ActorAvatar
                  actorType={issue.assignee_type!}
                  actorId={issue.assignee_id!}
                  size={20}
                  enableHoverCard
                />
              </span>
            </>
          )}
        </AppLink>
      </div>
    </IssueActionsContextMenu>
  );
});
