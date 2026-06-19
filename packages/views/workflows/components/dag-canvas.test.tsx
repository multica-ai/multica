// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, cleanup } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

// ── ReactFlow mock ──────────────────────────────────────────────
const rfPropsRef = vi.hoisted(() => [] as Record<string, unknown>[]);

vi.mock("@xyflow/react", () => ({
  ReactFlow: (props: Record<string, unknown>) => {
    rfPropsRef.push(props);
    const { nodes, edges, nodeTypes, edgeTypes, onNodeClick, onNodeDragStop,
      onConnect, onEdgeClick, onPaneClick, nodesDraggable, nodesConnectable,
      elementsSelectable, fitView, children } = props;
    return (
      <div data-testid="reactflow" data-draggable={String(nodesDraggable)}
        data-connectable={String(nodesConnectable)}>
        <div data-testid="rf-children">{children as React.ReactNode}</div>
        <div data-testid="rf-nodecount">{(nodes as unknown[]).length}</div>
        <div data-testid="rf-edgecount">{(edges as unknown[]).length}</div>
        <button data-testid="rf-nodeclick" onClick={() =>
          (onNodeClick as (e: unknown, n: unknown) => void)?.(null, { id: "n1" })} />
        <button data-testid="rf-nodedragstop" onClick={() =>
          (onNodeDragStop as (e: unknown, n: unknown) => void)?.(null, { id: "n1", position: { x: 100, y: 200 } })} />
        <button data-testid="rf-connect" onClick={() =>
          (onConnect as (c: unknown) => void)?.({ source: "n1", target: "n2" })} />
        <button data-testid="rf-edgeclick" onClick={() =>
          (onEdgeClick as (e: unknown, n: unknown) => void)?.(null, { id: "e1" })} />
        <button data-testid="rf-paneclick" onClick={() =>
          (onPaneClick as () => void)?.()} />
      </div>
    );
  },
  Background: () => <div data-testid="rf-background" />,
  Controls: () => <div data-testid="rf-controls" />,
  MarkerType: { ArrowClosed: "arrowclosed" },
  Position: { Top: "top", Bottom: "bottom", Left: "left", Right: "right" },
  ConnectionMode: { Loose: "loose", Strict: "strict" },
  Handle: () => null,
  BaseEdge: () => null,
}));

vi.mock("@xyflow/react/dist/style.css", () => ({}));

// ── Store mock ──────────────────────────────────────────────────
const storeRef = vi.hoisted(() => {
  const ref = {
    mode: "view" as string,
    selectedNodeId: null as string | null,
    selectedEdgeId: null as string | null,
    deletedNodeIds: [] as string[],
    cacheNodeDelete: (id: string) => {
      if (!ref.deletedNodeIds.includes(id)) {
        ref.deletedNodeIds = [...ref.deletedNodeIds, id];
      }
    },
  };
  return Object.assign(ref, {
    selectNode: (id: string | null) => {
      ref.selectedNodeId = id;
      ref.selectedEdgeId = null;
    },
    selectEdge: (id: string | null) => {
      ref.selectedEdgeId = id;
      ref.selectedNodeId = null;
    },
    cacheNodeDelete: ref.cacheNodeDelete,
  });
});

vi.mock("@multica/core/workflows/store", () => {
  const state = () => storeRef;
  const useWorkflowEditorStore = Object.assign(
    (sel?: (s: typeof storeRef) => unknown) => sel ? sel(state()) : state(),
    { getState: state },
  );
  return { useWorkflowEditorStore };
});

import { WorkflowCanvas, DAGCanvas } from "./dag-canvas";
import type { WorkflowNode, WorkflowEdge } from "@multica/core/types";

