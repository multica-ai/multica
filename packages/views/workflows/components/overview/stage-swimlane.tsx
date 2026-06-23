"use client";

import { useMemo } from "react";
import type { WorkflowStage, WorkflowNode, Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { PluginCard } from "./plugin-card";
import { CriticBadge } from "./critic-badge";

export interface StageSwimlaneProps {
  stage: WorkflowStage;
  nodes: WorkflowNode[];
  agentLookup: Map<string, Agent | null>;
  pluginLookup: Map<string, BuiltinPlugin | null>;
  onCardClick: (nodeId: string) => void;
}

export function StageSwimlane({
  stage,
  nodes,
  agentLookup,
  pluginLookup,
  onCardClick,
}: StageSwimlaneProps) {
  const stageNodes = useMemo(
    () => nodes.filter((n) => n.stage_id === stage.id),
    [nodes, stage.id],
  );

  // Separate worker nodes from critic nodes (critic_type non-empty → CriticBadge)
  const { workerNodes, criticNodes } = useMemo(() => {
    const workers: WorkflowNode[] = [];
    const critics: WorkflowNode[] = [];
    for (const n of stageNodes) {
      if (n.critic_type) {
        critics.push(n);
      } else {
        workers.push(n);
      }
    }
    return { workerNodes: workers, criticNodes: critics };
  }, [stageNodes]);

  return (
    <div data-testid="stage-swimlane" className="rounded-lg border bg-card/30 overflow-hidden">
      {/* Stage header */}
      <div className="px-4 py-2 border-b bg-muted/30">
        <h3 className="text-sm font-semibold text-center">{stage.name}</h3>
        {stage.description && (
          <p className="text-xs text-muted-foreground text-center mt-0.5">
            {stage.description}
          </p>
        )}
      </div>

      {/* Cards area */}
      <div className="p-3">
        {stageNodes.length === 0 ? (
          <div
            data-testid="stage-swimlane-empty"
            className="flex items-center justify-center h-16 text-xs text-muted-foreground"
          >
            No nodes in this stage
          </div>
        ) : (
          <div className="flex flex-wrap gap-2">
            {workerNodes.map((node) => {
              const agent = agentLookup.get(node.worker_id ?? "") ?? null;
              const plugin = agent?.plugin_id
                ? pluginLookup.get(agent.plugin_id) ?? null
                : null;
              return (
                <PluginCard
                  key={node.id}
                  node={node}
                  agent={agent}
                  plugin={plugin}
                  onClick={onCardClick}
                />
              );
            })}

            {criticNodes.map((node) => {
              // For critic nodes, the worker_id is the agent that performs critique
              const criticAgent = agentLookup.get(node.worker_id ?? "") ?? null;
              return (
                <CriticBadge
                  key={node.id}
                  node={node}
                  criticAgent={criticAgent}
                  onClick={onCardClick}
                />
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
