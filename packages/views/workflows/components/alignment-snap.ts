import type { Node } from "@xyflow/react";
import type { WorkflowNodeData } from "./reactflow-nodes";

export interface AlignmentGuide {
  orientation: "vertical" | "horizontal";
  /** Flow coordinate of the guide line (x for vertical, y for horizontal) */
  position: number;
  /** Start coordinate in the perpendicular axis */
  start: number;
  /** End coordinate in the perpendicular axis */
  end: number;
}

interface SnapResult {
  x: number;
  y: number;
  guides: AlignmentGuide[];
}

const SNAP_THRESHOLD = 5;

const DEFAULT_WIDTH = 150;
const DEFAULT_HEIGHT = 70;

function nodeEdges(node: Node<WorkflowNodeData>) {
  const w = (node.measured?.width ?? node.width ?? DEFAULT_WIDTH) as number;
  const h = (node.measured?.height ?? node.height ?? DEFAULT_HEIGHT) as number;
  const x = node.position.x;
  const y = node.position.y;
  return {
    left: x,
    right: x + w,
    top: y,
    bottom: y + h,
    centerX: x + w / 2,
    centerY: y + h / 2,
    width: w,
    height: h,
  };
}

/**
 * Compute snap position and alignment guides for a single dragging node.
 * Compares the dragging node's edges/center against all other visible nodes.
 */
export function computeAlignmentSnap(
  draggingNodeId: string,
  proposedX: number,
  proposedY: number,
  allNodes: Node<WorkflowNodeData>[],
  threshold: number = SNAP_THRESHOLD,
): SnapResult {
  const others = allNodes.filter((n) => n.id !== draggingNodeId);
  if (others.length === 0) return { x: proposedX, y: proposedY, guides: [] };

  const self = allNodes.find((n) => n.id === draggingNodeId);
  const dw = (self?.measured?.width ?? self?.width ?? DEFAULT_WIDTH) as number;
  const dh = (self?.measured?.height ?? self?.height ?? DEFAULT_HEIGHT) as number;

  const dLeft = proposedX;
  const dRight = proposedX + dw;
  const dTop = proposedY;
  const dBottom = proposedY + dh;
  const dCenterX = proposedX + dw / 2;
  const dCenterY = proposedY + dh / 2;

  let snappedX = proposedX;
  let snappedY = proposedY;
  const guides: AlignmentGuide[] = [];

  for (const other of others) {
    const o = nodeEdges(other);

    // ── Vertical (X-axis) ──
    if (Math.abs(dLeft - o.left) < threshold) {
      snappedX = o.left;
      guides.push({ orientation: "vertical", position: o.left, start: Math.min(dTop, o.top), end: Math.max(dBottom, o.bottom) });
    }
    if (Math.abs(dRight - o.right) < threshold) {
      snappedX = o.right - dw;
      guides.push({ orientation: "vertical", position: o.right, start: Math.min(dTop, o.top), end: Math.max(dBottom, o.bottom) });
    }
    if (Math.abs(dCenterX - o.centerX) < threshold) {
      snappedX = o.centerX - dw / 2;
      guides.push({ orientation: "vertical", position: o.centerX, start: Math.min(dTop, o.top), end: Math.max(dBottom, o.bottom) });
    }
    if (Math.abs(dLeft - o.right) < threshold) {
      snappedX = o.right;
      guides.push({ orientation: "vertical", position: o.right, start: Math.min(dTop, o.top), end: Math.max(dBottom, o.bottom) });
    }
    if (Math.abs(dRight - o.left) < threshold) {
      snappedX = o.left - dw;
      guides.push({ orientation: "vertical", position: o.left, start: Math.min(dTop, o.top), end: Math.max(dBottom, o.bottom) });
    }

    // ── Horizontal (Y-axis) ──
    if (Math.abs(dTop - o.top) < threshold) {
      snappedY = o.top;
      guides.push({ orientation: "horizontal", position: o.top, start: Math.min(dLeft, o.left), end: Math.max(dRight, o.right) });
    }
    if (Math.abs(dBottom - o.bottom) < threshold) {
      snappedY = o.bottom - dh;
      guides.push({ orientation: "horizontal", position: o.bottom, start: Math.min(dLeft, o.left), end: Math.max(dRight, o.right) });
    }
    if (Math.abs(dCenterY - o.centerY) < threshold) {
      snappedY = o.centerY - dh / 2;
      guides.push({ orientation: "horizontal", position: o.centerY, start: Math.min(dLeft, o.left), end: Math.max(dRight, o.right) });
    }
    if (Math.abs(dTop - o.bottom) < threshold) {
      snappedY = o.bottom;
      guides.push({ orientation: "horizontal", position: o.bottom, start: Math.min(dLeft, o.left), end: Math.max(dRight, o.right) });
    }
    if (Math.abs(dBottom - o.top) < threshold) {
      snappedY = o.top - dh;
      guides.push({ orientation: "horizontal", position: o.top, start: Math.min(dLeft, o.left), end: Math.max(dRight, o.right) });
    }
  }

  return { x: snappedX, y: snappedY, guides };
}
