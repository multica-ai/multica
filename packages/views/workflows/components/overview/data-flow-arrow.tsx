"use client";

import { useMemo } from "react";
import type { WorkflowEdge, WorkflowNode, WorkflowStage } from "@multica/core/types";

export interface DataFlowArrowProps {
  edges: WorkflowEdge[];
  nodes: WorkflowNode[];
  stages: WorkflowStage[];
}

/**
 * Renders cross-stage data-flow arrows between swimlanes.
 * Only shows when source and target nodes belong to different stages.
 */
export function DataFlowArrow({ edges, nodes }: DataFlowArrowProps) {
  const nodeMap = useMemo(
    () => new Map(nodes.map((n) => [n.id, n])),
    [nodes],
  );

  const crossStageEdges = useMemo(
    () =>
      edges.filter((e) => {
        const src = nodeMap.get(e.source_node_id);
        const tgt = nodeMap.get(e.target_node_id);
        if (!src || !tgt) return false;
        return src.stage_id !== tgt.stage_id;
      }),
    [edges, nodeMap],
  );

  if (crossStageEdges.length === 0) return null;

  return (
    <div
      data-testid="data-flow-arrow"
      className="flex items-center justify-center py-2"
    >
      <div className="flex items-center gap-1 text-muted-foreground">
        <svg
          width="24"
          height="24"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="rotate-90"
        >
          <line x1="12" y1="5" x2="12" y2="19" />
          <polyline points="19 12 12 19 5 12" />
        </svg>
        <span className="text-xs">{"↓"}</span>
      </div>
    </div>
  );
}
