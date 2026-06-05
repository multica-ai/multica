"use client";

import { memo, useCallback } from "react";
import { Handle, Position, NodeResizer, type NodeProps, type EdgeProps, BaseEdge, getBezierPath, getStraightPath } from "@xyflow/react";
import { cn } from "@multica/ui/lib/utils";
import type { NodeShape } from "@multica/core/types";

export interface WorkflowNodeData extends Record<string, unknown> {
  title: string;
  statusColor?: string;
  statusLabel?: string;
  isRunning?: boolean;
  isEditing?: boolean;
  shape?: NodeShape;
  nodeColor?: string;
  fontSize?: number;
  onNodeSelect?: (nodeId: string) => void;
  onNodeResizeStart?: () => void;
  onNodeResizeEnd?: (nodeId: string, width: number, height: number) => void;
}

export const NODE_WIDTH = 150;
export const NODE_HEIGHT = 70;
export const DIAMOND_SIZE = 180;
export const HEXAGON_SIZE = 200;

// --- SVG shape renderers ---

interface ShapeRendererProps {
  w: number;
  h: number;
  selected: boolean;
  statusColor?: string;
  nodeColor?: string;
}

function RectangleShape({ w, h, selected, statusColor, nodeColor }: ShapeRendererProps) {
  const fill = nodeColor ?? statusColor ?? "var(--card)";
  const stroke = selected ? "var(--primary)" : (nodeColor || statusColor) ? "transparent" : "var(--border)";
  return (
    <svg width={w} height={h} className="absolute inset-0 overflow-visible">
      <rect x="1" y="1" width={w - 2} height={h - 2} rx="8"
        fill={fill} stroke={stroke} strokeWidth="2" />
    </svg>
  );
}

function PillShape({ w, h, selected, statusColor, nodeColor }: ShapeRendererProps) {
  const fill = nodeColor ?? statusColor ?? "var(--card)";
  const stroke = selected ? "var(--primary)" : (nodeColor || statusColor) ? "transparent" : "var(--border)";
  const rx = (h - 2) / 2;
  return (
    <svg width={w} height={h} className="absolute inset-0 overflow-visible">
      <rect x="1" y="1" width={w - 2} height={h - 2} rx={rx}
        fill={fill} stroke={stroke} strokeWidth="2" />
    </svg>
  );
}

function DiamondShape({ w, h, selected, statusColor, nodeColor }: ShapeRendererProps) {
  const fill = nodeColor ?? statusColor ?? "var(--card)";
  const stroke = selected ? "var(--primary)" : (nodeColor || statusColor) ? "transparent" : "var(--border)";
  return (
    <svg width={w} height={h} className="absolute inset-0 overflow-visible">
      <polygon
        points={`${w / 2},1 ${w - 1},${h / 2} ${w / 2},${h - 1} 1,${h / 2}`}
        fill={fill} stroke={stroke} strokeWidth="2" />
    </svg>
  );
}

function HexagonShape({ w, h, selected, statusColor, nodeColor }: ShapeRendererProps) {
  const fill = nodeColor ?? statusColor ?? "var(--card)";
  const stroke = selected ? "var(--primary)" : (nodeColor || statusColor) ? "transparent" : "var(--border)";
  const dx = w * 0.25;
  return (
    <svg width={w} height={h} className="absolute inset-0 overflow-visible">
      <polygon
        points={`${dx},1 ${w - dx},1 ${w - 1},${h / 2} ${w - dx},${h - 1} ${dx},${h - 1} 1,${h / 2}`}
        fill={fill} stroke={stroke} strokeWidth="2" />
    </svg>
  );
}

const SHAPE_RENDERERS: Record<NodeShape, React.FC<ShapeRendererProps>> = {
  rectangle: RectangleShape,
  diamond: DiamondShape,
  pill: PillShape,
  hexagon: HexagonShape,
};

// --- Shared node renderer ---

