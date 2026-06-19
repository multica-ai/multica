"use client";

import { useCallback, useMemo } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MarkerType,
  type Node,
  type Edge,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import type { WorkflowNode, WorkflowEdge } from "@multica/core/types";
import { parseNodeShape } from "@multica/core/types";
import {
  WorkflowNode as WorkflowNodeRenderer,
  WorkflowEdge as WorkflowEdgeRenderer,
  NODE_WIDTH,
  NODE_HEIGHT,
  DIAMOND_SIZE,
  HEXAGON_SIZE,
  type WorkflowNodeData,
} from "../reactflow-nodes";
import { useT } from "../../../i18n";

export interface StageNodeDagProps {
  stageId: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  onNodeSelect: (nodeId: string) => void;
}

const nodeTypes = { workflow: WorkflowNodeRenderer };
const edgeTypes = { workflow: WorkflowEdgeRenderer };

export function StageNodeDag({ stageId, nodes, edges, onNodeSelect }: StageNodeDagProps) {
  const { t } = useT("workflows");

  // ── Filter nodes & edges by stage ──

  const stageNodes = useMemo(
    () => nodes.filter((n) => n.stage_id === stageId),
    [nodes, stageId],
  );

  const stageNodeIds = useMemo(
    () => new Set(stageNodes.map((n) => n.id)),
    [stageNodes],
  );

  const stageEdges = useMemo(
    () =>
      edges.filter(
        (e) =>
          stageNodeIds.has(e.source_node_id) && stageNodeIds.has(e.target_node_id),
      ),
    [edges, stageNodeIds],
  );

  // ── Map to ReactFlow nodes ──

  const rfNodes: Node<WorkflowNodeData>[] = useMemo(
    () =>
      stageNodes.map((n) => {
        const shape = parseNodeShape(n.format_schema);
        let nodeColor: string | undefined;
        if (
          n.format_schema &&
          typeof n.format_schema === "object" &&
          !Array.isArray(n.format_schema)
        ) {
          const obj = n.format_schema as Record<string, unknown>;
          if (typeof obj.color === "string" && obj.color !== "") nodeColor = obj.color;
        }

        const w =
          shape === "diamond"
            ? DIAMOND_SIZE
            : shape === "hexagon"
              ? HEXAGON_SIZE
              : NODE_WIDTH;
        const h =
          shape === "diamond" || shape === "hexagon"
            ? shape === "diamond"
              ? DIAMOND_SIZE
              : HEXAGON_SIZE
            : NODE_HEIGHT;

        return {
          id: n.id,
          type: "workflow",
          position: { x: n.position_x, y: n.position_y },
          width: w,
          height: h,
          data: {
            title: n.title,
            shape,
            nodeColor,
            isEditing: false,
            onNodeSelect,
          },
        };
      }),
    [stageNodes, onNodeSelect],
  );

  // ── Map to ReactFlow edges ──

  const rfEdges: Edge[] = useMemo(
    () =>
      stageEdges.map((e) => {
        // Determine best handle pair based on node positions
        const srcNode = stageNodes.find((n) => n.id === e.source_node_id);
        const tgtNode = stageNodes.find((n) => n.id === e.target_node_id);
        let sourceHandle = "bottom";
        let targetHandle = "top";
        if (srcNode && tgtNode) {
          const dx = tgtNode.position_x - srcNode.position_x;
          const dy = tgtNode.position_y - srcNode.position_y;
          if (Math.abs(dx) > Math.abs(dy)) {
            sourceHandle = "right";
            targetHandle = "left";
          }
        }

        return {
          id: e.id,
          type: "workflow",
          source: e.source_node_id,
          target: e.target_node_id,
          sourceHandle,
          targetHandle,
          markerEnd: { type: MarkerType.ArrowClosed, color: "#64748b" },
          interactionWidth: 20,
        };
      }),
    [stageEdges, stageNodes],
  );

  // ── Node click handler ──

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      onNodeSelect(node.id);
    },
    [onNodeSelect],
  );

  // ── Empty state ──

  if (stageNodes.length === 0) {
    return (
      <div
        data-testid="empty-nodes-state"
        className="flex items-center justify-center h-64 border border-dashed rounded-lg border-border"
      >
        <div className="flex flex-col items-center gap-2 text-center">
          <p className="text-sm font-medium text-muted-foreground">
            {t(($) => $.overview.node_dag.empty_title)}
          </p>
          <p className="text-xs text-muted-foreground max-w-sm">
            {t(($) => $.overview.node_dag.empty_description)}
          </p>
        </div>
      </div>
    );
  }

  // ── Read-only ReactFlow DAG ──

  return (
    <div
      key={stageId}
      data-testid="stage-node-dag"
      className="h-64 w-full border rounded-lg overflow-hidden"
    >
      <ReactFlow
        nodes={rfNodes}
        edges={rfEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        onNodeClick={handleNodeClick}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        fitView
        minZoom={0.1}
        maxZoom={2}
        proOptions={{ hideAttribution: true }}
      >
        <Background />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  );
}
