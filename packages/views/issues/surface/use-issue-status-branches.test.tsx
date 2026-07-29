/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type {
  Issue,
  IssueStatus,
  IssueTableQuerySpec,
  IssueTableRowsRequest,
} from "@multica/core/types";
import { useIssueStatusBranches } from "./use-issue-status-branches";

function makeIssue(id: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: id === "issue-1" ? 1 : 2,
    identifier: id === "issue-1" ? "MUL-1" : "MUL-2",
    title: id,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: id === "issue-1" ? 1 : 2,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

const query: IssueTableQuerySpec = {
  scope: { kind: "workspace" },
  filters: { include_sub_issues: true },
  sort: { field: "position", direction: "asc" },
};

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    );
  };
}

describe("useIssueStatusBranches", () => {
  afterEach(() => cleanup());

  it("uses one cursor chain per active status and exact facet totals", async () => {
    const first = makeIssue("issue-1");
    const second = makeIssue("issue-2");
    const listIssueTableRows = vi.fn(
      async (request: IssueTableRowsRequest) => {
        const continuation = request.page?.cursor === "cursor-2";
        const issue = continuation ? second : first;
        return {
          query_fingerprint: "test",
          group_key: request.group_key,
          parent_id: null,
          total: 0,
          rows: [{ issue, direct_child_count: 0 }],
          branch_total: 1,
          next_cursor: continuation ? null : "cursor-2",
        };
      },
    );
    setApiInstance({ listIssueTableRows } as unknown as ApiClient);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    const { result, rerender } = renderHook(
      ({ statuses }: { statuses: IssueStatus[] }) =>
        useIssueStatusBranches({
          wsId: "ws-1",
          query,
          statuses,
          facets: {
            query_fingerprint: "test",
            total: 2,
            facets: [
              {
                kind: "status",
                values: [{ key: "todo", count: 2 }],
              },
            ],
          },
          facetsPending: false,
          facetsFetching: false,
          enabled: true,
        }),
      {
        initialProps: { statuses: ["todo"] },
        wrapper: wrapper(queryClient),
      },
    );

    await waitFor(() =>
      expect(result.current.issues.map((issue) => issue.id)).toEqual([
        "issue-1",
      ]),
    );
    expect(result.current.pagination.todo.total).toBe(2);
    expect(result.current.pagination.todo.hasMore).toBe(true);
    expect(result.current.total).toBe(2);
    expect(result.current.isTotalKnown).toBe(true);

    act(() => result.current.pagination.todo.loadMore());
    await waitFor(() =>
      expect(result.current.issues.map((issue) => issue.id)).toEqual([
        "issue-1",
        "issue-2",
      ]),
    );
    expect(listIssueTableRows).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        group_key: "status:todo",
        page: { limit: 50, cursor: "cursor-2" },
      }),
    );

    // Collapsing a List section removes its active observers. Re-expanding
    // reuses the settled cursor pages instead of restarting another chain.
    rerender({ statuses: [] });
    expect(result.current.issues).toEqual([]);
    rerender({ statuses: ["todo"] });
    await waitFor(() => expect(result.current.issues).toHaveLength(2));
    expect(listIssueTableRows).toHaveBeenCalledTimes(2);

    queryClient.clear();
  });
});