function WorkflowNodeRenderer({ id, data, selected, width: nodeWidth, height: nodeHeight }: NodeProps) {
  const nodeData = data as unknown as WorkflowNodeData;
  const { title, statusColor, statusLabel, isRunning, isEditing, shape, nodeColor, fontSize, onNodeSelect, onNodeResizeStart, onNodeResizeEnd } = nodeData;
  // Allow NodeResizer to override dimensions; fall back to shape defaults.
  const baseW = nodeWidth ?? (shape === "diamond" ? DIAMOND_SIZE : shape === "hexagon" ? HEXAGON_SIZE : NODE_WIDTH);
  const baseH = nodeHeight ?? (shape === "diamond" || shape === "hexagon"
    ? (statusLabel ? (shape === "diamond" ? DIAMOND_SIZE + 20 : HEXAGON_SIZE + 20) : (shape === "diamond" ? DIAMOND_SIZE : HEXAGON_SIZE))
    : (statusLabel ? NODE_HEIGHT + 20 : NODE_HEIGHT));
  const w = Math.max(60, baseW);
  const h = Math.max(40, baseH);

  const ShapeComp = SHAPE_RENDERERS[shape as NodeShape];

  const handleClick = useCallback(() => {
    if (!isEditing) return;
    onNodeSelect?.(id);
  }, [id, isEditing, onNodeSelect]);

  return (
    <div
      onClick={handleClick}
      className={cn(
        "relative flex items-center justify-center text-card-foreground",
        shape !== "diamond" && shape !== "hexagon" && "rounded-lg",
        isEditing && "cursor-pointer",
      )}
      style={{ width: w, height: h }}
    >
      <ShapeComp w={w} h={h} selected={selected} statusColor={statusColor} nodeColor={nodeColor} />

      {isEditing && selected && (
        <NodeResizer
          isVisible
          minWidth={60}
          minHeight={40}
          keepAspectRatio={shape === "diamond" || shape === "hexagon"}
          onResizeStart={() => onNodeResizeStart?.()}
          onResizeEnd={(_, params) => onNodeResizeEnd?.(id, params.width, params.height)}
        />
      )}

      {/* Handles offset from viewport center */}
      <Handle type="target" position={Position.Top} id="top" isConnectable={isEditing} className={cn("!border-0 !bg-muted-foreground/40", !isEditing && "!pointer-events-none !opacity-0")} style={{ left: w / 2 - 2 }} />
      <Handle type="source" position={Position.Bottom} id="bottom" isConnectable={isEditing} className={cn("!border-0 !bg-muted-foreground/40", !isEditing && "!pointer-events-none !opacity-0")} style={{ left: w / 2 - 2 }} />
      <Handle type="source" position={Position.Right} id="right" isConnectable={isEditing} className={cn("!border-0 !bg-muted-foreground/40", !isEditing && "!pointer-events-none !opacity-0")} />
      <Handle type="target" position={Position.Left} id="left" isConnectable={isEditing} className={cn("!border-0 !bg-muted-foreground/40", !isEditing && "!pointer-events-none !opacity-0")} />

      <div
        className={cn(
          "relative z-10 flex flex-col items-center justify-center px-2 text-center",
          (shape === "diamond" || shape === "hexagon") && "px-4 max-w-[70%]",
        )}
      >
        <div className="flex items-center gap-1.5 min-w-0">
          {isRunning && (
            <svg className="animate-spin shrink-0" width="12" height="12" viewBox="0 0 12 12">
              <circle cx="6" cy="6" r="4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeDasharray="18 8" className="text-primary" />
            </svg>
          )}
          <span className="truncate font-medium" style={{ fontSize: fontSize ? `${fontSize}px` : undefined }}>{title}</span>
        </div>
        {statusLabel && (
          <span className="text-[10px] text-muted-foreground truncate mt-0.5">{statusLabel}</span>
        )}
      </div>
    </div>
  );
}

// --- Edge component ---
const STRAIGHT_ALIGNMENT_THRESHOLD = 60;

function shouldUseStraightPath(
  sourceX: number, sourceY: number,
  targetX: number, targetY: number,
  sourcePosition: Position, targetPosition: Position,
): boolean {
  // Source bottom → Target top: nodes are roughly stacked vertically
  if (sourcePosition === Position.Bottom && targetPosition === Position.Top) {
    return Math.abs(sourceX - targetX) < STRAIGHT_ALIGNMENT_THRESHOLD;
  }
  // Source right → Target left: nodes are roughly aligned horizontally
  if (sourcePosition === Position.Right && targetPosition === Position.Left) {
    return Math.abs(sourceY - targetY) < STRAIGHT_ALIGNMENT_THRESHOLD;
  }
  // L-shaped connections (e.g. Bottom → Left, Right → Top) always use step
  return false;
}

