// @vitest-environment node

import { describe, expect, it } from "vitest";
import { validateWorkflowGraph } from "./graph";
import { WorkflowGraphSchema, workflowGraphToWire } from "./schemas";
import type { WorkflowGraph } from "../types/workflow";

const node = (id: string, type: "start" | "agent" | "end") => ({
  id,
  type,
  label: id,
  position: { x: 0, y: 0 },
  ...(type === "agent" ? { agentId: "agent-1", instructions: "run" } : {}),
});

describe("validateWorkflowGraph", () => {
  it.each([
    ["serial", [node("start", "start"), node("agent", "agent"), node("end", "end")], [["start", "agent"], ["agent", "end"]], false],
    ["parallel merge", [node("start", "start"), node("left", "agent"), node("right", "agent"), node("end", "end")], [["start", "left"], ["start", "right"], ["left", "end"], ["right", "end"]], false],
    ["cycle", [node("start", "start"), node("agent", "agent"), node("end", "end")], [["start", "agent"], ["agent", "start"], ["agent", "end"]], true],
    ["isolated node", [node("start", "start"), node("agent", "agent"), node("orphan", "agent"), node("end", "end")], [["start", "agent"], ["agent", "end"]], true],
  ] as const)("handles %s", (_name, nodes, pairs, invalid) => {
    const graph: WorkflowGraph = {
      nodes: [...nodes],
      edges: pairs.map(([source, target], index) => ({ id: `e-${index}`, source, target })),
    };
    expect(validateWorkflowGraph(graph).length > 0).toBe(invalid);
  });

  it("rejects an output reference that is not an ancestor", () => {
    const graph: WorkflowGraph = {
      nodes: [
        node("start", "start"),
        { ...node("a", "agent"), inputRefs: ["b"] },
        node("b", "agent"),
        node("end", "end"),
      ],
      edges: [
        { id: "s-a", source: "start", target: "a" },
        { id: "a-e", source: "a", target: "end" },
        { id: "s-b", source: "start", target: "b" },
        { id: "b-e", source: "b", target: "end" },
      ],
    };
    expect(validateWorkflowGraph(graph)).toContain("invalid_reference");
  });

  it("keeps unknown nodes and their fields for read-only round trips", () => {
    const graph = WorkflowGraphSchema.parse({
      schema_version: 2,
      nodes: [{ id: "future", type: "loop_v3", label: "Future", position: { x: 1, y: 2 }, future_config: { mode: "bounded" } }],
      edges: [],
    });
    expect(graph.nodes[0]!.type).toBe("loop_v3");
    expect(graph.nodes[0]!.unknownFields).toEqual({ future_config: { mode: "bounded" } });
    expect(workflowGraphToWire(graph).nodes[0]).toMatchObject({ type: "loop_v3", future_config: { mode: "bounded" } });
  });
});
