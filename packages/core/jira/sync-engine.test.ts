import { describe, expect, it, vi } from "vitest";
import { syncJiraIssues, clearSyncedJiraIssues } from "./sync-engine";
import type { JiraConfig } from "./types";

const config: JiraConfig = {
  siteUrl: "https://acme.atlassian.net",
  email: "me@acme.com",
  jql: "assignee = currentUser()",
  statusMapping: {},
  pollIntervalMinutes: 0,
};

function fakeApi(existing: any[] = []): any {
  return {
    listIssues: vi.fn().mockResolvedValue({ issues: existing, total: existing.length }),
    createIssue: vi.fn(async (req: any) => ({ id: "new-" + req.title, ...req, metadata: {} })),
    updateIssue: vi.fn(async (id: string, req: any) => ({ id, ...req })),
    setIssueMetadata: vi.fn().mockResolvedValue(undefined),
    listComments: vi.fn().mockResolvedValue([]),
    createComment: vi.fn().mockResolvedValue({ id: "c1" }),
  };
}

function searchResponse(issues: any[]) {
  return { issues, total: issues.length };
}

describe("syncJiraIssues — create/update/skip", () => {
  it("creates a Multica issue for an unseen Jira issue and stamps metadata", async () => {
    const api = fakeApi([]);
    const transport = vi.fn(async ({ path }: any) =>
      path.includes("/search")
        ? searchResponse([
            {
              key: "PROJ-1",
              fields: {
                summary: "Fix login",
                description: null,
                duedate: null,
                updated: "2026-06-30T10:00:00.000+0000",
                status: { name: "To Do" },
                priority: null,
                subtasks: [],
                comment: { comments: [] },
              },
            },
          ])
        : {},
    );

    const result = await syncJiraIssues({ transport, api, config, currentMemberId: "m1" });

    expect(api.createIssue).toHaveBeenCalledTimes(1);
    expect(api.createIssue.mock.calls[0][0].title).toBe("Fix login");
    const keysWritten = api.setIssueMetadata.mock.calls.map((c: any[]) => c[1]);
    expect(keysWritten).toEqual([
      "source",
      "jira_key",
      "jira_url",
      "jira_status",
      "jira_updated_at",
      "jira_comments_synced_at",
    ]);
    expect(result.created).toBe(1);
    expect(result.errors).toEqual([]);
  });

  it("skips an already-synced unchanged Jira issue", async () => {
    const api = fakeApi([
      {
        id: "i1",
        title: "Fix login",
        metadata: {
          source: "jira",
          jira_key: "PROJ-1",
          jira_updated_at: "2026-06-30T10:00:00.000+0000",
          jira_comments_synced_at: "2026-06-30T10:00:00.000+0000",
        },
      },
    ]);
    const transport = vi.fn(async ({ path }: any) =>
      path.includes("/search")
        ? searchResponse([
            {
              key: "PROJ-1",
              fields: {
                summary: "Fix login",
                description: null,
                duedate: null,
                updated: "2026-06-30T10:00:00.000+0000",
                status: { name: "To Do" },
                priority: null,
                subtasks: [],
                comment: { comments: [] },
              },
            },
          ])
        : {},
    );

    const result = await syncJiraIssues({ transport, api, config, currentMemberId: "m1" });
    expect(api.createIssue).not.toHaveBeenCalled();
    expect(api.updateIssue).not.toHaveBeenCalled();
    expect(result.skipped).toBe(1);
  });

  it("updates a synced issue when Jira reports a newer update timestamp", async () => {
    const api = fakeApi([
      {
        id: "i1",
        title: "Old title",
        metadata: {
          source: "jira",
          jira_key: "PROJ-1",
          jira_updated_at: "2026-06-30T09:00:00.000+0000",
          jira_comments_synced_at: "2026-06-30T09:00:00.000+0000",
        },
      },
    ]);
    const transport = vi.fn(async ({ path }: any) =>
      path.includes("/search")
        ? searchResponse([
            {
              key: "PROJ-1",
              fields: {
                summary: "New title",
                description: null,
                duedate: null,
                updated: "2026-06-30T10:00:00.000+0000",
                status: { name: "Done" },
                priority: null,
                subtasks: [],
                comment: { comments: [] },
              },
            },
          ])
        : {},
    );

    const result = await syncJiraIssues({ transport, api, config, currentMemberId: "m1" });
    expect(api.updateIssue).toHaveBeenCalledTimes(1);
    expect(api.updateIssue.mock.calls[0][1].title).toBe("New title");
    expect(result.updated).toBe(1);
  });

  it("collects an error per failed issue without aborting the run", async () => {
    const api = fakeApi([]);
    api.createIssue.mockRejectedValueOnce(new Error("boom"));
    const transport = vi.fn(async ({ path }: any) =>
      path.includes("/search")
        ? searchResponse([
            {
              key: "PROJ-1",
              fields: {
                summary: "a",
                status: { name: "Done" },
                subtasks: [],
                comment: { comments: [] },
                updated: "t",
                description: null,
                duedate: null,
                priority: null,
              },
            },
          ])
        : {},
    );
    const result = await syncJiraIssues({ transport, api, config, currentMemberId: "m1" });
    expect(result.errors).toEqual([{ jiraKey: "PROJ-1", message: "boom" }]);
    expect(result.created).toBe(0);
  });
});

