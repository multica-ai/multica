"use client";

import { useCallback, useEffect, useMemo } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  addEdge,
  useNodesState,
  useEdgesState,
  type Connection,
  type Node,
  type Edge,
  type NodeTypes,
  type EdgeTypes,
  BackgroundVariant,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { AgentNodeComponent } from "./agent-node";
import { WorkflowEdgeComponent } from "./workflow-edge";
import type { WorkflowNode, WorkflowEdge } from "@multica/core/types/workflow";

const nodeTypes: NodeTypes = { agent: AgentNodeComponent };
const edgeTypes: EdgeTypes = { workflow: WorkflowEdgeComponent };

interface WorkflowCanvasProps {
  initialNodes: WorkflowNode[];
  initialEdges: WorkflowEdge[];
  onNodeUpdate?: (nodeId: string, data: Partial<WorkflowNode>) => void;
  onEdgeCreate?: (sourceId: string, targetId: string) => void;
  onNodesChange?: (nodes: WorkflowNode[]) => void;
}

export function WorkflowCanvas({
  initialNodes,
  initialEdges,
  onNodeUpdate,
  onEdgeCreate,
  onNodesChange,
}: WorkflowCanvasProps) {
  const nodes: Node<WorkflowNode>[] = useMemo(() =>
    initialNodes.map((n) => ({
      id: n.id,
      type: "agent",
      position: { x: n.position_x, y: n.position_y },
      data: n,
    })),
    [initialNodes]
  );

  const edges: Edge[] = useMemo(() =>
    initialEdges.map((e) => ({
      id: e.id,
      type: "workflow",
      source: e.source_node_id,
      target: e.target_node_id,
    })),
    [initialEdges]
  );

  const [flowNodes, setFlowNodes, onNodesChangeInternal] = useNodesState(nodes);
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState(edges);

  // Notify external consumer of position changes after each render
  useEffect(() => {
    onNodesChange?.(
      flowNodes.map((n) => ({
        ...n.data,
        position_x: n.position.x,
        position_y: n.position.y,
      }))
    );
  }, [flowNodes, onNodesChange]);

  const onConnect = useCallback(
    (connection: Connection) => {
      if (!connection.source || !connection.target) return;
      setFlowEdges((eds) => addEdge({ ...connection, type: "workflow" }, eds));
      onEdgeCreate?.(connection.source, connection.target);
    },
    [setFlowEdges, onEdgeCreate]
  );

  const onNodeDragStop = useCallback(
    (_event: unknown, node: Node) => {
      onNodeUpdate?.(node.id, {
        position_x: node.position.x,
        position_y: node.position.y,
      });
    },
    [onNodeUpdate]
  );

  return (
    <div className="w-full h-full">
      <ReactFlow
        nodes={flowNodes}
        edges={flowEdges}
        onNodesChange={onNodesChangeInternal}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onNodeDragStop={onNodeDragStop}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        defaultEdgeOptions={{ type: "workflow" }}
        fitView
        deleteKeyCode={["Backspace", "Delete"]}
      >
        <Background variant={BackgroundVariant.Dots} gap={16} size={1} />
        <Controls />
        <MiniMap
          nodeColor={(n) => {
            const s = n.data?.status as string | undefined;
            if (s === "completed") return "#22c55e";
            if (s === "failed") return "#ef4444";
            if (s === "running") return "#3b82f6";
            return "#888";
          }}
          maskColor="rgba(0,0,0,0.1)"
        />
      </ReactFlow>
    </div>
  );
}
