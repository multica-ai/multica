"use client";

import { memo } from "react";
import type { NodeProps } from "@xyflow/react";
import { ArrowRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import type { IssueStatusCategory } from "@multica/core/types";
import { useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { StatusIcon } from "../components/status-icon";
import { useT } from "../../i18n";
import { DAG_GROUP_HEADER_HEIGHT, DAG_STAGE_HEADER_HEIGHT } from "./dag-constants";
import type { DagFrameFlowNode } from "./dag-scene-flow";
import { DagRunBadge } from "./dag-task-card";

/**
 * A task-line frame: the outer box is the line itself, sub-lines nest inside.
 * The header shows the parent issue's status, identifier, title and progress.
 * The body is pointer-transparent so empty space behaves like the pane; the
 * header selects the parent issue. Stage columns render below the header when
 * the layout could keep them dependency-compatible, otherwise tasks carry
 * stage pills.
 */
export const DagFrameNode = memo(
  function DagFrameNode({ data }: NodeProps<DagFrameFlowNode>) {
    const { t } = useT("dag");
    const storeApi = useViewStoreApi();
    const { frame, box } = data;
    const { issue } = frame;
    const statusCategory = (issue.statusCategory as IssueStatusCategory) || "unstarted";
    if (data.simplified) {
      // Zoomed out past readability: the edge layer paints this frame as an
      // SVG rect; the DOM node stays as the interaction shell (OL-183 §7).
      return <div className="h-full w-full" data-dag-frame={frame.id} />;
    }
    return (
      <div
        className={cn(
          "pointer-events-none relative h-full w-full rounded-lg border",
          "border-muted-foreground/25",
        )}
        data-dag-frame={frame.id}
      >
        <button
          type="button"
          className="nopan pointer-events-auto absolute left-0 top-0 z-10 flex w-full items-center gap-2 rounded-t-lg bg-card/80 px-3 py-2 text-left outline-offset-2 focus-visible:outline-2 focus-visible:outline-brand"
          style={{ height: DAG_GROUP_HEADER_HEIGHT - 2 }}
          aria-label={`${issue.identifier} ${issue.title}`}
          data-dag-frame-header={frame.id}
          onClick={() => storeApi.getState().setDagSelectedNodeId(frame.id)}
        >
          <span data-dag-parent-status={issue.status}>
            <StatusIcon
              status={issue.status}
              category={statusCategory}
              color={data.statusColor}
              className="size-4 shrink-0"
            />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-body font-medium" title={issue.title}>
              <span className="mr-2 text-caption font-normal text-muted-foreground">
                {issue.identifier}
              </span>
              {issue.title}
            </span>
            <span className="mt-1 flex items-center gap-2 text-micro text-muted-foreground">
              {data.projectTitle && (
                <span className="max-w-32 truncate">{data.projectTitle}</span>
              )}
              <span className="shrink-0">
                {t(($) => $.group_progress, {
                  done: frame.progress.completed,
                  count: frame.progress.total,
                })}
              </span>
              <span
                aria-hidden
                className="h-1 w-12 shrink-0 overflow-hidden rounded-full bg-muted"
              >
                <span
                  className="block h-full bg-info/60"
                  style={{
                    width: `${frame.progress.total ? (frame.progress.completed / frame.progress.total) * 100 : 0}%`,
                  }}
                />
              </span>
            </span>
          </span>
          <DagRunBadge issue={issue} />
        </button>
        {box.stageMode === "columns" && box.bands.length > 0 && (
          <div
            className="absolute inset-x-0 bottom-0 overflow-hidden rounded-b-lg border-t"
            style={{ top: DAG_GROUP_HEADER_HEIGHT }}
          >
            {box.bands.map((band, i) => (
              <div
                key={band.stage}
                data-dag-stage={band.stage}
                className={cn(
                  "absolute border-l border-muted-foreground/25 first:border-l-0",
                  i % 2 ? "bg-muted/25" : "bg-card",
                )}
                style={{ left: band.start, top: 0, width: band.end - band.start, height: "100%" }}
              >
                <div
                  className="relative flex items-center border-b bg-muted/45 px-4 text-body font-semibold text-foreground"
                  style={{ height: DAG_STAGE_HEADER_HEIGHT }}
                >
                  {t(($) => $.stage_badge, { number: band.stage })}
                  {i < box.bands.length - 1 && (
                    <ArrowRight
                      className="absolute -right-1.5 size-3 text-faint-foreground"
                      aria-hidden
                    />
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    );
  },
  (prev, next) => prev.data === next.data,
);
