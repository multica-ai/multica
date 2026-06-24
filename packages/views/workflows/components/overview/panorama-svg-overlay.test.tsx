// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { computeEdgePaths } from "./panorama-svg-overlay";
import { PanoramaSvgOverlay } from "./panorama-svg-overlay";
import type { WorkflowEdge, WorkflowNode, WorkflowStage } from "@multica/core/types";

const MOCK_STAGES: WorkflowStage[] = [
  {
    id: "stage-1", workflow_id: "wf-1", name: "Intake", description: "",
    sort_order: 0, node_count: 2, created_at: "", updated_at: "",
  },
  {
    id: "stage-2", workflow_id: "wf-1", name: "Analysis", description: "",
    sort_order: 1, node_count: 1, created_at: "", updated_at: "",
  },
];

const MOCK_NODES: WorkflowNode[] = [
  {
    id: "n1", workflow_id: "wf-1", title: "A", description: "",
    position_x: 0, position_y: 0, format_schema: null,
    worker_type: "agent", worker_id: null,
    critic_type: "human", critic_id: null, critic_api_url: null,
    sort_order: 0, stage_id: "stage-1", created_at: "", updated_at: "",
  },
  {
    id: "n2", workflow_id: "wf-1", title: "B", description: "",
    position_x: 0, position_y: 0, format_schema: null,
    worker_type: "agent", worker_id: null,
    critic_type: "human", critic_id: null, critic_api_url: null,
    sort_order: 1, stage_id: "stage-1", created_at: "", updated_at: "",
  },
  {
    id: "n3", workflow_id: "wf-1", title: "C", description: "",
    position_x: 0, position_y: 0, format_schema: null,
    worker_type: "agent", worker_id: null,
    critic_type: "human", critic_id: null, critic_api_url: null,
    sort_order: 0, stage_id: "stage-2", created_at: "", updated_at: "",
  },
  {
    id: "n4", workflow_id: "wf-1", title: "D", description: "",
    position_x: 0, position_y: 0, format_schema: null,
    worker_type: "agent", worker_id: null,
    critic_type: "human", critic_id: null, critic_api_url: null,
    sort_order: 2, stage_id: "stage-1", created_at: "", updated_at: "",
  },
];

const MOCK_EDGES: WorkflowEdge[] = [
  {
    id: "e1", workflow_id: "wf-1",
    source_node_id: "n1", target_node_id: "n2",
    condition: null, created_at: "",
  },
  {
    id: "e2", workflow_id: "wf-1",
    source_node_id: "n2", target_node_id: "n3",
    condition: null, created_at: "",
  },
];

function fakeRect(x: number, y: number, w: number, h: number): DOMRect {
  return { x, y, left: x, top: y, right: x + w, bottom: y + h, width: w, height: h, toJSON() { return this; } };
}

