import dagre from "@dagrejs/dagre";
import type { WorkflowNode, WorkflowEdge } from "@multica/core/types";
import { parseNodeShape } from "@multica/core/types";

const SHAPE_DEFAULTS = {
  rectangle: { width: 150, height: 70 },
  pill: { width: 150, height: 70 },
  diamond: { width: 180, height: 180 },
  hexagon: { width: 200, height: 200 },
} as const;

interface LayoutResult {
  nodeId: string;
  x: number;
  y: number;
}

function getNodeDimensions(formatSchema: unknown): { width: number; height: number } {
  const shape = parseNodeShape(formatSchema);
  const shapeDefaults = SHAPE_DEFAULTS[shape];

  let width: number = shapeDefaults?.width ?? SHAPE_DEFAULTS.rectangle.width;
  let height: number = shapeDefaults?.height ?? SHAPE_DEFAULTS.rectangle.height;

  if (formatSchema && typeof formatSchema === "object" && formatSchema !== null) {
    const obj = formatSchema as Record<string, unknown>;
    if (typeof obj.width === "number" && obj.width > 0) width = obj.width;
    if (typeof obj.height === "number" && obj.height > 0) height = obj.height;
  }

  return { width, height };
}

export function computeAutoLayout(
  nodes: WorkflowNode[],
  edges: WorkflowEdge[],
): LayoutResult[] {
  if (nodes.length === 0) return [];

  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: "LR", nodesep: 60, ranksep: 150, marginx: 100, marginy: 100 });

  for (const node of nodes) {
    const { width, height } = getNodeDimensions(node.format_schema);
    g.setNode(node.id, { width, height });
  }

  for (const edge of edges) {
    g.setEdge(edge.source_node_id, edge.target_node_id);
  }

  dagre.layout(g);

  return nodes.map((n) => {
    const dagreNode = g.node(n.id);
    const { width, height } = getNodeDimensions(n.format_schema);
    return {
      nodeId: n.id,
      x: dagreNode.x - width / 2,
      y: dagreNode.y - height / 2,
    };
  });
}
