import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider, QueryObserver } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { issueKeys } from "@multica/core/issues/queries";
import { useSidebarKeptStore } from "@multica/core/issues/stores/sidebar-kept-store";
import type { Issue, ListIssuesResponse } from "@multica/core/types";
import { useIssueInTree } from "./work-tree";

const WS = "ws-1";
const SOURCE = "project:p-1";
const LIST_KEY = [...issueKeys.sidebarAll(WS), SOURCE, {}, 25];

function issue(id: string, identifier: string): Issue {
  return { id, identifier } as Issue;
}

function list(...issues: Issue[]): ListIssuesResponse {
  return { issues, total: issues.length } as ListIssuesResponse;
}

/** A mounted observer on `queryKey`, as a rendered list or detail would hold. */
function observe(qc: QueryClient, queryKey: readonly unknown[]) {
  return new QueryObserver(qc, { queryKey, enabled: false }).subscribe(() => {});
}

function setup(ref: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const hook = renderHook(() => useIssueInTree(WS, ref), { wrapper });
  return { qc, hook };
}

beforeEach(() => {
  useSidebarKeptStore.setState({ held: {} });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useIssueInTree", () => {
  it("finds the issue once an expanded row's list holds it", () => {
    const { qc, hook } = setup("MUL-1");
    expect(hook.result.current.issueId).toBeNull();

    act(() => {
      observe(qc, LIST_KEY);
      qc.setQueryData(LIST_KEY, list(issue("i-1", "MUL-1")));
    });

    expect(hook.result.current).toEqual({ issueId: "i-1", sources: [SOURCE] });
  });

  it("finds an issue a shown list keeps after its query dropped it", () => {
    const { qc, hook } = setup("i-1");
    act(() => {
      observe(qc, LIST_KEY);
      qc.setQueryData(LIST_KEY, list());
      useSidebarKeptStore.setState({ held: { [SOURCE]: { id: "i-1", index: 0 } } });
    });
    expect(hook.result.current.issueId).toBeNull();

    // The kept issue's detail arriving is what lets the answer match it.
    act(() => {
      qc.setQueryData(issueKeys.detail(WS, "i-1"), issue("i-1", "MUL-1"));
    });

    expect(hook.result.current).toEqual({ issueId: "i-1", sources: [SOURCE] });
  });

  // MUL-9375: opening an issue mounts hundreds of observers on unrelated
  // queries, and a rescan of the whole cache for each blocked the main thread.
  it("does not rescan the cache for queries the answer does not read", () => {
    const { qc } = setup("MUL-1");
    const findAll = vi.spyOn(qc.getQueryCache(), "findAll");

    act(() => {
      for (let i = 0; i < 20; i += 1) {
        observe(qc, ["comments", WS, `c-${i}`]);
        qc.setQueryData(["comments", WS, `c-${i}`], { n: i });
      }
      // A detail no list keeps.
      observe(qc, issueKeys.detail(WS, "i-2"));
      qc.setQueryData(issueKeys.detail(WS, "i-2"), issue("i-2", "MUL-2"));
    });

    expect(findAll).not.toHaveBeenCalled();
  });
});
