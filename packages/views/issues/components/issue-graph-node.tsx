"use client";

import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import {
  ArrowDownRight,
  ArrowUpLeft,
  GitBranch,
  Layers3,
} from "lucide-react";
import type { Issue } from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { PriorityIcon } from "./priority-icon";
import { ProgressRing } from "./progress-ring";
import { StatusIcon } from "./status-icon";
import {
  ISSUE_GRAPH_NODE_HEIGHT,
  ISSUE_GRAPH_NODE_WIDTH,
} from "./issue-graph-layout";

export interface IssueGraphNodeData extends Record<string, unknown> {
  issue: Issue;
  childCount: number;
  childProgress?: { done: number; total: number };
  contextual: boolean;
  initialHighlight: "root" | "chain" | null;
  initialSpotlightMuted: boolean;
  relation: "selected" | "ancestor" | "descendant" | "unrelated" | "none";
  relationLabel?: string;
}

export const IssueGraphNode = memo(function IssueGraphNode({
  data,
}: NodeProps) {
  const {
    issue,
    childCount,
    childProgress,
    contextual,
    initialHighlight,
    initialSpotlightMuted,
    relation,
    relationLabel,
  } = data as unknown as IssueGraphNodeData;
  const hasSelection = relation !== "none";
  const unrelated = relation === "unrelated";
  const initialRoot = initialHighlight === "root";
  const initialChain = initialHighlight === "chain";
  const statusConfig = STATUS_CONFIG[issue.status];

  return (
    <div
      className={cn(
        "group relative overflow-hidden rounded-xl border bg-card/95 text-card-foreground shadow-[0_12px_30px_-20px_rgba(0,0,0,0.55)] backdrop-blur-sm transition-[border-color,box-shadow,opacity,transform] duration-300",
        "hover:-translate-y-0.5 hover:border-foreground/20 hover:shadow-[0_18px_38px_-20px_rgba(0,0,0,0.65)]",
        statusConfig.columnBg,
        relation === "selected" &&
          "border-brand/70 ring-1 ring-brand/25 shadow-xl shadow-brand/15",
        relation === "ancestor" &&
          "border-info/55 bg-info/5 shadow-[0_16px_36px_-24px_var(--info)]",
        relation === "descendant" &&
          "border-success/55 bg-success/5 shadow-[0_16px_36px_-24px_var(--success)]",
        initialRoot &&
          "border-warning/70 bg-warning/5 outline outline-1 outline-offset-2 outline-warning/25 shadow-[0_14px_34px_-26px_var(--warning)]",
        initialChain &&
          "border-warning/45 bg-warning/5 shadow-[0_12px_30px_-26px_var(--warning)]",
        contextual && !hasSelection && !initialHighlight && "opacity-45",
        initialSpotlightMuted && !hasSelection && "opacity-30 saturate-75",
        unrelated && "opacity-20 saturate-50",
      )}
      style={{
        width: ISSUE_GRAPH_NODE_WIDTH,
        height: ISSUE_GRAPH_NODE_HEIGHT,
      }}
    >
      <div
        className={cn(
          "absolute inset-y-0 left-0 w-0.5 bg-border transition-colors",
          relation === "selected" && "bg-brand",
          relation === "ancestor" && "bg-info",
          relation === "descendant" && "bg-success",
          initialHighlight && relation === "none" && "bg-warning",
          !initialHighlight && relation === "none" && statusConfig.dividerColor,
          relation === "none" && "group-hover:bg-foreground/30",
        )}
      />
      <div className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-foreground/10 to-transparent" />

      <Handle
        type="target"
        position={Position.Left}
        className="!size-2 !border-2 !border-background !bg-muted-foreground/40"
      />
      <Handle
        type="source"
        position={Position.Right}
        className="!size-2 !border-2 !border-background !bg-brand/70"
      />

      <div className="flex h-full flex-col px-3.5 py-3">
        <div className="flex items-center gap-2">
          <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
          <span className="text-[11px] font-medium tracking-wide text-muted-foreground">
            {issue.identifier}
          </span>
          <div className="ml-auto flex items-center gap-1.5 text-muted-foreground">
            {relation === "ancestor" && (
              <span className="inline-flex items-center gap-1 rounded-full bg-info/10 px-1.5 py-0.5 text-[9px] font-medium text-info">
                <ArrowUpLeft className="size-2.5" />
                {relationLabel}
              </span>
            )}
            {relation === "descendant" && (
              <span className="inline-flex items-center gap-1 rounded-full bg-success/10 px-1.5 py-0.5 text-[9px] font-medium text-success">
                <ArrowDownRight className="size-2.5" />
                {relationLabel}
              </span>
            )}
            {issue.parent_issue_id && <GitBranch className="size-3" />}
            {childCount > 0 && (
              <span className="inline-flex items-center gap-1 text-[10px] tabular-nums">
                <Layers3 className="size-3" />
                {childCount}
              </span>
            )}
          </div>
        </div>

        <p className="mt-2 line-clamp-2 text-[13px] font-medium leading-[1.35]">
          {issue.title}
        </p>

        <div className="mt-auto flex min-h-5 items-center gap-2">
          <PriorityIcon priority={issue.priority} className="size-3.5" />
          {childProgress && (
            <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground tabular-nums">
              <ProgressRing
                done={childProgress.done}
                total={childProgress.total}
                size={13}
              />
              {childProgress.done}/{childProgress.total}
            </span>
          )}
          <div className="ml-auto">
            {issue.assignee_type && issue.assignee_id && (
              <ActorAvatar
                actorType={issue.assignee_type}
                actorId={issue.assignee_id}
                size={20}
              />
            )}
          </div>
        </div>
      </div>
    </div>
  );
});
