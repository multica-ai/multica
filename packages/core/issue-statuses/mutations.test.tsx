/**
 * @vitest-environment jsdom
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { issueKeys } from "../issues/queries";
import type { IssueStatusEntry, ListIssueStatusesResponse } from "../types";
import {
  useArchiveIssueStatus,
  useCreateIssueStatus,
  useReorderIssueStatuses,
  useUpdateIssueStatus,
} from "./mutations";
import { issueStatusKeys } from "./queries";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const CATEGORIES = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "cancelled",
  "blocked",
] as const;

function entry(overrides: Partial<IssueStatusEntry> & { id: string }): IssueStatusEntry {
  return {
    workspace_id: "ws-1",
    key: overrides.id,
    name: overrides.id,
    description: "",
    category: "in_review",
    color: "#6366f1",
    is_system: false,
    position: 1,
    archived_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const builtInReview = entry({
  id: "builtin-in-review",
  key: "in_review",
  name: "In Review",
  is_system: true,
  position: 0,
});

function catalog(statuses: IssueStatusEntry[]): ListIssueStatusesResponse {
  return { statuses, categories: [...CATEGORIES], total: statuses.length };
}

function wrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function createClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function cached(qc: QueryClient) {
  return qc.getQueryData<ListIssueStatusesResponse>(issueStatusKeys.list("ws-1"));
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

/**
 * What a completed realtime refetch does to the cache: replaces it wholesale
 * with whatever the server answered, including writes this client never made.
 */
function realtimeRefetchLands(qc: QueryClient, response: ListIssueStatusesResponse) {
  qc.setQueryData<ListIssueStatusesResponse>(issueStatusKeys.list("ws-1"), response);
}

