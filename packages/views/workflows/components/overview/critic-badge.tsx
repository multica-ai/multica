"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { ShieldAlert } from "lucide-react";

export interface CriticBadgeProps {
  node: WorkflowNode;
  criticAgent: Agent | null;
  onClick: (nodeId: string, focus: "critic") => void;
  isSelected?: boolean;
  elementRef?: (el: HTMLButtonElement | null) => void;
}

export function CriticBadge({
  node,
  criticAgent,
  onClick,
  isSelected = false,
  elementRef,
}: CriticBadgeProps) {
  const displayName = criticAgent?.name ?? node.title;

  return (
    <button
      type="button"
      data-testid={`critic-badge-${node.id}`}
      onClick={() => onClick(node.id, "critic")}
      ref={elementRef}
      className={cn(
        "group flex h-14 w-40 shrink-0 flex-col gap-1 rounded-lg border-2 border-dashed border-amber-400/80 bg-amber-50 p-2 text-left shadow-[0_1px_2px_rgba(180,83,9,0.08)] transition-all duration-150",
        "hover:-translate-y-0.5 hover:border-[var(--warning)]/75 hover:bg-[var(--warning)]/11 hover:shadow-[0_8px_18px_rgba(245,158,11,0.10)]",
        "active:translate-y-0 active:scale-[0.99]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected &&
          "border-[var(--warning)]/85 bg-[var(--warning)]/12 ring-1 ring-[var(--warning)]/20",
      )}
      aria-pressed={isSelected}
    >
      <span className="inline-flex items-center gap-1 rounded-full bg-[var(--warning)]/12 px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] text-amber-800">
        <ShieldAlert className="h-3 w-3" strokeWidth={1.9} />
        Critic
      </span>
      <span className="block truncate text-xs font-semibold text-foreground">
        {displayName}
      </span>
    </button>
  );
}
