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
        "group flex h-12 w-36 shrink-0 flex-col gap-0.5 rounded-md border border-dashed border-border/70 bg-muted/30 p-1.5 text-left shadow-none transition-all duration-150",
        "hover:-translate-y-0.5 hover:border-muted-foreground/30 hover:bg-muted/50",
        "active:translate-y-0 active:scale-[0.99]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected &&
          "border-primary/40 bg-accent/40 ring-1 ring-primary/15",
      )}
      aria-pressed={isSelected}
    >
      <span className="inline-flex items-center gap-1 rounded-full bg-muted-foreground/8 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
        <ShieldAlert className="h-2.5 w-2.5" strokeWidth={1.9} />
        Critic
      </span>
      <span className="block truncate text-xs font-semibold text-foreground">
        {displayName}
      </span>
    </button>
  );
}
