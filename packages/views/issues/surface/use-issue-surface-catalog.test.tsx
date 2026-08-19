/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import {
  getIssueSurfaceViewStore,
  pruneIssueSurfaceViewStates,
} from "@multica/core/issues/stores/surface-view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type { IssueStatusEntry, IssueTableGroupsRequest, IssueTableRowsRequest } from "@multica/core/types";
import { useIssueSurfaceController } from "./use-issue-surface-controller";

/**
 * The catalog is server state, so it can be late or absent. Two behaviours
 * depend on that and neither is expressible with a boolean "loaded" flag:
 *
 * - A CUSTOM status filter cannot be routed to a column until the catalog
 *   answers. Fetching zero branches meanwhile renders an empty board with no
 *   spinner; failing renders one permanently, with no way to retry.
 * - `status_category` is a server contract this feature introduced, so it must
 *   not be sent until the catalog proves this workspace has a custom status.
 */

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const QA_ENTRY: IssueStatusEntry = {
  id: "s-qa",
  workspace_id: "ws-1",
  key: "qa",
  name: "QA",
  description: "",
  category: "in_review",
  color: "#ff0000",
  is_system: false,
  position: 1,
  archived_at: null,
  created_at: "",
  updated_at: "",
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

let groupRequests: IssueTableGroupsRequest[] = [];
let rowRequests: IssueTableRowsRequest[] = [];

function installApi(listIssueStatuses: () => Promise<unknown>) {
  groupRequests = [];
  rowRequests = [];
  setApiInstance({
    listIssueStatuses,
    listIssueTableGroups: async (request: IssueTableGroupsRequest) => {
      groupRequests.push(request);
      return { query_fingerprint: "test", total: 0, groups: [], next_cursor: null };
    },
    listIssueTableRows: async (request: IssueTableRowsRequest) => {
      rowRequests.push(request);
      return {
        query_fingerprint: "test",
        group_key: request.group_key ?? null,
        parent_id: null,
        total: 0,
        rows: [],
        branch_total: 0,
        next_cursor: null,
      };
    },
    listIssueTableFacets: async () => ({ query_fingerprint: "test", total: 0, facets: [] }),
    listIssues: async () => ({ issues: [], total: 0 }),
    listProjects: async () => ({ projects: [], total: 0 }),
    getWorkspaceWorkingAgents: async () => [],
    getChildIssueProgress: async () => ({ progress: [] }),
    getAgentTaskSnapshot: async () => ({ tasks: [] }),
  } as unknown as ApiClient);
}

function makeWrapper(qc: QueryClient, surfaceKey: string) {
  const store = getIssueSurfaceViewStore(surfaceKey);
  return {
    store,
    Wrapper: function Wrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={qc}>
          <ViewStoreProvider store={store}>{children}</ViewStoreProvider>
        </QueryClientProvider>
      );
    },
  };
}

let qc: QueryClient;
beforeEach(() => {
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  pruneIssueSurfaceViewStates([]);
});
afterEach(() => {
  cleanup();
  qc.clear();
  vi.restoreAllMocks();
});

describe("useIssueSurfaceController — custom status filter vs a late catalog", () => {
  it("stays loading while the catalog a custom filter depends on is in flight", async () => {
    const catalog = deferred<{ statuses: IssueStatusEntry[]; categories: never[]; total: number }>();
    installApi(() => catalog.promise);
    const { store, Wrapper } = makeWrapper(qc, "workspace:catalog-pending");
    act(() => store.getState().toggleStatusFilter("qa"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["list"],
        }),
      { wrapper: Wrapper },
    );

    // The regression: the surface reported "loaded, zero results" and rendered
    // an empty board for the whole cold-load window.
    await waitFor(() => expect(result.current.isLoading).toBe(true));
    expect(result.current.isEmpty).toBe(false);
    // And nothing is fetched: with the filter unresolved the visible column set
    // falls back to ALL categories, so fetching would briefly show the
    // UNFILTERED board to someone who opened a saved `qa` view.
    expect(rowRequests).toEqual([]);

    await act(async () => {
      catalog.resolve({ statuses: [QA_ENTRY], categories: [], total: 1 });
      await catalog.promise;
    });

    // Once the catalog answers, the filter routes to the column `qa` behaves as.
    await waitFor(() => expect(result.current.visibleStatuses).toEqual(["in_review"]));
    await waitFor(() => expect(result.current.isLoading).toBe(false));
  });

  it("surfaces a retryable error instead of a silent empty board when the catalog fails", async () => {
    const catalog = deferred<never>();
    installApi(() => catalog.promise);
    const { store, Wrapper } = makeWrapper(qc, "workspace:catalog-failed");
    act(() => store.getState().toggleStatusFilter("qa"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["list"],
        }),
      { wrapper: Wrapper },
    );

    await act(async () => {
      catalog.reject(new Error("catalog unavailable"));
      await catalog.promise.catch(() => {});
    });

    await waitFor(() => expect(result.current.isStatusCatalogError).toBe(true));
    // Not "no issues" — the surface cannot answer the question that was asked.
    expect(result.current.isEmpty).toBe(false);
  });
});

describe("useIssueSurfaceController — swimlane protocol", () => {
  it("keeps the swimlane on the original contract with no custom statuses", async () => {
    installApi(async () => ({ statuses: [], categories: [], total: 0 }));
    const { store, Wrapper } = makeWrapper(qc, "workspace:swimlane-plain");
    act(() => store.getState().setViewMode("swimlane"));

    renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["swimlane"],
        }),
      { wrapper: Wrapper },
    );

    await waitFor(() => expect(groupRequests.length).toBeGreaterThan(0));
    // Every request, not just the first: a new Web build must never send a
    // group kind an un-upgraded backend would reject.
    for (const request of groupRequests) {
      expect(request.group).toMatchObject({ kind: "compound", secondary: "status" });
    }
    for (const request of rowRequests) {
      expect(request.group).toMatchObject({ kind: "compound", secondary: "status" });
    }
  });

  it("upgrades the swimlane to the category axis once a custom status exists", async () => {
    installApi(async () => ({ statuses: [QA_ENTRY], categories: [], total: 1 }));
    const { store, Wrapper } = makeWrapper(qc, "workspace:swimlane-custom");
    act(() => store.getState().setViewMode("swimlane"));

    renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["swimlane"],
        }),
      { wrapper: Wrapper },
    );

    await waitFor(() =>
      expect(
        groupRequests.some((request) =>
          request.group.kind === "compound" && request.group.secondary === "status_category",
        ),
      ).toBe(true),
    );
  });
});
