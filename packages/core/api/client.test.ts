import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError, CHAT_DRAFT_RESTORE_CAPABILITY } from "./client";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ApiClient server Table query", () => {
  it("posts the canonical query to the group and branch endpoints", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            query_fingerprint: "sha256:query",
            total: 1001,
            groups: [
              {
                key: "status:todo",
                value: { kind: "status", status: "todo" },
                count: 1001,
              },
            ],
            next_cursor: null,
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            query_fingerprint: "sha256:query",
            group_key: "status:todo",
            parent_id: null,
            total: 0,
            rows: [],
            branch_total: 0,
            next_cursor: "next-page",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            query_fingerprint: "sha256:query",
            total: 1001,
            facets: [
              {
                kind: "status",
                values: [
                  { key: "todo", count: 501 },
                  { key: "done", count: 500 },
                ],
              },
            ],
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const query = {
      scope: { kind: "workspace" as const },
      filters: { priorities: ["high" as const] },
      sort: { field: "title" as const, direction: "asc" as const },
    };

    await expect(
      client.listIssueTableGroups({
        query,
        group: { kind: "status" },
        page: { limit: 100, cursor: null },
      }),
    ).resolves.toMatchObject({ total: 1001, groups: [{ count: 1001 }] });
    await expect(
      client.listIssueTableRows({
        query,
        group: { kind: "status" },
        group_key: "status:todo",
        hierarchy: { enabled: true },
        parent_id: null,
        page: { limit: 50, cursor: null },
      }),
    ).resolves.toMatchObject({ branch_total: 0, next_cursor: "next-page" });
    await expect(
      client.listIssueTableFacets({
        query,
        facets: [{ kind: "status" }],
      }),
    ).resolves.toMatchObject({
      total: 1001,
      facets: [{ values: [{ key: "todo", count: 501 }, { key: "done", count: 500 }] }],
    });

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "https://api.example.test/api/issues/table/groups",
      "https://api.example.test/api/issues/table/rows",
      "https://api.example.test/api/issues/table/facets",
    ]);
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({
      method: "POST",
      body: expect.stringContaining('"kind":"status"'),
    });
  });

  it("falls back safely when Table responses are malformed", async () => {
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ total: "not-a-number" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const query = {
      scope: { kind: "workspace" as const },
      filters: {},
      sort: { field: "position" as const, direction: "asc" as const },
    };

    await expect(
      client.listIssueTableGroups({
        query,
        group: { kind: "status" },
        page: { limit: 100, cursor: null },
      }),
    ).resolves.toEqual({
      query_fingerprint: "",
      total: 0,
      groups: [],
      next_cursor: null,
    });
    await expect(
      client.listIssueTableRows({
        query,
        group: { kind: "none" },
        group_key: null,
        hierarchy: { enabled: true },
        parent_id: null,
        page: { limit: 50, cursor: null },
      }),
    ).resolves.toEqual({
      query_fingerprint: "",
      group_key: null,
      parent_id: null,
      total: 0,
      rows: [],
      branch_total: 0,
      next_cursor: null,
    });
    await expect(
      client.listIssueTableFacets({
        query,
        facets: [{ kind: "status" }],
      }),
    ).resolves.toEqual({
      query_fingerprint: "",
      total: 0,
      facets: [],
    });
  });

  it("preserves future Table status and actor enum values", async () => {
    const responses = [
      {
        query_fingerprint: "sha256:future-status",
        total: 1,
        groups: [
          {
            key: "status:paused",
            value: { kind: "status", status: "paused" },
            count: 1,
          },
        ],
        next_cursor: null,
      },
      {
        query_fingerprint: "sha256:future-actor",
        total: 1,
        groups: [
          {
            key: "service:bot-1",
            value: {
              kind: "assignee",
              actor: { type: "service", id: "bot-1" },
            },
            count: 1,
          },
        ],
        next_cursor: null,
      },
    ];
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify(responses.shift()), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const query = {
      scope: { kind: "workspace" as const },
      filters: {},
      sort: { field: "position" as const, direction: "asc" as const },
    };

    await expect(
      client.listIssueTableGroups({ query, group: { kind: "status" } }),
    ).resolves.toMatchObject({
      total: 1,
      groups: [{ value: { kind: "status", status: "paused" } }],
    });
    await expect(
      client.listIssueTableGroups({ query, group: { kind: "assignee" } }),
    ).resolves.toMatchObject({
      total: 1,
      groups: [
        { value: { kind: "assignee", actor: { type: "service", id: "bot-1" } } },
      ],
    });
  });

  it("parses compound lane descriptors and posts the additive union", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          query_fingerprint: "sha256:compound",
          total: 2,
          groups: [
            {
              key: "parent:parent-1",
              value: {
                kind: "parent",
                parent_id: "parent-1",
                parent: {
                  id: "parent-1",
                  number: 10,
                  identifier: "MUL-10",
                  title: "Parent",
                  status: "todo",
                },
                value_state: "value",
              },
              count: 2,
              secondary_groups: [
                {
                  key: "compound:opaque:status:todo",
                  value: { kind: "status", status: "todo" },
                  count: 2,
                },
              ],
            },
          ],
          next_cursor: null,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    const query = {
      scope: { kind: "workspace" as const },
      filters: {},
      sort: { field: "position" as const, direction: "asc" as const },
    };

    await expect(
      client.listIssueTableGroups({
        query,
        group: {
          kind: "compound",
          primary: "parent",
          secondary: "status",
          secondary_values: ["todo"],
        },
      }),
    ).resolves.toMatchObject({
      groups: [
        {
          value: { kind: "parent", parent: { title: "Parent" } },
          secondary_groups: [
            { value: { kind: "status", status: "todo" }, count: 2 },
          ],
        },
      ],
    });
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({
      body: expect.stringContaining(
        '"kind":"compound","primary":"parent","secondary":"status","secondary_values":["todo"]',
      ),
    });
  });
});

