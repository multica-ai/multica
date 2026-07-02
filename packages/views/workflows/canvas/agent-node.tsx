"use client";

import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import type { WorkflowNode } from "@multica/core/types/workflow";
import { cn } from "@multica/ui/lib/utils";

const STATUS_STYLES: Record<string, { border: string; bg: string; label: string }> = {
  pending:   { border: "border-dashed border-muted-foreground", bg: "bg-muted/30", label: "Pending" },
  queued:    { border: "border-blue-500",                     bg: "bg-blue-500/10", label: "Queued" },
  running:   { border: "border-blue-500 animate-pulse",       bg: "bg-blue-500/10", label: "Running" },
  completed: { border: "border-green-500",                    bg: "bg-green-500/10", label: "Completed" },
  failed:    { border: "border-red-500",                      bg: "bg-red-500/10", label: "Failed" },
  skipped:   { border: "border-muted-foreground",            bg: "bg-muted", label: "Skipped" },
};

function AgentNode({ data, selected }: NodeProps) {
  const nodeData = data as unknown as WorkflowNode;
  const status = nodeData.status ?? "pending";
  const style = STATUS_STYLES[status] ?? { border: "border-muted-foreground", bg: "bg-muted", label: status };

  return (
    <div
      className={cn(
        "min-w-[200px] max-w-[280px] rounded-lg border-2 p-3 shadow-sm transition-all",
        style.border,
        style.bg,
        selected && "ring-2 ring-primary"
      )}
    >
      <Handle type="target" position={Position.Left} className="!w-2 !h-2" />
      <div className="flex items-center justify-between mb-2">
        <span className="font-medium text-sm truncate">{nodeData.title}</span>
        <span className={cn(
          "text-xs px-1.5 py-0.5 rounded-full",
          status === "completed" && "bg-green-500/20 text-green-600",
          status === "failed" && "bg-red-500/20 text-red-600",
          status === "running" && "bg-blue-500/20 text-blue-600",
          status === "pending" && "bg-muted text-muted-foreground",
        )}>
          {style.label}
        </span>
      </div>
      <p className="text-xs text-muted-foreground line-clamp-2">
        {nodeData.prompt || "No prompt set"}
      </p>
      <Handle type="source" position={Position.Right} className="!w-2 !h-2" />
    </div>
  );
}

export const AgentNodeComponent = memo(AgentNode);
