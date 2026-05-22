import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { Issue, ListIssuesParams, ListIssuesResponse } from "../types";
import {
  ISSUE_PAGE_SIZE,
  PROJECT_GANTT_MAX_ISSUES,
  PROJECT_GANTT_PAGE_LIMIT,
  issueListOptions,
  issueKeys,
  projectGanttIssuesOptions,
  type IssueListFilter,
} from "./queries";

const WS_ID = "ws-1";
const PROJECT_ID = "project-1";

function makeIssue(idx: number): Issue {
  return {
    id: `issue-${idx}`,
    workspace_id: WS_ID,
    number: idx,
    identifier: `MUL-${idx}`,
    title: `Issue ${idx}`,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: PROJECT_ID,
    position: idx,
    start_date: "2026-05-01T00:00:00Z",
    due_date: null,
    labels: [],
    metadata: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  };
}

// Type-only shim — only the methods the queries.ts code path under test calls.
function installFakeApi(listIssues: (params?: ListIssuesParams) => Promise<ListIssuesResponse>) {
  setApiInstance({ listIssues } as unknown as ApiClient);
}

describe("issueListOptions", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("fetches only selected statuses and forwards server-side filters", async () => {
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockResolvedValue({ issues: [], total: 0 });
    installFakeApi(listIssues);

    const filter: IssueListFilter = {
      statuses: ["done"] as const,
      priorities: ["high"],
      assignees: [{ type: "member" as const, id: "user-1" }],
      creators: [{ type: "agent" as const, id: "agent-1" }],
      label_ids: ["label-1"],
      project_ids: ["project-1"],
      include_no_project: true,
    };

    await qc.fetchQuery(issueListOptions(WS_ID, filter));

    expect(listIssues).toHaveBeenCalledTimes(1);
    expect(listIssues).toHaveBeenCalledWith({
      status: "done",
      limit: ISSUE_PAGE_SIZE,
      offset: 0,
      priorities: ["high"],
      assignees: [{ type: "member", id: "user-1" }],
      creators: [{ type: "agent", id: "agent-1" }],
      label_ids: ["label-1"],
      project_ids: ["project-1"],
      include_no_project: true,
    });
  });

  it("keeps server filters in the query key so filter changes refetch", () => {
    const doneHigh = issueListOptions(WS_ID, {
      statuses: ["done"],
      priorities: ["high"],
    });
    const doneLow = issueListOptions(WS_ID, {
      statuses: ["done"],
      priorities: ["low"],
    });

    expect(doneHigh.queryKey).toEqual(
      issueKeys.list(WS_ID, { statuses: ["done"], priorities: ["high"] }),
    );
    expect(doneHigh.queryKey).not.toEqual(doneLow.queryKey);
  });
});

describe("projectGanttIssuesOptions", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("returns the first page directly when it fits under PROJECT_GANTT_PAGE_LIMIT", async () => {
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockResolvedValue({
        issues: [makeIssue(1), makeIssue(2)],
        total: 2,
      });
    installFakeApi(listIssues);

    const data = await qc.fetchQuery(projectGanttIssuesOptions(WS_ID, PROJECT_ID));

    expect(listIssues).toHaveBeenCalledTimes(1);
    expect(listIssues).toHaveBeenCalledWith({
      project_id: PROJECT_ID,
      scheduled: true,
      limit: PROJECT_GANTT_PAGE_LIMIT,
      offset: 0,
    });
    expect(data).toHaveLength(2);
  });

  it("loops through pages until total is satisfied (no silent truncation)", async () => {
    const total = PROJECT_GANTT_PAGE_LIMIT + 7;
    const firstPage = Array.from({ length: PROJECT_GANTT_PAGE_LIMIT }, (_, i) =>
      makeIssue(i),
    );
    const secondPage = Array.from({ length: 7 }, (_, i) =>
      makeIssue(PROJECT_GANTT_PAGE_LIMIT + i),
    );

    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => {
        if (!params) throw new Error("expected params");
        const offset = params.offset ?? 0;
        if (offset === 0)
          return { issues: firstPage, total };
        if (offset === PROJECT_GANTT_PAGE_LIMIT)
          return { issues: secondPage, total };
        throw new Error(`unexpected offset ${offset}`);
      });
    installFakeApi(listIssues);

    const data = await qc.fetchQuery(projectGanttIssuesOptions(WS_ID, PROJECT_ID));

    expect(listIssues).toHaveBeenCalledTimes(2);
    expect(data).toHaveLength(total);
  });

  it("stops looping when the server reports a smaller-than-limit page (safety net for total drift)", async () => {
    // Server says `total` is huge but only ever returns short pages — the
    // loop must terminate on the first short page to avoid an infinite fetch.
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockResolvedValue({
        issues: [makeIssue(1)],
        total: PROJECT_GANTT_MAX_ISSUES,
      });
    installFakeApi(listIssues);

    const data = await qc.fetchQuery(projectGanttIssuesOptions(WS_ID, PROJECT_ID));

    expect(listIssues).toHaveBeenCalledTimes(1);
    expect(data).toHaveLength(1);
  });

  it("uses the project-scoped Gantt cache key", () => {
    const options = projectGanttIssuesOptions(WS_ID, PROJECT_ID);
    expect(options.queryKey).toEqual(issueKeys.projectGantt(WS_ID, PROJECT_ID));
  });
});
