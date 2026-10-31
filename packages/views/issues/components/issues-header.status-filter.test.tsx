// @vitest-environment jsdom

/**
 * The status filter's list, against the REAL Base UI menu primitives.
 *
 * The filter lists every offerable status flat, in canonical category order,
 * with no category heading — the icon already carries the category, and a
 * heading per category doubled the menu's height (MUL-6399).
 *
 * That is also what keeps the menu from crashing. `DropdownMenuLabel` renders
 * Base UI's `Menu.GroupLabel`, whose `useMenuGroupRootContext()` THROWS
 * without a `Menu.Group` ancestor; the heading only rendered once a workspace
 * held a custom status, so the missing group stayed invisible until the first
 * one was created — and then opening the filter took the whole app down, since
 * no error boundary sits above the issues surface (MUL-6393, MUL-4819).
 *
 * These tests therefore must NOT mock `@multica/ui/components/ui/dropdown-menu`:
 * a flattened mock renders a heading outside a group perfectly happily, which
 * is exactly how that bug shipped.
 */

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createStore } from "zustand/vanilla";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import {
  BUILT_IN_STATUS_CATEGORY,
  BUILT_IN_STATUS_ORDER,
} from "@multica/core/issues/config";
import {
  type IssueViewState,
  viewStoreSlice,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type { IssueStatusEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueFilterMenu } from "./issues-header";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function entry(overrides: Partial<IssueStatusEntry>): IssueStatusEntry {
  return {
    id: `s-${overrides.key}`,
    workspace_id: "ws-1",
    key: "todo",
    name: "Todo",
    description: "",
    category: "unstarted",
    color: "#888888",
    is_system: true,
    position: 0,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...overrides,
  } as IssueStatusEntry;
}

/** What the server seeds every workspace with: seven concrete built-ins. */
const BUILT_INS = BUILT_IN_STATUS_ORDER.map((key) =>
  entry({ key, name: key, category: BUILT_IN_STATUS_CATEGORY[key], is_system: true }),
);

const HUMAN_REVIEW = entry({
  key: "human_review",
  name: "Human Review",
  category: "started",
  color: "#8b5cf6",
  is_system: false,
  position: 1,
});

function renderFilterMenu(
  statuses: IssueStatusEntry[],
  initial: Partial<IssueViewState> = {},
  menuProps: { summaryTitle?: string } = {},
) {
  setApiInstance({
    listIssueStatuses: async () => ({
      statuses,
      categories: [],
      total: statuses.length,
    }),
    listProperties: async () => ({ properties: [] }),
  } as unknown as ApiClient);

  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const store = createStore<IssueViewState>()(viewStoreSlice);
  store.setState(initial);

  renderWithI18n(
    <QueryClientProvider client={qc}>
      <ViewStoreProvider store={store}>
        <IssueFilterMenu trigger={<button type="button">Filter</button>} {...menuProps} />
      </ViewStoreProvider>
    </QueryClientProvider>,
  );
  return store;
}

/** Opens the Filter menu, then its Status submenu. */
async function openStatusSubmenu() {
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  const statusTrigger = await screen.findByRole("menuitem", { name: /^Status/ });
  fireEvent.click(statusTrigger);
  await waitFor(() =>
    expect(screen.getByRole("menuitemcheckbox", { name: /In Review/ })).toBeInTheDocument(),
  );
}

afterEach(() => {
  cleanup();
  // Base UI portals the menu onto document.body; leftovers would duplicate
  // labels across tests.
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("IssueFilterMenu status section", () => {
  it("lists a custom status inline, with no category heading", async () => {
    renderFilterMenu([...BUILT_INS, HUMAN_REVIEW]);

    // Opening at all is the MUL-6393 regression: the heading this list no
    // longer renders used to throw out of Base UI's Menu.GroupLabel and
    // unmount the app.
    await openStatusSubmenu();

    expect(document.querySelector("[data-slot='dropdown-menu-label']")).toBeNull();
    // The custom status sits directly after the built-in of the category it
    // behaves as — one flat list, top to bottom.
    // Built-ins are named by i18n, the custom one by the catalog.
    expect(
      screen.getAllByRole("menuitemcheckbox").map((el) => el.textContent?.trim()),
    ).toEqual([
      "Backlog",
      "Todo",
      "In Progress",
      "In Review",
      "Blocked",
      "Human Review",
      "Done",
      "Cancelled",
    ]);
  });

  it("lists the 7 built-ins for a workspace with no custom statuses", async () => {
    renderFilterMenu(BUILT_INS);

    await openStatusSubmenu();

    expect(
      document.querySelector("[data-slot='dropdown-menu-label']"),
    ).toBeNull();
    expect(screen.getAllByRole("menuitemcheckbox")).toHaveLength(BUILT_IN_STATUS_ORDER.length);
  });
});

describe("IssueFilterMenu active dimensions", () => {
  const dimensionRows = () =>
    screen.getAllByRole("menuitem").map((el) => el.textContent?.trim() ?? "");

  it("lists the dimensions already filtering first, with their values", async () => {
    renderFilterMenu(BUILT_INS, { priorityFilters: ["high"] });

    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByText("Active");

    const rows = dimensionRows();
    expect(rows[0]).toBe("PriorityisHigh");
    expect(rows.slice(1, 4)).toEqual(["Status", "Agent status", "Assignee"]);
  });

  it("keeps a dimension in place while the menu stays open", async () => {
    renderFilterMenu(BUILT_INS);

    await openStatusSubmenu();
    expect(screen.queryByText("Active")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: /In Review/ }));

    await waitFor(() =>
      expect(screen.getByRole("menuitem", { name: /^Status/ }).textContent).toContain("In Review"),
    );
    expect(screen.queryByText("Active")).not.toBeInTheDocument();
    expect(dimensionRows()[0]).toMatch(/^Status/);
  });

  // The hover summary wraps the trigger once a filter exists. Swapping the
  // wrapper on the first pick remounted the trigger, and the open menu lost
  // its anchor and jumped to the viewport corner.
  it("keeps the same trigger node when the first filter lands", async () => {
    renderFilterMenu(BUILT_INS, {}, { summaryTitle: "All issues" });
    const trigger = screen.getByRole("button", { name: "Filter" });

    await openStatusSubmenu();
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: /In Review/ }));
    await waitFor(() =>
      expect(screen.getByRole("menuitem", { name: /^Status/ }).textContent).toContain("In Review"),
    );

    expect(screen.getByRole("button", { name: "Filter" })).toBe(trigger);
  });
});

describe("IssueFilterMenu operator switch", () => {
  it("flips a dimension to is not without closing its submenu", async () => {
    const store = renderFilterMenu(BUILT_INS, { statusFilters: ["in_review"] });

    await openStatusSubmenu();
    fireEvent.click(screen.getByRole("menuitemradio", { name: "is not" }));

    await waitFor(() => expect(store.getState().negatedFilters).toEqual(["status"]));
    expect(screen.getByRole("menuitemcheckbox", { name: /In Review/ })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /^Status/ }).textContent).toBe(
      "Statusis notIn Review",
    );

    fireEvent.click(screen.getByRole("menuitemradio", { name: "is" }));
    await waitFor(() => expect(store.getState().negatedFilters).toEqual([]));
  });
});
