// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createStore } from "zustand/vanilla";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import {
  type IssueViewState,
  viewStoreSlice,
} from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { renderWithI18n } from "../../test/i18n";
import { IssueFilterMenu } from "./issues-header";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function renderFilterMenu(initial: Partial<IssueViewState> = {}) {
  setApiInstance({
    listIssueStatuses: async () => ({ statuses: [], categories: [], total: 0 }),
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
        <IssueFilterMenu trigger={<button type="button">Filter</button>} />
      </ViewStoreProvider>
    </QueryClientProvider>,
  );
  return store;
}

async function openMenu() {
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  await screen.findByRole("menuitem", { name: /^Status/ });
}

const agentRow = () => screen.getByRole("menuitemcheckbox", { name: "Agent is running" });

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("IssueFilterMenu agent is running", () => {
  it("toggles the filter from its own row without closing the menu", async () => {
    const store = renderFilterMenu();
    await openMenu();

    expect(agentRow()).toHaveAttribute("aria-checked", "false");
    fireEvent.click(agentRow());

    await waitFor(() => expect(store.getState().agentRunningFilter).toBe(true));
    expect(agentRow()).toHaveAttribute("aria-checked", "true");

    fireEvent.click(agentRow());
    await waitFor(() => expect(store.getState().agentRunningFilter).toBe(false));
  });

  it("sits right below Status", async () => {
    renderFilterMenu();
    await openMenu();

    const rows = screen
      .getAllByRole("menuitem")
      .concat(screen.getAllByRole("menuitemcheckbox"))
      .sort((a, b) =>
        a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1,
      )
      .map((el) => el.textContent?.trim());
    expect(rows.slice(0, 3)).toEqual(["Status", "Agent is running", "Priority"]);
  });

  it("is listed under Active when on, and Reset turns it off", async () => {
    const store = renderFilterMenu({ agentRunningFilter: true });
    await openMenu();

    const active = screen.getByText("Active");
    expect(
      active.compareDocumentPosition(agentRow()) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      agentRow().compareDocumentPosition(screen.getByRole("menuitem", { name: /^Status/ })) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("menuitem", { name: "Reset all filters" }));
    await waitFor(() => expect(store.getState().agentRunningFilter).toBe(false));
  });

  it("is found by searching for it", async () => {
    renderFilterMenu();
    await openMenu();

    fireEvent.change(screen.getByRole("textbox", { name: /Filter by field or value/ }), {
      target: { value: "running" },
    });

    await waitFor(() => expect(agentRow()).toBeInTheDocument());
    expect(screen.queryByRole("menuitem", { name: /^Status/ })).toBeNull();
  });
});
