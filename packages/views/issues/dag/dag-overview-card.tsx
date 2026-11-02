"use client";

import { memo } from "react";
import type { NodeProps } from "@xyflow/react";
import { CirclePause, Play as CirclePlay, Eye, CircleAlert as OctagonAlert } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { dagProjectColor } from "./dag-project-colors";
import { DAG_OVERVIEW_SIZE } from "./dag-constants";
import type { DagOverviewFlowNode } from "./dag-scene-flow";
import type { IssueStatusCategory } from "@multica/core/types";
import { IssueCostBadge } from "../components/issue-cost-badge";
import { StatusIcon } from "../components/status-icon";
import { DagRunBadge } from "./dag-task-card";

/**
 * One card per task line / standalone task / independent group on the
 * overview. Cancelled blockers pulled out of hidden lines keep the dashed
 * context border and the "cancelled, still blocking" mark (OL-182).
 */
export const DagOverviewCardView = memo(
  function DagOverviewCardView({ data }: NodeProps<DagOverviewFlowNode>) {
    const { t } = useT("dag");
    const { resolvedTheme } = useTheme();
    const { card } = data;
    const color = dagProjectColor(card.project?.id, resolvedTheme === "dark");
    const issue = card.issue;
    const title = issue?.title ?? t(($) => $.independent_group);
    const finished = card.attention === "finished";
    if (data.simplified) {
      // Zoomed out past readability: the edge layer paints this card as an
      // SVG rect; the DOM node stays as the interaction shell (OL-183 §7).
      return (
        <div
          className="nopan h-full w-full"
          style={{ width: DAG_OVERVIEW_SIZE.width, height: DAG_OVERVIEW_SIZE.height }}
          data-dag-overview-card={card.id}
          data-dag-kind={card.kind}
        />
      );
    }
    const counts = [
      {
        key: "blocked" as const,
        value: card.unfinished.blocked,
        icon: <OctagonAlert className="size-3" />,
        className: "text-destructive",
        label: t(($) => $.count_blocked),
      },
      {
        key: "inProgress" as const,
        value: card.unfinished.inProgress,
        icon: <CirclePlay className="size-3" />,
        className: "text-warning",
        label: t(($) => $.count_in_progress),
      },
      {
        key: "review" as const,
        value: card.unfinished.review,
        icon: <Eye className="size-3" />,
        className: "text-success",
        label: t(($) => $.count_review),
      },
      {
        key: "unstarted" as const,
        value: card.unfinished.unstarted,
        icon: <CirclePause className="size-3" />,
        className: "text-muted-foreground",
        label: t(($) => $.count_unstarted),
      },
    ];
    return (
      <div
        className={cn(
          "nopan relative flex h-full w-full flex-col overflow-hidden rounded-lg border bg-card py-2 pl-3.5 pr-2.5 text-left shadow-xs",
          "border-border hover:border-foreground/30",
          card.cancelledBlocking && "border-dashed",
        )}
        style={{ width: DAG_OVERVIEW_SIZE.width, height: DAG_OVERVIEW_SIZE.height }}
        data-dag-overview-card={card.id}
        data-dag-kind={card.kind}
      >
        <span
          aria-hidden
          className="absolute inset-y-0 left-0 w-1.5"
          style={{ backgroundColor: color }}
          title={card.project?.title}
        />
        <div className="flex h-4 items-center gap-1.5 text-micro text-muted-foreground">
          {card.project && <span className="truncate">{card.project.title}</span>}
          {card.role === "context" && !card.cancelledBlocking && (
            <span className="ml-auto min-w-0 truncate rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground">
              {t(($) => $.context_badge)}
            </span>
          )}
        </div>
        <div className="mt-0.5 flex min-w-0 items-center gap-1.5">
          {issue && (
            <StatusIcon
              status={issue.status}
              category={(issue.statusCategory as IssueStatusCategory) || "unstarted"}
              color={data.statusColor}
              className="size-3.5 shrink-0"
            />
          )}
          {issue && (
            <span className="shrink-0 text-caption text-muted-foreground tabular-nums">
              {issue.identifier}
            </span>
          )}
          <IssueCostBadge issueIds={card.memberIds} />
          {issue && <DagRunBadge issue={issue} />}
          {card.cancelledBlocking && (
            <span className="ml-auto min-w-0 truncate rounded-full bg-warning/10 px-1.5 py-0.5 text-micro font-medium text-warning">
              {t(($) => $.cancelled_blocking)}
            </span>
          )}
        </div>
        <div
          className={cn(
            "mt-0.5 line-clamp-2 min-h-8 text-body leading-snug",
            finished && "text-muted-foreground",
          )}
        >
          {title}
        </div>
        <div className="mt-auto flex items-center gap-2">
          <span
            aria-hidden
            className="h-1 min-w-0 flex-1 overflow-hidden rounded-full bg-muted"
          >
            <span
              className="block h-full bg-info/60"
              style={{
                width: `${card.progress.total ? (card.progress.completed / card.progress.total) * 100 : 0}%`,
              }}
            />
          </span>
          <span className="shrink-0 text-micro text-muted-foreground tabular-nums">
            {t(($) => $.group_progress, {
              done: card.progress.completed,
              count: card.progress.total,
            })}
          </span>
        </div>
        <div className="mt-1 flex items-center gap-2.5">
          {counts.map((count) => (
            <span
              key={count.key}
              title={count.label}
              aria-label={`${count.label}: ${count.value}`}
              className={cn(
                "inline-flex items-center gap-1 text-micro tabular-nums",
                count.value > 0 ? count.className : "text-faint-foreground",
              )}
            >
              {count.icon}
              {count.value}
            </span>
          ))}
        </div>
      </div>
    );
  },
  (prev, next) => prev.data === next.data,
);
