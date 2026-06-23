"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

export interface CriticBadgeProps {
  node: WorkflowNode;
  criticAgent: Agent | null;
  onClick: (nodeId: string) => void;
}

export function CriticBadge({ node, criticAgent, onClick }: CriticBadgeProps) {
  const displayName = criticAgent?.name ?? node.title;

  return (
    <button
      type="button"
      data-testid={`critic-badge-${node.id}`}
      onClick={() => onClick(node.id)}
      className={cn(
        "flex shrink-0 flex-col gap-1 rounded-md border-2 border-dashed border-[var(--warning)]/50 bg-[var(--warning)]/5 p-2 text-left transition-colors min-w-[140px]",
        "hover:bg-[var(--warning)]/10 hover:border-[var(--warning)]/70",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
      )}
    >
      <span className="text-[10px] text-muted-foreground uppercase tracking-wider">
        Critic
      </span>
      <span className="text-xs font-medium truncate">{displayName}</span>
      {criticAgent?.model && (
        <span className="text-[10px] text-muted-foreground/60 truncate">
          {criticAgent.model}
        </span>
      )}
    </button>
  );
}
