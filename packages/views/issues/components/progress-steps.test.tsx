import { describe, it, expect } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import {
  ProgressSteps,
  computeDagLayout,
  type ProgressPhase,
  type ProgressEdge,
} from "./progress-steps";

function phase(
  id: string,
  status: ProgressPhase["status"] = "pending",
  extra: Partial<ProgressPhase> = {},
): ProgressPhase {
  return { id, name: id, status, ...extra };
}

describe("computeDagLayout", () => {
  it("layers a fork-join graph so the join lands after every branch", () => {
    const phases = [
      phase("impl", "completed"),
      phase("review", "completed"),
      phase("test", "completed"),
      phase("ship", "running"),
    ];
    const edges: ProgressEdge[] = [
      { from: "impl", to: "review" },
      { from: "impl", to: "test" },
      { from: "review", to: "ship" },
      { from: "test", to: "ship" },
    ];

    const columns = computeDagLayout(phases, edges);
    expect(columns).not.toBeNull();
    expect(columns).toHaveLength(3);
    // Column 0: the single root.
    expect(columns![0]!.phases.map((p) => p.id)).toEqual(["impl"]);
    // Column 1: the two parallel branches (declaration order preserved).
    expect(columns![1]!.phases.map((p) => p.id)).toEqual(["review", "test"]);
    // Column 2: the join sits after ALL of its predecessors.
    expect(columns![2]!.phases.map((p) => p.id)).toEqual(["ship"]);
  });

  it("returns null when a phase is missing an id", () => {
    const phases = [phase("a"), { name: "no-id", status: "pending" } as ProgressPhase];
    expect(computeDagLayout(phases, [])).toBeNull();
  });

  it("returns null when an edge references an unknown id", () => {
    const phases = [phase("a"), phase("b")];
    expect(computeDagLayout(phases, [{ from: "a", to: "ghost" }])).toBeNull();
  });

  it("returns null on a cycle", () => {
    const phases = [phase("a"), phase("b"), phase("c")];
    const edges: ProgressEdge[] = [
      { from: "a", to: "b" },
      { from: "b", to: "c" },
      { from: "c", to: "a" },
    ];
    expect(computeDagLayout(phases, edges)).toBeNull();
  });
});

describe("ProgressSteps", () => {
  it("renders nothing when there are no phases", () => {
    const { container } = renderWithI18n(<ProgressSteps phases={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders every phase name in linear mode (no edges)", () => {
    const phases = [
      phase("Plan", "completed", { name: "Plan" }),
      phase("Build", "running", { name: "Build" }),
      phase("Ship", "pending", { name: "Ship" }),
    ];
    renderWithI18n(<ProgressSteps phases={phases} />);
    expect(screen.getByText("Plan")).toBeTruthy();
    expect(screen.getByText("Build")).toBeTruthy();
    expect(screen.getByText("Ship")).toBeTruthy();
  });

  it("renders every phase name in DAG mode (valid edges)", () => {
    const phases = [
      phase("impl", "completed", { name: "Implement" }),
      phase("review", "running", { name: "Review" }),
      phase("test", "pending", { name: "Test" }),
      phase("ship", "pending", { name: "Ship" }),
    ];
    const edges: ProgressEdge[] = [
      { from: "impl", to: "review" },
      { from: "impl", to: "test" },
      { from: "review", to: "ship" },
      { from: "test", to: "ship" },
    ];
    // Goes through DagLayout. jsdom has no layout (ResizeObserver is a no-op,
    // rects are zero) so we assert node presence only, never SVG geometry.
    renderWithI18n(<ProgressSteps phases={phases} edges={edges} />);
    expect(screen.getByText("Implement")).toBeTruthy();
    expect(screen.getByText("Review")).toBeTruthy();
    expect(screen.getByText("Test")).toBeTruthy();
    expect(screen.getByText("Ship")).toBeTruthy();
  });

  it("falls back to linear rendering on an invalid DAG (does not throw)", () => {
    const phases = [
      phase("a", "completed", { name: "Alpha" }),
      phase("b", "running", { name: "Beta" }),
    ];
    // Dangling edge → invalid DAG → linear fallback.
    renderWithI18n(
      <ProgressSteps phases={phases} edges={[{ from: "a", to: "ghost" }]} />,
    );
    expect(screen.getByText("Alpha")).toBeTruthy();
    expect(screen.getByText("Beta")).toBeTruthy();
  });
});
