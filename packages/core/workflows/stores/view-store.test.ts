// @vitest-environment jsdom

import { describe, it, expect } from "vitest";
import { useWorkflowViewStore } from "./view-store";

describe("useWorkflowViewStore", () => {
  it("defaults to panorama view mode", () => {
    const { viewMode } = useWorkflowViewStore.getState();
    expect(viewMode).toBe("panorama");
  });

  it("setViewMode switches to editor", () => {
    useWorkflowViewStore.getState().setViewMode("editor");
    expect(useWorkflowViewStore.getState().viewMode).toBe("editor");
  });

  it("setViewMode switches to panorama", () => {
    useWorkflowViewStore.getState().setViewMode("editor");
    useWorkflowViewStore.getState().setViewMode("panorama");
    expect(useWorkflowViewStore.getState().viewMode).toBe("panorama");
  });

  it("setViewMode switches to overview", () => {
    useWorkflowViewStore.getState().setViewMode("panorama");
    useWorkflowViewStore.getState().setViewMode("overview");
    expect(useWorkflowViewStore.getState().viewMode).toBe("overview");
  });
});
