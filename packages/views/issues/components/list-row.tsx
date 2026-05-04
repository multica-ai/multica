"use client";

import { memo } from "react";
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
import { issueListOptions } from "@multica/core/issues/queries";
import { ProjectIcon } from "../../projects/components/project-icon";
import { PriorityIcon } from "./priority-icon";
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
  indentLevel = 0,
  hasChildren = false,
  collapsed = false,
  onToggleCollapsed,
  isOrphan = false,
}: {
  issue: Issue;
  childProgress?: ChildProgress;
  /** Visual indentation level. 0 = top-level, 1 = nested under a parent. */
  indentLevel?: number;
  /** Whether the row has visible sub-issues to expand into. */
  hasChildren?: boolean;
  /** When true, the sub-issue group is hidden. */
  collapsed?: boolean;
  /** Click handler for the expand/collapse chevron. */
  onToggleCollapsed?: () => void;
  /**
   * True when this row is itself a child whose parent is filtered out of
   * the current view, so the row appears at the top level. We surface a
   * subtle "in PARENT-ID" chip so the user understands the relationship
   * without having to open the issue.
   */
  isOrphan?: boolean;
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
  // Used only to resolve the parent's identifier for the orphan chip.
  // Reads the same cache the page already populated, so no extra fetch.
  const { data: allIssues = [] } = useQuery({
    ...issueListOptions(wsId),
    enabled: isOrphan && !!issue.parent_issue_id,
  });
  const project = issue.project_id ? projects.find((pr) => pr.id === issue.project_id) : undefined;
  const labels = issue.labels ?? [];
  const orphanParent =
    isOrphan && issue.parent_issue_id
      ? allIssues.find((i) => i.id === issue.parent_issue_id)
      : undefined;

  const showProject = storeProperties.project && project;
  const showChildProgress = storeProperties.childProgress && childProgress;
  const showAssignee = storeProperties.assignee && issue.assignee_type && issue.assignee_id;
  const showDueDate = storeProperties.dueDate && issue.due_date;
  const showLabels = storeProperties.labels && labels.length > 0;

  // Indent in pixels per level — keeps alignment with the existing 16px
  // priority-icon column. 24px per level matches the visual rhythm of the
  // sub-issues section on the issue detail page.
  const indentPx = indentLevel * 24;

  return (
    <IssueActionsContextMenu issue={issue}>
      <div
        className={`group/row flex h-9 items-center gap-2 px-4 text-sm transition-colors hover:not-data-[popup-open]:bg-accent/60 data-[popup-open]:bg-accent ${
          selected ? "bg-accent/30" : ""
        }`}
        style={indentPx ? { paddingLeft: `${16 + indentPx}px` } : undefined}
      >
        {/*
         * Chevron column.
         * - Parent with children: visible toggle.
         * - Anything else: keep the column reserved (so titles align across rows)
         *   but render an empty spacer.
         */}
        {hasChildren && onToggleCollapsed ? (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onToggleCollapsed();
            }}
            className="flex shrink-0 items-center justify-center w-4 h-4 rounded text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
            aria-expanded={!collapsed}
            aria-label={collapsed ? "Expand sub-issues" : "Collapse sub-issues"}
          >
            <ChevronRight
              className={`size-3.5 transition-transform ${collapsed ? "" : "rotate-90"}`}
            />
          </button>
        ) : (
          <span aria-hidden className="shrink-0 w-4 h-4" />
        )}
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
            {orphanParent && (
              <span
                className="inline-flex shrink-0 items-center gap-1 rounded bg-muted/40 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground"
                title={`Sub-issue of ${orphanParent.identifier} ${orphanParent.title}`}
              >
                in {orphanParent.identifier}
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
              {formatDate(issue.due_date!)}
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
