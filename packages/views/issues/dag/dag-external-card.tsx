"use client";

import { memo } from "react";
import type { NodeProps } from "@xyflow/react";
import { LogOut as ArrowRightFromLine, ArrowRightToLine } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { DAG_EXTERNAL_SIZE } from "./dag-constants";
import { useDagCanvasActions } from "./dag-canvas-actions";
import type { DagExternalFlowNode } from "./dag-scene-flow";

/**
 * The dashed side card compressing this line's dependencies on another line
 * (or standalone task): which line, how many dependencies, and which way they
 * point. Clicking jumps there — into the line, or back to the overview for a
 * standalone task.
 */
export const DagExternalCard = memo(
  function DagExternalCard({ data }: NodeProps<DagExternalFlowNode>) {
    const { t } = useT("dag");
    const actions = useDagCanvasActions();
    const { group, side } = data;
    if (data.simplified) {
      // Zoomed out past readability: the edge layer paints this card as an
      // SVG rect; the DOM node stays as the interaction shell — the jump on
      // click keeps working (OL-183 §7).
      return (
        <button
          type="button"
          className="nopan block h-full w-full cursor-pointer"
          style={{ width: DAG_EXTERNAL_SIZE.width, height: DAG_EXTERNAL_SIZE.height }}
          data-dag-external={group.id}
          data-dag-external-side={side}
          aria-label={t(($) => $.external_jump_hint)}
          onClick={() => actions?.onExternalJump(group, side)}
        />
      );
    }
    return (
      <button
        type="button"
        className={cn(
          "nopan flex h-full w-full cursor-pointer flex-col justify-center gap-0.5 rounded-lg border border-dashed border-muted-foreground/45 bg-muted/20 px-2.5 py-2 text-left hover:border-foreground/50 hover:bg-muted/40 outline-offset-2 focus-visible:outline-2 focus-visible:outline-brand",
        )}
        style={{ width: DAG_EXTERNAL_SIZE.width, height: DAG_EXTERNAL_SIZE.height }}
        data-dag-external={group.id}
        data-dag-external-side={side}
        title={t(($) => $.external_jump_hint)}
        onClick={() => actions?.onExternalJump(group, side)}
      >
        <div className="flex items-center gap-1.5 text-micro text-muted-foreground">
          {side === "upstream" ? (
            <ArrowRightToLine className="size-3 shrink-0" aria-hidden />
          ) : (
            <ArrowRightFromLine className="size-3 shrink-0" aria-hidden />
          )}
          <span className="truncate">
            {side === "upstream"
              ? t(($) => $.external_from)
              : t(($) => $.external_to)}
          </span>
          <span className="shrink-0 tabular-nums">
            {t(($) => $.edge_count, { count: group.count })}
          </span>
        </div>
        <div className="line-clamp-2 text-caption font-medium leading-snug text-foreground">
          <span className="mr-1.5 text-muted-foreground tabular-nums">
            {group.issue.identifier}
          </span>
          {group.issue.title}
        </div>
      </button>
    );
  },
  (prev, next) => prev.data === next.data,
);
