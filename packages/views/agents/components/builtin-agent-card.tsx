"use client";

import { Zap } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";

interface BuiltinAgentCardProps {
  agent: Agent;
  onClick: (agentId: string) => void;
}

export function BuiltinAgentCard({ agent, onClick }: BuiltinAgentCardProps) {
  return (
    <button
      type="button"
      className="flex flex-col items-start gap-1.5 rounded-lg border px-4 py-3 text-left transition-colors hover:bg-accent/40 hover:border-primary/30 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      onClick={() => onClick(agent.id)}
    >
      <div className="flex items-center gap-2 w-full">
        <Zap className="h-4 w-4 shrink-0 text-primary" />
        <span className="text-sm font-medium truncate">{agent.name}</span>
        <Badge variant="outline" className="shrink-0 ml-auto">
          <Zap className="h-3 w-3 text-amber-500" />
          内置
        </Badge>
      </div>
      {agent.description && (
        <p className="text-xs text-muted-foreground line-clamp-2">
          {agent.description}
        </p>
      )}
      <div className="flex items-center gap-1 text-[10px] text-muted-foreground mt-0.5">
        <Zap className="h-3 w-3" />
        内置 Agent
      </div>
    </button>
  );
}
