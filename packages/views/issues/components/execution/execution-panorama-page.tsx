"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  workflowDetailOptions,
  workflowStagesOptions,
  workflowNodesOptions,
  workflowNodeRunsOptions,
} from "@multica/core/workflows/queries";
import { agentListOptions, builtinPluginListOptions } from "@multica/core/workspace/queries";
import { workerTypeToActorType } from "@multica/core/types";
import type {
  WorkflowNode,
  WorkflowNodeRun,
  WorkflowStage,
  Agent,
} from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { StageLane } from "../../../workflows/components/overview/stage-lane";
import { ExecutionDetailPanel } from "./execution-detail-panel";
import { useT } from "@multica/views/i18n";
import { Loader2 } from "lucide-react";

export interface ExecutionPanoramaPageProps {
  workflowId: string;
  runId: string | null;
  wsId: string;
}

/**
 * Main issue-execution panorama view.
 *
 * Composes StageLane (runtime mode) + PanoramaSvgOverlay + ExecutionDetailPanel
 * into a scrollable full-page view of all workflow stages, nodes, and their
 * per-run status.
 */
export function ExecutionPanoramaPage({
  workflowId,
  runId,
  wsId,
}: ExecutionPanoramaPageProps) {
  const { t } = useT("issues");

  // ---- Data queries ----
  const { isLoading: wfLoading } = useQuery(
    workflowDetailOptions(wsId, workflowId),
  );
  const { data: stages, isLoading: stLoading } = useQuery(
    workflowStagesOptions(wsId, workflowId),
  );
  const { data: nodes, isLoading: ndLoading } = useQuery(
    workflowNodesOptions(wsId, workflowId),
  );
  const { data: nodeRuns } = useQuery({
    ...workflowNodeRunsOptions(wsId, workflowId, runId ?? ""),
    enabled: !!runId,
  });
  const { data: agents } = useQuery(agentListOptions(wsId));

  // builtinPluginListOptions is global (no wsId parameter)
  const { data: plugins } = useQuery(builtinPluginListOptions());

  // ---- Local state ----
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

  // ---- Lookup maps ----
  const nodeRunMap = useMemo(() => {
    const map = new Map<string, WorkflowNodeRun>();
    if (nodeRuns) {
      for (const nr of nodeRuns) {
        map.set(nr.workflow_node_id, nr);
      }
    }
    return map;
  }, [nodeRuns]);

  const agentLookup = useMemo(() => {
    const map = new Map<string, Agent | null>();
    if (agents) {
      for (const a of agents) map.set(a.id, a);
    }
    return map;
  }, [agents]);

  const pluginLookup = useMemo(() => {
    const map = new Map<string, BuiltinPlugin | null>();
    if (plugins) {
      for (const p of plugins.items) map.set(p.id, p);
    }
    return map;
  }, [plugins]);

  const getActorName = (type: string, id: string): string | null => {
    if (type === "agent" || type === "human" || type === "member") {
      return agentLookup.get(id)?.name ?? null;
    }
    return null;
  };

  // ---- Derived ----
  const isLoading = wfLoading || stLoading || ndLoading;

  if (isLoading) {
    return (
      <div
        role="status"
        className="flex items-center justify-center py-20"
      >
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const allStages: WorkflowStage[] = stages ?? [];
  const allNodes: WorkflowNode[] = nodes ?? [];

  const nodesByStage = new Map<string | null, WorkflowNode[]>();
  for (const node of allNodes) {
    const key = node.stage_id ?? null;
    if (!nodesByStage.has(key)) nodesByStage.set(key, []);
    nodesByStage.get(key)!.push(node);
  }

  const unassignedNodes = nodesByStage.get(null) ?? [];
  const selectedNode = allNodes.find((n) => n.id === selectedNodeId) ?? null;
  const selectedRun = selectedNodeId
    ? nodeRunMap.get(selectedNodeId) ?? null
    : null;

  return (
    <div
      className="relative flex flex-col min-h-0"
      data-testid="execution-panorama"
    >
      <div
        className="relative overflow-auto p-3"
        data-testid="panorama-canvas"
      >
        {/* SVG edge overlay is deferred in runtime mode — PanoramaSvgOverlay
        needs element refs from StageLane, which are not wired yet. */}

        {allStages.length === 0 ? (
          <StageLane
            stage={{
              id: "unassigned",
              workflow_id: workflowId,
              name: t(($) => $.execution.panorama.unassigned),
              description: "",
              sort_order: 0,
              node_count: unassignedNodes.length,
              created_at: "",
              updated_at: "",
            }}
            nodeIds={unassignedNodes}
            getActorName={getActorName}
            agentLookup={agentLookup}
            pluginLookup={pluginLookup}
            onCardClick={() => {}}
            nodeElementRefs={new Map()}
            criticElementRefs={new Map()}
            mode="runtime"
            nodeRuns={nodeRunMap}
            onNodeClick={(id) => setSelectedNodeId(id)}
          />
        ) : (
          [...allStages]
            .sort((a, b) => a.sort_order - b.sort_order)
            .map((stage, i) => (
              <div key={stage.id}>
                {i > 0 && (
                  <div
                    className="h-2 bg-gradient-to-b from-slate-50/40 to-stone-50/40"
                    data-testid="stage-transition-gradient"
                  />
                )}
                <StageLane
                  stage={stage}
                  nodeIds={nodesByStage.get(stage.id) ?? []}
                  getActorName={getActorName}
                  agentLookup={agentLookup}
                  pluginLookup={pluginLookup}
                  onCardClick={() => {}}
                  nodeElementRefs={new Map()}
                  criticElementRefs={new Map()}
                  mode="runtime"
                  nodeRuns={nodeRunMap}
                  onNodeClick={(id) => setSelectedNodeId(id)}
                />
              </div>
            ))
        )}

        {/* Unassigned nodes (stage_id = NULL) when stages exist */}
        {allStages.length > 0 && unassignedNodes.length > 0 && (
          <StageLane
            stage={{
              id: "unassigned",
              workflow_id: workflowId,
              name: t(($) => $.execution.panorama.unassigned),
              description: "",
              sort_order: 999,
              node_count: unassignedNodes.length,
              created_at: "",
              updated_at: "",
            }}
            nodeIds={unassignedNodes}
            getActorName={getActorName}
            agentLookup={agentLookup}
            pluginLookup={pluginLookup}
            onCardClick={() => {}}
            nodeElementRefs={new Map()}
            criticElementRefs={new Map()}
            mode="runtime"
            nodeRuns={nodeRunMap}
            onNodeClick={(id) => setSelectedNodeId(id)}
          />
        )}
      </div>

      {/* Detail panel */}
      {selectedNodeId && selectedNode && (
        <ExecutionDetailPanel
          node={selectedNode}
          nodeRun={selectedRun}
          workerName={
            selectedNode.worker_id
              ? getActorName(
                  workerTypeToActorType(selectedNode.worker_type),
                  selectedNode.worker_id,
                )
              : null
          }
          criticName={
            selectedNode.critic_id
              ? getActorName(
                  selectedNode.critic_type ?? "agent",
                  selectedNode.critic_id,
                )
              : null
          }
          onClose={() => setSelectedNodeId(null)}
          wsId={wsId}
        />
      )}
    </div>
  );
}
