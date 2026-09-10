/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { Issue, IssueStatusCategory, IssueTableRowsRequest } from "@multica/core/types";
import { useIssueStatusBranches } from "./use-issue-status-branches";

/** Built-ins and custom statuses sharing a category use separate branches. */

function makeIssue(id: string, status: string, category: IssueStatusCategory): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: id,
    description: null,
    status,
    status_category: category,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "u-1",
    project_id: null,
    parent_issue_id: null,
    stage: null,
    position: 1,
    start_date: null,
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  } as Issue;
}

const QA_ENTRY = {
  id: "s-qa",
  workspace_id: "ws-1",
  key: "qa",
  name: "QA",
  description: "",
  // Legacy servers return the concrete behavior category. The client folds it
  // into the four-category model during a rolling upgrade.
  category: "in_review" as unknown as IssueStatusCategory,
  color: "#ff0000",
  is_system: false,
  position: 1,
  archived_at: null,
  created_at: "",
  updated_at: "",
};

const QUERY = {
  scope: { kind: "workspace" as const },
  filters: {},
  sort: { field: "position" as const, direction: "asc" as const },
};

function wrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("useIssueStatusBranches — exact status columns", () => {
  it("pages custom statuses separately and rejects stale rows from a sibling status", async () => {
    const requests: IssueTableRowsRequest[] = [];
    const listIssueTableRows = vi.fn(async (request: IssueTableRowsRequest) => {
      requests.push(request);
      const rows =
        request.group_key === "status:qa"
          ? [
              { issue: makeIssue("qa-1", "qa", "started"), direct_child_count: 0 },
              { issue: makeIssue("std-1", "in_review", "started"), direct_child_count: 0 },
            ]
          : request.group_key === "status:in_review" ? [{ issue: makeIssue("std-1", "in_review", "started"), direct_child_count: 0 }] : [];
      return {
        query_fingerprint: "test",
        group_key: request.group_key ?? null,
        parent_id: null,
        total: rows.length,
        rows,
        branch_total: rows.length,
        next_cursor: null,
      };
    });
    setApiInstance({
      listIssueTableRows,
      // The catalog resolves the custom row and its legacy category value.
      listIssueStatuses: async () => ({ statuses: [QA_ENTRY], categories: [], total: 1 }),
    } as unknown as ApiClient);

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const { result } = renderHook(
      () =>
        useIssueStatusBranches({
          wsId: "ws-1",
          query: QUERY,
          statuses: ["qa", "in_review"],
          facets: undefined,
          facetsPending: false,
          facetsFetching: false,
          enabled: true,
        }),
      { wrapper: wrapper(qc) },
    );

    await waitFor(() => expect(result.current.issues.length).toBe(2));

    // Each column requests its own exact key, even within the same category.
    expect(requests[0]?.group).toEqual({ kind: "status" });
    expect(requests[0]?.group_key).toBe("status:qa");
    expect(requests.at(-1)?.group).toEqual({ kind: "status" });
    expect(requests.map((r) => r.group_key)).toContain("status:in_review");
    // Both independent branches contribute rows to the visible surface.
    expect(result.current.issues.map((i) => i.id).sort()).toEqual(["qa-1", "std-1"]);
  });

  it("keeps independent facet totals for statuses in the same category", async () => {
    setApiInstance({
      listIssueTableRows: async (request: IssueTableRowsRequest) => ({
        query_fingerprint: "test",
        group_key: request.group_key ?? null,
        parent_id: null,
        total: 0,
        rows: [],
        branch_total: 0,
        next_cursor: null,
      }),
      listIssueStatuses: async () => ({ statuses: [QA_ENTRY], categories: [], total: 1 }),
    } as unknown as ApiClient);

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const { result } = renderHook(
      () =>
        useIssueStatusBranches({
          wsId: "ws-1",
          query: QUERY,
          statuses: ["qa", "in_review"],
          facets: {
            query_fingerprint: "test",
            total: 5,
            facets: [
              {
                kind: "status",
                // The facet is keyed by concrete status KEY. Discarding the
                // custom key made the column header disagree with its cards.
                values: [
                  { key: "in_review", count: 2 },
                  { key: "qa", count: 3 },
                ],
              },
            ],
          },
          facetsPending: false,
          facetsFetching: false,
          enabled: true,
        }),
      { wrapper: wrapper(qc) },
    );

    await waitFor(() =>
      expect(result.current.pagination.qa?.total).toBe(3),
    );
    expect(result.current.pagination.in_review?.total).toBe(2);
  });

  // The status facet is disjunctive: the server answers it with the status
  // filter dropped so the filter MENU can show per-option counts. A column
  // header asks the opposite question, so an active filter has to narrow the
  // fold — otherwise filtering by one custom status headed the In Review
  // column with every in_review issue while showing only the matching cards
  // beneath it. (MUL-6409)
  it("counts only the selected statuses while a status filter is active", async () => {
    setApiInstance({
      listIssueTableRows: async (request: IssueTableRowsRequest) => ({
        query_fingerprint: "test",
        group_key: request.group_key ?? null,
        parent_id: null,
        total: 0,
        rows: [],
        branch_total: 0,
        next_cursor: null,
      }),
      listIssueStatuses: async () => ({ statuses: [QA_ENTRY], categories: [], total: 1 }),
    } as unknown as ApiClient);

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const { result } = renderHook(
      () =>
        useIssueStatusBranches({
          wsId: "ws-1",
          query: { ...QUERY, filters: { statuses: ["qa"] } },
          statuses: ["qa", "in_review"],
          facets: {
            query_fingerprint: "test",
            total: 5,
            facets: [
              {
                kind: "status",
                values: [
                  { key: "in_review", count: 2 },
                  { key: "qa", count: 3 },
                ],
              },
            ],
          },
          facetsPending: false,
          facetsFetching: false,
          enabled: true,
        }),
      { wrapper: wrapper(qc) },
    );

    await waitFor(() =>
      expect(result.current.pagination.qa?.total).toBe(3),
    );
  });
});

/**
 * A workspace without custom statuses still needs category grouping because
 * Started contains three concrete built-in states.
 */
describe("useIssueStatusBranches — built-in-only workspaces", () => {
  it("uses exact status grouping without custom statuses", async () => {
    const requests: IssueTableRowsRequest[] = [];
    const listIssueStatuses = vi.fn(async () => ({
      // Built-ins only — the state of every workspace until an admin creates
      // a custom status, which the rollout flag gates.
      statuses: [
        { ...QA_ENTRY, id: "s-in-review", key: "in_review", name: "In Review", is_system: true, position: 0 },
      ],
      categories: [],
      total: 1,
    }));
    setApiInstance({
      listIssueTableRows: async (request: IssueTableRowsRequest) => {
        requests.push(request);
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
      listIssueStatuses,
    } as unknown as ApiClient);

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    renderHook(
      () =>
        useIssueStatusBranches({
          wsId: "ws-1",
          query: QUERY,
          statuses: ["in_review"],
          facets: undefined,
          facetsPending: false,
          facetsFetching: false,
          enabled: true,
        }),
      { wrapper: wrapper(qc) },
    );

    await waitFor(() => expect(listIssueStatuses).toHaveBeenCalled());
    await waitFor(() => expect(requests.length).toBeGreaterThan(0));

    for (const request of requests) {
      expect(request.group).toEqual({ kind: "status" });
      expect(request.group_key).toBe("status:in_review");
    }
  });
})
