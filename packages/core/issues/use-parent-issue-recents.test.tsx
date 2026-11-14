// @vitest-environment jsdom

import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { createQueryClient } from "../query-client";
import type { Issue } from "../types";
import { useRecentIssuesStore } from "./stores/recent-issues-store";
import { useParentIssueRecents } from "./use-parent-issue-recents";

const getIssue = vi.hoisted(() => vi.fn());
vi.mock("../api", () => ({ api: { getIssue } }));
let client: ReturnType<typeof createQueryClient>;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
const entry = { id: "entry", workspace_id: "ws-a", title: "Authorized this open" } as Issue;
beforeEach(() => {
  client = createQueryClient(); getIssue.mockReset();
  useRecentIssuesStore.setState({ byWorkspace: { "ws-a": [{ id: "entry", visitedAt: 1 }] } });
});
afterEach(() => { cleanup(); client.clear(); });

it("clears successful suggestions on close and requires another successful read on reopening while still mounted", async () => {
  getIssue.mockResolvedValueOnce(entry).mockRejectedValueOnce(new Error("403"));
  const view = renderHook(({ open }) => useParentIssueRecents("ws-a", open), { initialProps: { open: false }, wrapper });
  expect(getIssue).not.toHaveBeenCalled();
  view.rerender({ open: true });
  await waitFor(() => expect(view.result.current).toEqual([entry]));
  view.rerender({ open: false });
  expect(view.result.current).toEqual([]);
  view.rerender({ open: true });
  expect(view.result.current).toEqual([]);
  await waitFor(() => expect(client.isFetching()).toBe(0));
  expect(getIssue).toHaveBeenCalledTimes(2);
  expect(view.result.current).toEqual([]);
});

it("keeps simultaneous picker sessions independent and cancels only the closed picker's pending reads", async () => {
  const resolves: ((issue: Issue) => void)[] = [];
  const signals: AbortSignal[] = [];
  getIssue.mockImplementation((_id, { signal }) => {
    signals.push(signal);
    return new Promise<Issue>((resolve) => resolves.push(resolve));
  });
  const first = renderHook(({ open }) => useParentIssueRecents("ws-a", open), { initialProps: { open: true }, wrapper });
  const second = renderHook(() => useParentIssueRecents("ws-a", true), { wrapper });
  await waitFor(() => expect(getIssue).toHaveBeenCalledTimes(2));
  first.rerender({ open: false });
  expect(signals[0]!.aborted).toBe(true);
  expect(signals[1]!.aborted).toBe(false);
  await act(async () => { resolves[0]!(entry); resolves[1]!(entry); });
  await waitFor(() => expect(second.result.current).toEqual([entry]));
  expect(first.result.current).toEqual([]);
});
