import { afterEach, describe, expect, it, vi } from "vitest";
import { createAuthStore } from "../auth";
import { configStore } from "../config";
import type { StorageAdapter, User } from "../types";
import { ApiClient, ApiError, CHAT_DRAFT_RESTORE_CAPABILITY, clientErrorMessage } from "./client";
import { EMPTY_PLUGIN_PACKAGE_LIST, EMPTY_PLUGIN_PREVIEW, EMPTY_PLUGIN_SURFACE_LAUNCH } from "./schemas";

afterEach(() => {
  configStore.getState().setAgentConversationStartersSupported(false);
  vi.unstubAllGlobals();
});

describe("ApiClient agent conversation-starter compatibility", () => {
  const prompt = {
    label: "Review a PR",
    prompt: "Review the open pull request.",
  };

  it("rejects create writes before an older backend can drop them", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await expect(
      client.createAgent({
        name: "Reviewer",
        runtime_id: "runtime-1",
        conversation_starters: [prompt],
      }),
    ).rejects.toThrow(/server version does not support agent conversation starters/i);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects update writes before an older backend can drop them", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await expect(
      client.updateAgent("agent-1", { conversation_starters: [prompt] }),
    ).rejects.toThrow(/server version does not support agent conversation starters/i);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("allows declared-capability create and update writes through", async () => {
    configStore.getState().setAgentConversationStartersSupported(true);
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({ id: "agent-1" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await client.createAgent({
      name: "Reviewer",
      runtime_id: "runtime-1",
      conversation_starters: [prompt],
    });
    await client.updateAgent("agent-1", {
      conversation_starters: [prompt],
    });

    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      name: "Reviewer",
      runtime_id: "runtime-1",
      conversation_starters: [prompt],
    });
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
      conversation_starters: [prompt],
    });
  });
});

describe("ApiClient edit guards", () => {
  it("serializes field baselines for issue and comment writes", async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(
      new Response("{}", {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await client.updateIssue("issue-1", { title: "Latest", title_base: "Original" });
    await client.updateComment("comment-1", "Latest", [], undefined, "Original");

    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toMatchObject({
      title: "Latest",
      title_base: "Original",
    });
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toMatchObject({
      content: "Latest",
      content_base: "Original",
    });
  });

  it("accepts an older issue response without revision and rejects a malformed revision", async () => {
    const legacyIssue = {
      id: "issue-1",
      workspace_id: "ws-1",
      number: 1,
      identifier: "MUL-1",
      title: "Legacy issue",
      description: null,
      status: "todo",
      priority: "none",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 0,
      start_date: null,
      due_date: null,
      created_at: "2026-08-16T00:00:00Z",
      updated_at: "2026-08-16T00:00:00Z",
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ issues: [legacyIssue], total: 1 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        issues: [{ ...legacyIssue, revision: "invalid" }],
        total: 1,
      }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    const parsedLegacy = await client.listIssues();
    expect(parsedLegacy).toMatchObject({ issues: [{ id: "issue-1" }], total: 1 });
    expect(parsedLegacy.issues[0]).not.toHaveProperty("revision");
    await expect(client.listIssues()).resolves.toEqual({ issues: [], total: 0 });
  });
});

describe("ApiClient pull-request response schema", () => {
  const validPR = {
    id: "pr-1",
    provider: "github",
    workspace_id: "ws-1",
    repo_owner: "acme",
    repo_name: "widget",
    number: 7,
    title: "MUL-1: fix",
    state: "open",
    html_url: "https://github.example/acme/widget/pull/7",
    branch: "fix/mul-1",
    author_login: "octocat",
    author_avatar_url: null,
    merged_at: null,
    closed_at: null,
    pr_created_at: "2026-01-01T00:00:00Z",
    pr_updated_at: "2026-01-01T00:00:00Z",
    snapshot_available: true,
    checks_rollup: "failure",
    failed_check_names: ["backend"],
  };

  it("parses and defaults a valid pull-request list", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ pull_requests: [validPR] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const result = await new ApiClient("https://api.example.test").listIssuePullRequests("issue-1");
    expect(result.pull_requests[0]).toMatchObject({
      id: "pr-1",
      failed_check_names: ["backend"],
      checks_total: 0,
    });
  });

  it("falls back safely when failed_check_names is malformed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            pull_requests: [{ ...validPR, failed_check_names: "backend" }],
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        ),
      ),
    );

    await expect(
      new ApiClient("https://api.example.test").listIssuePullRequests("issue-1"),
    ).resolves.toEqual({ pull_requests: [] });
  });
});

