"use client";

import { useMemo } from "react";
import type { WorkflowEdge, WorkflowNode } from "@multica/core/types";

export interface DataFlowArrowProps {
  edges: WorkflowEdge[];
  nodes: WorkflowNode[];
  sourceStageId: string;
  targetStageId: string;
}

export function DataFlowArrow({
  edges,
  nodes,
  sourceStageId,
  targetStageId,
}: DataFlowArrowProps) {
  const nodeMap = useMemo(
    () => new Map(nodes.map((node) => [node.id, node])),
    [nodes],
  );

  const matchingEdges = useMemo(
    () =>
      edges
        .map((edge) => {
          const sourceNode = nodeMap.get(edge.source_node_id);
          const targetNode = nodeMap.get(edge.target_node_id);
          if (!sourceNode || !targetNode) return null;
          if (sourceNode.stage_id !== sourceStageId) return null;
          if (targetNode.stage_id !== targetStageId) return null;
          return {
            edge,
            sourceNode,
            targetNode,
          };
        })
        .filter((item): item is NonNullable<typeof item> => item !== null),
    [edges, nodeMap, sourceStageId, targetStageId],
  );

  const label = matchingEdges
    .slice(0, 2)
    .map(({ sourceNode, targetNode }) => `${sourceNode.title} -> ${targetNode.title}`)
    .join(" | ");

  return (
    <div
      data-testid="data-flow-arrow"
      className="flex items-center justify-center py-1"
    >
      <div className="flex flex-col items-center gap-1 text-slate-500">
        <div className="h-1.5 w-px bg-slate-300/90" aria-hidden="true" />
        <div
          data-testid="data-flow-arrow-pill"
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border border-slate-300/80 bg-white/95 text-slate-500 shadow-[0_6px_14px_rgba(148,163,184,0.16)]"
        >
          <svg
            data-testid="data-flow-arrow-line"
            width="18"
            height="18"
            viewBox="0 0 22 22"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.9"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M11 4v10" />
            <path d="m7 11 4 5 4-5" />
          </svg>
        </div>
        {label ? (
          <div className="max-w-[min(92vw,680px)] rounded-full border border-slate-300/65 bg-white/88 px-3 py-1 text-[11px] font-medium text-slate-600 shadow-sm backdrop-blur-sm">
            {label}
          </div>
        ) : null}
        <div className="h-1.5 w-px bg-slate-300/90" aria-hidden="true" />
      </div>
    </div>
  );
}
