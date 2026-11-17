/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { createQueryClient } from "../query-client";
import { issueViewContainerKey, useActiveIssueViewStore } from "./active-view-store";
import { useActiveIssueView } from "./use-active-view";
import type { IssueViewScope } from "./queries";

const WORKSPACE: IssueViewScope = { scope_type: "workspace" };
const PROJECT: IssueViewScope = { scope_type: "project", scope_id: "p1" };

describe("useActiveIssueView and the built-in filtered views", () => {
  let qc: QueryClient;

  function render(scope: IssueViewScope) {
    return renderHook(() => useActiveIssueView("ws-1", scope), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={qc}>{children}</QueryClientProvider>
      ),
    });
  }

  beforeEach(() => {
    qc = createQueryClient();
    setApiInstance({ listIssueViews: vi.fn(async () => []) } as unknown as ApiClient);
    useActiveIssueViewStore.setState({ active: {} });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it.each([
    ["my_work", { relationFilters: ["assigned", "my_agents"] }],
    ["working", { agentRunningFilter: true }],
    ["subscribed", { relationFilters: ["subscribed"] }],
  ])("opens %s on the workspace page without a saved view behind it", async (id, query) => {
    act(() => useActiveIssueViewStore.getState().setActive(issueViewContainerKey("ws-1", WORKSPACE), id));
    const { result } = render(WORKSPACE);
    await waitFor(() => expect(result.current.viewsReady).toBe(true));
    expect(result.current.activeView).toMatchObject({ id, query });
    expect(result.current.missing).toBe(false);
  });

  it("no longer opens the retired Unread view", async () => {
    act(() => useActiveIssueViewStore.getState().setActive(issueViewContainerKey("ws-1", WORKSPACE), "unread"));
    const { result } = render(WORKSPACE);
    await waitFor(() => expect(result.current.missing).toBe(true));
  });

  it("is not a view on other surfaces, so the id there reads as missing", async () => {
    act(() => useActiveIssueViewStore.getState().setActive(issueViewContainerKey("ws-1", PROJECT), "subscribed"));
    const { result } = render(PROJECT);
    await waitFor(() => expect(result.current.missing).toBe(true));
    expect(result.current.activeView).toBeNull();
  });
});
