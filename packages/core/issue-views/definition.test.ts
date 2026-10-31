import { describe, expect, it } from "vitest";
import { createStore } from "zustand/vanilla";
import { viewStoreSlice, type IssueViewState } from "../issues/stores/view-store";
import {
  isIssueViewModified,
  issueViewDefinitionFromState,
  issueViewSeedDefinition,
  modifiedFilterDimensions,
} from "./definition";

function liveStateFor(view: { query: Record<string, unknown>; display: Record<string, unknown> }) {
  const store = createStore<IssueViewState>()(viewStoreSlice);
  const seed = issueViewSeedDefinition(view);
  store.setState({ ...seed.query, ...seed.display } as Partial<IssueViewState>);
  return store;
}

describe("isIssueViewModified", () => {
  const view = {
    query: { statusFilters: ["todo"] },
    display: { viewMode: "list", sortBy: "priority" },
  };

  it("reports a freshly opened view as unmodified, even with sparse definitions", () => {
    expect(isIssueViewModified(view, liveStateFor(view).getState())).toEqual({
      filters: false,
      display: false,
    });
  });

  it("separates filter edits from display edits", () => {
    const store = liveStateFor(view);
    store.setState({ priorityFilters: ["high"] });
    expect(isIssueViewModified(view, store.getState())).toEqual({ filters: true, display: false });
    store.setState({ priorityFilters: [], viewMode: "board" });
    expect(isIssueViewModified(view, store.getState())).toEqual({ filters: false, display: true });
  });

  it("treats 'is not' on an empty dimension as no change", () => {
    const store = liveStateFor(view);
    store.getState().setFilterNegated("label", true);
    expect(isIssueViewModified(view, store.getState()).filters).toBe(false);
    store.getState().setFilterNegated("status", true);
    expect(isIssueViewModified(view, store.getState()).filters).toBe(true);
  });

  it("round-trips a negated view without reporting it modified", () => {
    const negatedView = {
      query: { statusFilters: ["done"], negatedFilters: ["status"], relationFilters: ["assigned"] },
      display: {},
    };
    const store = liveStateFor(negatedView);
    expect(store.getState().negatedFilters).toEqual(["status"]);
    expect(isIssueViewModified(negatedView, store.getState()).filters).toBe(false);
    expect(issueViewDefinitionFromState(store.getState()).query).toMatchObject({
      negatedFilters: ["status"],
      relationFilters: ["assigned"],
    });
  });
});

describe("modifiedFilterDimensions", () => {
  const view = {
    query: { statusFilters: ["done"], negatedFilters: ["status"], priorityFilters: ["high"] },
    display: {},
  };

  it("names the dimensions whose values or relation changed", () => {
    const store = liveStateFor(view);
    expect([...modifiedFilterDimensions(view, store.getState())]).toEqual([]);
    store.getState().toggleRelationFilter("created");
    store.getState().setFilterNegated("status", false);
    store.getState().setPropertyFilterValues("prop-1", ["x"]);
    expect([...modifiedFilterDimensions(view, store.getState())].sort()).toEqual(
      ["property:prop-1", "relation", "status"],
    );
  });
});

describe("agent running in a saved view", () => {
  const view = { query: { agentRunningFilter: true }, display: {} };

  it("saves the filter and opens with it on, unmodified", () => {
    const store = liveStateFor(view);
    expect(store.getState().agentRunningFilter).toBe(true);
    expect(issueViewDefinitionFromState(store.getState()).query).toMatchObject({
      agentRunningFilter: true,
    });
    expect(isIssueViewModified(view, store.getState()).filters).toBe(false);
  });

  it("reports turning it off as a modified filter", () => {
    const store = liveStateFor(view);
    store.getState().toggleAgentRunningFilter();
    expect(isIssueViewModified(view, store.getState()).filters).toBe(true);
    expect([...modifiedFilterDimensions(view, store.getState())]).toEqual(["agentRunning"]);
  });

  it("treats a view saved before the filter existed as off", () => {
    const legacy = { query: { statusFilters: ["todo"] }, display: {} };
    const store = liveStateFor(legacy);
    expect(store.getState().agentRunningFilter).toBe(false);
    expect(isIssueViewModified(legacy, store.getState()).filters).toBe(false);
  });
});
