"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";

export interface PluginCardProps {
  node: WorkflowNode;
  agent: Agent | null;
  plugin: BuiltinPlugin | null;
  onClick: (nodeId: string) => void;
}

const statusDotColors: Record<string, string> = {
  working: "bg-[var(--success)]",
  idle: "bg-[var(--info)]",
  blocked: "bg-[var(--warning)]",
  error: "bg-destructive",
  offline: "bg-muted-foreground/40",
};

export function PluginCard({ node, agent, plugin, onClick }: PluginCardProps) {
  const displayName = plugin?.name ?? node.title;
  const displayDesc = plugin?.description ?? node.description ?? "";

  return (
    <button
      data-testid={`plugin-card-${node.id}`}
      onClick={() => onClick(node.id)}
      className={cn(
        "flex shrink-0 flex-col gap-1.5 rounded-lg border bg-card p-3 text-left transition-colors min-w-[160px]",
        "hover:bg-accent/50 hover:border-primary/50",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
      )}
    >
      <span className="text-sm font-medium truncate">{displayName}</span>

      {displayDesc && (
        <span className="text-xs text-muted-foreground line-clamp-2">{displayDesc}</span>
      )}

      {agent && (
        <div className="flex items-center gap-1.5 mt-1">
          <span
            className={cn(
              "inline-block w-1.5 h-1.5 rounded-full shrink-0",
              statusDotColors[agent.status] ?? "bg-muted-foreground/40",
            )}
          />
          <span className="text-xs text-muted-foreground truncate">
            {agent.name}
          </span>
          {agent.model && (
            <span className="text-[10px] text-muted-foreground/60 truncate ml-auto">
              {agent.model}
            </span>
          )}
        </div>
      )}
    </button>
  );
}
