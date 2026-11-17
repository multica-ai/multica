// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@/data/api";
import { invalidateSessionEpoch } from "@/data/session-epoch";
import { groupIssuesByStatus } from "@/lib/group-issues-by-status";
import { issueListOptions } from "./issues";
import { myIssueListOptions } from "./my-issues";
import { projectIssuesOptions } from "./projects";

const workspace = vi.hoisted(() => ({ currentWorkspaceId: "ws-a" }));
vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: { getState: () => workspace },
}));
vi.mock("@/data/api", () => ({ api: { listIssues: vi.fn() } }));

const rows = Array.from({ length: 205 }, (_, index) => ({
  id: `issue-${index}`,
  position: index,
  status: index < 200 ? "done" : ["backlog", "todo", "in_progress", "in_review", "blocked"][index - 200],
})) as Issue[];
const options = () => issueListOptions("ws-a");
const client = () => new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
const list = vi.mocked(api.listIssues);

beforeEach(() => {
  list.mockReset();
  workspace.currentWorkspaceId = "ws-a";
});

function serve(pageSize = 100, data = rows) {
  list.mockImplementation(async (params = {}) => ({
    issues: data.slice(params.offset ?? 0, (params.offset ?? 0) + pageSize),
    total: data.length,
  }));
}

describe("complete mobile issue queries", () => {
  it.each([
    ["workspace", (qc: QueryClient) => qc.fetchQuery(options()), options().queryKey, {}],
    ["my issues", (qc: QueryClient) => qc.fetchQuery(myIssueListOptions("ws-a", "assigned", { assignee_id: "me" })), myIssueListOptions("ws-a", "assigned", { assignee_id: "me" }).queryKey, { assignee_id: "me" }],
    ["project", (qc: QueryClient) => qc.fetchQuery(projectIssuesOptions("ws-a", "project-a")), projectIssuesOptions("ws-a", "project-a").queryKey, { project_id: "project-a" }],
  ] as const)("loads 200 done issues and all later statuses for %s", async (_name, fetch, key, filter) => {
    serve();
    const qc = client();
    const result = await fetch(qc);
    expect(result).toEqual(rows);
    expect(qc.getQueryData(key)).toEqual(rows);
    expect(list.mock.calls.map(([params]) => params?.offset)).toEqual([0, 100, 200]);
    for (const [params, request] of list.mock.calls) {
      expect(params).toMatchObject({ ...filter, limit: 100, sort_by: "created_at", sort_direction: "asc" });
      expect(request?.signal).toBeInstanceOf(AbortSignal);
    }
    const sections = groupIssuesByStatus(result, [], "workflow");
    expect(sections.map(({ status, data }) => [status, data.length])).toEqual([
      ["backlog", 1], ["todo", 1], ["in_progress", 1], ["in_review", 1], ["blocked", 1], ["done", 200],
    ]);
    qc.clear();
  });

  it("continues past short server-capped pages and preserves manual row order", async () => {
    serve(40, rows.map((row, index) => ({ ...row, position: rows.length - index })));
    const qc = client();
    expect((await qc.fetchQuery(options())).map((row) => row.id)).toEqual([...rows].reverse().map((row) => row.id));
    expect(list.mock.calls.map(([params]) => params?.offset)).toEqual([0, 40, 80, 120, 160, 200]);
    qc.clear();
  });

  it.each(["failure", "empty page", "repeated page", "invalid total"])("does not cache a partial list after a later %s", async (failure) => {
    serve();
    list.mockImplementationOnce(async () => ({ issues: rows.slice(0, 100), total: rows.length }));
    list.mockImplementationOnce(async () => {
      if (failure === "failure") throw new Error("Offline");
      return {
        issues: failure === "empty page" ? [] : failure === "invalid total" ? rows.slice(100, 200) : rows.slice(0, 100),
        total: failure === "invalid total" ? 0 : rows.length,
      };
    });
    const qc = client();
    await expect(qc.fetchQuery(options())).rejects.toThrow();
    expect(qc.getQueryData(options().queryKey)).toBeUndefined();
    expect(list).toHaveBeenCalledTimes(2);
    qc.clear();
  });

  it.each(["session", "workspace", "cancel"])("does not continue or publish old pages after a %s change", async (change) => {
    let complete!: (value: { issues: Issue[]; total: number }) => void;
    list.mockImplementationOnce(() => new Promise((resolve) => { complete = resolve; }));
    const qc = client();
    const pending = qc.fetchQuery(options()).catch((error) => error);
    await vi.waitFor(() => expect(list).toHaveBeenCalledOnce());
    if (change === "session") invalidateSessionEpoch();
    else if (change === "workspace") workspace.currentWorkspaceId = "ws-b";
    else await qc.cancelQueries({ queryKey: options().queryKey });
    complete({ issues: rows.slice(0, 100), total: rows.length });
    await pending;
    await Promise.resolve();
    expect(list).toHaveBeenCalledOnce();
    expect(qc.getQueryData(options().queryKey)).toBeUndefined();
    qc.clear();
  });
});