// ── Helpers ─────────────────────────────────────────────────────
function makeNode(overrides: Partial<WorkflowNode> = {}): WorkflowNode {
  return {
    id: "n1",
    workflow_id: "wf-1",
    title: "Test Node",
    description: "",
    position_x: 0,
    position_y: 0,
    format_schema: null,
    worker_type: "agent",
    worker_id: null,
    critic_type: "human",
    critic_id: null,
    critic_api_url: null,
    sort_order: 1,
    stage_id: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeEdge(overrides: Partial<WorkflowEdge> = {}): WorkflowEdge {
  return {
    id: "e1",
    workflow_id: "wf-1",
    source_node_id: "n1",
    target_node_id: "n2",
    condition: null,
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function lastProps(): Record<string, unknown> {
  return rfPropsRef[rfPropsRef.length - 1];
}

// ── Tests ───────────────────────────────────────────────────────
describe("WorkflowCanvas", () => {
  beforeEach(() => {
    rfPropsRef.length = 0;
    storeRef.mode = "view";
    storeRef.selectedNodeId = null;
    storeRef.selectedEdgeId = null;
    // Restore original store methods (may have been replaced by spies in previous tests)
    storeRef.selectNode = (id: string | null) => {
      storeRef.selectedNodeId = id;
      storeRef.selectedEdgeId = null;
    };
    storeRef.selectEdge = (id: string | null) => {
      storeRef.selectedEdgeId = id;
      storeRef.selectedNodeId = null;
    };
    cleanup();
  });

  // ── Rendering ──────────────────────────────────────────────────

  it("renders ReactFlow with correct node count", () => {
    const nodes = [makeNode({ id: "n1" }), makeNode({ id: "n2" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    expect(lastProps().nodes).toHaveLength(2);
  });

  it("renders ReactFlow with correct edge count", () => {
    const nodes = [makeNode({ id: "n1" }), makeNode({ id: "n2" })];
    const edges = [makeEdge({ id: "e1", source_node_id: "n1", target_node_id: "n2" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={edges} />);
    expect(lastProps().edges).toHaveLength(1);
  });

  it("maps node position from position_x/position_y", () => {
    const nodes = [makeNode({ id: "n1", position_x: 42, position_y: 99 })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    const rfNodes = lastProps().nodes as { position: { x: number; y: number } }[];
    expect(rfNodes[0].position).toEqual({ x: 42, y: 99 });
  });

  it("maps node data with title and default status fields", () => {
    const nodes = [makeNode({ id: "n1", title: "Hello" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    const rfNodes = lastProps().nodes as { data: Record<string, unknown> }[];
    expect(rfNodes[0].data.title).toBe("Hello");
    expect(rfNodes[0].data.statusColor).toBeUndefined();
    expect(rfNodes[0].data.isRunning).toBe(false);
  });

  it("maps node data with status colors and running flags", () => {
    const nodes = [makeNode({ id: "n1", title: "Hello" })];
    renderWithI18n(
      <WorkflowCanvas
        nodes={nodes}
        edges={[]}
        nodeStatusColors={{ n1: "#ff0000" }}
        nodeStatuses={{ n1: { status: "working", isRunning: true } }}
      />,
    );
    const rfNodes = lastProps().nodes as { data: Record<string, unknown> }[];
    expect(rfNodes[0].data.statusColor).toBe("#ff0000");
    expect(rfNodes[0].data.statusLabel).toBe("working");
    expect(rfNodes[0].data.isRunning).toBe(true);
  });

  // ── Interactions ───────────────────────────────────────────────

  it("calls selectNode and onNodeClick on node click", () => {
    const onNodeClick = vi.fn();
    const nodes = [makeNode({ id: "n1" })];

    const selectNodeSpy = vi.fn();
    const selectEdgeSpy = vi.fn();
    storeRef.selectNode = selectNodeSpy;
    storeRef.selectEdge = selectEdgeSpy;

    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} onNodeClick={onNodeClick} />);
    fireEvent.click(document.querySelector("[data-testid='rf-nodeclick']")!);
    expect(selectNodeSpy).toHaveBeenCalledWith("n1");
    expect(selectEdgeSpy).toHaveBeenCalledWith(null);
    expect(onNodeClick).toHaveBeenCalledWith("n1");
  });

  it("calls onNodeDragStop with rounded coordinates", () => {
    const onNodeDragStop = vi.fn();
    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} onNodeDragStop={onNodeDragStop} />);
    fireEvent.click(document.querySelector("[data-testid='rf-nodedragstop']")!);
    expect(onNodeDragStop).toHaveBeenCalledWith("n1", 100, 200);
  });

  it("calls onEdgeCreate when a connection is made", () => {
    const onEdgeCreate = vi.fn();
    const nodes = [makeNode({ id: "n1" }), makeNode({ id: "n2" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} onEdgeCreate={onEdgeCreate} />);
    fireEvent.click(document.querySelector("[data-testid='rf-connect']")!);
    expect(onEdgeCreate).toHaveBeenCalledWith("n1", "n2");
  });

  it("selects edge on edge click and deselects node", () => {
    storeRef.selectedNodeId = "n1";
    const selectNodeSpy = vi.fn();
    const selectEdgeSpy = vi.fn();
    storeRef.selectNode = selectNodeSpy;
    storeRef.selectEdge = selectEdgeSpy;

    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    fireEvent.click(document.querySelector("[data-testid='rf-edgeclick']")!);
    expect(selectEdgeSpy).toHaveBeenCalledWith("e1");
    expect(selectNodeSpy).toHaveBeenCalledWith(null);
  });

  it("deselects both node and edge on pane click", () => {
    storeRef.selectedNodeId = "n1";
    storeRef.selectedEdgeId = "e1";
    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    fireEvent.click(document.querySelector("[data-testid='rf-paneclick']")!);
    expect(storeRef.selectedNodeId).toBeNull();
    expect(storeRef.selectedEdgeId).toBeNull();
  });

  it("does not call onNodeClick when callback is not provided", () => {
    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    expect(() =>
      fireEvent.click(document.querySelector("[data-testid='rf-nodeclick']"!)),
    ).not.toThrow();
  });

  it("does not call onEdgeCreate when callback is not provided", () => {
    const nodes = [makeNode({ id: "n1" }), makeNode({ id: "n2" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    expect(() =>
      fireEvent.click(document.querySelector("[data-testid='rf-connect']"!)),
    ).not.toThrow();
  });

  // ── Modes ──────────────────────────────────────────────────────

  it("enables dragging in edit mode", () => {
    storeRef.mode = "edit";
    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    const el = document.querySelector("[data-testid='reactflow']")!;
    expect(el.getAttribute("data-draggable")).toBe("true");
    expect(el.getAttribute("data-connectable")).toBe("true");
  });

  it("enables dragging in connect mode", () => {
    storeRef.mode = "connect";
    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    const el = document.querySelector("[data-testid='reactflow']")!;
    expect(el.getAttribute("data-draggable")).toBe("true");
    expect(el.getAttribute("data-connectable")).toBe("true");
  });

  it("disables dragging and connecting in view mode", () => {
    storeRef.mode = "view";
    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    const el = document.querySelector("[data-testid='reactflow']")!;
    expect(el.getAttribute("data-draggable")).toBe("false");
    expect(el.getAttribute("data-connectable")).toBe("false");
  });

  // ── Edge cases ─────────────────────────────────────────────────

  it("handles empty nodes array", () => {
    renderWithI18n(<WorkflowCanvas nodes={[]} edges={[]} />);
    expect(lastProps().nodes).toHaveLength(0);
    expect(lastProps().edges).toHaveLength(0);
  });

  it("handles missing nodeStatusColors gracefully", () => {
    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    const rfNodes = lastProps().nodes as { data: Record<string, unknown> }[];
    expect(rfNodes[0].data.statusColor).toBeUndefined();
    expect(rfNodes[0].data.statusLabel).toBeUndefined();
    expect(rfNodes[0].data.isRunning).toBe(false);
  });

  it("passes onEdgeDelete to edge data", () => {
    const onEdgeDelete = vi.fn();
    const nodes = [makeNode({ id: "n1" }), makeNode({ id: "n2" })];
    const edges = [makeEdge({ id: "e1", source_node_id: "n1", target_node_id: "n2" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={edges} onEdgeDelete={onEdgeDelete} />);
    const rfEdges = lastProps().edges as { data: Record<string, unknown> }[];
    expect(rfEdges[0].data.onEdgeDelete).toBe(onEdgeDelete);
  });

  it("selects bottom→top handles when nodes are vertically aligned", () => {
    const nodes = [
      makeNode({ id: "src", position_x: 100, position_y: 0 }),
      makeNode({ id: "tgt", position_x: 100, position_y: 200 }),
    ];
    const edges = [makeEdge({ id: "e1", source_node_id: "src", target_node_id: "tgt" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={edges} />);
    const rfEdges = lastProps().edges as { sourceHandle: string; targetHandle: string }[];
    expect(rfEdges[0].sourceHandle).toBe("bottom");
    expect(rfEdges[0].targetHandle).toBe("top");
  });

  it("selects right→left handles when nodes are horizontally aligned", () => {
    const nodes = [
      makeNode({ id: "src", position_x: 0, position_y: 100 }),
      makeNode({ id: "tgt", position_x: 200, position_y: 100 }),
    ];
    const edges = [makeEdge({ id: "e1", source_node_id: "src", target_node_id: "tgt" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={edges} />);
    const rfEdges = lastProps().edges as { sourceHandle: string; targetHandle: string }[];
    expect(rfEdges[0].sourceHandle).toBe("right");
    expect(rfEdges[0].targetHandle).toBe("left");
  });

  it("includes Background and Controls as children", () => {
    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    expect(document.querySelector("[data-testid='rf-background']")).toBeTruthy();
    expect(document.querySelector("[data-testid='rf-controls']")).toBeTruthy();
  });

  it("sets node type to 'workflow'", () => {
    const nodes = [makeNode({ id: "n1" })];
    renderWithI18n(<WorkflowCanvas nodes={nodes} edges={[]} />);
    const rfNodes = lastProps().nodes as { type: string }[];
    expect(rfNodes[0].type).toBe("workflow");
  });

  it("DAGCanvas alias equals WorkflowCanvas", () => {
    expect(DAGCanvas).toBe(WorkflowCanvas);
  });
});