describe("issue status catalog mutations", () => {
  afterEach(() => vi.restoreAllMocks());

  // The realtime `issue_status:changed` event refreshes this catalog in every
  // tab, the writing one included. A second invalidate here would make the
  // admin who did the writing the only client that reads the catalog twice.
  // (MUL-6458)
  it("leaves the catalog refresh to the realtime event on a successful write", async () => {
    const qc = createClient();
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview]));
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    setApiInstance({
      createIssueStatus: vi.fn(async () => entry({ id: "qa", key: "qa", name: "QA" })),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useCreateIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ name: "QA", category: "in_review", color: "#6366f1" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const catalogInvalidations = invalidate.mock.calls.filter(
      ([filters]) => (filters?.queryKey as string[] | undefined)?.[0] === "issue-statuses",
    );
    expect(catalogInvalidations).toHaveLength(0);
  });

  it("still refetches the catalog when a write fails", async () => {
    const qc = createClient();
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview]));
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    setApiInstance({
      createIssueStatus: vi.fn(async () => {
        throw new Error("409");
      }),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useCreateIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ name: "QA", category: "in_review", color: "#6366f1" }));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(invalidate).toHaveBeenCalledWith({ queryKey: issueStatusKeys.all("ws-1") });
  });

  // Without the sort, a created status lands at the end of the array while the
  // server puts it inside its category — and nothing corrects that until the
  // realtime refetch lands, which is exactly the window the user is looking at.
  it("sorts a created status into its category instead of appending it", async () => {
    const qc = createClient();
    const done = entry({ id: "builtin-done", key: "done", category: "done", is_system: true, position: 0 });
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview, done]));
    setApiInstance({
      createIssueStatus: vi.fn(async () => entry({ id: "qa", key: "qa", name: "QA" })),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useCreateIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ name: "QA", category: "in_review", color: "#6366f1" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(cached(qc)?.statuses.map((s) => s.id)).toEqual(["builtin-in-review", "qa", "builtin-done"]);
    expect(cached(qc)?.total).toBe(3);
  });

  // `parseWithFallback` degrades a malformed response to an empty stub. Writing
  // that would put a blank row in the picker until the realtime refetch lands.
  it("ignores a create response that degraded to the empty schema fallback", async () => {
    const qc = createClient();
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview]));
    setApiInstance({
      createIssueStatus: vi.fn(async () => entry({ id: "", key: "", name: "", position: 0 })),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useCreateIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ name: "QA", category: "in_review", color: "#6366f1" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(cached(qc)?.statuses).toEqual([builtInReview]);
  });

  // A rename changes a label the boards resolve from THIS catalog at render
  // time, so nothing cached under the issues scope can be stale. Refetching it
  // meant one word cost a workspace-wide board/list/table refetch. (MUL-6458)
  it("does not refetch the issue caches when a status is renamed", async () => {
    const qc = createClient();
    const qa = entry({ id: "qa", key: "qa", name: "QA" });
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview, qa]));
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    setApiInstance({
      updateIssueStatus: vi.fn(async () => ({ ...qa, name: "Quality Gate" })),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useUpdateIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ id: "qa", name: "Quality Gate" }));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: issueKeys.all("ws-1") });
  });

  it("shows a rename immediately and rolls it back when the write fails", async () => {
    const qc = createClient();
    const qa = entry({ id: "qa", key: "qa", name: "QA" });
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview, qa]));
    const write = deferred<IssueStatusEntry>();
    setApiInstance({
      updateIssueStatus: vi.fn(() => write.promise),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useUpdateIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ id: "qa", name: "Quality Gate" }));
    await waitFor(() =>
      expect(cached(qc)?.statuses.find((s) => s.id === "qa")?.name).toBe("Quality Gate"),
    );

    await act(async () => {
      write.resolve(Promise.reject(new Error("409")) as unknown as IssueStatusEntry);
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(cached(qc)?.statuses.find((s) => s.id === "qa")?.name).toBe("QA");
  });

  // The two channels are independent, and the realtime refresh is debounced, so
  // "someone else's later write is already in the cache when my response lands"
  // is a legal ordering — not a rare interleaving. Installing the response body
  // here would roll the catalog back to a state no further event corrects, in
  // exactly the concurrent-editing scenario this feature exists for. (MUL-6458)
  it("does not let a slow rename response overwrite a newer catalog", async () => {
    const qc = createClient();
    const qa = entry({ id: "qa", key: "qa", name: "QA" });
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview, qa]));
    const write = deferred<IssueStatusEntry>();
    setApiInstance({
      updateIssueStatus: vi.fn(() => write.promise),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useUpdateIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ id: "qa", name: "Quality Gate" }));
    // The optimistic patch shows first — the ordering under test is what
    // happens AFTER it, not a race with it.
    await waitFor(() =>
      expect(cached(qc)?.statuses.find((s) => s.id === "qa")?.name).toBe("Quality Gate"),
    );

    // Another admin renames the same row afterwards; this client's refetch
    // picks their version up while our own response is still in flight.
    realtimeRefetchLands(
      qc,
      catalog([builtInReview, { ...qa, name: "Ready for QA", updated_at: "2026-03-03T00:00:00Z" }]),
    );

    await act(async () => {
      write.resolve({ ...qa, name: "Quality Gate", updated_at: "2026-02-02T00:00:00Z" });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(cached(qc)?.statuses.find((s) => s.id === "qa")?.name).toBe("Ready for QA");
  });

  // Same ordering, worse blast radius: reorder answers with the WHOLE catalog,
  // so one late response would revert every concurrent edit at once — here, a
  // status another admin created while the drag was in flight.
  it("does not let a slow reorder response overwrite a newer catalog", async () => {
    const qc = createClient();
    const first = entry({ id: "qa", key: "qa", position: 1 });
    const second = entry({ id: "sec", key: "sec", position: 2 });
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview, first, second]));
    const write = deferred<ListIssueStatusesResponse>();
    setApiInstance({
      reorderIssueStatuses: vi.fn(() => write.promise),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useReorderIssueStatuses(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ category: "in_review", ordered: [second, first] }));
    // The drag shows immediately, without waiting for the round trip.
    await waitFor(() =>
      expect(cached(qc)?.statuses.map((s) => s.id)).toEqual(["builtin-in-review", "sec", "qa"]),
    );

    const third = entry({ id: "third", key: "third", position: 3 });
    realtimeRefetchLands(
      qc,
      catalog([builtInReview, { ...second, position: 1 }, { ...first, position: 2 }, third]),
    );

    await act(async () => {
      write.resolve(catalog([builtInReview, { ...second, position: 1 }, { ...first, position: 2 }]));
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(cached(qc)?.statuses.map((s) => s.id)).toEqual([
      "builtin-in-review",
      "sec",
      "qa",
      "third",
    ]);
  });

  it("restores the pre-drag order when a reorder fails", async () => {
    const qc = createClient();
    const first = entry({ id: "qa", key: "qa", position: 1 });
    const second = entry({ id: "sec", key: "sec", position: 2 });
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview, first, second]));
    setApiInstance({
      reorderIssueStatuses: vi.fn(async () => {
        throw new Error("409");
      }),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useReorderIssueStatuses(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ category: "in_review", ordered: [second, first] }));
    await waitFor(() => expect(result.current.isError).toBe(true));

    expect(cached(qc)?.statuses.map((s) => s.id)).toEqual(["builtin-in-review", "qa", "sec"]);
  });

  // An archived status stays in the cache on purpose: issues still sitting on
  // it resolve their name, color and category through it.
  it("keeps an archived status in the catalog with its archived_at set", async () => {
    const qc = createClient();
    const qa = entry({ id: "qa", key: "qa", name: "QA" });
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview, qa]));
    setApiInstance({
      archiveIssueStatus: vi.fn(async () => ({ ...qa, archived_at: "2026-02-02T00:00:00Z" })),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useArchiveIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate("qa"));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(cached(qc)?.statuses.find((s) => s.id === "qa")?.archived_at).toBe("2026-02-02T00:00:00Z");
    expect(cached(qc)?.total).toBe(2);
  });

  // Archiving is terminal, so the FLAG is safe to apply to whatever row the
  // cache holds. The rest of the returned row is not: a rename that landed
  // while the archive was in flight would be reverted by a whole-row install.
  it("archives without reverting a rename that landed meanwhile", async () => {
    const qc = createClient();
    const qa = entry({ id: "qa", key: "qa", name: "QA" });
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview, qa]));
    const write = deferred<IssueStatusEntry>();
    setApiInstance({
      archiveIssueStatus: vi.fn(() => write.promise),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useArchiveIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate("qa"));

    realtimeRefetchLands(qc, catalog([builtInReview, { ...qa, name: "Ready for QA" }]));

    await act(async () => {
      write.resolve({ ...qa, archived_at: "2026-02-02T00:00:00Z" });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    const stored = cached(qc)?.statuses.find((s) => s.id === "qa");
    expect(stored?.name).toBe("Ready for QA");
    expect(stored?.archived_at).toBe("2026-02-02T00:00:00Z");
  });

  // The only way this client already holds the created id is a refetch that
  // read it back — so its copy is never older than this response.
  it("does not re-insert a created status the catalog already picked up", async () => {
    const qc = createClient();
    const created = entry({ id: "qa", key: "qa", name: "QA" });
    qc.setQueryData(issueStatusKeys.list("ws-1"), catalog([builtInReview]));
    const write = deferred<IssueStatusEntry>();
    setApiInstance({
      createIssueStatus: vi.fn(() => write.promise),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useCreateIssueStatus(), { wrapper: wrapper(qc) });
    act(() => result.current.mutate({ name: "QA", category: "in_review", color: "#6366f1" }));

    realtimeRefetchLands(qc, catalog([builtInReview, { ...created, name: "QA (renamed)" }]));

    await act(async () => {
      write.resolve(created);
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(cached(qc)?.statuses.map((s) => s.name)).toEqual(["In Review", "QA (renamed)"]);
    expect(cached(qc)?.total).toBe(2);
  });
});
