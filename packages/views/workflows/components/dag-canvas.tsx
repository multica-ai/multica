"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  MarkerType,
  ConnectionMode,
  applyNodeChanges,
  applyEdgeChanges,
  useReactFlow,
  type Node,
  type Edge,
  type Connection,
  type OnNodesChange,
  type OnEdgesChange,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import { useWorkflowEditorStore } from "@multica/core/workflows/store";
import type { WorkflowNode as WorkflowNodeType, WorkflowEdge as WorkflowEdgeType } from "@multica/core/types";
import { parseNodeShape } from "@multica/core/types";
import {
  WorkflowNode,
  AnnotationNode,
  WorkflowEdge as WorkflowEdgeComponent,
  AnnotationConnectorEdge,
  ANNO_WIDTH,
  ANNO_HEIGHT,
  NODE_WIDTH,
  NODE_HEIGHT,
  DIAMOND_SIZE,
  HEXAGON_SIZE,
  type WorkflowNodeData,
} from "./reactflow-nodes";
import { computeAlignmentSnap, type AlignmentGuide } from "./alignment-snap";

function parseNodeFormat(formatSchema: unknown): {
  shape: ReturnType<typeof parseNodeShape>;
  nodeColor: string | undefined;
  fontSize: number | undefined;
  nodeWidth: number | undefined;
  nodeHeight: number | undefined;
} {
  const shape = parseNodeShape(formatSchema);
  let nodeColor: string | undefined;
  let fontSize: number | undefined;
  let nodeWidth: number | undefined;
  let nodeHeight: number | undefined;
  if (formatSchema && typeof formatSchema === "object" && formatSchema !== null) {
    const obj = formatSchema as Record<string, unknown>;
    if (typeof obj.color === "string" && obj.color !== "") nodeColor = obj.color;
    if (typeof obj.fontSize === "number") fontSize = obj.fontSize;
    if (typeof obj.width === "number") nodeWidth = obj.width;
    if (typeof obj.height === "number") nodeHeight = obj.height;
  }
  return { shape, nodeColor, fontSize, nodeWidth, nodeHeight };
}

const nodeTypes = { workflow: WorkflowNode, annotation: AnnotationNode };
const edgeTypes = { workflow: WorkflowEdgeComponent, annotation: AnnotationConnectorEdge };

export interface WorkflowCanvasProps {
  nodes: WorkflowNodeType[];
  edges: WorkflowEdgeType[];
  onNodeDragStop?: (nodeId: string, x: number, y: number) => void;
  onEdgeCreate?: (sourceNodeId: string, targetNodeId: string) => void;
  onEdgeDelete?: (edgeId: string) => void;
  onNodeClick?: (nodeId: string) => void;
  onNodeCreate?: (type: string, x: number, y: number) => void;
  nodeStatusColors?: Record<string, string>;
  nodeStatuses?: Record<string, { status: string; isRunning: boolean; isAwaitingInput?: boolean }>;
  showMiniMap?: boolean;
}

