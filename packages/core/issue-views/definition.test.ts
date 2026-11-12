import { describe, expect, it } from "vitest";
import { createStore } from "zustand/vanilla";
import { viewStoreSlice, type IssueViewState } from "../issues/stores/view-store";
import {
  isIssueViewModified,
  isIssueViewUndefined,
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

describe("isIssueViewUndefined", () => {
  const created = { ...issueViewSeedDefinition({ query: {}, display: {} }), scope_variant: null };

  it("holds for a view as it is created: no conditions, default display", () => {
    expect(isIssueViewUndefined(created)).toBe(true);
    expect(isIssueViewUndefined({ query: {}, display: {}, scope_variant: "any" })).toBe(true);
  });

  it("ignores 'is not' on a dimension with no values", () => {
    expect(isIssueViewUndefined({ ...created, query: { negatedFilters: ["label"] } })).toBe(true);
  });

  it("stops holding once the view saves a condition, a display or a narrower scope", () => {
    expect(isIssueViewUndefined({ ...created, query: { labelFilters: ["bug"] } })).toBe(false);
    expect(isIssueViewUndefined({ ...created, query: { agentRunningFilter: true } })).toBe(false);
    expect(isIssueViewUndefined({ ...created, display: { viewMode: "list" } })).toBe(false);
    expect(isIssueViewUndefined({ ...created, scope_variant: "agents" })).toBe(false);
    expect(isIssueViewUndefined({ ...created, scope_variant: "assigned" })).toBe(false);
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
  it("round-trips Idle and tracks switching to Running as one changed dimension", () => {
    const idle = { query: { agentIdleFilter: true }, display: {} };
    const store = liveStateFor(idle);
    expect(issueViewDefinitionFromState(store.getState()).query).toMatchObject({ agentIdleFilter: true, agentRunningFilter: false });
    expect(isIssueViewModified(idle, store.getState()).filters).toBe(false);
    store.getState().toggleAgentRunningFilter();
    expect(store.getState().agentIdleFilter).toBe(false);
    expect([...modifiedFilterDimensions(idle, store.getState())]).toEqual(["agentRunning"]);
  });

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

describe("unread in a saved view", () => {
  const view = { query: { unreadFilter: true }, display: {} };

  it("saves the filter and opens with it on, unmodified", () => {
    const store = liveStateFor(view);
    expect(store.getState().unreadFilter).toBe(true);
    expect(issueViewDefinitionFromState(store.getState()).query).toMatchObject({
      unreadFilter: true,
    });
    expect(isIssueViewModified(view, store.getState()).filters).toBe(false);
  });

  it("reports turning it off as a modified filter", () => {
    const store = liveStateFor(view);
    store.getState().toggleUnreadFilter();
    expect(isIssueViewModified(view, store.getState()).filters).toBe(true);
    expect([...modifiedFilterDimensions(view, store.getState())]).toEqual(["unread"]);
  });

  it("treats a view saved before the filter existed as off", () => {
    const legacy = { query: { statusFilters: ["todo"] }, display: {} };
    const store = liveStateFor(legacy);
    expect(store.getState().unreadFilter).toBe(false);
    expect(isIssueViewModified(legacy, store.getState()).filters).toBe(false);
  });
});