describe("computeEdgePaths", () => {
  it("returns empty array when positions are empty", () => {
    const paths = computeEdgePaths(MOCK_EDGES, MOCK_NODES, MOCK_STAGES, new Map(), new Map());
    expect(paths).toEqual([]);
  });

  it("computes horizontal path for same-stage adjacent nodes", () => {
    const positions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES.slice(0, 1), MOCK_NODES, MOCK_STAGES, positions, new Map());
    expect(paths.length).toBe(1);
    expect(paths[0]!.type).toBe("horizontal");
    expect(paths[0]!.d).toContain("M");
  });

  it("computes channeled orthogonal path for edges between stages", () => {
    const positions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
      ["n3", fakeRect(130, 200, 120, 72)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES, MOCK_NODES, MOCK_STAGES, positions, new Map());
    const crossStage = paths.filter((p) => p?.type === "cross-stage");
    expect(crossStage.length).toBe(1);
    expect(crossStage[0]!.d).not.toContain("C");
    expect(crossStage[0]!.d).not.toContain("Q");
    expect(crossStage[0]!.d).toContain("L");
    expect(crossStage[0]!.d).toContain("136");
  });

  it("routes same-stage non-adjacent edges as orthogonal rails above the node row", () => {
    const positions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 80, 120, 72)],
      ["n4", fakeRect(390, 80, 120, 72)],
    ]);
    const edge: WorkflowEdge = {
      id: "e-arc", workflow_id: "wf-1",
      source_node_id: "n1", target_node_id: "n4",
      condition: null, created_at: "",
    };
    const paths = computeEdgePaths([edge], MOCK_NODES, MOCK_STAGES, positions, new Map());
    expect(paths[0]!.type).toBe("arc");
    expect(paths[0]!.d).not.toContain("C");
    expect(paths[0]!.d).not.toContain("Q");
    expect(paths[0]!.d).toContain("L");
    expect(paths[0]!.d).toContain("44");
  });

  it("uses source stage color index instead of source node sort order", () => {
    const positions = new Map<string, DOMRect>([
      ["n2", fakeRect(0, 0, 120, 72)],
      ["n3", fakeRect(0, 180, 120, 72)],
    ]);
    const edge: WorkflowEdge = {
      id: "e-stage-color", workflow_id: "wf-1",
      source_node_id: "n2", target_node_id: "n3",
      condition: null, created_at: "",
    };
    const paths = computeEdgePaths([edge], MOCK_NODES, MOCK_STAGES, positions, new Map());
    expect(paths[0]!.colorIndex).toBe(0);
  });

  it("uses stage sort order for UUID-like stage ids", () => {
    const uuidStages: WorkflowStage[] = [
      { ...MOCK_STAGES[0]!, id: "7f7dfdb4-9b2c-4000-a000-000000000001", sort_order: 4 },
      { ...MOCK_STAGES[1]!, id: "84372fd7-7809-4000-a000-000000000002", sort_order: 5 },
    ];
    const uuidNodes = MOCK_NODES.map((node) => ({
      ...node,
      stage_id: node.stage_id === "stage-1" ? uuidStages[0]!.id : uuidStages[1]!.id,
    }));
    const positions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES.slice(0, 1), uuidNodes, uuidStages, positions, new Map());
    expect(paths[0]!.colorIndex).toBe(4);
  });

  it("computes critic dashed line for worker-critic pairs", () => {
    const nodePositions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
    ]);
    const criticPositions = new Map<string, DOMRect>([
      ["n1", fakeRect(20, 80, 120, 64)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES.slice(0, 1), MOCK_NODES, MOCK_STAGES, nodePositions, criticPositions);
    const criticPaths = paths.filter((p) => p?.type === "critic");
    expect(criticPaths.length).toBe(1);
    expect(criticPaths[0]!.dashed).toBe(true);
  });

  it("returns empty for edges with missing node positions", () => {
    const positions = new Map<string, DOMRect>([["n1", fakeRect(0, 0, 120, 72)]]);
    const paths = computeEdgePaths(MOCK_EDGES, MOCK_NODES, MOCK_STAGES, positions, new Map());
    expect(paths.length).toBe(0);
  });

  it("uses per-color arrow markers so arrowheads match connector stroke classes", () => {
    const positions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(180, 0, 120, 72)],
    ]);
    const { container } = render(
      <PanoramaSvgOverlay
        edges={MOCK_EDGES.slice(0, 1)}
        nodes={MOCK_NODES}
        stages={MOCK_STAGES}
        nodePositions={positions}
        criticPositions={new Map()}
      />,
    );
    const markerPath = container.querySelector("marker path");
    const connector = container.querySelector("svg > path");
    expect(container.querySelector("#panorama-arrowhead-0")).toBeTruthy();
    expect(connector?.getAttribute("marker-end")).toBe("url(#panorama-arrowhead-0)");
    expect(markerPath?.getAttribute("fill")).toBe("currentColor");
    expect(markerPath?.getAttribute("opacity")).toBe("1");
    expect(connector?.getAttribute("stroke-linecap")).toBe("round");
    expect(connector?.getAttribute("stroke-linejoin")).toBe("round");
  });
});
