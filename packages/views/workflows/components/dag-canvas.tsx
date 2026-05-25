"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useWorkflowEditorStore } from "@multica/core/workflows/store";
import type { WorkflowNode, WorkflowEdge } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

const NODE_WIDTH = 180;
const NODE_HEIGHT = 64;

interface DAGCanvasProps {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  onNodeMoved?: (nodeId: string, x: number, y: number) => void;
  onEdgeCreate?: (sourceNodeId: string, targetNodeId: string) => void;
  onNodeClick?: (nodeId: string) => void;
  onNodeDoubleClick?: (nodeId: string) => void;
  onAutoLayout?: () => void;
  nodeStatusColors?: Record<string, string>;
}

interface NodeRect {
  id: string;
  title: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

function getNodeRect(node: WorkflowNode): NodeRect {
  return {
    id: node.id,
    title: node.title,
    x: node.position_x,
    y: node.position_y,
    w: NODE_WIDTH,
    h: NODE_HEIGHT,
  };
}

function nodeCenter(rect: NodeRect): { cx: number; cy: number } {
  return { cx: rect.x + rect.w / 2, cy: rect.y + rect.h / 2 };
}

export function DAGCanvas({
  nodes,
  edges,
  onNodeMoved,
  onEdgeCreate,
  onNodeClick,
  onNodeDoubleClick,
  nodeStatusColors,
}: DAGCanvasProps) {
  const selectedNodeId = useWorkflowEditorStore((s) => s.selectedNodeId);
  const mode = useWorkflowEditorStore((s) => s.mode);
  const pendingEdgeSource = useWorkflowEditorStore((s) => s.pendingEdgeSource);
  const setPendingEdgeSource = useWorkflowEditorStore((s) => s.setPendingEdgeSource);
  const selectNode = useWorkflowEditorStore((s) => s.selectNode);

  const svgRef = useRef<SVGSVGElement>(null);
  const [dragging, setDragging] = useState<{ nodeId: string; startX: number; startY: number; nodeStartX: number; nodeStartY: number } | null>(null);
  const [panning, setPanning] = useState<{ startX: number; startY: number; startOffsetX: number; startOffsetY: number } | null>(null);
  const pannedRef = useRef(false);
  const [mousePos, setMousePos] = useState<{ x: number; y: number } | null>(null);
  const [scale, setScale] = useState(1.5);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const scaleRef = useRef(scale);
  const offsetRef = useRef(offset);
  scaleRef.current = scale;
  offsetRef.current = offset;

  const rects = nodes.map(getNodeRect);
  const rectMap = new Map(rects.map((r) => [r.id, r]));

  const svgToLocal = useCallback((clientX: number, clientY: number) => {
    const svg = svgRef.current;
    if (!svg) return { x: 0, y: 0 };
    const pt = svg.createSVGPoint();
    pt.x = clientX;
    pt.y = clientY;
    const ctm = svg.getScreenCTM();
    if (!ctm) return { x: 0, y: 0 };
    const local = pt.matrixTransform(ctm.inverse());
    return { x: local.x, y: local.y };
  }, []);

  // Convert screen coords into the inner <g> coordinate space (after zoom+pan).
  const screenToGroup = useCallback((clientX: number, clientY: number) => {
    const local = svgToLocal(clientX, clientY);
    return {
      x: (local.x - offset.x) / scale,
      y: (local.y - offset.y) / scale,
    };
  }, [svgToLocal, offset, scale]);

  const handleMouseDown = (e: React.MouseEvent, nodeId: string) => {
    if (e.button !== 0) return;
    e.stopPropagation();
    e.preventDefault();

    if (mode === "connect") {
      if (!pendingEdgeSource) {
        setPendingEdgeSource(nodeId);
        selectNode(null);
      } else if (pendingEdgeSource !== nodeId) {
        onEdgeCreate?.(pendingEdgeSource, nodeId);
        setPendingEdgeSource(null);
      }
      return;
    }

    selectNode(nodeId);
    onNodeClick?.(nodeId);

    const rect = rectMap.get(nodeId);
    if (rect && mode === "edit") {
      const local = screenToGroup(e.clientX, e.clientY);
      setDragging({
        nodeId,
        startX: local.x,
        startY: local.y,
        nodeStartX: rect.x,
        nodeStartY: rect.y,
      });
    }
  };

  // Use window-level listeners during drag/connect/pan so movement tracks
  // smoothly even when the cursor leaves the SVG.
  useEffect(() => {
    if (!dragging && !pendingEdgeSource && !panning) return;

    const handleWindowMove = (e: MouseEvent) => {
      if (panning) {
        const local = svgToLocal(e.clientX, e.clientY);
        const dx = local.x - panning.startX;
        const dy = local.y - panning.startY;
        if (Math.abs(dx) > 2 || Math.abs(dy) > 2) pannedRef.current = true;
        setOffset({ x: panning.startOffsetX + dx, y: panning.startOffsetY + dy });
        return;
      }
      const local = screenToGroup(e.clientX, e.clientY);
      if (dragging) {
        const dx = local.x - dragging.startX;
        const dy = local.y - dragging.startY;
        onNodeMoved?.(dragging.nodeId, Math.round(dragging.nodeStartX + dx), Math.round(dragging.nodeStartY + dy));
      }
      if (pendingEdgeSource) {
        setMousePos(local);
      }
    };

    const handleWindowUp = () => {
      setDragging(null);
      setPanning(null);
    };

    window.addEventListener("mousemove", handleWindowMove);
    window.addEventListener("mouseup", handleWindowUp);
    return () => {
      window.removeEventListener("mousemove", handleWindowMove);
      window.removeEventListener("mouseup", handleWindowUp);
    };
  }, [dragging, pendingEdgeSource, panning, screenToGroup, svgToLocal, onNodeMoved]);

  const handleCanvasMouseDown = (e: React.MouseEvent) => {
    // Nodes call stopPropagation so this only fires on background.
    if (e.button !== 0) return;
    pannedRef.current = false;
    const local = svgToLocal(e.clientX, e.clientY);
    setPanning({ startX: local.x, startY: local.y, startOffsetX: offset.x, startOffsetY: offset.y });
  };

  const handleCanvasClick = () => {
    if (mode === "connect" && pendingEdgeSource) {
      setPendingEdgeSource(null);
    } else if (!pannedRef.current) {
      selectNode(null);
    }
  };

  // Mouse wheel zoom (centered on cursor)
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;

    const handleWheel = (e: WheelEvent) => {
      e.preventDefault();
      const s = scaleRef.current;
      const o = offsetRef.current;
      const local = svgToLocal(e.clientX, e.clientY);
      const factor = e.deltaY < 0 ? 1.12 : 1 / 1.12;
      const newScale = Math.max(0.15, Math.min(5, s * factor));
      const ratio = newScale / s;
      setScale(newScale);
      setOffset({
        x: local.x - (local.x - o.x) * ratio,
        y: local.y - (local.y - o.y) * ratio,
      });
    };

    svg.addEventListener("wheel", handleWheel, { passive: false });
    return () => svg.removeEventListener("wheel", handleWheel);
  }, [svgToLocal]);

  const handleDoubleClick = (e: React.MouseEvent, nodeId: string) => {
    e.stopPropagation();
    onNodeDoubleClick?.(nodeId);
  };

  return (
    <svg
      ref={svgRef}
      className="w-full h-full cursor-grab active:cursor-grabbing"
      onMouseDown={handleCanvasMouseDown}
      onClick={handleCanvasClick}
      viewBox="0 0 2000 1400"
    >
      <defs>
        <marker
          id="arrowhead"
          viewBox="0 0 10 10"
          refX="9"
          refY="5"
          markerWidth="10"
          markerHeight="10"
          orient="auto"
          overflow="visible"
        >
          <polygon points="0,0 10,5 0,10" fill="#94a3b8" />
        </marker>
      </defs>

      <g transform={`translate(${offset.x} ${offset.y}) scale(${scale})`}>
      {/* Edges */}
      {edges.map((edge) => {
        const source = rectMap.get(edge.source_node_id);
        const target = rectMap.get(edge.target_node_id);
        if (!source || !target) return null;

        const s = nodeCenter(source);
        const t = nodeCenter(target);

	        const isSelected = selectedNodeId === edge.id;

	        // Compute line endpoints at node edges (not centers)
	        const angle = Math.atan2(t.cy - s.cy, t.cx - s.cx);
	        const hw = NODE_WIDTH / 2, hh = NODE_HEIGHT / 2;
	        // Target: stop at rect edge
	        const tEdgeX = t.cx - (hw / Math.abs(Math.cos(angle)) < hh / Math.abs(Math.sin(angle)) ? hw / Math.abs(Math.cos(angle)) : hh / Math.abs(Math.sin(angle))) * Math.cos(angle) * 1.05;
	        const tEdgeY = t.cy - (hw / Math.abs(Math.cos(angle)) < hh / Math.abs(Math.sin(angle)) ? hw / Math.abs(Math.cos(angle)) : hh / Math.abs(Math.sin(angle))) * Math.sin(angle) * 1.05;
	        const tx = Math.abs(Math.cos(angle)) < 0.001 ? t.cx : tEdgeX;
	        const ty = Math.abs(Math.sin(angle)) < 0.001 ? t.cy : tEdgeY;
	        // Arrowhead
	        const aw = 9, ah = 5;
	        const a1x = tx - aw * Math.cos(angle) + ah * Math.sin(angle);
	        const a1y = ty - aw * Math.sin(angle) - ah * Math.cos(angle);
	        const a2x = tx - aw * Math.cos(angle) - ah * Math.sin(angle);
	        const a2y = ty - aw * Math.sin(angle) + ah * Math.cos(angle);

	        return (
	          <g key={edge.id}>
	            <line
	              x1={s.cx}
	              y1={s.cy}
	              x2={tx}
	              y2={ty}
	              stroke="#64748b"
	              className={cn(isSelected && "stroke-primary")}
	              strokeWidth={1.5}
	            />
	            <polygon
	              points={`${tx},${ty} ${a1x},${a1y} ${a2x},${a2y}`}
	              fill="#64748b"
	              className={cn(isSelected && "fill-primary")}
	            />
	          </g>
	        );
      })}

      {/* Pending edge (connect mode) */}
      {pendingEdgeSource && mousePos && (() => {
        const source = rectMap.get(pendingEdgeSource);
        if (!source) return null;
        const s = nodeCenter(source);
        return (
          <line
            x1={s.cx}
            y1={s.cy}
            x2={mousePos.x}
            y2={mousePos.y}
            stroke="currentColor"
            className="text-primary/60"
            strokeWidth={1.5}
            strokeDasharray="6 4"
          />
        );
      })()}

      {/* Nodes */}
      {rects.map((rect) => {
        const isSelected = selectedNodeId === rect.id;
        const statusColor = nodeStatusColors?.[rect.id];

        return (
          <g
            key={rect.id}
            className={cn("cursor-pointer", mode === "connect" && pendingEdgeSource !== rect.id && "cursor-crosshair")}
            onMouseDown={(e) => handleMouseDown(e, rect.id)}
            onDoubleClick={(e) => handleDoubleClick(e, rect.id)}
            onClick={(e) => e.stopPropagation()}
          >
            <rect
              x={rect.x}
              y={rect.y}
              width={rect.w}
              height={rect.h}
              rx={8}
              fill={statusColor ?? "currentColor"}
              className={cn(
                "text-card",
                isSelected && "stroke-primary stroke-[2]",
                !isSelected && "stroke-border"
              )}
              strokeWidth={1}
            />
            <foreignObject
              x={rect.x + 4}
              y={rect.y + 4}
              width={rect.w - 8}
              height={rect.h - 8}
            >
              <div className="flex items-center justify-center h-full text-sm text-center px-2 overflow-hidden">
                <span className="truncate font-medium text-foreground">{rect.title}</span>
              </div>
            </foreignObject>
          </g>
        );
      })}
      </g>
    </svg>
  );
}