function WorkflowEdgeComponent({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  selected,
  markerEnd,
}: EdgeProps) {
  const useStraight = shouldUseStraightPath(
    sourceX, sourceY, targetX, targetY,
    sourcePosition, targetPosition,
  );

  const [edgePath] = useStraight
    ? getStraightPath({ sourceX, sourceY, targetX, targetY })
    : getBezierPath({
        sourceX,
        sourceY,
        sourcePosition,
        targetX,
        targetY,
        targetPosition,
      });

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        className={cn(
          selected ? "!stroke-primary" : "!stroke-muted-foreground/60",
          selected && "[filter:drop-shadow(0_0_3px_hsl(var(--primary)/0.4))]",
        )}
        strokeWidth={selected ? 2.5 : 1.5}
        markerEnd={markerEnd}
      />
    </>
  );
}

// --- Annotation node (sticky note) ---

export const ANNO_WIDTH = 160;
export const ANNO_HEIGHT = 100;

function AnnotationNodeRenderer({ id, data, selected }: NodeProps) {
  const nodeData = data as unknown as WorkflowNodeData;
  const { title, nodeColor, fontSize, onNodeSelect, isEditing } = nodeData;

  const handleClick = useCallback(() => {
    if (!isEditing) return;
    onNodeSelect?.(id);
  }, [id, isEditing, onNodeSelect]);

  const bg = nodeColor ?? "#fef9c3";

  return (
    <div
      onClick={handleClick}
      className="relative cursor-pointer"
      style={{ width: ANNO_WIDTH, height: ANNO_HEIGHT }}
    >
      {/* Hidden handle for edge-based connector lines */}
      <Handle type="source" position={Position.Right} id="anno-right" isConnectable={false} className="!pointer-events-none !opacity-0" style={{ top: ANNO_HEIGHT / 2, right: 0 }} />
      <Handle type="target" position={Position.Left} id="anno-left" isConnectable={false} className="!pointer-events-none !opacity-0" style={{ top: ANNO_HEIGHT / 2, left: 0 }} />
      {/* Sticky note shadow/body */}
      <div
        className="absolute inset-0 rounded-sm shadow-md"
        style={{
          backgroundColor: bg,
          transform: "rotate(-0.8deg)",
          border: "1px solid rgba(0,0,0,0.08)",
        }}
      />
      {/* Text content */}
      <div
        className="absolute inset-0 flex items-center justify-center p-3"
        style={{ transform: "rotate(-0.8deg)" }}
      >
        <span
          className="text-center leading-snug text-foreground/80"
          style={{ fontSize: fontSize ? `${fontSize}px` : "12px", fontFamily: "var(--font-sans, 'Comic Sans MS', 'KaiTi', cursive)" }}
        >
          {title || "Note"}
        </span>
      </div>
      {/* Selected ring */}
      {selected && (
        <div className="absolute -inset-1 rounded-sm ring-2 ring-primary pointer-events-none" style={{ transform: "rotate(-0.8deg)" }} />
      )}
    </div>
  );
}

// --- Annotation connector edge (dashed, no arrow) ---

function AnnotationConnectorEdgeRenderer({
  sourceX, sourceY, targetX, targetY,
}: EdgeProps) {
  const [edgePath] = getStraightPath({ sourceX, sourceY, targetX, targetY });
  return (
    <BaseEdge
      path={edgePath}
      className="!stroke-slate-400"
      strokeWidth={1}
      strokeDasharray="5 4"
    />
  );
}

export const WorkflowNode = memo(WorkflowNodeRenderer);
export const AnnotationNode = memo(AnnotationNodeRenderer);
export const WorkflowEdge = memo(WorkflowEdgeComponent);
export const AnnotationConnectorEdge = memo(AnnotationConnectorEdgeRenderer);
