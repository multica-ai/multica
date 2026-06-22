// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";

// ── ReactFlow mocks ─────────────────────────────────────────────
vi.mock("@xyflow/react", () => ({
  Handle: ({ type, position, id }: { type: string; position: string; id: string }) => (
    <div data-testid={`handle-${id}`} data-type={type} data-position={position} />
  ),
  NodeResizer: () => <div data-testid="node-resizer" />,
  Position: { Top: "top", Bottom: "bottom", Left: "left", Right: "right" },
  BaseEdge: ({ id, path, className, strokeWidth }: {
    id: string; path: string; className?: string; strokeWidth?: number;
  }) => (
    <g data-testid="base-edge" data-id={id} data-path={path}
      data-classname={className} data-strokewidth={strokeWidth} />
  ),
  getSmoothStepPath: () => ["M0,0 C50,0 50,100 100,100", 50, 50],
  getStraightPath: () => ["M0,0 L100,100"],
  MarkerType: { ArrowClosed: "arrowclosed" },
}));

// ── Store mock ───────────────────────────────────────────────────
const storeRef = vi.hoisted(() => ({
  mode: "edit" as string,
  selectNode: vi.fn(),
  selectEdge: vi.fn(),
}));

vi.mock("@multica/core/workflows/store", () => {
  const state = () => storeRef;
  const useWorkflowEditorStore = Object.assign(
    (sel?: (s: typeof storeRef) => unknown) => (sel ? sel(state()) : state()),
    { getState: state },
  );
  return { useWorkflowEditorStore };
});

import { WorkflowNode, WorkflowEdge, type WorkflowNodeData } from "./reactflow-nodes";

// ── Helpers ─────────────────────────────────────────────────────
function makeNodeProps(overrides: Partial<{ data: WorkflowNodeData; selected: boolean }> = {}) {
  return {
    id: "n1",
    type: "workflow",
    position: { x: 0, y: 0 },
    data: {
      title: "Test Node",
      ...overrides.data,
    },
    selected: overrides.selected ?? false,
  } as unknown as Parameters<typeof WorkflowNode>[0];
}

function makeEdgeProps(overrides: Partial<{
  id: string; sourceX: number; sourceY: number; targetX: number; targetY: number;
  sourcePosition: string; targetPosition: string; selected: boolean;
  data: { onEdgeDelete?: (id: string) => void };
}> = {}) {
  return {
    id: "e1",
    sourceX: 0,
    sourceY: 0,
    targetX: 100,
    targetY: 100,
    sourcePosition: "right",
    targetPosition: "left",
    selected: false,
    data: {},
    ...overrides,
  } as unknown as Parameters<typeof WorkflowEdge>[0];
}

// ── Tests ───────────────────────────────────────────────────────

describe("WorkflowNode", () => {
  it("renders the node title", () => {
    const { container } = render(<WorkflowNode {...makeNodeProps({ data: { title: "Hello World" } })} />);
    expect(container.textContent).toContain("Hello World");
  });

  it("renders 4 handles at top/bottom/left/right", () => {
    const { container } = render(<WorkflowNode {...makeNodeProps({ data: { title: "Test Node", isEditing: true } })} />);
    expect(container.querySelector("[data-testid='handle-top']")).toBeTruthy();
    expect(container.querySelector("[data-testid='handle-bottom']")).toBeTruthy();
    expect(container.querySelector("[data-testid='handle-left']")).toBeTruthy();
    expect(container.querySelector("[data-testid='handle-right']")).toBeTruthy();
  });

  it("shows running spinner when isRunning is true", () => {
    const { container } = render(
      <WorkflowNode {...makeNodeProps({ data: { title: "Running", isRunning: true } })} />,
    );
    const spinner = container.querySelector("svg.animate-spin");
    expect(spinner).toBeTruthy();
  });

  it("does not show spinner when isRunning is false", () => {
    const { container } = render(
      <WorkflowNode {...makeNodeProps({ data: { title: "Idle", isRunning: false } })} />,
    );
    expect(container.querySelector("svg.animate-spin")).toBeFalsy();
  });

  it("shows status label when provided", () => {
    const { container } = render(
      <WorkflowNode {...makeNodeProps({ data: { title: "N", statusLabel: "working" } })} />,
    );
    expect(container.textContent).toContain("working");
  });

  it("applies border-primary class when selected", () => {
    const { container } = render(<WorkflowNode {...makeNodeProps({ selected: true })} />);
    const root = container.firstElementChild!;
    expect(root.className).toContain("border-primary");
  });

  it("applies default border-border class when not selected and no statusColor", () => {
    const { container } = render(<WorkflowNode {...makeNodeProps({ selected: false })} />);
    const root = container.firstElementChild!;
    expect(root.className).toContain("border-border");
  });

  it("applies background color from statusColor", () => {
    const { container } = render(
      <WorkflowNode {...makeNodeProps({ data: { title: "N", statusColor: "#ff0000" } })} />,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.backgroundColor).toBe("rgb(255, 0, 0)");
  });

  it("has rounded-lg class", () => {
    const { container } = render(<WorkflowNode {...makeNodeProps()} />);
    expect(container.firstElementChild!.className).toContain("rounded-lg");
  });
});

describe("WorkflowEdge", () => {
  it("renders BaseEdge with correct path", () => {
    const { container } = render(<WorkflowEdge {...makeEdgeProps()} />);
    const baseEdge = container.querySelector("[data-testid='base-edge']")!;
    expect(baseEdge.getAttribute("data-path")).toBe("M0,0 C50,0 50,100 100,100");
  });

  it("uses primary color class when selected", () => {
    const { container } = render(<WorkflowEdge {...makeEdgeProps({ selected: true })} />);
    const baseEdge = container.querySelector("[data-testid='base-edge']")!;
    expect(baseEdge.getAttribute("data-classname")).toContain("stroke-primary");
  });

  it("uses muted-foreground color class when not selected", () => {
    const { container } = render(<WorkflowEdge {...makeEdgeProps({ selected: false })} />);
    const baseEdge = container.querySelector("[data-testid='base-edge']")!;
    expect(baseEdge.getAttribute("data-classname")).toContain("stroke-muted-foreground");
  });

  it("has thicker stroke when selected", () => {
    const { container } = render(<WorkflowEdge {...makeEdgeProps({ selected: true })} />);
    const baseEdge = container.querySelector("[data-testid='base-edge']")!;
    expect(baseEdge.getAttribute("data-strokewidth")).toBe("2");
  });

  it("has thinner stroke when not selected", () => {
    const { container } = render(<WorkflowEdge {...makeEdgeProps({ selected: false })} />);
    const baseEdge = container.querySelector("[data-testid='base-edge']")!;
    expect(baseEdge.getAttribute("data-strokewidth")).toBe("1.5");
  });
});
