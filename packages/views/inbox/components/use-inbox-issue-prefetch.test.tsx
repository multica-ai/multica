import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { InboxItem } from "@multica/core/types";
import { useInboxIssuePrefetch } from "./use-inbox-issue-prefetch";

const prefetchRoute = vi.fn();
const viewer = vi.hoisted(() => ({ guest: false }));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ prefetch: prefetchRoute }) }));
vi.mock("@multica/core/permissions", () => ({ useIsGuest: () => viewer.guest }));
vi.mock("@multica/core/api", () => ({
  api: {
    getIssue: vi.fn(async (ref: string) => {
      const id = ref.replace(/^MUL-/, "");
      return { id, identifier: `MUL-${id}` };
    }),
    listTimeline: vi.fn(async () => []),
    listTasksByIssue: vi.fn(async () => []),
    listChildIssues: vi.fn(async () => ({ issues: [] })),
  },
  ApiError: class extends Error {},
}));

function row(id: string, read = false): InboxItem {
  return { id: `n-${id}`, issue_id: id, issue_identifier: `MUL-${id}`, read } as InboxItem;
}

function setup(rows: InboxItem[], openKey?: string | null) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  const hook = renderHook(
    ({ items, activeKey }) =>
      useInboxIssuePrefetch("ws", items, (item) => `/ws/issues/${item.issue_identifier}`, activeKey),
    {
      wrapper,
      initialProps: { items: rows, activeKey: openKey },
    },
  );
  return { qc, hook };
}

describe("useInboxIssuePrefetch", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    viewer.guest = false;
  });

  it("warms the first unread notifications' issues once the list is idle", async () => {
    const { qc } = setup([row("a"), row("b", true), row("c"), row("d"), row("e")]);
    expect(api.getIssue).not.toHaveBeenCalled();

    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });

    // Read rows are skipped; three unread issues are warmed, the fourth is left for hover.
    // By identifier, the key a link to the issue warms too, so the two share a request.
    expect(vi.mocked(api.getIssue).mock.calls.map(([id]) => id)).toEqual(["MUL-a", "MUL-c", "MUL-d"]);
    expect(vi.mocked(api.listTimeline).mock.calls.map(([id]) => id)).toEqual(["a", "c", "d"]);
    expect(vi.mocked(api.listTasksByIssue).mock.calls.map(([id]) => id)).toEqual(["a", "c", "d"]);
    expect(vi.mocked(api.listChildIssues).mock.calls.map(([id]) => id)).toEqual(["a", "c", "d"]);
    expect(prefetchRoute.mock.calls.map(([href]) => href)).toEqual(["/ws/issues/MUL-a", "/ws/issues/MUL-c", "/ws/issues/MUL-d"]);
    // An identifier URL resolves from cache, and the page reads the issue by UUID.
    expect(qc.getQueryData(issueKeys.detail("ws", "MUL-a"))).toEqual({ id: "a", identifier: "MUL-a" });
    expect(qc.getQueryData(issueKeys.detail("ws", "a"))).toEqual({ id: "a", identifier: "MUL-a" });
  });

  it("warms a pointed-at row on demand, and never fetches a warm issue twice", async () => {
    const { hook } = setup([row("a")]);
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    await act(async () => { await hook.result.current(row("e", true)); });
    await act(async () => { await hook.result.current(row("a")); });

    expect(vi.mocked(api.getIssue).mock.calls.map(([id]) => id)).toEqual(["MUL-a", "MUL-e"]);
    expect(vi.mocked(api.listTimeline).mock.calls.map(([id]) => id)).toEqual(["a", "e"]);
  });

  it("leaves out the runs for a guest, whose issue page has none", async () => {
    viewer.guest = true;
    const { hook } = setup([row("a")]);
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    await act(async () => { await hook.result.current(row("e", true)); });

    expect(vi.mocked(api.listTimeline).mock.calls.map(([id]) => id)).toEqual(["a", "e"]);
    expect(vi.mocked(api.listChildIssues).mock.calls.map(([id]) => id)).toEqual(["a", "e"]);
    expect(api.listTasksByIssue).not.toHaveBeenCalled();
  });

  it("does not restart when realtime replaces the same rows", async () => {
    const { hook } = setup([row("a")]);
    hook.rerender({ items: [row("a")], activeKey: undefined });
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(api.getIssue).toHaveBeenCalledTimes(1);
  });

  it("anchors prefetch on the next two rows after the currently open row, including read rows past index 3", async () => {
    const rows = [
      row("a"),
      row("b"),
      row("c"),
      row("d", true),
      row("e", true),
      row("f", true),
      row("g"),
    ];
    setup(rows, "d");

    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });

    // next1 ("e", read) and next2 ("f", read) warm first, followed by the top unread rows.
    expect(vi.mocked(api.getIssue).mock.calls.map(([id]) => id)).toEqual([
      "MUL-e",
      "MUL-f",
      "MUL-a",
      "MUL-b",
      "MUL-c",
    ]);
  });
});