describe("ApiClient Plugin preview response schema", () => {
  it("degrades a malformed preview so a blank scope list is never shown as approval", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ scopes: "issues:read" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(new ApiClient("https://api.example.test").previewPlugin(
      "workspace-1",
      { version_id: "version-1" },
    )).resolves.toEqual(EMPTY_PLUGIN_PREVIEW);
  });

  // A malformed launch must not become a partly trusted frame URL.
  it("falls back to an empty surface launch when the response is malformed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ url: 42, bridge_token: "proof" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(new ApiClient("https://api.example.test").getPluginSurfaceLaunch(
      "workspace-1",
      "installation-1",
      "hello",
    )).resolves.toEqual(EMPTY_PLUGIN_SURFACE_LAUNCH);
  });

  it("falls back to an empty package list when the response is malformed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ packages: "nope" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(new ApiClient("https://api.example.test").listPluginPackages("workspace-1"))
      .resolves.toEqual(EMPTY_PLUGIN_PACKAGE_LIST);
  });
});

describe("ApiClient Plugin surface bridge routes", () => {
  it("relays Action API calls through the session-only bridge prefix", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await new ApiClient("https://api.example.test").callPluginAction(
      "installation-1",
      { method: "GET", path: "/context", issueId: "MUL-42" },
    );

    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      "https://api.example.test/api/plugin-bridge/v1/context?issue_id=MUL-42",
    );
    expect(fetchMock.mock.calls[0]?.[1]).toMatchObject({
      method: "GET",
      headers: expect.objectContaining({
        "X-Multica-Plugin-Installation": "installation-1",
      }),
    });
  });

  it("invokes UI hooks through the session-only bridge prefix", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({
        status: "ok",
        hook_key: "summarize",
        trigger: "ui",
        latency_ms: 1,
        attempts: 1,
      }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    vi.stubGlobal("fetch", fetchMock);

    await new ApiClient("https://api.example.test").invokePluginHook(
      "installation-1",
      "summarize",
      { trigger: "ui", issueId: "issue-1" },
    );

    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      "https://api.example.test/api/plugin-bridge/v1/hooks/summarize",
    );
  });
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

