// @vitest-environment jsdom

import { describe, it, expect } from "vitest";
import { useWorkflowViewStore } from "./view-store";

describe("useWorkflowViewStore", () => {
  it("defaults to overview view mode", () => {
    const { viewMode } = useWorkflowViewStore.getState();
    expect(viewMode).toBe("overview");
  });

  it("setViewMode switches to editor", () => {
    useWorkflowViewStore.getState().setViewMode("editor");
    expect(useWorkflowViewStore.getState().viewMode).toBe("editor");
  });

  it("setViewMode switches back to overview", () => {
    useWorkflowViewStore.getState().setViewMode("editor");
    useWorkflowViewStore.getState().setViewMode("overview");
    expect(useWorkflowViewStore.getState().viewMode).toBe("overview");
  });
});
