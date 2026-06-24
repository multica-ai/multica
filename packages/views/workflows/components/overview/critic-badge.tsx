"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { ArrowUpRight, ShieldAlert } from "lucide-react";

export interface CriticBadgeProps {
  node: WorkflowNode;
  criticAgent: Agent | null;
  onClick: (nodeId: string, focus: "critic") => void;
  isSelected?: boolean;
}

export function CriticBadge({ node, criticAgent, onClick, isSelected = false }: CriticBadgeProps) {
  const displayName = criticAgent?.name ?? node.title;

  return (
    <button
      type="button"
      data-testid={`critic-badge-${node.id}`}
      onClick={() => onClick(node.id, "critic")}
      className={cn(
        "group flex min-h-[96px] min-w-[168px] shrink-0 flex-col gap-2 rounded-xl border-2 border-dashed border-[var(--warning)]/45 bg-[var(--warning)]/6 p-3 text-left transition-all duration-150",
        "hover:-translate-y-0.5 hover:border-[var(--warning)]/75 hover:bg-[var(--warning)]/11 hover:shadow-[0_10px_24px_rgba(245,158,11,0.12)]",
        "active:translate-y-0 active:scale-[0.99]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected && "border-[var(--warning)]/85 bg-[var(--warning)]/12 ring-1 ring-[var(--warning)]/20",
      )}
      aria-pressed={isSelected}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <span className="inline-flex items-center gap-1 rounded-full bg-[var(--warning)]/12 px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] text-amber-800">
            <ShieldAlert className="h-3 w-3" strokeWidth={1.9} />
            Critic
          </span>
          <span className="mt-2 block truncate text-sm font-semibold text-foreground">{displayName}</span>
        </div>
        <span className="rounded-full border border-[var(--warning)]/25 bg-background/75 p-1 text-amber-700 transition-colors group-hover:border-[var(--warning)]/45 group-hover:text-amber-900">
          <ArrowUpRight className="h-3.5 w-3.5" strokeWidth={1.9} />
        </span>
      </div>
      {criticAgent?.model && (
        <span className="mt-auto truncate text-[10px] text-muted-foreground/70">
          {criticAgent.model}
        </span>
      )}
    </button>
  );
}
