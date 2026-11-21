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
import { baselineFromQuery, type IssueViewBaseline } from "@multica/core/issue-views/baseline";
import { renderWithI18n } from "../../test/i18n";
import { IssueFilterMenu } from "./issues-header";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function renderFilterMenu(
  initial: Partial<IssueViewState> = {},
  viewBaseline?: IssueViewBaseline,
) {
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
        <IssueFilterMenu
          trigger={<button type="button">Filter</button>}
          viewBaseline={viewBaseline}
        />
      </ViewStoreProvider>
    </QueryClientProvider>,
  );
  return store;
}

const agentStatusRow = () => screen.getByRole("menuitem", { name: /^Agent status/ });

async function openAgentStatus() {
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: /^Agent status/ }));
  return screen.findByRole("menuitemcheckbox", { name: "Running" });
}

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("IssueFilterMenu agent status", () => {
  it("switches between Idle and Running and clears the selection", async () => {
    const store = renderFilterMenu();
    const running = await openAgentStatus();
    const idle = screen.getByRole("menuitemcheckbox", { name: "Idle" });
    fireEvent.click(idle);
    await waitFor(() => expect(store.getState()).toMatchObject({ agentIdleFilter: true, agentRunningFilter: false }));
    expect(agentStatusRow().textContent).toBe("Agent statusisIdle");
    fireEvent.click(running);
    await waitFor(() => expect(store.getState()).toMatchObject({ agentIdleFilter: false, agentRunningFilter: true }));
    fireEvent.click(idle);
    fireEvent.click(idle);
    await waitFor(() => expect(store.getState()).toMatchObject({ agentIdleFilter: false, agentRunningFilter: false }));
  });

  it("keeps both choices fixed when the saved view requires Idle", async () => {
    const baseline = baselineFromQuery({ agentIdleFilter: true });
    renderFilterMenu({ agentIdleFilter: true }, baseline);
    const running = await openAgentStatus();
    expect(running).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("menuitemcheckbox", { name: "Idle" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("menuitemcheckbox", { name: "Idle" })).toHaveAttribute("aria-disabled", "true");
  });

  it("is a field like the others, right below Status", async () => {
    renderFilterMenu();
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByRole("menuitem", { name: /^Status/ });

    const rows = screen.getAllByRole("menuitem").map((el) => el.textContent?.trim());
    expect(rows.slice(0, 4)).toEqual(["Status", "Agent status", "Read state", "Priority"]);
  });

  it("filters on Running from its submenu and reads as a sentence", async () => {
    const store = renderFilterMenu();
    const running = await openAgentStatus();

    fireEvent.click(running);
    await waitFor(() => expect(store.getState().agentRunningFilter).toBe(true));
    expect(running).toHaveAttribute("aria-checked", "true");
    expect(agentStatusRow().textContent).toBe("Agent statusisRunning");

    fireEvent.click(running);
    await waitFor(() => expect(store.getState().agentRunningFilter).toBe(false));
  });

  it("is listed under Active when on, and Reset turns it off", async () => {
    const store = renderFilterMenu({ agentRunningFilter: true });
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByText("Active");

    expect(screen.getAllByRole("menuitem")[0]?.textContent).toBe("Agent statusisRunning");
    fireEvent.click(screen.getByRole("menuitem", { name: "Reset all filters" }));
    await waitFor(() => expect(store.getState().agentRunningFilter).toBe(false));
  });

  it("is found by searching for its value", async () => {
    renderFilterMenu();
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByRole("menuitem", { name: /^Status/ });

    fireEvent.change(screen.getByRole("textbox", { name: /Filter by field or value/ }), {
      target: { value: "running" },
    });

    await waitFor(() => expect(agentStatusRow()).toBeInTheDocument());
    expect(screen.queryByRole("menuitem", { name: /^Status/ })).toBeNull();
  });

  it("is fixed inside a saved view that filters on it", async () => {
    const baseline = baselineFromQuery({ agentRunningFilter: true });
    const store = renderFilterMenu({ agentRunningFilter: true }, baseline);
    const running = await openAgentStatus();

    expect(running).toHaveAttribute("aria-checked", "true");
    expect(running).toHaveAttribute("aria-disabled", "true");
    expect(store.getState().agentRunningFilter).toBe(true);
  });
});

const readStateRow = () => screen.getByRole("menuitem", { name: /^Read state/ });

async function openReadState() {
  fireEvent.click(screen.getByRole("button", { name: "Filter" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: /^Read state/ }));
  return screen.findByRole("menuitemcheckbox", { name: "Unread" });
}

describe("IssueFilterMenu read state", () => {
  it("filters on Unread from its submenu and reads as a sentence", async () => {
    const store = renderFilterMenu();
    const unread = await openReadState();

    fireEvent.click(unread);
    await waitFor(() => expect(store.getState().unreadFilter).toBe(true));
    expect(unread).toHaveAttribute("aria-checked", "true");
    expect(readStateRow().textContent).toBe("Read stateisUnread");

    fireEvent.click(unread);
    await waitFor(() => expect(store.getState().unreadFilter).toBe(false));
  });

  it("is listed under Active when on, and Reset turns it off", async () => {
    const store = renderFilterMenu({ unreadFilter: true });
    fireEvent.click(screen.getByRole("button", { name: "Filter" }));
    await screen.findByText("Active");

    expect(screen.getAllByRole("menuitem")[0]?.textContent).toBe("Read stateisUnread");
    fireEvent.click(screen.getByRole("menuitem", { name: "Reset all filters" }));
    await waitFor(() => expect(store.getState().unreadFilter).toBe(false));
  });

  it("is fixed inside a saved view that filters on it", async () => {
    const baseline = baselineFromQuery({ unreadFilter: true });
    const store = renderFilterMenu({ unreadFilter: true }, baseline);
    const unread = await openReadState();

    expect(unread).toHaveAttribute("aria-checked", "true");
    expect(unread).toHaveAttribute("aria-disabled", "true");
    expect(store.getState().unreadFilter).toBe(true);
  });
});