describe("ApiClient workspace working agents", () => {
  it("supports an optional source-type filter", async () => {
    const payload = [
      {
        id: "agent-1",
        name: "Agent 1",
        avatar_url: null,
        running_task_count: 2,
        issue_ids: ["issue-1"],
      },
    ];
    const fetchMock = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify(payload), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await expect(
      client.getWorkspaceWorkingAgents("issue", "assigned"),
    ).resolves.toEqual(payload);
    await expect(
      client.getWorkspaceWorkingAgents("issue"),
    ).resolves.toEqual(payload);
    await expect(client.getWorkspaceWorkingAgents()).resolves.toEqual(payload);
    await expect(
      client.getWorkspaceWorkingAgents("issue", undefined, "parent-1"),
    ).resolves.toEqual(payload);
    // The server rejects parent alongside scope, so a My Issues relation wins
    // and the parent is dropped rather than sent into a 400.
    await expect(
      client.getWorkspaceWorkingAgents("issue", "assigned", "parent-1"),
    ).resolves.toEqual(payload);
    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "https://api.example.test/api/working-agents?type=issue&scope=mine&relation=assigned",
      "https://api.example.test/api/working-agents?type=issue",
      "https://api.example.test/api/working-agents",
      "https://api.example.test/api/working-agents?type=issue&parent=parent-1",
      "https://api.example.test/api/working-agents?type=issue&scope=mine&relation=assigned",
    ]);
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

describe("ApiClient Inbox response schemas", () => {
  const legacyRow = {
    id: "inbox-1",
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "member-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Legacy Inbox row",
    body: null,
    read: false,
    archived: false,
    created_at: "2026-08-24T00:00:00Z",
  };

  it("schema-parses the main Inbox and preserves omitted legacy projections", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify([legacyRow]), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const result = await new ApiClient("https://api.example.test").listInbox();

    expect(result).toHaveLength(1);
    expect(result[0]).not.toHaveProperty("issue_status");
    expect(result[0]).not.toHaveProperty("issue_priority");
  });

  it("falls back safely when the main Inbox projection is wrong-typed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify([{ ...legacyRow, issue_priority: 3 }]),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        ),
      ),
    );

    await expect(
      new ApiClient("https://api.example.test").listInbox(),
    ).resolves.toEqual([]);
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

  it("parses per-run token usage on task runs", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify([
            {
              id: "task-1",
              status: "completed",
              usage: [
                {
                  provider: "anthropic",
                  model: "claude-opus-5",
                  input_tokens: 96_000,
                  output_tokens: 34_000,
                  cache_read_tokens: 712_000,
                  cache_write_tokens: 50_000,
                  cost_usd_ticks: 19_990_000_000,
                },
              ],
            },
            // No usage at all — a run from before usage reporting.
            { id: "task-2", status: "completed" },
          ]),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    const client = new ApiClient("https://api.example.test");
    const tasks = await client.listTasksByIssue("issue-1");

    expect(tasks[0]?.usage).toHaveLength(1);
    expect(tasks[0]?.usage?.[0]).toMatchObject({
      model: "claude-opus-5",
      input_tokens: 96_000,
      cache_read_tokens: 712_000,
      cost_usd_ticks: 19_990_000_000,
    });
    // Absent, not [] — "we have no figure" must stay distinguishable from
    // "the figure is zero" all the way to the UI.
    expect(tasks[1]?.usage).toBeUndefined();
  });

  it("keeps task runs when per-run usage is malformed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify([
            // Usage is not an array at all.
            { id: "task-1", status: "completed", usage: "1.2M" },
            // Usage is an array, but an entry has a string where a count belongs.
            {
              id: "task-2",
              status: "completed",
              usage: [{ model: "claude-opus-5", input_tokens: "many" }],
            },
            {
              id: "task-3",
              status: "completed",
              usage: [{ model: "gpt-5.6-terra", input_tokens: 31_000 }],
            },
          ]),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    const client = new ApiClient("https://api.example.test");
    const tasks = await client.listTasksByIssue("issue-1");

    // A bad usage payload costs that row its figure and nothing else — the
    // execution log still lists every run.
    expect(tasks).toHaveLength(3);
    expect(tasks[0]?.usage).toBeUndefined();
    expect(tasks[1]?.usage).toBeUndefined();
    expect(tasks[2]?.usage?.[0]?.input_tokens).toBe(31_000);
    expect(tasks[2]?.usage?.[0]?.output_tokens).toBe(0);
  });

  it("keeps agent detail task history on the lightweight endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify([
          { id: "task-1", status: "completed", created_at: "2026-08-27T03:00:00Z" },
          { id: "task-2", status: "completed", created_at: "2026-08-27T02:00:00Z" },
        ]),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    const tasks = await client.listAgentTasks("agent-1");

    expect(tasks.map((task) => task.id)).toEqual(["task-1", "task-2"]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      "https://api.example.test/api/agents/agent-1/tasks",
    );
  });

  it("falls back to an empty agent task history for a malformed response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ tasks: "not-an-array" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const client = new ApiClient("https://api.example.test");
    await expect(client.listAgentTasks("agent-1")).resolves.toEqual([]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
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
    await client.getAutopilotQuotaUsage();
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
      idempotencyKey: (init?.headers as Record<string, string> | undefined)?.["Idempotency-Key"],
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
      {
        url: "https://api.example.test/api/autopilots/ap-1/trigger",
        method: "POST",
        idempotencyKey: expect.any(String),
      },
      { url: "https://api.example.test/api/autopilots/usage", method: "GET" },
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
      context: {
        kind: "desktop_route_error",
        trigger: "route-errorElement",
        error: {
          name: "TypeError",
          message: "Cannot read properties of undefined",
          stack: "TypeError: Cannot read properties of undefined",
        },
      },
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
          context: {
            kind: "desktop_route_error",
            trigger: "route-errorElement",
            error: {
              name: "TypeError",
              message: "Cannot read properties of undefined",
              stack: "TypeError: Cannot read properties of undefined",
            },
          },
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
        {
          id: "m1",
          chat_session_id: "session-1",
          role: "user",
          content: "hi",
          task_id: null,
          created_at: "2026-06-01T00:00:00Z",
          quick_actions: [],
        },
        {
          id: "m2",
          chat_session_id: "session-1",
          role: "assistant",
          content: "yo",
          task_id: "task-1",
          created_at: "2026-06-01T00:00:01Z",
          quick_actions: [
            { label: "Continue", prompt: "Continue with the next step", primary: true },
          ],
        },
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

    it("keeps a valid reply when its optional quick actions are malformed", async () => {
      const malformed = [{
        id: "m1",
        chat_session_id: "session-1",
        role: "assistant",
        content: "safe reply",
        task_id: "task-1",
        created_at: "2026-06-01T00:00:00Z",
        quick_actions: [{ label: 42, prompt: false }],
      }];
      vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(malformed, 200)));

      const client = new ApiClient("https://api.example.test");
      await expect(client.listChatMessages("session-1")).resolves.toEqual([
        expect.objectContaining({ content: "safe reply", quick_actions: [] }),
      ]);
    });

    it("falls back to an empty page for a malformed paged response", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(jsonResponse({ messages: "broken" }, 200)),
      );

      const client = new ApiClient("https://api.example.test");
      await expect(
        client.listChatMessagesPage("session-1", { limit: 25 }),
      ).resolves.toEqual({ messages: [], limit: 25, has_more: false, next_cursor: null });
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

    it("scopes queued edit cancellation to the expected chat session", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify(taskResponse), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
      vi.stubGlobal("fetch", fetchMock);

      await new ApiClient("https://api.example.test").cancelTaskById("task-1", {
        queuedAction: "edit",
        sessionId: "session-1",
      });

      expect(fetchMock.mock.calls[0]?.[0]).toBe(
        "https://api.example.test/api/tasks/task-1/cancel" +
          "?expected_status=queued&chat_session_id=session-1&queue_action=edit",
      );
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

  it("clears a chat queue with one session-scoped request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);

    await new ApiClient("https://api.example.test").clearQueuedChatTasks("session-1");

    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.example.test/api/chat/sessions/session-1/queued-tasks",
      expect.objectContaining({ method: "DELETE" }),
    );
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

    it("threads an AbortSignal into fetch so the coordinator can cancel it (MUL-5181)", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ id: "att-1", url: "https://cdn/x" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const controller = new AbortController();
      const file = new File(["hi"], "hi.png", { type: "image/png" });
      await client.uploadFile(file, { issueId: "issue-1" }, controller.signal);

      const [, init] = fetchMock.mock.calls[0]!;
      expect(init?.signal).toBe(controller.signal);
    });

    it("rejects with the fetch AbortError when the signal is already aborted", async () => {
      const fetchMock = vi.fn().mockImplementation((_url, init?: RequestInit) => {
        if (init?.signal?.aborted) {
          const err = new Error("The operation was aborted");
          err.name = "AbortError";
          return Promise.reject(err);
        }
        return Promise.resolve(new Response("{}", { status: 200 }));
      });
      vi.stubGlobal("fetch", fetchMock);

      const client = new ApiClient("https://api.example.test");
      const controller = new AbortController();
      controller.abort();
      const file = new File(["hi"], "hi.png", { type: "image/png" });

      await expect(
        client.uploadFile(file, undefined, controller.signal),
      ).rejects.toMatchObject({ name: "AbortError" });
    });

    it("sendChatMessage serialises attachment_ids onto the JSON body when present", async () => {
      const fetchMock = vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ message_id: "m1", task_id: "t1", created_at: "2026-08-01T00:00:00Z" }), {
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
          new Response(JSON.stringify({ message_id: "m1", task_id: "t1", created_at: "2026-08-01T00:00:00Z" }), {
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

    it("sendChatMessage accepts the server's null attachment_ids for text-only sends", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify({
            message_id: "m1",
            task_id: "t1",
            created_at: "2026-08-01T00:00:00Z",
            attachment_ids: null,
          }), {
            status: 201,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );

      await expect(
        new ApiClient("https://api.example.test").sendChatMessage("session-1", "hello"),
      ).resolves.toMatchObject({ attachment_ids: undefined });
    });

    it("sendChatMessage rejects a malformed response", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue(
          new Response(JSON.stringify({ message_id: "m1", task_id: 42 }), {
            status: 201,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      );

      await expect(
        new ApiClient("https://api.example.test").sendChatMessage("session-1", "hello"),
      ).rejects.toThrow();
    });
  });
});

