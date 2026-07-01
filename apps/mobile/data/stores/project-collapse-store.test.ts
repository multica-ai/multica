import { describe, it, expect, beforeEach } from "vitest";
import { useProjectCollapseStore } from "./project-collapse-store";

beforeEach(() => {
  useProjectCollapseStore.setState({ collapsedByProject: {} });
});

describe("useProjectCollapseStore", () => {
  it("isCollapsed returns false for unknown project/status", () => {
    const { isCollapsed } = useProjectCollapseStore.getState();
    expect(isCollapsed("proj-1", "todo")).toBe(false);
  });

  it("toggle collapses a status that was open", () => {
    useProjectCollapseStore.getState().toggle("proj-1", "todo");
    expect(useProjectCollapseStore.getState().isCollapsed("proj-1", "todo")).toBe(true);
  });

  it("toggle expands a status that was collapsed", () => {
    useProjectCollapseStore.getState().toggle("proj-1", "todo");
    useProjectCollapseStore.getState().toggle("proj-1", "todo");
    expect(useProjectCollapseStore.getState().isCollapsed("proj-1", "todo")).toBe(false);
  });

  it("toggle on one project does not affect another", () => {
    useProjectCollapseStore.getState().toggle("proj-1", "todo");
    expect(useProjectCollapseStore.getState().isCollapsed("proj-2", "todo")).toBe(false);
  });
});