describe("ApiClient issue move intent", () => {
  it("posts relative anchors without a client-authored position", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: "issue-1", position: 15 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await client.moveIssue("issue-1", {
      status: "in_progress",
      before_id: "issue-0",
      after_id: "issue-2",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.example.test/api/issues/issue-1/move",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          status: "in_progress",
          before_id: "issue-0",
          after_id: "issue-2",
        }),
      }),
    );
  });
});

describe("ApiClient label response schemas", () => {
  it("falls back safely for malformed label catalog, label, and resource responses", async () => {
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ labels: "not-an-array", total: "not-a-number" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");

    await expect(client.listLabels("agent")).resolves.toEqual({ labels: [], total: 0 });
    await expect(client.getLabel("label-1")).resolves.toMatchObject({ id: "" });
    await expect(
      client.createLabel({ resource_type: "agent", name: "Ops", color: "#3b82f6" }),
    ).resolves.toMatchObject({ id: "" });
    await expect(
      client.updateLabel("label-1", { name: "Operations" }),
    ).resolves.toMatchObject({ id: "" });

    await expect(client.listLabelsForIssue("issue-1")).resolves.toEqual({ labels: [] });
    await expect(client.attachLabel("issue-1", "label-1")).resolves.toEqual({ labels: [] });
    await expect(client.detachLabel("issue-1", "label-1")).resolves.toEqual({ labels: [] });

    await expect(client.listLabelsForResource("agent", "agent-1")).resolves.toEqual({ labels: [] });
    await expect(
      client.attachLabelToResource("agent", "agent-1", "label-1"),
    ).resolves.toEqual({ labels: [] });
    await expect(
      client.detachLabelFromResource("agent", "agent-1", "label-1"),
    ).resolves.toEqual({ labels: [] });

    expect(fetchMock).toHaveBeenCalledTimes(10);
  });
});

