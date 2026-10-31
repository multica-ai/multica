// @vitest-environment jsdom

/**
 * The filter menu's search and its way into advanced mode, against the real
 * Base UI primitives (see issues-header.property-filter.test.tsx).
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createStore, type StoreApi } from "zustand/vanilla";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { type IssueViewState, viewStoreSlice } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { createFilterCondition, createFilterGroup } from "@multica/core/issues/stores/filter-tree";
import { renderWithI18n } from "../../test/i18n";
import { IssueFilterMenu } from "./issues-header";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

function renderFilterMenu(setup?: (store: StoreApi<IssueViewState>) => void, withProperty = false) {
  setApiInstance({
    listIssueStatuses: async () => ({ statuses: [], categories: [], total: 0 }),
    listProperties: async () => ({ properties: withProperty ? [{
      id: "estimate", workspace_id: "ws-1", name: "Estimate", type: "number",
      config: {}, position: 1, archived: false, created_at: "", updated_at: "",
    }] : [] }),
    listMembers: async () => [],
    listAgents: async () => [
      { id: "agent-lambda", name: "Lambda", archived_at: null, owner_id: "u-1" },
    ],
    listSquads: async () => [],
    listProjects: async () => ({ projects: [], total: 0 }),
    listLabels: async () => ({ labels: [{ id: "label-bug", name: "Bug", color: "#f00" }], total: 1 }),
  } as unknown as ApiClient);
  registerAuthStore(
    createAuthStore({
      api: {} as ApiClient,
      storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
    }),
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const store = createStore<IssueViewState>()(viewStoreSlice);
  setup?.(store);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <ViewStoreProvider store={store}>
        <IssueFilterMenu trigger={<button type="button">Filter</button>} />
      </ViewStoreProvider>
    </QueryClientProvider>,
  );
  return store;
}

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("IssueFilterMenu search", () => {
  it("offers values as conditions and toggles them", async () => {
    const store = renderFilterMenu();
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    const search = await screen.findByRole("textbox", { name: "Filter by field or value…" });
    await userEvent.type(search, "lamb");

    const row = await screen.findByRole("menuitemcheckbox", { name: /Assignee\s*is\s*Lambda/ });
    fireEvent.click(row);
    expect(store.getState().assigneeFilters).toEqual([{ type: "agent", id: "agent-lambda" }]);
    // Creator offers the same agent as its own condition.
    expect(screen.getByRole("menuitemcheckbox", { name: /Creator\s*is\s*Lambda/ })).toBeInTheDocument();
  });

  it("matches fields by name and buckets by label", async () => {
    const store = renderFilterMenu();
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    const search = await screen.findByRole("textbox", { name: "Filter by field or value…" });
    await userEvent.type(search, "overdue");
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /Due date\s*is\s*Overdue/ }));
    expect(store.getState().dueDateFilters).toEqual(["overdue"]);

    await userEvent.clear(search);
    await userEvent.type(search, "due");
    expect(await screen.findByRole("menuitem", { name: /Due date/ })).toBeInTheDocument();
  });
});

describe("IssueFilterMenu advanced mode", () => {
  it("turns property filters into conditions and offers properties as fields", async () => {
    const store = renderFilterMenu((s) => {
      s.getState().setPropertyFilterValues("estimate", ["3"]);
      s.getState().togglePriorityFilter("urgent");
    }, true);
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    expect(await screen.findByRole("menuitem", { name: /Estimate/ })).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("menuitem", { name: /Switch to advanced filter/ }));
    await screen.findByTestId("advanced-filter-panel");

    expect(store.getState().propertyFilters).toEqual({});
    expect(store.getState().advancedFilter?.children).toMatchObject([
      { field: "priority", values: ["urgent"] },
      { field: "property:estimate", values: ["3"] },
    ]);
    const rows = screen.getAllByTestId("advanced-filter-row");
    expect(rows).toHaveLength(2);
    expect(rows[1]).toHaveTextContent("Estimate");
    expect(screen.queryByRole("button", { name: "Add property filter" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^Add condition$/ }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Estimate/ }));
    expect(store.getState().advancedFilter?.children[2]).toMatchObject({
      field: "property:estimate",
      values: [],
    });

    fireEvent.click(screen.getByRole("button", { name: "Clear all" }));
    expect(store.getState().advancedFilter?.children).toEqual([]);
    expect(screen.getByRole("button", { name: "Clear all" })).toBeDisabled();
  });

  it("edits a number property with a typed value and its own operators", async () => {
    const store = renderFilterMenu(
      (s) =>
        s.getState().setAdvancedFilter(
          createFilterGroup("all", [createFilterCondition("property:estimate", [])]),
        ),
      true,
    );
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByTestId("advanced-filter-panel");
    const input = await screen.findByRole("spinbutton", { name: "Values" });
    fireEvent.change(input, { target: { value: "3" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(store.getState().advancedFilter?.children).toMatchObject([
      { field: "property:estimate", negated: false, values: ["3"] },
    ]);

    await userEvent.click(screen.getByRole("combobox", { name: "Match" }));
    await userEvent.click(await screen.findByRole("option", { name: ">" }));
    expect(store.getState().advancedFilter?.children).toMatchObject([
      { values: [{ op: "gt", value: "3" }] },
    ]);

    await userEvent.click(screen.getByRole("combobox", { name: "Match" }));
    await userEvent.click(await screen.findByRole("option", { name: "is empty" }));
    expect(store.getState().advancedFilter?.children).toMatchObject([{ values: ["__none__"] }]);
    expect(screen.queryByRole("spinbutton", { name: "Values" })).not.toBeInTheDocument();
    // A plain "is empty" property condition is still a simple filter.
    expect(screen.getByRole("button", { name: "Switch to simple filter" })).toBeEnabled();
  });

  it("lists actor values under their headings", async () => {
    const store = renderFilterMenu((s) =>
      s.getState().setAdvancedFilter(createFilterGroup("all", [createFilterCondition("assignee", [])])),
    );
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByTestId("advanced-filter-panel");
    fireEvent.click(screen.getByRole("button", { name: "Values" }));
    expect(await screen.findByText("Agents")).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /Lambda/ }));
    expect(store.getState().advancedFilter?.children).toMatchObject([{ values: ["agent:agent-lambda"] }]);
  });

  it("names set operators for labels", async () => {
    renderFilterMenu((s) =>
      s.getState().setAdvancedFilter(createFilterGroup("all", [createFilterCondition("label", ["label-bug"])])),
    );
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByTestId("advanced-filter-panel");
    expect(screen.getByRole("combobox", { name: "Match" })).toHaveTextContent("includes any of");
  });

  it("adds an empty group and opens its add menu", async () => {
    const store = renderFilterMenu((s) =>
      s.getState().setAdvancedFilter(createFilterGroup("all", [createFilterCondition("status", ["todo"])])),
    );
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByTestId("advanced-filter-panel");
    fireEvent.click(screen.getByRole("button", { name: "Add condition group" }));

    expect(store.getState().advancedFilter?.children[1]).toMatchObject({ type: "group", match: "any", children: [] });
    expect(screen.getByTestId("advanced-filter-group")).toHaveTextContent(/joined with “or”/);
    expect(await screen.findByText("Add to group")).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("menuitem", { name: /Priority/ }));
    expect(store.getState().advancedFilter?.children[1]).toMatchObject({
      type: "group",
      children: [{ field: "priority", values: [] }],
    });
  });

  it("turns a row into a group and reads the tree as a sentence", async () => {
    const store = renderFilterMenu((s) =>
      s.getState().setAdvancedFilter(
        createFilterGroup("all", [
          createFilterCondition("label", ["label-bug"]),
          createFilterCondition("priority", ["urgent"]),
        ]),
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByTestId("advanced-filter-panel");
    expect(screen.queryByTestId("advanced-filter-summary")).not.toBeInTheDocument();

    const rows = screen.getAllByTestId("advanced-filter-row");
    fireEvent.click(within(rows[1]!).getByRole("button", { name: "Condition actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Turn into group" }));
    expect(store.getState().advancedFilter?.children[1]).toMatchObject({
      type: "group",
      match: "any",
      children: [{ field: "priority", values: ["urgent"] }],
    });
    await waitFor(() =>
      expect(screen.getByTestId("advanced-filter-summary")).toHaveTextContent(
        "Shows issues where Label includes any of Bug and Priority is Urgent",
      ),
    );
  });

  it("switches to advanced with the current filters and opens the panel", async () => {
    const store = renderFilterMenu((s) => {
      s.getState().togglePriorityFilter("urgent");
      s.getState().toggleLabelFilter("label-bug");
    });
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Switch to advanced filter/ }));

    expect(store.getState().priorityFilters).toEqual([]);
    expect(store.getState().advancedFilter?.children).toHaveLength(2);
    const panel = await screen.findByTestId("advanced-filter-panel");
    expect(panel).toHaveTextContent("Advanced filter");
    expect(screen.getAllByTestId("advanced-filter-row")).toHaveLength(2);
    // Two plain "and" rows are still simple filters.
    expect(screen.getByRole("button", { name: "Switch to simple filter" })).toBeEnabled();
  });

  it("disables switching back with the reason when the tree has a group", async () => {
    renderFilterMenu((s) =>
      s.getState().setAdvancedFilter(
        createFilterGroup("all", [
          createFilterCondition("project", ["p1"]),
          createFilterGroup("any", [
            createFilterCondition("priority", ["urgent"]),
            createFilterCondition("label", ["label-bug"]),
          ]),
        ]),
      ),
    );
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByTestId("advanced-filter-panel");
    expect(screen.getByTestId("advanced-filter-group")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Switch to simple filter" })).toBeDisabled();
    expect(screen.getByText(/Simple filters can't show groups/)).toBeInTheDocument();
  });

  it("goes back to the simple menu with the same filters", async () => {
    const store = renderFilterMenu((s) =>
      s.getState().setAdvancedFilter(createFilterGroup("all", [createFilterCondition("status", ["todo"], true)])),
    );
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByTestId("advanced-filter-panel");
    fireEvent.click(screen.getByRole("button", { name: "Switch to simple filter" }));
    await waitFor(() => expect(store.getState().advancedFilter).toBeNull());
    expect(store.getState().statusFilters).toEqual(["todo"]);
    expect(store.getState().negatedFilters).toEqual(["status"]);
  });
});