// The onboarding flow acts on a workspace the app has not navigated to, so
// these calls pass the slug explicitly. The server reads X-Workspace-Slug
// before ?workspace_id, so the header — not the param — is what has to carry
// the target workspace.
describe("ApiClient explicit workspace targeting", () => {
  function stubOk(body: unknown) {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  function slugHeaderOf(fetchMock: ReturnType<typeof vi.fn>): unknown {
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    return (init.headers as Record<string, string>)["X-Workspace-Slug"];
  }

  it("sends the given slug on Mika creation", async () => {
    const fetchMock = stubOk({ id: "agent-1" });
    await new ApiClient("https://api.example.test").createMikaAgent(
      { runtime_id: "runtime-1", language: "en" },
      "proxima-centauri",
    );
    expect(slugHeaderOf(fetchMock)).toBe("proxima-centauri");
  });

  it("sends the given slug when listing another workspace's runtimes", async () => {
    const fetchMock = stubOk([]);
    await new ApiClient("https://api.example.test").listRuntimes(
      { workspace_id: "ws-2", owner: "me" },
      "proxima-centauri",
    );
    expect(slugHeaderOf(fetchMock)).toBe("proxima-centauri");
  });

  it("omits the header when no slug is given, leaving the ambient one", async () => {
    const fetchMock = stubOk([]);
    await new ApiClient("https://api.example.test").listRuntimes({
      workspace_id: "ws-2",
    });
    expect(slugHeaderOf(fetchMock)).toBeUndefined();
  });
});

describe("ApiClient model discovery response schema", () => {
  const completed = {
    id: "req-1",
    runtime_id: "rt-1",
    status: "completed",
    supported: true,
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T00:00:01Z",
    models: [{ id: "claude-sonnet-4-6", label: "Claude Sonnet 4.6" }],
  };

  function stubJSON(body: unknown) {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
  }

  it("parses a live completed discovery", async () => {
    stubJSON(completed);

    const result = await new ApiClient("https://api.example.test")
      .initiateListModels("rt-1");

    expect(result).toMatchObject({
      status: "completed",
      supported: true,
      models: [{ id: "claude-sonnet-4-6" }],
    });
  });

  it("keeps the cache markers on a server-cached snapshot", async () => {
    stubJSON({ ...completed, cached: true, cached_at: "2026-07-29T00:00:00Z" });

    const result = await new ApiClient("https://api.example.test")
      .initiateListModels("rt-1");

    expect(result.cached).toBe(true);
    expect(result.cached_at).toBe("2026-07-29T00:00:00Z");
  });

  it("requests a live model list when force refresh is selected", async () => {
    stubJSON(completed);

    await new ApiClient("https://api.example.test").initiateListModels("rt-1", {
      force: true,
    });

    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe(
      "https://api.example.test/api/runtimes/rt-1/models?force=true",
    );
    expect(vi.mocked(fetch).mock.calls[0]?.[1]).toMatchObject({ method: "POST" });
  });

  // The picker drives a state machine off `status`, so a malformed body must
  // become an explicit failure — not a fabricated empty catalog, and not an
  // endless "discovering models" spinner.
  it("degrades a malformed initiate response to an explicit failure", async () => {
    stubJSON({ status: 7, models: "nope" });

    const result = await new ApiClient("https://api.example.test")
      .initiateListModels("rt-1");

    expect(result.status).toBe("failed");
    expect(result.supported).toBe(true);
    expect(result.error).toBe("invalid model discovery response");
    expect(result.runtime_id).toBe("rt-1");
  });

  it("degrades a malformed poll response to an explicit failure", async () => {
    stubJSON("not-an-object");

    const result = await new ApiClient("https://api.example.test")
      .getListModelsResult("rt-1", "req-9");

    expect(result.status).toBe("failed");
    expect(result.id).toBe("req-9");
    expect(result.runtime_id).toBe("rt-1");
  });

  it("stays usable against a backend that omits supported", async () => {
    const { supported: _omitted, ...withoutSupported } = completed;
    stubJSON(withoutSupported);

    const result = await new ApiClient("https://api.example.test")
      .getListModelsResult("rt-1", "req-1");

    expect(result.supported).toBe(true);
    expect(result.status).toBe("completed");
  });
});

/**
 * Mixed-version contract for subtree unsubscribe (MUL-5483).
 *
 * Web/desktop staging deploys on merge while the backend is deployed by hand,
 * so this client routinely runs against an older server. Subtree unsubscribe
 * must therefore be carried by its own PATH, never by a body field: Go's JSON
 * decoder drops unknown fields, so an old server would unsubscribe only the
 * root and still answer 200 — telling the user the whole tree was muted while
 * every child kept notifying. An unknown path 404s, which surfaces as a
 * rejected mutation the user can act on.
 */
describe("ApiClient unsubscribe endpoints", () => {
  function stubOK() {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  function requestOf(fetchMock: ReturnType<typeof vi.fn>) {
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    return { url, body: JSON.parse(String(init.body ?? "{}")) as Record<string, unknown> };
  }

  it("sends the subtree variant to its own endpoint", async () => {
    const fetchMock = stubOK();

    await new ApiClient("https://api.example.test")
      .unsubscribeFromIssueSubtree("issue-1", "user-1", "member");

    const { url, body } = requestOf(fetchMock);
    expect(url).toBe("https://api.example.test/api/issues/issue-1/unsubscribe/subtree");
    expect(body).toEqual({ user_id: "user-1", user_type: "member" });
  });

  it("never encodes subtree as a body field on the shared endpoint", async () => {
    const fetchMock = stubOK();

    await new ApiClient("https://api.example.test")
      .unsubscribeFromIssue("issue-1", "user-1", "member");

    const { url, body } = requestOf(fetchMock);
    expect(url).toBe("https://api.example.test/api/issues/issue-1/unsubscribe");
    // A `subtree` key here would be silently ignored by an older backend,
    // which is the exact silent-success failure this split exists to prevent.
    expect(body).not.toHaveProperty("subtree");
  });

  it("rejects when the backend does not know the subtree route", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "not found" }), {
          status: 404,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(
      new ApiClient("https://api.example.test")
        .unsubscribeFromIssueSubtree("issue-1", "user-1", "member"),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("ApiClient startMikaOnboarding", () => {
  it("returns the opening a well-formed response reports", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            started: true,
            message_id: "message-1",
            created_at: "2026-01-01T00:00:00Z",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    await expect(
      new ApiClient("https://api.example.test").startMikaOnboarding("session-1", {
        language: "en",
      }),
    ).resolves.toEqual({
      started: true,
      message_id: "message-1",
      created_at: "2026-01-01T00:00:00Z",
    });
  });

  it("falls back to started=false when the response is malformed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ started: "yes" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    // started=false is the safe reading: the flow treats it as "someone else
    // already opened this conversation" and navigates, rather than acting on a
    // body it could not understand.
    await expect(
      new ApiClient("https://api.example.test").startMikaOnboarding("session-1", {
        language: "en",
      }),
    ).resolves.toEqual({ started: false });
  });

  it("tolerates a backend that omits the optional opening fields", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ started: false }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(
      new ApiClient("https://api.example.test").startMikaOnboarding("session-1", {
        language: "en",
      }),
    ).resolves.toEqual({ started: false });
  });
});

describe("ApiClient refreshSkill response schema", () => {
  const validSkill = {
    id: "skill-1",
    workspace_id: "ws-1",
    name: "review-helper",
    description: "refreshed",
    content: "# refreshed",
    config: { origin: { type: "github", source_url: "https://github.com/acme/skills" } },
    created_by: "user-1",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
    files: [
      {
        id: "file-1",
        skill_id: "skill-1",
        path: "ref.md",
        content: "ref",
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-02T00:00:00Z",
      },
    ],
  };

  it("parses a valid refreshed skill", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify(validSkill), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const result = await new ApiClient("https://api.example.test").refreshSkill("skill-1");
    expect(result).toMatchObject({
      id: "skill-1",
      name: "review-helper",
      files: [{ path: "ref.md" }],
    });
  });

  it("falls back to the empty skill when the response is malformed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ skill: 42 }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const result = await new ApiClient("https://api.example.test").refreshSkill("skill-1");
    expect(result).toMatchObject({ id: "", name: "", files: [] });
  });

  it("defaults optional fields the server may omit", async () => {
    const { description, content, files, ...minimal } = validSkill;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify(minimal), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    const result = await new ApiClient("https://api.example.test").refreshSkill("skill-1");
    expect(result).toMatchObject({
      id: "skill-1",
      description: "",
      content: "",
      files: [],
    });
  });
});

