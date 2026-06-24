"use client";

import type { WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";
import { ArrowUpRight, Bot } from "lucide-react";

export interface PluginCardProps {
  node: WorkflowNode;
  agent: Agent | null;
  plugin: BuiltinPlugin | null;
  onClick: (nodeId: string, focus: "worker") => void;
  isSelected?: boolean;
}

const statusDotColors: Record<string, string> = {
  working: "bg-[var(--success)]",
  idle: "bg-[var(--info)]",
  blocked: "bg-[var(--warning)]",
  error: "bg-destructive",
  offline: "bg-muted-foreground/40",
};

export function PluginCard({ node, agent, plugin, onClick, isSelected = false }: PluginCardProps) {
  const displayName = plugin?.name ?? node.title;
  const displayDesc = plugin?.description ?? node.description ?? "";

  return (
    <button
      type="button"
      data-testid={`plugin-card-${node.id}`}
      onClick={() => onClick(node.id, "worker")}
      className={cn(
        "group flex min-h-[112px] min-w-[176px] shrink-0 flex-col gap-2 rounded-xl border bg-card/95 p-3 text-left transition-all duration-150",
        "hover:-translate-y-0.5 hover:border-primary/45 hover:bg-background hover:shadow-[0_12px_28px_rgba(15,23,42,0.08)]",
        "active:translate-y-0 active:scale-[0.99]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected && "border-primary/55 bg-background shadow-[0_14px_32px_rgba(59,130,246,0.12)] ring-1 ring-primary/15",
      )}
      aria-pressed={isSelected}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <span className="block truncate text-sm font-semibold text-foreground">{displayName}</span>
          <span className="mt-1 inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
            <Bot className="h-3 w-3" strokeWidth={1.8} />
            Plugin
          </span>
        </div>
        <span className="rounded-full border border-border/70 bg-background/80 p-1 text-muted-foreground transition-colors group-hover:border-primary/35 group-hover:text-foreground">
          <ArrowUpRight className="h-3.5 w-3.5" strokeWidth={1.9} />
        </span>
      </div>

      {displayDesc && (
        <span className="line-clamp-2 text-xs leading-5 text-muted-foreground">{displayDesc}</span>
      )}

      {agent && (
        <div className="mt-auto flex items-center gap-2 rounded-lg border border-border/60 bg-muted/45 px-2.5 py-2">
          <span
            className={cn(
              "inline-block h-2 w-2 shrink-0 rounded-full",
              statusDotColors[agent.status] ?? "bg-muted-foreground/40",
            )}
          />
          <div className="min-w-0 flex-1">
            <div className="truncate text-xs font-medium text-foreground/90">{agent.name}</div>
            {agent.model && (
              <div className="truncate text-[10px] text-muted-foreground/75">{agent.model}</div>
            )}
          </div>
        </div>
      )}
    </button>
  );
}
