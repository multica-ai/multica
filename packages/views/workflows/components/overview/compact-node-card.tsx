"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";

export interface CompactNodeCardProps {
  node: WorkflowNode;
  agent: Agent | null;
  plugin: BuiltinPlugin | null;
  onClick: (nodeId: string, focus: "worker") => void;
  isSelected?: boolean;
  elementRef?: (el: HTMLButtonElement | null) => void;
}

const statusDotColors: Record<string, string> = {
  working: "bg-[var(--success)]",
  idle: "bg-[var(--info)]",
  blocked: "bg-[var(--warning)]",
  error: "bg-destructive",
  offline: "bg-muted-foreground/40",
};

export function CompactNodeCard({
  node,
  agent,
  plugin,
  onClick,
  isSelected = false,
  elementRef,
}: CompactNodeCardProps) {
  const displayName = plugin?.name ?? node.title;

  return (
    <button
      type="button"
      data-testid={`compact-node-card-${node.id}`}
      onClick={() => onClick(node.id, "worker")}
      ref={elementRef}
      className={cn(
        "group flex min-h-[72px] min-w-[120px] shrink-0 flex-col gap-1.5 rounded-xl border bg-card/95 p-2.5 text-left transition-all duration-150",
        "hover:-translate-y-0.5 hover:border-primary/45 hover:bg-background hover:shadow-[0_8px_20px_rgba(15,23,42,0.06)]",
        "active:translate-y-0 active:scale-[0.99]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected &&
          "border-primary/55 bg-background shadow-[inset_0_0_0_1px_rgba(59,130,246,0.08),0_2px_12px_rgba(15,23,42,0.06)]",
      )}
      aria-pressed={isSelected}
    >
      <span className="block truncate text-xs font-semibold text-foreground">
        {displayName}
      </span>

      {agent && (
        <div className="mt-auto flex items-center gap-1.5">
          <span
            className={cn(
              "inline-block h-1.5 w-1.5 shrink-0 rounded-full",
              statusDotColors[agent.status] ?? "bg-muted-foreground/40",
            )}
          />
          <span className="truncate text-[11px] text-muted-foreground">
            {agent.name}
          </span>
        </div>
      )}
    </button>
  );
}
