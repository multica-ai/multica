"use client";

import { useMemo } from "react";
import type { WorkflowEdge, WorkflowNode, WorkflowStage } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

export interface EdgePath {
  edgeId: string;
  d: string;
  type: "horizontal" | "cross-stage" | "arc" | "critic";
  dashed: boolean;
  colorIndex: number;
}

export interface PanoramaSvgOverlayProps {
  edges: WorkflowEdge[];
  nodes: WorkflowNode[];
  stages: WorkflowStage[];
  nodePositions: Map<string, DOMRect>;
  criticPositions: Map<string, DOMRect>;
  className?: string;
}

function stageColorIndex(node: WorkflowNode, stageMap: Map<string, WorkflowStage>): number {
  if (!node.stage_id) return 0;
  const stage = stageMap.get(node.stage_id);
  return Math.abs(stage?.sort_order ?? 0) % 6;
}

/** Pure function: compute SVG path strings from edge + position data. */
export function computeEdgePaths(
  edges: WorkflowEdge[],
  nodes: WorkflowNode[],
  stages: WorkflowStage[],
  nodePositions: Map<string, DOMRect>,
  criticPositions: Map<string, DOMRect>,
): EdgePath[] {
  const nodeMap = new Map(nodes.map((n) => [n.id, n]));
  const stageMap = new Map(stages.map((s) => [s.id, s]));
  const results: EdgePath[] = [];
  const arcLaneCounts = new Map<string, number>();
  const crossLaneCounts = new Map<string, number>();

  for (const edge of edges) {
    const sourceNode = nodeMap.get(edge.source_node_id);
    const targetNode = nodeMap.get(edge.target_node_id);
    if (!sourceNode || !targetNode) continue;

    const sourceRect = nodePositions.get(edge.source_node_id);
    const targetRect = nodePositions.get(edge.target_node_id);
    if (!sourceRect || !targetRect) continue;

    const colorIndex = stageColorIndex(sourceNode, stageMap);
    const isSameStage = sourceNode.stage_id === targetNode.stage_id;
    const isAdjacent = Math.abs(sourceNode.sort_order - targetNode.sort_order) === 1;

    if (isSameStage && isAdjacent) {
      // Horizontal: source right edge center -> target left edge center
      const x1 = sourceRect.right;
      const y1 = sourceRect.top + sourceRect.height / 2;
      const x2 = targetRect.left;
      const y2 = y1;
      const midX = Math.round((x1 + x2) / 2);
      results.push({
        edgeId: edge.id,
        d: `M ${x1} ${y1} L ${midX} ${y1} L ${midX} ${y2} L ${x2} ${y2}`,
        type: "horizontal",
        dashed: false,
        colorIndex,
      });
    } else if (!isSameStage) {
      // Cross-stage: source bottom center -> target top center through a shared orthogonal channel.
      const x1 = sourceRect.left + sourceRect.width / 2;
      const y1 = sourceRect.bottom;
      const x2 = targetRect.left + targetRect.width / 2;
      const y2 = targetRect.top;
      const key = `${sourceNode.stage_id ?? "none"}:${targetNode.stage_id ?? "none"}`;
      const lane = crossLaneCounts.get(key) ?? 0;
      crossLaneCounts.set(key, lane + 1);
      const laneOffset = (lane % 2 === 0 ? 1 : -1) * Math.ceil(lane / 2) * 18;
      const channelY = Math.round((y1 + y2) / 2);
      const channelX = Math.round((x1 + x2) / 2 + laneOffset);
      results.push({
        edgeId: edge.id,
        d: `M ${x1} ${y1} L ${x1} ${channelY} L ${channelX} ${channelY} L ${x2} ${channelY} L ${x2} ${y2}`,
        type: "cross-stage",
        dashed: false,
        colorIndex,
      });
    } else {
      // Same stage, non-adjacent: route above the node row on separated orthogonal lanes.
      const x1 = sourceRect.right;
      const y1 = sourceRect.top + sourceRect.height / 2;
      const x2 = targetRect.left;
      const y2 = targetRect.top + targetRect.height / 2;
      const key = sourceNode.stage_id ?? "none";
      const lane = arcLaneCounts.get(key) ?? 0;
      arcLaneCounts.set(key, lane + 1);
      const arcY = Math.max(12, Math.min(sourceRect.top, targetRect.top) - 36 - lane * 16);
      results.push({
        edgeId: edge.id,
        d: `M ${x1} ${y1} L ${x1 + 24} ${y1} L ${x1 + 24} ${arcY} L ${x2 - 24} ${arcY} L ${x2 - 24} ${y2} L ${x2} ${y2}`,
        type: "arc",
        dashed: false,
        colorIndex,
      });
    }
  }

  // Critic connections: worker card bottom -> critic card top
  for (const [nodeId, criticRect] of criticPositions) {
    const nodeRect = nodePositions.get(nodeId);
    if (!nodeRect) continue;
    const x1 = nodeRect.left + nodeRect.width / 2;
    const y1 = nodeRect.bottom;
    const x2 = criticRect.left + criticRect.width / 2;
    const y2 = criticRect.top;
    results.push({
      edgeId: `critic-${nodeId}`,
      d: `M ${x1} ${y1} L ${x2} ${y2}`,
      type: "critic",
      dashed: true,
      colorIndex: stageColorIndex(nodeMap.get(nodeId) ?? nodes[0]!, stageMap),
    });
  }

  return results;
}

const STAGE_LINE_COLORS = [
  "text-slate-400",
  "text-stone-400",
  "text-blue-400",
  "text-rose-300",
  "text-violet-400",
  "text-amber-400",
] as const;

export function PanoramaSvgOverlay({
  edges,
  nodes,
  stages,
  nodePositions,
  criticPositions,
  className,
}: PanoramaSvgOverlayProps) {
  const paths = useMemo(
    () => computeEdgePaths(edges, nodes, stages, nodePositions, criticPositions),
    [edges, nodes, stages, nodePositions, criticPositions],
  );

  if (paths.length === 0) return null;

  return (
    <svg
      className={cn("pointer-events-none absolute inset-0 z-10", className)}
      width="100%"
      height="100%"
      aria-hidden="true"
    >
      <defs>
        {STAGE_LINE_COLORS.map((colorClass, index) => (
          <marker
            key={colorClass}
            id={`panorama-arrowhead-${index}`}
            className={colorClass}
            viewBox="0 0 10 10"
            refX={6}
            refY={5}
            markerWidth={8}
            markerHeight={8}
            orient="auto-start-reverse"
          >
            <path
              d="M 0 0 L 10 5 L 0 10 z"
              fill="currentColor"
              opacity={1}
            />
          </marker>
        ))}
      </defs>
      {paths.map((path) => (
        <path
          key={path.edgeId}
          d={path.d}
          className={cn(
            STAGE_LINE_COLORS[path.colorIndex] ?? STAGE_LINE_COLORS[0],
            "opacity-35",
          )}
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeDasharray={path.dashed ? "4 3" : undefined}
          markerEnd={`url(#panorama-arrowhead-${path.colorIndex})`}
        />
      ))}
    </svg>
  );
}
