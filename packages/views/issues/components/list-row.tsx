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
import { ProgressRing } from "./progress-ring";
import { IssueActionsContextMenu } from "../actions";
import { LabelChip } from "../../labels/label-chip";
import { useLocale } from "@multica/i18n/react";

export interface ChildProgress {
  done: number;
  total: number;
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
  const { locale } = useLocale();
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
  const showDueDate = storeProperties.dueDate && issue.due_date;
  const showLabels = storeProperties.labels && labels.length > 0;

  return (
    <IssueActionsContextMenu issue={issue}>
      <div
        className={`group/row flex h-9 items-center gap-2 px-4 text-sm transition-colors hover:not-data-[popup-open]:bg-accent/60 data-[popup-open]:bg-accent ${
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
          {showProject && (
            <span className="inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground max-w-[140px]">
              <ProjectIcon project={project} size="sm" />
              <span className="truncate">{project!.title}</span>
            </span>
          )}
          {showDueDate && (
            <span className="shrink-0 text-xs text-muted-foreground">
              {new Date(issue.due_date!).toLocaleDateString(locale, { month: "short", day: "numeric" })}
            </span>
          )}
          {showAssignee && (
            <ActorAvatar
              actorType={issue.assignee_type!}
              actorId={issue.assignee_id!}
              size={20}
              enableHoverCard
            />
          )}
        </AppLink>
      </div>
    </IssueActionsContextMenu>
  );
});
