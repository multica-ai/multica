// Sanity-level coverage for the tool registry. We're deliberately not
// testing every handler against a mock server (that's mostly testing
// the HTTP client which has its own suite); instead we assert the
// registry contract and that a few representative tools forward
// arguments to the right endpoint with the right shape.

import { describe, expect, it, vi } from "vitest";
import { allTools } from "../tools/index.js";
import type { MulticaClient } from "../client.js";
import type { ToolContext } from "../tool.js";
import {
  issueCreateTool,
  issueListTool,
  issueStatusTool,
} from "../tools/issues.js";
import { channelHistoryTool } from "../tools/channels.js";

function fakeClient(): { client: MulticaClient; calls: { method: string; path: string; body?: unknown; opts?: unknown }[] } {
  const calls: { method: string; path: string; body?: unknown; opts?: unknown }[] = [];
  const client = {
    apiUrl: "https://api.example",
    defaultWorkspaceId: "ws-1",
    get: vi.fn(async (path: string, opts?: unknown) => {
      calls.push({ method: "GET", path, opts });
      return { stub: "get" };
    }),
    post: vi.fn(async (path: string, body: unknown, opts?: unknown) => {
      calls.push({ method: "POST", path, body, opts });
      return { stub: "post" };
    }),
    patch: vi.fn(async (path: string, body: unknown, opts?: unknown) => {
      calls.push({ method: "PATCH", path, body, opts });
      return { stub: "patch" };
    }),
    delete: vi.fn(async (path: string, opts?: unknown) => {
      calls.push({ method: "DELETE", path, opts });
      return { stub: "delete" };
    }),
  } as unknown as MulticaClient;
  return { client, calls };
}

describe("tool registry", () => {
  it("has no duplicate tool names", () => {
    const seen = new Set<string>();
    for (const t of allTools) {
      expect(seen.has(t.name), `duplicate tool name: ${t.name}`).toBe(false);
      seen.add(t.name);
    }
  });

  it("every tool name uses the multica_ prefix", () => {
    for (const t of allTools) {
      expect(t.name.startsWith("multica_")).toBe(true);
    }
  });

  it("every tool has a non-empty description and an inputSchema", () => {
    for (const t of allTools) {
      expect(t.description.length).toBeGreaterThan(10);
      expect(t.inputSchema).toBeDefined();
    }
  });
});

describe("representative handlers", () => {
  it("issueListTool forwards filters as query params", async () => {
    const { client, calls } = fakeClient();
    const ctx: ToolContext = { client };
    await issueListTool.handler(
      { status: "todo", priority: "high", limit: 25 },
      ctx,
    );
    expect(calls).toEqual([
      {
        method: "GET",
        path: "/api/issues",
        opts: {
          query: {
            status: "todo",
            priority: "high",
            assignee: undefined,
            project: undefined,
            limit: 25,
            offset: undefined,
          },
        },
      },
    ]);
  });

  it("issueCreateTool POSTs the canonical create payload", async () => {
    const { client, calls } = fakeClient();
    const ctx: ToolContext = { client };
    await issueCreateTool.handler(
      { title: "Hello", description: "world" },
      ctx,
    );
    expect(calls).toHaveLength(1);
    expect(calls[0]!.method).toBe("POST");
    expect(calls[0]!.path).toBe("/api/issues");
    expect(calls[0]!.body).toEqual({
      title: "Hello",
      description: "world",
      status: "todo",
      priority: "none",
      assignee_type: null,
      assignee_id: null,
      parent_issue_id: null,
      project_id: null,
      due_date: null,
    });
  });

  it("issueStatusTool URL-encodes the id", async () => {
    const { client, calls } = fakeClient();
    const ctx: ToolContext = { client };
    await issueStatusTool.handler({ id: "MUL-123", status: "in_progress" }, ctx);
    expect(calls[0]!.method).toBe("PATCH");
    expect(calls[0]!.path).toBe("/api/issues/MUL-123");
    expect(calls[0]!.body).toEqual({ status: "in_progress" });
  });

  it("channelHistoryTool passes pagination params through", async () => {
    const { client, calls } = fakeClient();
    const ctx: ToolContext = { client };
    await channelHistoryTool.handler(
      {
        channel_id: "11111111-2222-3333-4444-555555555555",
        limit: 100,
        before: "2026-04-30T12:00:00Z",
        include_threaded: true,
      },
      ctx,
    );
    expect(calls[0]!.method).toBe("GET");
    expect(calls[0]!.path).toBe(
      "/api/channels/11111111-2222-3333-4444-555555555555/messages",
    );
    expect(calls[0]!.opts).toEqual({
      query: {
        limit: 100,
        before: "2026-04-30T12:00:00Z",
        include_threaded: true,
      },
    });
  });

  it("rejects malformed input via the inputSchema", () => {
    expect(() =>
      issueCreateTool.inputSchema.parse({ title: "" }),
    ).toThrow();
    expect(() =>
      channelHistoryTool.inputSchema.parse({ channel_id: "not-a-uuid" }),
    ).toThrow();
  });
});
