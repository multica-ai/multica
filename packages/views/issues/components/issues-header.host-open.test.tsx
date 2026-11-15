// @vitest-environment jsdom

import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createStore } from "zustand/vanilla";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { type IssueViewState, viewStoreSlice } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { renderWithI18n } from "../../test/i18n";
import { IssueFilterMenu } from "./issues-header";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function Host() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open from host
      </button>
      <span data-testid="host-open">{String(open)}</span>
      <IssueFilterMenu trigger={<button type="button">Filter</button>} open={open} onOpenChange={setOpen} />
    </>
  );
}

function renderHost() {
  setApiInstance({
    listIssueStatuses: async () => ({ statuses: [], categories: [], total: 0 }),
    listProperties: async () => ({ properties: [] }),
  } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const store = createStore<IssueViewState>()(viewStoreSlice);
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <ViewStoreProvider store={store}>
        <Host />
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

describe("IssueFilterMenu opened by its host", () => {
  it("opens without a click and keeps rows in place while a filter lands", async () => {
    const store = renderHost();
    fireEvent.click(screen.getByRole("button", { name: "Open from host" }));
    expect(await screen.findByRole("textbox", { name: "Filter by field or value…" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitem", { name: /^Agent status/ }));
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: "Running" }));
    await waitFor(() => expect(store.getState().agentRunningFilter).toBe(true));
    expect(screen.queryByText("Active")).toBeNull();
  });

  it("reports closing back to the host", async () => {
    renderHost();
    fireEvent.click(screen.getByRole("button", { name: "Open from host" }));
    const search = await screen.findByRole("textbox", { name: "Filter by field or value…" });

    fireEvent.keyDown(search, { key: "Escape" });
    await waitFor(() => expect(screen.getByTestId("host-open")).toHaveTextContent("false"));
  });

  it("opens the advanced panel in advanced mode, and closes it on leaving that mode", async () => {
    const store = renderHost();
    act(() => store.getState().enterAdvancedFilter());
    fireEvent.click(screen.getByRole("button", { name: "Open from host" }));
    expect(await screen.findByTestId("advanced-filter-panel")).toBeInTheDocument();

    act(() => store.getState().exitAdvancedFilter());
    await waitFor(() => expect(screen.getByTestId("host-open")).toHaveTextContent("false"));
    expect(screen.queryByTestId("advanced-filter-panel")).toBeNull();
    expect(screen.queryByRole("textbox", { name: "Filter by field or value…" })).toBeNull();
  });
});
