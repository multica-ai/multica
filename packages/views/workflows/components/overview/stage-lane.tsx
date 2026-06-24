"use client";

import { useMemo } from "react";
import type { WorkflowStage, WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";
import { CompactNodeCard } from "./compact-node-card";
import { CriticBadge } from "./critic-badge";

export interface StageLaneProps {
  stage: WorkflowStage;
  nodeIds: WorkflowNode[];
  agentLookup: Map<string, Agent | null>;
  pluginLookup: Map<string, BuiltinPlugin | null>;
  onCardClick: (nodeId: string, focus: "worker" | "critic") => void;
  selectedCard?: { nodeId: string; focus: "worker" | "critic" } | null;
  nodeElementRefs: Map<string, (el: HTMLDivElement | null) => void>;
  criticElementRefs: Map<string, (el: HTMLButtonElement | null) => void>;
}

const STAGE_BG_COLORS = [
  "bg-slate-50/40",
  "bg-stone-50/40",
  "bg-blue-50/40",
  "bg-rose-50/40",
  "bg-violet-50/40",
  "bg-amber-50/40",
] as const;

const STAGE_LABEL_COLORS = [
  "text-slate-800",
  "text-stone-800",
  "text-blue-900",
  "text-rose-900",
  "text-violet-900",
  "text-amber-900",
] as const;

export function StageLane({
  stage,
  nodeIds,
  agentLookup,
  pluginLookup,
  onCardClick,
  selectedCard = null,
  nodeElementRefs,
  criticElementRefs,
}: StageLaneProps) {
  const colorIndex = Math.abs(stage.sort_order) % STAGE_BG_COLORS.length;
  const stageBg = STAGE_BG_COLORS[colorIndex] ?? STAGE_BG_COLORS[0];
  const labelColor = STAGE_LABEL_COLORS[colorIndex] ?? STAGE_LABEL_COLORS[0];

  const hasCriticAttachment = (node: WorkflowNode) =>
    Boolean(node.critic_id || node.critic_api_url);

  const sortedNodes = useMemo(
    () => [...nodeIds].sort((a, b) => a.sort_order - b.sort_order),
    [nodeIds],
  );

  return (
    <section
      data-testid={`stage-lane-${stage.id}`}
      className={cn("px-3 py-2.5", stageBg)}
    >
      <div className="mb-1.5 flex items-center gap-2">
        <span className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Stage {stage.sort_order + 1}
        </span>
        <span className={cn("text-xs font-semibold tracking-tight", labelColor)}>
          {stage.name}
        </span>
      </div>

      {sortedNodes.length === 0 ? (
        <div
          data-testid="stage-lane-empty"
          className="flex h-8 items-center text-[11px] text-muted-foreground"
        >
          No plugins in this stage
        </div>
      ) : (
        <div className="flex items-start gap-2.5 flex-nowrap overflow-x-auto">
          {sortedNodes.map((node) => {
            const agent = agentLookup.get(node.worker_id ?? "") ?? null;
            const plugin = agent?.plugin_id
              ? pluginLookup.get(agent.plugin_id) ?? null
              : null;
            const criticAgent = node.critic_id
              ? agentLookup.get(node.critic_id) ?? null
              : null;

            return (
              <div key={node.id} className="flex flex-col items-start gap-1.5">
                <CompactNodeCard
                  node={node}
                  agent={agent}
                  plugin={plugin}
                  onClick={onCardClick}
                  isSelected={
                    selectedCard?.nodeId === node.id &&
                    selectedCard.focus === "worker"
                  }
                  elementRef={nodeElementRefs.get(node.id)}
                />
                {hasCriticAttachment(node) && (
                  <div className="ml-4">
                    <CriticBadge
                      node={node}
                      criticAgent={criticAgent}
                      onClick={onCardClick}
                      isSelected={
                        selectedCard?.nodeId === node.id &&
                        selectedCard.focus === "critic"
                      }
                      elementRef={criticElementRefs.get(node.id)}
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}
