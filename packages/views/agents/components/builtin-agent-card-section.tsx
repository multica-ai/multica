"use client";

import { Zap } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { BuiltinAgentCard } from "./builtin-agent-card";

interface BuiltinAgentCardSectionProps {
  agents: Agent[];
  loading: boolean;
  onCardClick: (agentId: string) => void;
}

export function BuiltinAgentCardSection({
  agents,
  loading,
  onCardClick,
}: BuiltinAgentCardSectionProps) {
  if (!loading && agents.length === 0) return null;

  return (
    <div className="px-5 py-3 border-b">
      <div className="flex items-center gap-2 mb-2">
        <Zap className="h-3.5 w-3.5 text-amber-500" />
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
          内置 Agent
        </span>
        {agents.length > 0 && (
          <span className="font-mono text-[10px] tabular-nums text-muted-foreground/70">
            {agents.length}
          </span>
        )}
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        {loading
          ? Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-[88px] w-full rounded-lg" />
            ))
          : agents.map((agent) => (
              <BuiltinAgentCard
                key={agent.id}
                agent={agent}
                onClick={onCardClick}
              />
            ))}
      </div>
    </div>
  );
}
