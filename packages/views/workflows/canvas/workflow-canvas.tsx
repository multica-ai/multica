"use client";

import { useCallback, useMemo, useState } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  addEdge,
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
}

export function WorkflowCanvas({
  initialNodes,
  initialEdges,
  onNodeUpdate,
  onEdgeCreate,
}: WorkflowCanvasProps) {
  const nodes: Node[] = useMemo(() =>
    initialNodes.map((n) => ({
      id: n.id,
      type: "agent",
      position: { x: n.position_x, y: n.position_y },
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      data: n as any,
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

  const [flowNodes, setFlowNodes] = useState<Node[]>(nodes);
  const [flowEdges, setFlowEdges] = useEdgesState(edges);

  const onConnect = useCallback(
    (connection: Connection) => {
      if (!connection.source || !connection.target) return;
      setFlowEdges((eds: Edge[]) => addEdge({ ...connection, type: "workflow" }, eds));
      onEdgeCreate?.(connection.source, connection.target);
    },
    [setFlowEdges, onEdgeCreate]
  );

  const onNodeDragStop = useCallback(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (_event: any, node: Node) => {
      setFlowNodes((nds) =>
        nds.map((n) =>
          n.id === node.id ? { ...n, position: node.position } : n
        )
      );
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
          nodeColor={(n: Node) => {
            const s = (n.data as { status?: string })?.status;
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