describe("ApiClient workspace MCP servers", () => {
  function stubJSON(body: unknown) {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    return fetchMock;
  }

  const server = {
    id: "srv-1",
    workspace_id: "ws-1",
    name: "linear",
    transport: "http",
    created_at: "2026-08-14T00:00:00Z",
    updated_at: "2026-08-14T00:00:00Z",
  };

  it("parses the workspace library", async () => {
    stubJSON([server]);

    const result = await new ApiClient("https://api.example.test")
      .listWorkspaceMcpServers("ws-1");

    expect(result).toHaveLength(1);
    expect(result[0]).toMatchObject({ id: "srv-1", name: "linear", transport: "http" });
    // No binding in the library listing, so no toggle to report.
    expect(result[0]?.enabled).toBeUndefined();
  });

  it("falls back safely when the library response is malformed", async () => {
    stubJSON({ nope: true });

    const result = await new ApiClient("https://api.example.test")
      .listWorkspaceMcpServers("ws-1");

    expect(result).toEqual([]);
  });

  // The write-only boundary is enforced on the client too: a server that
  // regressed to returning the stored entry must not have it land in the
  // parsed object or the query cache.
  it("strips secret-bearing fields the server should never have sent", async () => {
    stubJSON([{
      ...server,
      config: { headers: { Authorization: "Bearer sk-live-doc" } },
      url: "https://mcp.example/sk-live-url",
      headers: { Authorization: "Bearer sk-live-header" },
    }]);

    const result = await new ApiClient("https://api.example.test")
      .listWorkspaceMcpServers("ws-1");

    expect(JSON.stringify(result)).not.toContain("sk-live");
    expect(result[0]).toEqual(server);
  });

  it("keeps an unknown transport rather than dropping the server", async () => {
    // Enum drift from a newer backend must degrade, not disappear: the row
    // still renders and the UI's default branch labels it.
    stubJSON([{ ...server, transport: "websocket" }]);

    const result = await new ApiClient("https://api.example.test")
      .listWorkspaceMcpServers("ws-1");

    expect(result).toHaveLength(1);
    expect(result[0]?.transport).toBe("websocket");
  });

  it("POSTs a name and entry when creating a library server", async () => {
    const fetchMock = stubJSON(server);

    await new ApiClient("https://api.example.test")
      .createWorkspaceMcpServer("ws-1", "linear", { url: "https://mcp.example" });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/workspaces/ws-1/mcp-servers");
    expect(init.method).toBe("POST");
    expect(JSON.parse(String(init.body))).toEqual({
      name: "linear",
      config: { url: "https://mcp.example" },
    });
  });

  it("sends only what changed when updating a library server", async () => {
    const fetchMock = stubJSON(server);

    await new ApiClient("https://api.example.test")
      .updateWorkspaceMcpServer("ws-1", "srv-1", { name: "linear-v2" });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/workspaces/ws-1/mcp-servers/srv-1");
    expect(init.method).toBe("PUT");
    // A rename must not blank the stored entry.
    expect(JSON.parse(String(init.body))).toEqual({ name: "linear-v2" });
  });

  it("assigns and un-assigns a server on the agent routes", async () => {
    const assigned = [{ ...server, enabled: true }];
    let fetchMock = stubJSON(assigned);

    const added = await new ApiClient("https://api.example.test")
      .addAgentMcpServer("agent-1", "srv-1");
    expect(added[0]?.enabled).toBe(true);
    let [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/agents/agent-1/mcp-servers");
    expect(init.method).toBe("POST");
    expect(JSON.parse(String(init.body))).toEqual({ server_id: "srv-1" });

    fetchMock = stubJSON([{ ...server, enabled: false }]);
    const toggled = await new ApiClient("https://api.example.test")
      .setAgentMcpServerEnabled("agent-1", "srv-1", false);
    expect(toggled[0]?.enabled).toBe(false);
    [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/agents/agent-1/mcp-servers/srv-1/enabled");
    expect(init.method).toBe("PUT");

    fetchMock = stubJSON([]);
    await new ApiClient("https://api.example.test").removeAgentMcpServer("agent-1", "srv-1");
    [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain("/api/agents/agent-1/mcp-servers/srv-1");
    expect(init.method).toBe("DELETE");
  });
});

describe("importSkillArchive", () => {
  it("POSTs multipart form data without a JSON content-type", async () => {
    const skill = {
      id: "skill-1",
      workspace_id: "ws-1",
      name: "review-helper",
      description: "Reviews code",
      content: "# Review",
      config: {},
      files: [],
      created_by: "user-1",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ status: "created", skill }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const file = new File(["pk"], "review-helper.skill", { type: "application/zip" });
    const result = await new ApiClient("https://api.example.test").importSkillArchive(
      file,
      "fail",
    );

    expect(result.id).toBe("skill-1");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe("https://api.example.test/api/skills/import");
    expect(init?.method).toBe("POST");
    const headers = init?.headers as Record<string, string>;
    expect(headers["Content-Type"]).toBeUndefined();
    const body = init?.body as FormData;
    expect(body).toBeInstanceOf(FormData);
    expect(body.get("on_conflict")).toBe("fail");
    const uploaded = body.get("file");
    expect(uploaded).toBeInstanceOf(File);
    expect((uploaded as File).name).toBe("review-helper.skill");
  });

  it("falls back to Import failed when the archive envelope is malformed", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ not_a_status: true, skill: 42 }), {
        status: 201,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const file = new File(["pk"], "review-helper.skill");
    await expect(
      new ApiClient("https://api.example.test").importSkillArchive(file),
    ).rejects.toThrow(/import failed/i);
  });

  it("keeps the server reason for a status this client does not know", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ status: "quarantined", reason: "pending review" }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const file = new File(["pk"], "review-helper.skill");
    await expect(
      new ApiClient("https://api.example.test").importSkillArchive(file),
    ).rejects.toThrow("pending review");
  });

  it("throws the structured reason on a name conflict", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          status: "conflict",
          reason: "a skill with this name already exists",
        }),
        { status: 409, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    const file = new File(["pk"], "review-helper.skill");
    await expect(
      new ApiClient("https://api.example.test").importSkillArchive(file),
    ).rejects.toMatchObject({
      message: "a skill with this name already exists",
      status: 409,
    });
  });
});

