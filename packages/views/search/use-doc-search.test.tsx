import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const index = vi.hoisted(() => ({
  isLocalSearchReady: vi.fn(() => true),
  searchIssues: vi.fn(),
  searchProjects: vi.fn(),
  searchDocs: vi.fn(),
}));
vi.mock("@multica/core/search-index", () => index);

import { DOC_SEARCH_DEBOUNCE_MS, useDocSearch } from "./use-doc-search";
import { useServerSearch } from "./use-server-search";

const issue = { id: "i1", identifier: "MUL-1", title: "Rotate keys" };

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  index.isLocalSearchReady.mockReturnValue(true);
  index.searchIssues.mockResolvedValue({ issues: [issue], total: 1 });
  index.searchProjects.mockResolvedValue({ projects: [], total: 0 });
});
afterEach(() => vi.useRealTimers());

describe("palette doc search", () => {
  it("shows local issue results without waiting for a doc search that never returns", async () => {
    index.searchDocs.mockReturnValue(new Promise(() => {}));
    const { result } = renderHook(({ q }) => ({ server: useServerSearch(q, { projects: true }), docs: useDocSearch(q, true) }), {
      initialProps: { q: "rot" },
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(DOC_SEARCH_DEBOUNCE_MS + 50);
    });
    expect(result.current.server.results).toMatchObject({ query: "rot", issues: [issue] });
    expect(result.current.server.stale).toBe(false);
    expect(result.current.docs.docs).toEqual([]);
  });

  it("sends one doc request for a burst of keystrokes, for the final query", async () => {
    index.searchDocs.mockResolvedValue({ docs: [{ id: "d1", title: "Runbook" }] });
    const { result, rerender } = renderHook(({ q }) => useDocSearch(q, true), { initialProps: { q: "r" } });
    for (const q of ["ro", "rot", "rota"]) {
      rerender({ q });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(50);
      });
    }
    await act(async () => {
      await vi.advanceTimersByTimeAsync(DOC_SEARCH_DEBOUNCE_MS);
    });
    expect(index.searchDocs).toHaveBeenCalledTimes(1);
    expect(index.searchDocs).toHaveBeenCalledWith(expect.objectContaining({ q: "rota", limit: 10 }));
    expect(result.current).toEqual({ query: "rota", docs: [{ id: "d1", title: "Runbook" }] });
  });

  it("does not search docs in a scoped search", async () => {
    renderHook(() => useDocSearch("rota", false));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(DOC_SEARCH_DEBOUNCE_MS * 2);
    });
    expect(index.searchDocs).not.toHaveBeenCalled();
  });
});
