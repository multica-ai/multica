// @vitest-environment jsdom
import { describe, it, expect } from "vitest";
import { computeEdgePaths } from "./panorama-svg-overlay";
import type { WorkflowEdge, WorkflowNode } from "@multica/core/types";

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
    const paths = computeEdgePaths(MOCK_EDGES, MOCK_NODES, new Map(), new Map());
    expect(paths).toEqual([]);
  });

  it("computes horizontal path for same-stage adjacent nodes", () => {
    const positions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES.slice(0, 1), MOCK_NODES, positions, new Map());
    expect(paths.length).toBe(1);
    expect(paths[0]!.type).toBe("horizontal");
    expect(paths[0]!.d).toContain("M");
  });

  it("computes cross-stage bezier path for edges between stages", () => {
    const positions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
      ["n3", fakeRect(130, 200, 120, 72)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES, MOCK_NODES, positions, new Map());
    const crossStage = paths.filter((p) => p?.type === "cross-stage");
    expect(crossStage.length).toBe(1);
    expect(crossStage[0]!.d).toContain("Q");
  });

  it("computes critic dashed line for worker-critic pairs", () => {
    const nodePositions = new Map<string, DOMRect>([
      ["n1", fakeRect(0, 0, 120, 72)],
      ["n2", fakeRect(130, 0, 120, 72)],
    ]);
    const criticPositions = new Map<string, DOMRect>([
      ["n1", fakeRect(20, 80, 120, 64)],
    ]);
    const paths = computeEdgePaths(MOCK_EDGES.slice(0, 1), MOCK_NODES, nodePositions, criticPositions);
    const criticPaths = paths.filter((p) => p?.type === "critic");
    expect(criticPaths.length).toBe(1);
    expect(criticPaths[0]!.dashed).toBe(true);
  });

  it("returns empty for edges with missing node positions", () => {
    const positions = new Map<string, DOMRect>([["n1", fakeRect(0, 0, 120, 72)]]);
    const paths = computeEdgePaths(MOCK_EDGES, MOCK_NODES, positions, new Map());
    expect(paths.length).toBe(0);
  });
});
