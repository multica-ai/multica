"use client";

import { memo } from "react";
import type { NodeProps } from "@xyflow/react";
import { AlertTriangle, CircleHelp, Loader2 } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { issueGraphReadiness } from "@multica/core/api";
import type { IssueStatusCategory } from "@multica/core/types";
import type { DagIssue } from "@multica/core/issues/dag";
import { IssueCostBadge } from "../components/issue-cost-badge";
import { PriorityIcon } from "../components/priority-icon";
import { StatusIcon } from "../components/status-icon";
import { ActorAvatar } from "../../common/actor-avatar";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";
import { DAG_NODE_HEIGHT, DAG_NODE_WIDTH } from "./dag-constants";
import type { DagTaskFlowNode } from "./dag-scene-flow";

export function dagTaskRunState(issue: DagIssue): "running" | "queued" | "none" {
  const run = issue.runSummary;
  if (!run) return "none";
  if (run.running > 0) return "running";
  return run.queued > 0 || run.dispatched > 0 || run.waitingLocalDirectory > 0
    ? "queued"
    : "none";
}

export function DagRunBadge({ issue }: { issue: DagIssue }) {
  const { t } = useT("dag");
  const runState = dagTaskRunState(issue);
  if (runState === "none") return null;
  const running = runState === "running";
  return (
    <span
      className={cn(
        "inline-flex min-w-0 items-center gap-1 overflow-hidden rounded-full px-1.5 py-0.5 text-micro font-medium",
        running ? "bg-brand/10 text-brand" : "bg-muted/70 text-muted-foreground",
      )}
      title={running ? t(($) => $.run_active) : t(($) => $.run_queued)}
    >
      <Loader2 className={cn("size-3 shrink-0", running && "animate-spin")} />
      <span className="truncate">{running ? t(($) => $.run_active) : t(($) => $.run_queued)}</span>
    </span>
  );
}

function DependencyBadge({ issue }: { issue: DagIssue }) {
  const { t } = useT("dag");
  const readiness = issueGraphReadiness(issue.dependencySummary ?? undefined);
  if (readiness === "ready") return null;
  if (readiness === "unknown") {
    return (
      <span
        className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/70 px-1.5 py-0.5 text-micro text-muted-foreground"
        title={t(($) => $.readiness_unknown)}
      >
        <CircleHelp className="size-3" />
        {t(($) => $.readiness_unknown)}
      </span>
    );
  }
  const summary = issue.dependencySummary;
  const restricted = summary?.hasRestrictedBlockers === true;
  return (
    <span
      className="inline-flex shrink-0 items-center gap-1 rounded-full bg-warning/10 px-1.5 py-0.5 text-micro font-medium text-warning"
      title={restricted ? t(($) => $.blocked_badge_restricted) : undefined}
    >
      <AlertTriangle className="size-3" />
      {t(($) => $.blocked_badge, {
        count: summary?.visibleUnsatisfiedCount ?? 0,
      })}
    </span>
  );
}

/**
 * The 248×116 task card, unchanged in content from the previous canvas.
 * Finished tasks fade out (OL-183 comparison ③); context and
 * cancelled-still-blocking cards keep the dashed border.
 */
export const DagTaskCard = memo(
  function DagTaskCard({ data }: NodeProps<DagTaskFlowNode>) {
    const { t } = useT("dag");
    const { task } = data;
    const issue: DagIssue = task.issue;
    const statusCategory = (issue.statusCategory as IssueStatusCategory) || "unstarted";
    const done = statusCategory === "done";
    if (data.simplified) {
      // Zoomed out past readability: the edge layer paints this card as an
      // SVG rect; the DOM node stays as the interaction shell (OL-183 §7).
      return (
        <div
          className="nopan h-full w-full"
          style={{ width: DAG_NODE_WIDTH, height: DAG_NODE_HEIGHT }}
          data-dag-task={issue.identifier}
        />
      );
    }
    return (
      <div
        className={cn(
          "nopan flex h-full w-full flex-col gap-1 rounded-lg border bg-card px-2.5 py-2 text-left shadow-xs transition-opacity",
          "border-border hover:border-foreground/30",
          done && "opacity-55",
          (task.role === "context" || task.cancelledBlocking) && "border-dashed",
        )}
        style={{ width: DAG_NODE_WIDTH, height: DAG_NODE_HEIGHT }}
        data-dag-task={issue.identifier}
      >
        <div className="flex min-w-0 items-center gap-1.5">
          <PriorityIcon priority={issue.priority} className="size-3.5 shrink-0" />
          <span className="shrink-0 text-caption text-muted-foreground tabular-nums">
            {issue.identifier}
          </span>
          <StatusIcon
            status={issue.status}
            category={statusCategory}
            color={data.statusColor}
            className="size-3.5 shrink-0"
          />
          <IssueCostBadge issueIds={issue.id} />
          <DagRunBadge issue={issue} />
          {task.cancelledBlocking ? (
            <span className="ml-auto min-w-0 truncate rounded-full bg-warning/10 px-1.5 py-0.5 text-micro font-medium text-warning">
              {t(($) => $.cancelled_blocking)}
            </span>
          ) : (
            task.role === "context" && (
              <span className="ml-auto min-w-0 truncate rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground">
                {t(($) => $.context_badge)}
              </span>
            )
          )}
        </div>
        <div className="line-clamp-2 text-body leading-snug">{issue.title}</div>
        <div className="mt-auto flex items-center gap-1.5">
          {issue.assignee ? (
            <ActorAvatar actorType={issue.assignee.type} actorId={issue.assignee.id} size="xs" />
          ) : null}
          {data.showStage && issue.stage != null && (
            <span className="shrink-0 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground tabular-nums">
              {t(($) => $.stage_badge, { number: issue.stage })}
            </span>
          )}
          {data.projectTitle && (
            <span className="inline-flex min-w-0 shrink items-center gap-1 text-micro text-muted-foreground">
              <ProjectIcon project={null} size="sm" />
              <span className="truncate">{data.projectTitle}</span>
            </span>
          )}
          <DependencyBadge issue={issue} />
        </div>
      </div>
    );
  },
  (prev, next) => prev.data === next.data,
);