describe("ApiClient agent builder runtime switch", () => {
  it("PATCHes the session runtime endpoint and returns the runtime the server bound", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ runtime_id: "runtime-b" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await expect(
      client.switchAgentBuilderRuntime("session-1", { runtime_id: "runtime-b" }),
    ).resolves.toEqual({ runtime_id: "runtime-b" });

    const call = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(call[0]).toContain("/api/agent-builder/sessions/session-1/runtime");
    expect(call[1].method).toBe("PATCH");
    expect(JSON.parse(String(call[1].body))).toEqual({ runtime_id: "runtime-b" });
  });

  it("falls back to the requested runtime id for a malformed success body", async () => {
    // A 2xx means the rebind committed onto the runtime we asked for, so the
    // fallback must say so. Reporting "unknown" here would leave the picker on
    // the old runtime while the conversation executes on the new one — the very
    // split this endpoint exists to close.
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ runtime_id: 42 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await expect(
      client.switchAgentBuilderRuntime("session-1", { runtime_id: "runtime-b" }),
    ).resolves.toEqual({ runtime_id: "runtime-b" });
  });

  it("rejects without a fallback when the switch is refused", async () => {
    // 409 (a reply in flight) means nothing was committed, so the caller must
    // see a rejection and keep the old runtime selected.
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: "stop the current reply before switching runtime" }), {
        status: 409,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await expect(
      client.switchAgentBuilderRuntime("session-1", { runtime_id: "runtime-b" }),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("ApiClient notification preferences", () => {
  it("sends atomic preference updates with PATCH", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          workspace_id: "workspace-1",
          preferences: {
            status_changes: "muted",
            comments: "muted",
          },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await expect(
      client.updateNotificationPreferences(
        { comments: "muted" },
        "workspace-one",
      ),
    ).resolves.toEqual({
      workspace_id: "workspace-1",
      preferences: {
        status_changes: "muted",
        comments: "muted",
      },
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      "https://api.example.test/api/notification-preferences",
    );
    expect(fetchMock.mock.calls[0]?.[1]).toEqual(
      expect.objectContaining({
        method: "PATCH",
        headers: expect.objectContaining({
          "X-Workspace-Slug": "workspace-one",
        }),
        body: JSON.stringify({ preferences: { comments: "muted" } }),
      }),
    );
  });

  it("falls back safely when a preference response is malformed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({ workspace_id: "workspace-1", preferences: [] }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    const client = new ApiClient("https://api.example.test");
    await expect(client.getNotificationPreferences()).resolves.toEqual({
      workspace_id: "",
      preferences: {},
    });
  });
});

describe("ApiClient", () => {
  it("preserves HTTP status on failed requests", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "workspace slug already exists" }), {
          status: 409,
          statusText: "Conflict",
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const client = new ApiClient("https://api.example.test");

    try {
      await client.createWorkspace({ name: "Test", slug: "test" });
      throw new Error("expected createWorkspace to fail");
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError);
      expect(error).toMatchObject({
        message: "workspace slug already exists",
        status: 409,
        statusText: "Conflict",
      });
    }
  });

  it("preserves planned and delivered comment coverage from issue task runs", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify([
            {
              id: "task-1",
              status: "queued",
              trigger_comment_id: "comment-3",
              coalesced_comment_ids: ["comment-1", "comment-2"],
              delivered_comment_ids: ["comment-1", "comment-2", "comment-3"],
            },
          ]),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    const client = new ApiClient("https://api.example.test");
    const tasks = await client.listTasksByIssue("issue-1");

    expect(tasks[0]?.trigger_comment_id).toBe("comment-3");
    expect(tasks[0]?.coalesced_comment_ids).toEqual([
      "comment-1",
      "comment-2",
    ]);
    expect(tasks[0]?.delivered_comment_ids).toEqual([
      "comment-1",
      "comment-2",
      "comment-3",
    ]);
  });

  it("keeps task runs when optional comment coverage is malformed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify([
            {
              id: "task-1",
              status: "queued",
              coalesced_comment_ids: ["comment-1", 2],
              delivered_comment_ids: "not-an-array",
            },
            {
              id: "task-2",
              status: "completed",
              delivered_comment_ids: ["comment-2", "comment-3"],
            },
          ]),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    const client = new ApiClient("https://api.example.test");
    const tasks = await client.listTasksByIssue("issue-1");

    expect(tasks).toHaveLength(2);
    expect(tasks[0]?.coalesced_comment_ids).toBeUndefined();
    expect(tasks[0]?.delivered_comment_ids).toBeUndefined();
    expect(tasks[1]?.delivered_comment_ids).toEqual([
      "comment-2",
      "comment-3",
    ]);
  });

  it("uses the expected HTTP contract for autopilot endpoints", async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(
      new Response(JSON.stringify({ autopilots: [], runs: [], total: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ));
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");

    await client.listAutopilots({ status: "active" });
    await client.getAutopilot("ap-1");
    await client.createAutopilot({
      title: "Daily triage",
      project_id: "project-1",
      assignee_id: "agent-1",
      execution_mode: "create_issue",
    });
    await client.updateAutopilot("ap-1", { status: "paused", project_id: null });
    await client.deleteAutopilot("ap-1");
    await client.triggerAutopilot("ap-1");
    await client.listAutopilotRuns("ap-1", { limit: 10, offset: 20 });
    await client.createAutopilotTrigger("ap-1", {
      kind: "schedule",
      cron_expression: "0 9 * * *",
      timezone: "UTC",
    });
    await client.updateAutopilotTrigger("ap-1", "tr-1", { enabled: false });
    await client.deleteAutopilotTrigger("ap-1", "tr-1");
    await client.rotateAutopilotTriggerWebhookToken("ap-1", "tr-1");

    const calls = fetchMock.mock.calls.map(([url, init]) => ({
      url,
      method: init?.method ?? "GET",
      body: init?.body,
    }));

    expect(calls).toMatchObject([
      { url: "https://api.example.test/api/autopilots?status=active", method: "GET" },
      { url: "https://api.example.test/api/autopilots/ap-1", method: "GET" },
      {
        url: "https://api.example.test/api/autopilots",
        method: "POST",
        body: JSON.stringify({
          title: "Daily triage",
          project_id: "project-1",
          assignee_id: "agent-1",
          execution_mode: "create_issue",
        }),
      },
      {
        url: "https://api.example.test/api/autopilots/ap-1",
        method: "PATCH",
        body: JSON.stringify({ status: "paused", project_id: null }),
      },
      { url: "https://api.example.test/api/autopilots/ap-1", method: "DELETE" },
      { url: "https://api.example.test/api/autopilots/ap-1/trigger", method: "POST" },
      { url: "https://api.example.test/api/autopilots/ap-1/runs?limit=10&offset=20", method: "GET" },
      {
        url: "https://api.example.test/api/autopilots/ap-1/triggers",
        method: "POST",
        body: JSON.stringify({
          kind: "schedule",
          cron_expression: "0 9 * * *",
          timezone: "UTC",
        }),
      },
      {
        url: "https://api.example.test/api/autopilots/ap-1/triggers/tr-1",
        method: "PATCH",
        body: JSON.stringify({ enabled: false }),
      },
      { url: "https://api.example.test/api/autopilots/ap-1/triggers/tr-1", method: "DELETE" },
      {
        url: "https://api.example.test/api/autopilots/ap-1/triggers/tr-1/rotate-webhook-token",
        method: "POST",
      },
    ]);
  });

  it("emits X-Client-* headers when identity is configured", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test", {
      identity: { platform: "desktop", version: "1.2.3", os: "macos" },
    });
    await client.listWorkspaces();

    const headers = fetchMock.mock.calls[0]![1]!.headers as Record<string, string>;
    expect(headers["X-Client-Platform"]).toBe("desktop");
    expect(headers["X-Client-Version"]).toBe("1.2.3");
    expect(headers["X-Client-OS"]).toBe("macos");
  });

  it("omits X-Client-* headers when identity is not configured", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify([]), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.listWorkspaces();

    const headers = fetchMock.mock.calls[0]![1]!.headers as Record<string, string>;
    expect(headers["X-Client-Platform"]).toBeUndefined();
    expect(headers["X-Client-Version"]).toBeUndefined();
    expect(headers["X-Client-OS"]).toBeUndefined();
  });

  it("posts feedback kind and parses the response through the schema", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ id: "feedback-1", created_at: "2026-06-26T00:00:00Z" }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const response = await client.createFeedback({
      message: "Desktop route crashed",
      url: "app://desktop/acme/issues",
      workspace_id: "ws-1",
      kind: "bug",
    });

    expect(response).toEqual({
      id: "feedback-1",
      created_at: "2026-06-26T00:00:00Z",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.example.test/api/feedback",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          message: "Desktop route crashed",
          url: "app://desktop/acme/issues",
          workspace_id: "ws-1",
          kind: "bug",
        }),
      }),
    );
  });

  it("falls back to an empty feedback response when the server shape drifts", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ id: 42, created_at: "2026-06-26T00:00:00Z" }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const client = new ApiClient("https://api.example.test");
    await expect(client.createFeedback({ message: "hello" })).resolves.toEqual({
      id: "",
      created_at: "",
    });
  });

  it("uses the expected HTTP contract for comment trigger preview and suppress", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ agents: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({
          id: "comment-1",
          issue_id: "issue-1",
          author_type: "member",
          author_id: "user-1",
          content: "hello",
          type: "comment",
          parent_id: null,
          reactions: [],
          attachments: [],
          created_at: "2026-06-05T00:00:00Z",
          updated_at: "2026-06-05T00:00:00Z",
        }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({
          id: "comment-1",
          issue_id: "issue-1",
          author_type: "member",
          author_id: "user-1",
          content: "updated",
          type: "comment",
          parent_id: null,
          reactions: [],
          attachments: [],
          created_at: "2026-06-05T00:00:00Z",
          updated_at: "2026-06-05T00:01:00Z",
        }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.previewCommentTriggers("issue-1", "hello", "parent-1", "comment-1");
    await client.createComment(
      "issue-1",
      "hello",
      "comment",
      "parent-1",
      ["attachment-1"],
      ["agent-1"],
    );
    await client.updateComment("comment-1", "updated", ["attachment-1"], ["agent-1"]);

    expect(fetchMock.mock.calls.map(([url, init]) => ({
      url,
      method: init?.method,
      body: init?.body,
    }))).toMatchObject([
      {
        url: "https://api.example.test/api/issues/issue-1/comments/trigger-preview",
        method: "POST",
        body: JSON.stringify({ content: "hello", parent_id: "parent-1", editing_comment_id: "comment-1" }),
      },
      {
        url: "https://api.example.test/api/issues/issue-1/comments",
        method: "POST",
        body: JSON.stringify({
          content: "hello",
          type: "comment",
          parent_id: "parent-1",
          attachment_ids: ["attachment-1"],
          suppress_agent_ids: ["agent-1"],
        }),
      },
      {
        url: "https://api.example.test/api/comments/comment-1",
        method: "PUT",
        body: JSON.stringify({
          content: "updated",
          attachment_ids: ["attachment-1"],
          suppress_agent_ids: ["agent-1"],
        }),
      },
    ]);
  });

  it("uses the Cloud Runtime node API contract", async () => {
    const node = {
      id: "node-1",
      owner_id: "user-1",
      instance_id: "i-0123456789abcdef0",
      region: "us-west-2",
      instance_type: "g5.xlarge",
      image_id: "ami-1",
      subnet_id: "subnet-1",
      name: "gpu-dev-01",
      status: "launching",
      tags: {},
      metadata: {},
      created_at: "2026-05-21T08:30:00Z",
      updated_at: "2026-05-21T08:30:00Z",
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify([]), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify(node), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.listCloudRuntimeNodes({ limit: 20, offset: 5 });
    await client.createCloudRuntimeNode(
      { instance_type: "g5.xlarge", name: "gpu-dev-01" },
    );

    const listCall = fetchMock.mock.calls[0]!;
    const createCall = fetchMock.mock.calls[1]!;
    expect(listCall[0]).toBe(
      "https://api.example.test/api/cloud-runtime/nodes?limit=20&offset=5",
    );
    expect(createCall[0]).toBe(
      "https://api.example.test/api/cloud-runtime/nodes",
    );
    expect(createCall[1]).toMatchObject({
      method: "POST",
      body: JSON.stringify({
        instance_type: "g5.xlarge",
        name: "gpu-dev-01",
      }),
    });
  });

  it("falls back when Cloud Runtime node responses drift", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify([{ id: 123 }]), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ id: 123 }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");

    await expect(client.listCloudRuntimeNodes()).resolves.toEqual([]);
    await expect(
      client.createCloudRuntimeNode({ instance_type: "g5.xlarge" }),
    ).resolves.toMatchObject({ id: "", status: "" });
  });

  it("deleteCloudRuntimeNode sends DELETE with JSON body containing instance id", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(
      new Response(null, { status: 204 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.deleteCloudRuntimeNode("i-0123456789abcdef0");

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, opts] = fetchMock.mock.calls[0]!;
    expect(url).toBe("https://api.example.test/api/cloud-runtime/nodes");
    expect(opts).toMatchObject({
      method: "DELETE",
      body: JSON.stringify({ instance_id: "i-0123456789abcdef0" }),
    });
    expect((opts.headers as Record<string, string>)["Content-Type"]).toBe(
      "application/json",
    );
  });

  describe("getAttachment", () => {
    it("returns the parsed attachment for a well-formed response", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(
            JSON.stringify({
              id: "att-1",
              workspace_id: "ws-1",
              issue_id: null,
              comment_id: null,
              uploader_type: "member",
              uploader_id: "u-1",
              filename: "report.md",
              url: "https://static.example.test/ws/att-1.md",
              download_url:
                "https://static.example.test/ws/att-1.md?Policy=p&Signature=s&Key-Pair-Id=k",
              content_type: "text/markdown",
              size_bytes: 123,
              created_at: "2026-05-11T00:00:00Z",
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const att = await client.getAttachment("att-1");

      expect(att.id).toBe("att-1");
      expect(att.download_url).toContain("Policy=");
    });

    it("falls back to an empty attachment when the response is missing download_url", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify({ id: "att-1" }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const att = await client.getAttachment("att-1");

      // parseWithFallback returns the EMPTY_ATTACHMENT record so callers can
      // safely read `download_url` without crashing — they'll see "" and
      // surface a user-facing error instead of opening `undefined`.
      expect(att.id).toBe("");
      expect(att.download_url).toBe("");
    });
  });

  describe("getAttachmentTextContent", () => {
    it("returns body text and the original content type from the X-* header", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response("# heading\n\nbody\n", {
            status: 200,
            headers: {
              "Content-Type": "text/plain; charset=utf-8",
              "X-Original-Content-Type": "text/markdown",
            },
          }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const { text, originalContentType } =
        await client.getAttachmentTextContent("att-1");

      expect(text).toBe("# heading\n\nbody\n");
      expect(originalContentType).toBe("text/markdown");
    });

    it("throws PreviewTooLargeError on 413", async () => {
      const { PreviewTooLargeError } = await import("./client");
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response("", { status: 413, statusText: "Payload Too Large" }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      await expect(client.getAttachmentTextContent("att-1")).rejects.toBeInstanceOf(
        PreviewTooLargeError,
      );
    });

    it("throws PreviewUnsupportedError on 415", async () => {
      const { PreviewUnsupportedError } = await import("./client");
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response("", { status: 415, statusText: "Unsupported Media Type" }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      await expect(client.getAttachmentTextContent("att-1")).rejects.toBeInstanceOf(
        PreviewUnsupportedError,
      );
    });
  });

  describe("listChatMessagesPage deployment-order fallback", () => {
    const jsonResponse = (body: unknown, status: number, statusText = "") =>
      new Response(JSON.stringify(body), {
        status,
        statusText,
        headers: { "Content-Type": "application/json" },
      });

    it("falls back to the legacy full-list endpoint when the paged route 404s", async () => {
      const legacy = [
        { id: "m1", role: "user", content: "hi", created_at: "2026-06-01T00:00:00Z" },
        { id: "m2", role: "assistant", content: "yo", created_at: "2026-06-01T00:00:01Z" },
      ];
      const fetchMock = vi
        .fn()
        .mockResolvedValueOnce(jsonResponse({ error: "not found" }, 404, "Not Found"))
        .mockResolvedValueOnce(jsonResponse(legacy, 200));
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const page = await client.listChatMessagesPage("session-1", { limit: 50 });

      expect(fetchMock).toHaveBeenCalledTimes(2);
      expect(fetchMock.mock.calls[0]![0]).toBe(
        "https://api.example.test/api/chat/sessions/session-1/messages/page?limit=50",
      );
      expect(fetchMock.mock.calls[1]![0]).toBe(
        "https://api.example.test/api/chat/sessions/session-1/messages",
      );
      expect(page).toEqual({ messages: legacy, limit: 50, has_more: false, next_cursor: null });
    });

    it("does NOT fall back on a cursor request — a 404 there propagates", async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValue(jsonResponse({ error: "not found" }, 404, "Not Found"));
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      await expect(
        client.listChatMessagesPage("session-1", {
          before: { created_at: "2026-06-01T00:00:00Z", id: "m1" },
        }),
      ).rejects.toBeInstanceOf(ApiError);
      // Only the paged request fires; no legacy full-list call that would duplicate messages.
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });

    it("propagates non-404 errors instead of masking them with the legacy list", async () => {
      const fetchMock = vi
        .fn()
        .mockResolvedValue(jsonResponse({ error: "boom" }, 500, "Internal Server Error"));
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      await expect(client.listChatMessagesPage("session-1")).rejects.toMatchObject({
        status: 500,
      });
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });
  });

  describe("cancelTaskById response parsing", () => {
    const taskResponse = {
      id: "task-1",
      agent_id: "agent-1",
      runtime_id: "runtime-1",
      issue_id: "",
      status: "cancelled",
      priority: 0,
      dispatched_at: null,
      started_at: null,
      completed_at: "2026-06-12T06:40:00Z",
      result: null,
      error: null,
      created_at: "2026-06-12T06:39:00Z",
    };

    it("parses the cancelled chat message payload", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({
          ...taskResponse,
          cancelled_chat_message: {
            chat_session_id: "session-1",
            message_id: "message-1",
            content: "restore me",
            restore_to_input: true,
          },
        }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const result = await client.cancelTaskById("task-1");

      expect(fetchMock.mock.calls[0]).toMatchObject([
        "https://api.example.test/api/tasks/task-1/cancel",
        { method: "POST" },
      ]);
      expect(result.cancelled_chat_message).toEqual({
        chat_session_id: "session-1",
        message_id: "message-1",
        content: "restore me",
        restore_to_input: true,
      });
    });

    it("parses task attribution when the backend enriches it", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify({
            ...taskResponse,
            attribution: {
              source: "direct_human",
              precise: true,
              initiator: { id: "user-1", name: "Ada", avatar_url: "https://x/a.png" },
              originator: { id: "user-1", name: "Ada" },
              evidence: { kind: "comment", ref_id: "comment-1" },
            },
          }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const result = await client.cancelTaskById("task-1");

      expect(result.attribution).toEqual({
        source: "direct_human",
        precise: true,
        initiator: { id: "user-1", name: "Ada", avatar_url: "https://x/a.png" },
        originator: { id: "user-1", name: "Ada" },
        evidence: { kind: "comment", ref_id: "comment-1" },
      });
    });

    it("leaves attribution absent on servers that predate it", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify(taskResponse), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const result = await client.cancelTaskById("task-1");

      expect(result.attribution).toBeUndefined();
    });

    // The server only defers the empty-transcript judgment — and so only
    // withholds the synchronous restore — for clients that advertise this
    // capability (#5219). Drop the header and this client is treated as a
    // pre-#5219 build, quietly losing the deferred path it actually implements.
    it("advertises the durable draft-restore capability", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify(taskResponse), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
      vi.stubGlobal("fetch", fetchMock);

      await new ApiClient("https://api.example.test").cancelTaskById("task-1");

      const init = fetchMock.mock.calls[0]?.[1] as { headers: Record<string, string> };
      expect(init.headers["X-Client-Capabilities"]).toBe(CHAT_DRAFT_RESTORE_CAPABILITY);
    });

    it("treats a null cancelled chat message as absent", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify({
            ...taskResponse,
            cancelled_chat_message: null,
          }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const result = await client.cancelTaskById("task-1");

      expect(result.id).toBe("task-1");
      expect(result.cancelled_chat_message).toBeUndefined();
    });

    it.each([
      ["a missing task id", { ...taskResponse, id: undefined }],
      [
        "a malformed cancelled chat message",
        {
          ...taskResponse,
          cancelled_chat_message: {
            chat_session_id: "session-1",
            message_id: "message-1",
            content: "restore me",
            restore_to_input: "true",
          },
        },
      ],
      ["a null body", null],
    ])("falls back for %s", async (_label, body) => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify(body), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );

      const client = new ApiClient("https://api.example.test");
      const result = await client.cancelTaskById("task-1");

      expect(result.id).toBe("");
      expect(result.cancelled_chat_message).toBeUndefined();
    });
  });

  describe("chat attachment wiring", () => {
    it("uploadFile includes chat_session_id in the FormData body", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ id: "att-1", url: "https://cdn/x" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const file = new File(["hi"], "hi.png", { type: "image/png" });
      await client.uploadFile(file, { chatSessionId: "session-123" });

      expect(fetchMock).toHaveBeenCalledTimes(1);
      const [url, init] = fetchMock.mock.calls[0]!;
      expect(url).toBe("https://api.example.test/api/upload-file");
      expect(init?.method).toBe("POST");
      const body = init?.body as FormData;
      expect(body).toBeInstanceOf(FormData);
      expect(body.get("chat_session_id")).toBe("session-123");
      expect(body.get("issue_id")).toBeNull();
      expect(body.get("comment_id")).toBeNull();
    });

    it("sendChatMessage serialises attachment_ids onto the JSON body when present", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ message_id: "m1", task_id: "t1", created_at: "" }), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        }),
      );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      await client.sendChatMessage("session-1", "hello", ["att-1", "att-2"]);

      const [, init] = fetchMock.mock.calls[0]!;
      expect(JSON.parse(init?.body as string)).toEqual({
        content: "hello",
        attachment_ids: ["att-1", "att-2"],
      });
    });

    it("sendChatMessage omits attachment_ids when the list is empty or undefined", async () => {
      const fetchMock = vi.fn().mockImplementation(() =>
        Promise.resolve(
          new Response(JSON.stringify({ message_id: "m1", task_id: "t1", created_at: "" }), {
            status: 201,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      await client.sendChatMessage("session-1", "hello");
      await client.sendChatMessage("session-1", "again", []);

      expect(JSON.parse(fetchMock.mock.calls[0]![1]?.body as string)).toEqual({ content: "hello" });
      expect(JSON.parse(fetchMock.mock.calls[1]![1]?.body as string)).toEqual({ content: "again" });
    });
  });
});
