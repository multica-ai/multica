"use client";

import { memo } from "react";
import { BaseEdge, type EdgeProps, getBezierPath } from "@xyflow/react";

function WorkflowEdge({
  id,
  sourceX, sourceY, targetX, targetY,
  sourcePosition, targetPosition,
  selected,
}: EdgeProps) {
  const [edgePath] = getBezierPath({
    sourceX, sourceY, sourcePosition,
    targetX, targetY, targetPosition,
  });

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        className={selected ? "!stroke-primary !stroke-[3px]" : "!stroke-muted-foreground/60"}
      />
      {/* Arrow head */}
      <circle
        cx={targetX}
        cy={targetY}
        r={4}
        fill={selected ? "var(--primary)" : "hsl(var(--muted-foreground))"}
      />
    </>
  );
}

export const WorkflowEdgeComponent = memo(WorkflowEdge);
