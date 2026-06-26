"use client";

import type { WorkflowNode, WorkflowNodeRun } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { NodeRunStatusIcon } from "./node-run-status-icon";
import { Bot, User, Building2, Paperclip } from "lucide-react";
import { useT } from "@multica/views/i18n";

export interface RuntimeNodeCardProps {
  node: WorkflowNode;
  nodeRun: WorkflowNodeRun | null;
  workerName: string | null;
  criticName: string | null;
  onClick: (nodeId: string) => void;
  isSelected?: boolean;
  elementRef?: (el: HTMLButtonElement | null) => void;
}

/** Maps worker/critic type to its Lucide icon component. */
function typeIcon(t: string) {
  if (t === "agent") return Bot;
  if (t === "squad") return Building2;
  return User; // "human" or fallback
}

export function RuntimeNodeCard({
  node,
  nodeRun,
  workerName,
  criticName,
  onClick,
  isSelected = false,
  elementRef,
}: RuntimeNodeCardProps) {
  const { t } = useT("issues");
  const hasWorkerOutput = nodeRun?.worker_output != null;
  const hasCriticOutput = nodeRun?.critic_output != null;

  // Build artifact names from outputs — use output labels as artifact descriptors
  const artifactNames: string[] = [];
  if (hasWorkerOutput) {
    artifactNames.push(t(($) => $.execution.detail_panel.worker_output));
  }
  if (hasCriticOutput) {
    artifactNames.push(t(($) => $.execution.detail_panel.critic_output));
  }

  const WorkerIcon = typeIcon(node.worker_type);

  return (
    <button
      type="button"
      data-testid={`runtime-node-card-${node.id}`}
      ref={elementRef}
      aria-pressed={isSelected}
      onClick={() => onClick(node.id)}
      className={cn(
        "flex min-w-[240px] min-h-[104px] flex-col gap-2 rounded-lg border border-border/80 bg-background p-3 text-left shadow-[0_1px_2px_rgba(15,23,42,0.06)]",
        "transition-all hover:-translate-y-0.5 hover:border-primary/45 hover:shadow-md",
        isSelected &&
          "border-primary/55 shadow-[inset_0_0_0_1px_rgba(59,130,246,0.08),0_2px_12px_rgba(15,23,42,0.06)]",
      )}
    >
      {/* Row 1: node title + status icon */}
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium truncate">{node.title}</span>
        {nodeRun ? (
          <NodeRunStatusIcon status={nodeRun.status} className="h-4 w-4" />
        ) : (
          <NodeRunStatusIcon status="pending" className="h-4 w-4" />
        )}
      </div>

      {/* Row 2: Worker (type icon + label + name + status) */}
      <div className="flex items-center gap-2 h-6 text-[11px] text-muted-foreground">
        <WorkerIcon className="h-3 w-3 shrink-0" />
        <span className="font-medium">{t(($) => $.execution.card.worker_label)}:</span>
        <span className={cn(!workerName && "italic")}>
          {workerName ?? "--"}
        </span>
        {nodeRun && (
          <NodeRunStatusIcon status={nodeRun.status} className="h-3.5 w-3.5 shrink-0 ml-auto" />
        )}
      </div>

      {/* Row 3: Critic (only when configured; type icon + label + name) */}
      {(node.critic_type || node.critic_id) && (
        <div className="flex items-center gap-2 h-6 text-[11px] text-muted-foreground">
          {node.critic_type === "agent" ? (
            <Bot className="h-3 w-3 shrink-0" />
          ) : node.critic_type === "squad" ? (
            <Building2 className="h-3 w-3 shrink-0" />
          ) : (
            <User className="h-3 w-3 shrink-0" />
          )}
          <span className="font-medium">{t(($) => $.execution.card.critic_label)}:</span>
          <span className={cn(!criticName && "italic")}>
            {criticName ?? "--"}
          </span>
        </div>
      )}

      {/* Row 4: Artifact names (only when artifacts exist) */}
      {artifactNames.length > 0 && (
        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <Paperclip className="h-3 w-3 shrink-0" />
          <span className="truncate">
            {t(($) => $.execution.card.artifacts_label)}: {artifactNames.join(", ")}
          </span>
        </div>
      )}
    </button>
  );
}
