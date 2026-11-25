/**
 * @vitest-environment jsdom
 */
import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { useUpdateIssueBlockers, useUpdateIssueBlocking } from "./blockers";
import { issueKeys } from "./queries";

const WS_ID = "ws-1";

// A blocker write must settle once the open issue's detail is fresh, without
// waiting for every other issue query in the workspace (MUL-10169).
function setup() {
  const updateIssue = vi.fn(async () => ({ id: "issue-1" }));
  setApiInstance({ updateIssue } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );

  const detailFetch = vi.fn(async () => ({ id: "issue-1" }));
  let releaseList: () => void = () => {};
  const listFetch = vi.fn(
    () => new Promise<string[]>((resolve) => { releaseList = () => resolve([]); }),
  );
  // Mounted observers, so invalidation refetches both queries.
  renderHook(() => {
    useQuery({ queryKey: issueKeys.detail(WS_ID, "issue-1"), queryFn: detailFetch });
    useQuery({ queryKey: issueKeys.list(WS_ID), queryFn: listFetch });
  }, { wrapper });

  return { qc, wrapper, updateIssue, detailFetch, listFetch, releaseList: () => releaseList() };
}

describe("blocker write hooks", () => {
  it("adding a blocker settles after the detail refetch, not the workspace-wide one", async () => {
    const { wrapper, updateIssue, detailFetch, listFetch, releaseList } = setup();
    await vi.waitFor(() => expect(listFetch).toHaveBeenCalledTimes(1));
    releaseList();
    await vi.waitFor(() => expect(detailFetch).toHaveBeenCalledTimes(1));

    const { result } = renderHook(() => useUpdateIssueBlockers(WS_ID, "issue-1"), { wrapper });
    await act(() => result.current.mutateAsync({ blocked_by: ["blocker-1"] }));

    expect(updateIssue).toHaveBeenCalledWith("issue-1", { blocked_by: ["blocker-1"] });
    expect(detailFetch).toHaveBeenCalledTimes(2);
    // The list refetch started but is still out: the write did not wait for it.
    expect(listFetch).toHaveBeenCalledTimes(2);
  });

  it("blocking another issue writes on that issue and refreshes this one's detail", async () => {
    const { wrapper, updateIssue, detailFetch, listFetch, releaseList } = setup();
    await vi.waitFor(() => expect(listFetch).toHaveBeenCalledTimes(1));
    releaseList();
    await vi.waitFor(() => expect(detailFetch).toHaveBeenCalledTimes(1));

    const { result } = renderHook(() => useUpdateIssueBlocking(WS_ID, "issue-1"), { wrapper });
    await act(() => result.current.mutateAsync({ dependentId: "dependent-1" }));

    expect(updateIssue).toHaveBeenCalledWith("dependent-1", { blocked_by: ["issue-1"] });
    expect(detailFetch).toHaveBeenCalledTimes(2);
    expect(listFetch).toHaveBeenCalledTimes(2);
  });
});