// ── Auto-layout helpers ──────────────────────────────────────────────────────

interface LayoutResult {
  nodeId: string;
  x: number;
  y: number;
}

const LAYER_SPACING_X = 280;
const NODE_SPACING_Y = 100;

export function computeAutoLayout(
  nodes: WorkflowNode[],
  edges: WorkflowEdge[],
): LayoutResult[] {
  if (nodes.length === 0) return [];

  // Build adjacency and in-degree maps
  const children = new Map<string, string[]>();
  const indegree = new Map<string, number>();
  for (const n of nodes) {
    children.set(n.id, []);
    indegree.set(n.id, 0);
  }
  for (const e of edges) {
    children.get(e.source_node_id)?.push(e.target_node_id);
    indegree.set(e.target_node_id, (indegree.get(e.target_node_id) ?? 0) + 1);
  }

  // Topological sort + layer assignment (BFS from roots)
  const layer = new Map<string, number>();
  const queue: string[] = [];
  for (const n of nodes) {
    if ((indegree.get(n.id) ?? 0) === 0) {
      layer.set(n.id, 0);
      queue.push(n.id);
    }
  }
  // Handle disconnected nodes
  if (queue.length === 0 && nodes.length > 0) {
    const first = nodes[0]!;
    queue.push(first.id);
    layer.set(first.id, 0);
  }

  while (queue.length > 0) {
    const cur = queue.shift()!;
    const curLayer = layer.get(cur) ?? 0;
    for (const childId of children.get(cur) ?? []) {
      const newLayer = curLayer + 1;
      if (!layer.has(childId) || (layer.get(childId) ?? 0) < newLayer) {
        layer.set(childId, newLayer);
      }
      const deg = (indegree.get(childId) ?? 1) - 1;
      indegree.set(childId, deg);
      if (deg === 0) queue.push(childId);
    }
  }

  // Assign layer to any remaining nodes
  for (const n of nodes) {
    if (!layer.has(n.id)) layer.set(n.id, 0);
  }

  // Group nodes by layer
  const layerGroups = new Map<number, string[]>();
  for (const n of nodes) {
    const l = layer.get(n.id) ?? 0;
    if (!layerGroups.has(l)) layerGroups.set(l, []);
    layerGroups.get(l)!.push(n.id);
  }

  // Position nodes
  const results: LayoutResult[] = [];
  const sortedLayers = [...layerGroups.keys()].sort((a, b) => a - b);
  for (const l of sortedLayers) {
    const ids = layerGroups.get(l)!;
    const totalHeight = (ids.length - 1) * NODE_SPACING_Y;
    const startY = -totalHeight / 2;
    ids.forEach((id, i) => {
      results.push({
        nodeId: id,
        x: 100 + l * LAYER_SPACING_X,
        y: 300 + startY + i * NODE_SPACING_Y,
      });
    });
  }

  return results;
}