describe("syncJiraIssues — subtasks and comments", () => {
  it("creates subtasks as Multica child issues under their parent", async () => {
    const api = fakeApi([]);
    const transport = vi.fn(async ({ path }: any) => {
      if (path.includes("/search")) {
        return searchResponse([
          {
            key: "PROJ-1",
            fields: {
              summary: "Parent",
              description: null,
              duedate: null,
              updated: "2026-06-30T10:00:00.000+0000",
              status: { name: "To Do" },
              priority: null,
              subtasks: [{ key: "PROJ-2" }],
              comment: { comments: [] },
            },
          },
        ]);
      }
      if (path.includes("/issue/PROJ-2")) {
        return {
          key: "PROJ-2",
          fields: {
            summary: "Child",
            description: null,
            duedate: null,
            updated: "2026-06-30T09:00:00.000+0000",
            status: { name: "Done" },
            priority: null,
            subtasks: [],
            comment: { comments: [] },
          },
        };
      }
      return {};
    });

    const result = await syncJiraIssues({ transport, api, config, currentMemberId: "m1" });
    expect(result.created).toBe(2);
    const childReq = api.createIssue.mock.calls.find((c: any[]) => c[0].title === "Child")[0];
    expect(childReq.parent_issue_id).toBe("new-Parent");
  });

  it("adds only Jira comments newer than the high-water mark", async () => {
    const api = fakeApi([]);
    const transport = vi.fn(async ({ path }: any) =>
      path.includes("/search")
        ? searchResponse([
            {
              key: "PROJ-1",
              fields: {
                summary: "Parent",
                description: null,
                duedate: null,
                updated: "2026-06-30T10:00:00.000+0000",
                status: { name: "To Do" },
                priority: null,
                subtasks: [],
                comment: {
                  comments: [
                    { id: "c1", created: "2026-06-30T08:00:00.000+0000", body: "old" },
                    { id: "c2", created: "2026-06-30T11:00:00.000+0000", body: "new" },
                  ],
                },
              },
            },
          ])
        : {},
    );

    const result = await syncJiraIssues({ transport, api, config, currentMemberId: "m1" });
    // On a freshly-created issue the high-water mark = issue.updated (10:00),
    // so only c2 (11:00) is newer.
    expect(api.createComment).toHaveBeenCalledTimes(1);
    expect(api.createComment.mock.calls[0][1]).toContain("new");
    expect(result.commentsAdded).toBe(1);
  });
});

describe("clearSyncedJiraIssues", () => {
  it("deletes only issues marked with the jira source", async () => {
    const api: any = {
      listIssues: vi.fn().mockResolvedValue({
        issues: [
          { id: "i1", metadata: { source: "jira", jira_key: "PROJ-1" } },
          { id: "i2", metadata: { source: "jira", jira_key: "PROJ-2" } },
        ],
        total: 2,
      }),
      batchDeleteIssues: vi.fn().mockResolvedValue({ deleted: 2 }),
    };

    const result = await clearSyncedJiraIssues(api);

    expect(api.listIssues).toHaveBeenCalledWith({
      metadata: { source: "jira" },
      limit: 1000,
    });
    expect(api.batchDeleteIssues).toHaveBeenCalledWith(["i1", "i2"]);
    expect(result.deleted).toBe(2);
  });

  it("does not call batch delete when there is nothing synced", async () => {
    const api: any = {
      listIssues: vi.fn().mockResolvedValue({ issues: [], total: 0 }),
      batchDeleteIssues: vi.fn(),
    };

    const result = await clearSyncedJiraIssues(api);

    expect(api.batchDeleteIssues).not.toHaveBeenCalled();
    expect(result.deleted).toBe(0);
  });
});