export function WorkflowCanvas({
  nodes,
  edges,
  onNodeDragStop,
  onEdgeCreate,
  onEdgeDelete,
  onNodeClick,
  onNodeCreate,
  nodeStatusColors,
  nodeStatuses,
  showMiniMap = true,
}: WorkflowCanvasProps) {
  const mode = useWorkflowEditorStore((s) => s.mode);
  const selectNode = useWorkflowEditorStore((s) => s.selectNode);
  const selectEdge = useWorkflowEditorStore((s) => s.selectEdge);
  const setSelectedNodeIds = useWorkflowEditorStore((s) => s.setSelectedNodeIds);
  const cacheNodeDelete = useWorkflowEditorStore((s) => s.cacheNodeDelete);
  const deletedNodeIds = useWorkflowEditorStore((s) => s.deletedNodeIds);
  const canvasColorMode = useWorkflowEditorStore((s) => s.canvasColorMode);
  const { screenToFlowPosition } = useReactFlow();

  const cacheNodeEdit = useWorkflowEditorStore((s) => s.cacheNodeEdits);

  const handleNodeSelect = useCallback((nodeId: string) => {
    useWorkflowEditorStore.setState({ selectedNodeId: nodeId, selectedEdgeId: null });
  }, []);

  // Persist node dimensions after resize ends
  const handleNodeResizeStart = useCallback(() => {
    resizingRef.current = true;
  }, []);

  const handleNodeResize = useCallback(
    (nodeId: string, width: number, height: number) => {
      resizingRef.current = false;
      const node = nodes.find((n) => n.id === nodeId);
      if (!node) return;
      const parsed = node.format_schema && typeof node.format_schema === "object" && !Array.isArray(node.format_schema)
        ? { ...(node.format_schema as Record<string, unknown>) }
        : {};
      parsed.width = Math.round(width);
      parsed.height = Math.round(height);
      cacheNodeEdit(nodeId, { format_schema: parsed });
    },
    [nodes, cacheNodeEdit],
  );

  // Check if a node is an annotation by its format_schema
  function isAnnotationNode(fs: unknown): boolean {
    if (fs && typeof fs === "object" && !Array.isArray(fs)) {
      return (fs as Record<string, unknown>).type === "annotation";
    }
    return false;
  }

  // Build ReactFlow nodes from props — positions frozen in a ref so they
  // only change when the caller explicitly updates nodes (not during drag).
  // Also filters out nodes that have been marked for deletion.
  const propNodes: Node<WorkflowNodeData>[] = useMemo(
    () => {
      return nodes
        .filter((n) => !deletedNodeIds.includes(n.id))
        .filter((n) => !isAnnotationNode(n.format_schema))
        .map((n) => {
          const annotation = isAnnotationNode(n.format_schema);
          const { shape, nodeColor, fontSize, nodeWidth, nodeHeight } = parseNodeFormat(n.format_schema);

          return {
            id: n.id,
            type: annotation ? "annotation" : "workflow",
            position: { x: n.position_x, y: n.position_y },
            zIndex: annotation ? -1 : 0,
            width: annotation ? ANNO_WIDTH : (nodeWidth ?? (shape === "diamond" ? DIAMOND_SIZE : shape === "hexagon" ? HEXAGON_SIZE : NODE_WIDTH)),
            height: annotation ? ANNO_HEIGHT : (nodeHeight ?? (shape === "diamond" || shape === "hexagon" ? (shape === "diamond" ? DIAMOND_SIZE : HEXAGON_SIZE) : NODE_HEIGHT)),
            data: {
              title: n.title,
              statusColor: nodeStatusColors?.[n.id],
              statusLabel: nodeStatuses?.[n.id]?.status,
              isRunning: nodeStatuses?.[n.id]?.isRunning ?? false,
              isAwaitingInput: nodeStatuses?.[n.id]?.isAwaitingInput ?? false,
              isEditing: mode !== "view",
              shape,
              nodeColor,
              fontSize,
              onNodeSelect: handleNodeSelect,
              onNodeResizeStart: handleNodeResizeStart,
              onNodeResizeEnd: handleNodeResize,
            },
          };
        });
    },
    [nodes, nodeStatusColors, nodeStatuses, deletedNodeIds, mode, handleNodeSelect, handleNodeResizeStart, handleNodeResize],
  );

  // Fit only on initial mount to prevent viewport jumps on mode switch.
  const [shouldFitView, setShouldFitView] = useState(true);
  useEffect(() => {
    const id = requestAnimationFrame(() => setShouldFitView(false));
    return () => cancelAnimationFrame(id);
  }, []);

  // Local state drives ReactFlow rendering so that applyNodeChanges
  // produces immediate visual updates during drag.
  const [rfNodes, setRfNodes] = useState(propNodes);
  const draggingRef = useRef(false);
  const resizingRef = useRef(false);
  const rfNodesRef = useRef(rfNodes);
  rfNodesRef.current = rfNodes;
  const [alignmentGuides, setAlignmentGuides] = useState<AlignmentGuide[]>([]);

  // Sync from props when the data layer changes, but NOT while the user
  // is actively dragging or resizing (otherwise ReactFlow resets the position/dimensions).
  // Use a patch approach: add/remove by id, update existing nodes in-place
  // so ReactFlow's internal selection is preserved across data-only changes.
  useEffect(() => {
    if (draggingRef.current || resizingRef.current) return;
    setRfNodes((prev) => {
      const prevMap = new Map(prev.map((n) => [n.id, n]));
      const nextMap = new Map(propNodes.map((n) => [n.id, n]));
      const result: Node<WorkflowNodeData>[] = [];

      // Keep or create nodes present in propNodes
      for (const [id, nextNode] of nextMap) {
        const prevNode = prevMap.get(id);
        if (prevNode) {
          // Only update data, preserve ReactFlow-managed position state
          result.push({ ...prevNode, data: nextNode.data });
        } else {
          result.push(nextNode);
        }
      }
      return result;
    });
  }, [propNodes]);

  // Resolve the best handle pair for each edge based on node positions.
  // Nodes with larger horizontal gap → Right source, Left target.
  // Nodes with larger vertical gap   → Bottom source, Top target.
  const handlePairs = useMemo(() => {
    const posMap = new Map(nodes.map((n) => [n.id, { x: n.position_x, y: n.position_y }]));
    return new Map<string, { sourceHandle: string; targetHandle: string }>(
      edges.map((e) => {
        const src = posMap.get(e.source_node_id);
        const tgt = posMap.get(e.target_node_id);
        if (!src || !tgt) return [e.id, { sourceHandle: "bottom", targetHandle: "top" }];
        const dx = tgt.x - src.x;
        const dy = tgt.y - src.y;
        if (Math.abs(dx) > Math.abs(dy)) {
          return [e.id, { sourceHandle: "right", targetHandle: "left" }];
        }
        return [e.id, { sourceHandle: "bottom", targetHandle: "top" }];
      }),
    );
  }, [nodes, edges]);

  const propEdges: Edge[] = useMemo(() => {
    const base = edges
      .map((e) => ({
        id: e.id,
        type: "workflow",
        source: e.source_node_id,
        target: e.target_node_id,
        sourceHandle: handlePairs.get(e.id)?.sourceHandle ?? "bottom",
        targetHandle: handlePairs.get(e.id)?.targetHandle ?? "top",
        data: { onEdgeDelete },
        markerEnd: { type: MarkerType.ArrowClosed, color: "#64748b" },
        interactionWidth: 20,
      }));

    // Generate annotation → target connector edges (dashed, no arrow)
    const annoEdges: Edge[] = [];
    for (const n of nodes) {
      if (!isAnnotationNode(n.format_schema)) continue;
      const fs = n.format_schema as Record<string, unknown> | null;
      const targetId = fs?.annotation_target_node_id as string | undefined;
      if (!targetId) continue;
      const target = nodes.find((t) => t.id === targetId && !isAnnotationNode(t.format_schema));
      if (!target) continue;
      annoEdges.push({
        id: `anno-link-${n.id}`,
        type: "annotation",
        source: n.id,
        target: targetId,
        sourceHandle: "anno-right",
        targetHandle: "left",
        hidden: false,
      });
    }

    return [...base, ...annoEdges];
  }, [edges, onEdgeDelete, handlePairs, nodes]);

  // Local state drives ReactFlow rendering so that applyEdgeChanges
  // produces immediate visual updates when edges are removed.
  const [rfEdges, setRfEdges] = useState(propEdges);

  // Sync from props when the data layer changes.
  useEffect(() => {
    setRfEdges((currentEdges) => {
      // Preserve ReactFlow's internal state (selected, etc.) when merging new props.
      // When propEdges recomputes due to a parent re-render (e.g. after a store
      // update), we must carry over the selected flag so ReactFlow doesn't lose
      // the user's current edge selection.
      const stateByKey = new Map(
        currentEdges.map((e) => [e.id, { selected: e.selected }] as const),
      );
      return propEdges.map((e) => {
        const existing = stateByKey.get(e.id);
        return existing ? { ...e, selected: existing.selected } : e;
      });
    });
  }, [propEdges]);

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      selectNode(node.id);
      selectEdge(null);
      onNodeClick?.(node.id);
    },
    [selectNode, selectEdge, onNodeClick],
  );

  // Sync ReactFlow's internal selection to the Zustand store so batch
  // operations (move, delete) know which nodes are currently selected.
  const handleSelectionChange = useCallback(
    ({ nodes: selectedNodes }: { nodes: Node[] }) => {
      setSelectedNodeIds(selectedNodes.map((n) => n.id));
    },
    [setSelectedNodeIds],
  );

  const handleNodeDragStart = useCallback(() => {
    draggingRef.current = true;
  }, []);

  const handleNodeDragStop = useCallback(
    (_: MouseEvent | TouchEvent, node: Node) => {
      draggingRef.current = false;
      setAlignmentGuides([]);
      // ReactFlow only fires onNodeDragStop once (for the primary node)
      // during multi-select drag. Save positions for ALL selected nodes
      // so the batch move is fully persisted.
      const ids = useWorkflowEditorStore.getState().selectedNodeIds;
      if (ids.length > 1) {
        for (const id of ids) {
          const current = rfNodesRef.current.find((n) => n.id === id);
          if (current) {
            onNodeDragStop?.(id, Math.round(current.position.x), Math.round(current.position.y));
          }
        }
      } else {
        const current = rfNodesRef.current.find((n) => n.id === node.id);
        const x = Math.round(current?.position.x ?? node.position.x);
        const y = Math.round(current?.position.y ?? node.position.y);
        onNodeDragStop?.(node.id, x, y);
      }
    },
    [onNodeDragStop],
  );

  const handleNodesChange: OnNodesChange = useCallback(
    (changes) => {
      let guides: AlignmentGuide[] = [];

      for (const change of changes) {
        if (change.type === "remove") {
          cacheNodeDelete(change.id);
        }
      }

      // For multi-node drags, snap the first dragged node and apply the
      // same delta to all others so the group preserves its relative layout.
      let snapDeltaX = 0;
      let snapDeltaY = 0;
      let firstSnapped = false;

      const snappedChanges = changes.map((change) => {
        if (change.type === "position" && change.dragging && change.position) {
          if (!firstSnapped) {
            const result = computeAlignmentSnap(change.id, change.position.x, change.position.y, rfNodesRef.current);
            guides.push(...result.guides);
            snapDeltaX = result.x - change.position.x;
            snapDeltaY = result.y - change.position.y;
            firstSnapped = true;
            return { ...change, position: { x: result.x, y: result.y } };
          }
          return {
            ...change,
            position: {
              x: change.position.x + snapDeltaX,
              y: change.position.y + snapDeltaY,
            },
          };
        }
        return change;
      });

      setAlignmentGuides(guides);
      setRfNodes((nds) => {
        const next = applyNodeChanges(snappedChanges, nds) as Node<WorkflowNodeData>[];
        // Sync the ref synchronously so handleNodeDragStop (which fires
        // in the same event tick) reads the snapped positions, not the
        // pre-snap values from the previous render.
        rfNodesRef.current = next;
        return next;
      });
    },
    [cacheNodeDelete],
  );

  const handleEdgesChange: OnEdgesChange = useCallback(
    (changes) => {
      for (const change of changes) {
        if (change.type === "remove" && mode !== "view") {
          onEdgeDelete?.(change.id);
        }
      }
      setRfEdges((eds) => applyEdgeChanges(changes, eds));
    },
    [onEdgeDelete, mode],
  );

  const handleNodesDelete = useCallback(
    (deletedNodes: Node[]) => {
      for (const node of deletedNodes) {
        cacheNodeDelete(node.id);
      }
      setRfNodes((nds) => nds.filter((n) => !deletedNodes.some((d) => d.id === n.id)));
    },
    [cacheNodeDelete],
  );

  // Handle Backspace and undo/redo at the document level because ReactFlow's deleteKeyCode
  // only works when its container has focus, but clicking nodes often moves
  // focus to BODY.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (mode !== "edit") return;
      const tag = (e.target as HTMLElement)?.tagName;
      const editable = tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || (e.target as HTMLElement)?.isContentEditable;

      // Ctrl+Z / Cmd+Z → undo
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && e.key === "z") {
        if (editable) return;
        e.preventDefault();
        useWorkflowEditorStore.getState().undo();
        return;
      }

      // Ctrl+Shift+Z / Cmd+Shift+Z → redo
      if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === "z") {
        if (editable) return;
        e.preventDefault();
        useWorkflowEditorStore.getState().redo();
        return;
      }

      if (e.key === "Backspace") {
        if (editable) return;
        const store = useWorkflowEditorStore.getState();
        const nodeIds = store.selectedNodeIds.length > 0 ? store.selectedNodeIds : store.selectedNodeId ? [store.selectedNodeId] : [];
        if (nodeIds.length > 0) {
          e.preventDefault();
          for (const nodeId of nodeIds) {
            cacheNodeDelete(nodeId);
          }
          store.setSelectedNodeIds([]);
          store.selectNode(null);
          setRfNodes((nds) => nds.filter((n) => !nodeIds.includes(n.id)));
        } else if (store.selectedEdgeId) {
          e.preventDefault();
          const edgeId = store.selectedEdgeId;
          store.selectEdge(null);
          onEdgeDelete?.(edgeId);
        }
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [mode, cacheNodeDelete, onEdgeDelete]);

  const handleConnect = useCallback(
    (conn: Connection) => {
      if (conn.source && conn.target) {
        onEdgeCreate?.(conn.source, conn.target);
      }
    },
    [onEdgeCreate],
  );

  const handleEdgeClick = useCallback(
    (_: React.MouseEvent, edge: Edge) => {
      selectEdge(edge.id);
    },
    [selectEdge],
  );

  const handlePaneClick = useCallback(() => {
    selectNode(null);
    selectEdge(null);
    setSelectedNodeIds([]);
  }, [selectNode, selectEdge, setSelectedNodeIds]);

  const handleDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  }, []);

  const handleDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();
      const dragType = event.dataTransfer.getData("application/x-multica-shape");
      if (!dragType) return;
      const position = screenToFlowPosition({ x: event.clientX, y: event.clientY });
      onNodeCreate?.(dragType, position.x, position.y);
    },
    [screenToFlowPosition, onNodeCreate],
  );

  // Build a nodeId -> color map for the MiniMap preview
  const miniMapNodeColors = useMemo(() => {
    const map: Record<string, string> = {};
    for (const n of nodes) {
      const { nodeColor } = parseNodeFormat(n.format_schema);
      if (nodeColor) map[n.id] = nodeColor;
    }
    return map;
  }, [nodes]);

  return (
    <ReactFlow
      nodes={rfNodes}
      edges={rfEdges}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      onNodeClick={handleNodeClick}
      onNodeDragStart={handleNodeDragStart}
      onNodeDragStop={handleNodeDragStop}
      onNodesChange={handleNodesChange}
      onConnect={handleConnect}
      onEdgeClick={handleEdgeClick}
      onEdgesChange={handleEdgesChange}
      onPaneClick={handlePaneClick}
      onDragOver={handleDragOver}
      onDrop={handleDrop}
      onNodesDelete={handleNodesDelete}
      onSelectionChange={handleSelectionChange}
      selectionOnDrag={mode !== "view"}
      multiSelectionKeyCode="Shift"
      deleteKeyCode={mode !== "view" ? "Backspace" : null}
      connectionMode={ConnectionMode.Loose}
      nodesDraggable={mode !== "view"}
      nodesConnectable={mode !== "view"}
      nodesFocusable
      elementsSelectable
      fitView={shouldFitView}
      colorMode={canvasColorMode}
    >
      <Background />
      <Controls />
      {showMiniMap && <MiniMap nodeColor={(node) => miniMapNodeColors[node.id] ?? "#e2e8f0"} />}
      {alignmentGuides.length > 0 && (
        <svg
          style={{
            position: "absolute",
            top: 0,
            left: 0,
            width: "100%",
            height: "100%",
            overflow: "visible",
            pointerEvents: "none",
            zIndex: 10,
          }}
        >
          {alignmentGuides.map((g, i) => (
            <line
              key={i}
              x1={g.orientation === "vertical" ? g.position : g.start}
              y1={g.orientation === "vertical" ? g.start : g.position}
              x2={g.orientation === "vertical" ? g.position : g.end}
              y2={g.orientation === "vertical" ? g.end : g.position}
              stroke="#3b82f6"
              strokeWidth={1}
              strokeDasharray="4 2"
            />
          ))}
        </svg>
      )}
    </ReactFlow>
  );
}

/** @deprecated Use WorkflowCanvas instead */
export const DAGCanvas = WorkflowCanvas;
