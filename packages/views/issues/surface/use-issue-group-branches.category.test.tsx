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
  IssueStatusCategory,
  IssueTableGroupsRequest,
  IssueTableRowsRequest,
} from "@multica/core/types";
import { useIssueGroupBranches } from "./use-issue-group-branches";

/**
 * Swimlane cells are CATEGORIES when the compound secondary axis says so. The
 * server returning a custom `qa` row into the `in_review` cell is only half the
 * job — the client re-checks each row against its descriptor, and matching the
 * raw key there (`qa !== "in_review"`) threw the card away again.
 */

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
    project_id: "p1",
    parent_issue_id: null,
    stage: null,
    position: 1,
    start_date: null,
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  } as Issue;
}

const QUERY = {
  scope: { kind: "workspace" as const },
  filters: {},
  sort: { field: "position" as const, direction: "asc" as const },
};

const CELL_KEY = "compound:project:p1:status_category:in_review";

function wrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("useIssueGroupBranches — category secondary axis", () => {
  function setup(secondary: "status" | "status_category") {
    const listIssueTableGroups = vi.fn(async (_request: IssueTableGroupsRequest) => ({
      query_fingerprint: "test",
      total: 2,
      groups: [
        {
          key: "project:p1",
          value: { kind: "project" as const, project_id: "p1" },
          count: 2,
          secondary_groups: [
            {
              key: CELL_KEY,
              value: { kind: "status" as const, status: "in_review" },
              count: 2,
            },
          ],
        },
      ],
      next_cursor: null,
    }));
    const listIssueTableRows = vi.fn(async (request: IssueTableRowsRequest) => ({
      query_fingerprint: "test",
      group_key: request.group_key ?? null,
      parent_id: null,
      total: 2,
      rows: [
        { issue: makeIssue("qa-1", "qa", "in_review"), direct_child_count: 0 },
        { issue: makeIssue("std-1", "in_review", "in_review"), direct_child_count: 0 },
      ],
      branch_total: 2,
      next_cursor: null,
    }));
    setApiInstance({ listIssueTableGroups, listIssueTableRows } as unknown as ApiClient);

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    return renderHook(
      () =>
        useIssueGroupBranches({
          wsId: "ws-1",
          query: QUERY,
          group: {
            kind: "compound",
            primary: "project",
            secondary,
            secondary_values: ["in_review"],
          },
          secondaryValues: ["in_review"],
          enabled: true,
        }),
      { wrapper: wrapper(qc) },
    );
  }

  // The regression: the server correctly folded `qa` into the in_review cell,
  // and the client then dropped the card because `qa !== "in_review"`.
  it("keeps a custom-status row in its category cell", async () => {
    const { result } = setup("status_category");

    // Branches are lazy: the lane activates its cell when it scrolls into view.
    await waitFor(() => expect(result.current.pagination[CELL_KEY]).toBeDefined());
    act(() => result.current.pagination[CELL_KEY]!.loadMore());

    await waitFor(() => expect(result.current.issues.length).toBe(2));
    expect(result.current.issues.map((i) => i.id).sort()).toEqual(["qa-1", "std-1"]);
  });

  // The key axis is still exact: on the `status` contract a `qa` row does NOT
  // belong to an `in_review` cell, and must still be dropped.
  it("still matches on the exact key when the axis is the status key", async () => {
    const { result } = setup("status");

    await waitFor(() => expect(result.current.pagination[CELL_KEY]).toBeDefined());
    act(() => result.current.pagination[CELL_KEY]!.loadMore());

    await waitFor(() => expect(result.current.issues.length).toBe(1));
    expect(result.current.issues.map((i) => i.id)).toEqual(["std-1"]);
  });
});