describe("clientErrorMessage", () => {
  it("returns a 4xx message, which handlers write for the user", () => {
    expect(clientErrorMessage(new ApiError("autopilot is not active", 400, "Bad Request")))
      .toBe("autopilot is not active");
    expect(clientErrorMessage(new ApiError("forbidden", 403, "Forbidden"))).toBe("forbidden");
  });

  it("withholds a 5xx message, which carries internal server detail", () => {
    // MUL-6472: the pre-fix body for a failed autopilot trigger looked like
    // this, and it was rendered verbatim in the run-now toast.
    const leaky = new ApiError(
      'failed to trigger autopilot: create run: ERROR: duplicate key value violates unique constraint "autopilot_run_pkey" (SQLSTATE 23505)',
      500,
      "Internal Server Error",
    );
    expect(clientErrorMessage(leaky)).toBeUndefined();
    expect(clientErrorMessage(new ApiError("internal error", 503, "Service Unavailable"))).toBeUndefined();
  });

  it("withholds a transport failure, whose message says nothing actionable", () => {
    expect(clientErrorMessage(new TypeError("Failed to fetch"))).toBeUndefined();
    expect(clientErrorMessage(undefined)).toBeUndefined();
  });
});

// The wiring this exercises is the one CoreProvider installs: the client's
// 401 hook drives the auth store's session teardown. Before MUL-7028 the hook
// only dropped the stored token, so the shell stayed mounted with a live
// `user` and every following request came back "missing authorization" with
// no way for the user to get to the login page.
describe("ApiClient session expiry", () => {
  function makeStorage(
    initial: Record<string, string> = {},
  ): StorageAdapter {
    const values = { ...initial };
    return {
      getItem: (key) => values[key] ?? null,
      setItem: (key, value) => {
        values[key] = value;
      },
      removeItem: (key) => {
        delete values[key];
      },
    };
  }

  it("ends the session when the server rejects the credential mid-flight", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "missing authorization" }), {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const storage = makeStorage({ multica_token: "live-token" });
    // The client is constructed before the store it notifies, exactly as
    // CoreProvider's initCore does; the hook only ever runs from a request.
    const session: { store?: ReturnType<typeof createAuthStore> } = {};
    const client = new ApiClient("https://api.example.test", {
      onUnauthorized: () => session.store?.getState().sessionExpired(),
    });
    const store = createAuthStore({ api: client, storage });
    session.store = store;
    store.setState({
      user: { id: "u1", email: "a@example.com" } as User,
      isLoading: false,
      status: "authenticated",
    });
    client.setToken("live-token");

    await expect(client.listProjects()).rejects.toBeInstanceOf(ApiError);

    expect(store.getState().user).toBeNull();
    expect(store.getState().status).toBe("unauthenticated");
    expect(store.getState().expired).toBe(true);
    expect(storage.getItem("multica_token")).toBeNull();
  });
});
