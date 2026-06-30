"use client";

import { useMemo } from "react";
import type { WorkflowStage, WorkflowNode, Agent, WorkflowNodeRun } from "@multica/core/types";
import { workerTypeToActorType } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";
import { CompactNodeCard } from "./compact-node-card";
import { CriticBadge } from "./critic-badge";
import { RuntimeNodeCard } from "../../../issues/components/execution/runtime-node-card";

export interface StageLaneProps {
  stage: WorkflowStage;
  nodeIds: WorkflowNode[];
  getActorName: (type: string, id: string) => string | null;
  agentLookup: Map<string, Agent | null>;
  pluginLookup: Map<string, BuiltinPlugin | null>;
  onCardClick: (nodeId: string, focus: "worker" | "critic") => void;
  selectedCard?: { nodeId: string; focus: "worker" | "critic" } | null;
  nodeElementRefs: Map<string, (el: HTMLButtonElement | null) => void>;
  criticElementRefs: Map<string, (el: HTMLButtonElement | null) => void>;
  mode?: "template" | "runtime";
  nodeRuns?: Map<string, WorkflowNodeRun>;
  onNodeClick?: (nodeId: string) => void;
}

const STAGE_BG_COLORS = [
  "bg-slate-50/70",
  "bg-stone-50/70",
  "bg-blue-50/45",
  "bg-rose-50/45",
  "bg-violet-50/45",
  "bg-amber-50/45",
] as const;

const STAGE_LABEL_COLORS = [
  "text-slate-400",
  "text-stone-400",
  "text-blue-400",
  "text-rose-400",
  "text-violet-400",
  "text-amber-400",
] as const;

export function StageLane({
  stage,
  nodeIds,
  getActorName,
  agentLookup,
  pluginLookup,
  onCardClick,
  selectedCard = null,
  nodeElementRefs,
  criticElementRefs,
  mode = "template",
  nodeRuns,
  onNodeClick,
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
      className={cn(
        "relative z-0 border-y border-border/60 px-3 py-3",
        stageBg,
      )}
    >
      <div
        data-testid={`stage-lane-shell-${stage.id}`}
        className="relative z-20 grid min-h-[108px] grid-cols-[112px_minmax(960px,1fr)] items-stretch gap-4"
      >
        <div className="flex flex-col justify-start border-r border-border/50 pr-3 pt-1">
          <span className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
            Stage {stage.sort_order + 1}
          </span>
          <span className={cn("mt-1 text-xs font-semibold leading-snug", labelColor)}>
            {stage.name}
          </span>
        </div>

        <div className="flex min-w-0 overflow-x-auto py-1">
          {sortedNodes.length === 0 ? (
            <div
              data-testid="stage-lane-empty"
              className="flex h-16 items-center justify-center text-[11px] text-muted-foreground"
            >
              No plugins in this stage
            </div>
          ) : (
            <div
              data-testid={`stage-lane-node-row-${stage.id}`}
              className="flex w-full min-w-[960px] flex-nowrap items-start justify-evenly gap-8 px-4"
            >
              {sortedNodes.map((node) => {
                if (mode === "runtime") {
                  const nodeRun = nodeRuns?.get(node.id) ?? null;
                  const workerName = node.worker_id
                    ? getActorName(workerTypeToActorType(node.worker_type), node.worker_id)
                    : null;
                  const criticName = node.critic_id
                    ? getActorName(node.critic_type ?? "agent", node.critic_id)
                    : null;

                  return (
                    <RuntimeNodeCard
                      key={node.id}
                      node={node}
                      nodeRun={nodeRun}
                      workerName={workerName}
                      criticName={criticName}
                      onClick={(id) => onNodeClick?.(id)}
                      elementRef={nodeElementRefs.get(node.id)}
                    />
                  );
                }

                const workerName = node.worker_id
                  ? getActorName(workerTypeToActorType(node.worker_type), node.worker_id)
                  : null;
                const agent = agentLookup.get(node.worker_id ?? "") ?? null;
                const plugin = agent?.plugin_id
                  ? pluginLookup.get(agent.plugin_id) ?? null
                  : null;
                const criticAgent = node.critic_id
                  ? agentLookup.get(node.critic_id) ?? null
                  : null;

                return (
                  <div
                    key={node.id}
                    data-testid={`stage-lane-node-stack-${node.id}`}
                    className="flex flex-col items-center gap-5"
                  >
                    <CompactNodeCard
                      node={node}
                      workerName={workerName}
                      plugin={plugin}
                      onClick={onCardClick}
                      isSelected={
                        selectedCard?.nodeId === node.id &&
                        selectedCard.focus === "worker"
                      }
                      elementRef={nodeElementRefs.get(node.id)}
                    />
                    {hasCriticAttachment(node) && (
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
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
