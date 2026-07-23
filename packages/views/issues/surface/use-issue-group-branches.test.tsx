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
  IssueTableGroupsRequest,
  IssueTableQuerySpec,
  IssueTableRowsRequest,
} from "@multica/core/types";
import { useIssueGroupBranches } from "./use-issue-group-branches";

function makeIssue(id: string, status: Issue["status"]): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: id === "child-1" ? 1 : 2,
    identifier: id === "child-1" ? "MUL-1" : "MUL-2",
    title: id,
    description: null,
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: "parent-1",
    project_id: null,
    position: id === "child-1" ? 1 : 2,
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

describe("useIssueGroupBranches", () => {
  afterEach(() => cleanup());

  it("pages group headers only when the surface sentinel asks", async () => {
    const listIssueTableGroups = vi.fn(
      async (request: IssueTableGroupsRequest) => {
        const secondPage = request.page?.cursor === "groups-next";
        return {
          query_fingerprint: "test",
          total: 2,
          groups: [
            {
              key: secondPage
                ? "assignee:unassigned"
                : "assignee:member:user-1",
              value: {
                kind: "assignee" as const,
                actor: secondPage
                  ? null
                  : { type: "member" as const, id: "user-1" },
              },
              count: 1,
            },
          ],
          next_cursor: secondPage ? null : "groups-next",
        };
      },
    );
    setApiInstance({
      listIssueTableGroups,
      listIssueTableRows: vi.fn(),
    } as unknown as ApiClient);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const { result } = renderHook(
      () =>
        useIssueGroupBranches({
          wsId: "ws-1",
          query,
          group: { kind: "assignee" },
          enabled: true,
        }),
      { wrapper: wrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.descriptors).toHaveLength(1));
    expect(listIssueTableGroups).toHaveBeenCalledTimes(1);
    expect(result.current.hasMoreGroups).toBe(true);

    act(() => result.current.loadMoreGroups());
    await waitFor(() => expect(result.current.descriptors).toHaveLength(2));
    expect(listIssueTableGroups).toHaveBeenCalledTimes(2);

    queryClient.clear();
  });

  it("keeps exact compound descriptors while paging only visible cells", async () => {
    const first = makeIssue("child-1", "todo");
    const second = makeIssue("child-2", "todo");
    const listIssueTableGroups = vi.fn(async () => ({
      query_fingerprint: "test",
      total: 2,
      groups: [
        {
          key: "parent:parent-1",
          value: {
            kind: "parent" as const,
            parent_id: "parent-1",
            parent: {
              id: "parent-1",
              number: 10,
              identifier: "MUL-10",
              title: "Parent",
              status: "in_progress",
            },
            value_state: "value" as const,
          },
          count: 3,
          secondary_groups: [
            {
              key: "compound:cGFyZW50OnBhcmVudC0x:status:todo",
              value: { kind: "status" as const, status: "todo" },
              count: 2,
            },
            {
              key: "compound:cGFyZW50OnBhcmVudC0x:status:done",
              value: { kind: "status" as const, status: "done" },
              count: 1,
            },
          ],
        },
      ],
      next_cursor: null,
    }));
    const listIssueTableRows = vi.fn(
      async (request: IssueTableRowsRequest) => {
        const continuation = request.page?.cursor === "next";
        return {
          query_fingerprint: "test",
          group_key: request.group_key,
          parent_id: null,
          total: 0,
          rows: [{
            issue: continuation ? second : first,
            direct_child_count: 0,
          }],
          branch_total: 1,
          next_cursor: continuation ? null : "next",
        };
      },
    );
    setApiInstance({
      listIssueTableGroups,
      listIssueTableRows,
    } as unknown as ApiClient);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    const { result } = renderHook(
      () =>
        useIssueGroupBranches({
          wsId: "ws-1",
          query,
          group: {
            kind: "compound",
            primary: "parent",
            secondary: "status",
            secondary_values: ["todo"],
          },
          secondaryValues: ["todo"],
          enabled: true,
        }),
      { wrapper: wrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.descriptors).toHaveLength(1));
    expect(result.current.descriptors[0]?.secondary_groups).toHaveLength(2);
    expect(result.current.total).toBe(2);
    expect(listIssueTableRows).not.toHaveBeenCalled();

    const todoKey =
      result.current.descriptors[0]?.secondary_groups?.[0]?.key ?? "";
    expect(result.current.pagination[todoKey]?.total).toBe(2);
    act(() => result.current.pagination[todoKey]?.loadMore());
    await waitFor(() =>
      expect(result.current.issues.map((issue) => issue.id)).toEqual([
        "child-1",
      ]),
    );
    expect(listIssueTableRows).toHaveBeenCalledTimes(1);
    expect(listIssueTableRows.mock.calls[0]?.[0].group_key).toContain(
      ":status:todo",
    );
    act(() => result.current.pagination[todoKey]?.loadMore());
    await waitFor(() => expect(result.current.issues).toHaveLength(2));
    expect(listIssueTableRows).toHaveBeenCalledTimes(2);

    queryClient.clear();
  });

  it("uses the query-wide compound total before all group pages are loaded", async () => {
    const listIssueTableGroups = vi.fn(async () => ({
      query_fingerprint: "test",
      // A later group page owns the visible card. The loaded descriptor is
      // intentionally hidden-only to catch regressions that reduce `total`
      // from the currently loaded page instead of trusting the server.
      total: 1,
      groups: [
        {
          key: "parent:hidden",
          value: {
            kind: "parent" as const,
            parent_id: "hidden",
            parent: null,
            value_state: "unavailable" as const,
          },
          count: 1,
          secondary_groups: [
            {
              key: "compound:aGlkZGVu:status:done",
              value: { kind: "status" as const, status: "done" },
              count: 1,
            },
          ],
        },
      ],
      next_cursor: "groups-next",
    }));
    setApiInstance({
      listIssueTableGroups,
      listIssueTableRows: vi.fn(),
    } as unknown as ApiClient);
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    const { result } = renderHook(
      () =>
        useIssueGroupBranches({
          wsId: "ws-1",
          query,
          group: {
            kind: "compound",
            primary: "parent",
            secondary: "status",
            secondary_values: ["todo"],
          },
          secondaryValues: ["todo"],
          enabled: true,
        }),
      { wrapper: wrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.descriptors).toHaveLength(1));
    expect(result.current.total).toBe(1);
    expect(result.current.hasMoreGroups).toBe(true);

    queryClient.clear();
  });
});
