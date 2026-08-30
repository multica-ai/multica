import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { setSchemaLogger } from "./schema";

const digest = `sha256:${"a".repeat(64)}`;
const agentId = "22222222-2222-4222-8222-222222222222";
const approvalId = "55555555-5555-4555-8555-555555555555";

function stubJSON(body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    ),
  );
}

function lastURL(): string {
  const call = vi.mocked(fetch).mock.calls.at(-1);
  return String(call?.[0]);
}

afterEach(() => {
  vi.unstubAllGlobals();
  setSchemaLogger({
    debug: () => undefined,
    info: () => undefined,
    warn: () => undefined,
    error: () => undefined,
  });
});

describe("ApiClient operational controls", () => {
  it("reads and revision-replaces one agent policy", async () => {
    const policy = {
      configured: true,
      revision: 1,
      status: "active",
      policy_digest: digest,
      default_effect: "deny",
      rules: [],
    };
    stubJSON(policy);
    const client = new ApiClient("https://api.example.test");

    await expect(client.getAgentToolPolicy(agentId)).resolves.toEqual(policy);
    expect(lastURL()).toBe(
      `https://api.example.test/api/agents/${agentId}/tool-policy`,
    );

    await expect(
      client.replaceAgentToolPolicy(agentId, {
        expected_revision: 1,
        rules: [],
      }),
    ).resolves.toEqual(policy);
    expect(lastURL()).toBe(
      `https://api.example.test/api/agents/${agentId}/tool-policy`,
    );
    expect(vi.mocked(fetch).mock.calls.at(-1)?.[1]).toMatchObject({
      method: "PUT",
    });
  });

  it("encodes bounded action filters without leaking them into another agent key", async () => {
    stubJSON({ items: [], next_cursor: "next" });
    const client = new ApiClient("https://api.example.test");

    await expect(
      client.listAgentToolActions(agentId, {
        event_type: "failed",
        since: "2026-08-29T12:00:00Z",
        cursor: "cursor/value",
        limit: 25,
      }),
    ).resolves.toEqual({ items: [], next_cursor: "next" });

    const url = new URL(lastURL());
    expect(url.pathname).toBe(`/api/agents/${agentId}/tool-actions`);
    expect(Object.fromEntries(url.searchParams)).toEqual({
      event_type: "failed",
      since: "2026-08-29T12:00:00Z",
      cursor: "cursor/value",
      limit: "25",
    });
  });

  it("lists and decides approvals through human-only workspace routes", async () => {
    stubJSON({ items: [] });
    const client = new ApiClient("https://api.example.test");

    await client.listAgentToolApprovals({
      agent_id: agentId,
      status: "pending",
      limit: 50,
    });
    const url = new URL(lastURL());
    expect(url.pathname).toBe("/api/approvals");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      agent_id: agentId,
      status: "pending",
      limit: "50",
    });

    stubJSON({
      id: approvalId,
      workspace_id: "11111111-1111-4111-8111-111111111111",
      agent_id: agentId,
      task_id: "33333333-3333-4333-8333-333333333333",
      invocation_id: "44444444-4444-4444-8444-444444444444",
      transport_kind: "managed_mcp",
      server_key: "linear",
      tool_name: "list_issues",
      schema_digest: digest,
      policy_revision: 1,
      schema_field_names: [],
      argument_bytes: 0,
      status: "approved",
      requested_at: "2026-08-29T12:00:00Z",
      expires_at: "2026-08-29T12:05:00Z",
      decided_at: "2026-08-29T12:01:00Z",
    });
    await client.getAgentToolApproval(approvalId);
    expect(new URL(lastURL()).pathname).toBe(
      `/api/approvals/${approvalId}`,
    );

    await client.decideAgentToolApproval(approvalId, {
      decision: "approve",
      reason_code: "operator_approved",
      expected_status: "pending",
    });
    expect(new URL(lastURL()).pathname).toBe(
      `/api/approvals/${approvalId}/decision`,
    );
    expect(vi.mocked(fetch).mock.calls.at(-1)?.[1]).toMatchObject({
      method: "POST",
      body: JSON.stringify({
        decision: "approve",
        reason_code: "operator_approved",
        expected_status: "pending",
      }),
    });
  });

  it("reads capability and operational summary contracts", async () => {
    const client = new ApiClient("https://api.example.test");
    stubJSON({ capabilities: [] });
    await expect(client.listOperationalCapabilities()).resolves.toEqual({
      capabilities: [],
    });
    expect(new URL(lastURL()).pathname).toBe(
      "/api/operational-controls/capabilities",
    );

    stubJSON({
      workspace_id: "11111111-1111-4111-8111-111111111111",
      days: 7,
      timezone: "America/New_York",
      generated_at: "2026-08-29T12:00:00Z",
      pending: 0,
      approved: 0,
      denied: 0,
      expired: 0,
      failed: 0,
      intercepted_invocation_count: 0,
      declaration_only_count: 0,
      median_decision_time_ms: null,
      configured_agent_capability_gaps: 0,
    });
    await client.getOperationalSummary({ days: 7, tz: "America/New_York" });
    const url = new URL(lastURL());
    expect(url.pathname).toBe("/api/dashboard/operations/summary");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      days: "7",
      tz: "America/New_York",
    });
  });

  it("fails closed on malformed protected responses with sanitized diagnostics", async () => {
    const warn = vi.fn();
    setSchemaLogger({
      debug: vi.fn(),
      info: vi.fn(),
      warn,
      error: vi.fn(),
    });
    const client = new ApiClient("https://api.example.test");
    const cases: Array<[string, () => Promise<unknown>]> = [
      [
        "GET /api/agents/:id/tool-policy",
        () => client.getAgentToolPolicy(agentId),
      ],
      [
        "GET /api/agents/:id/tool-actions",
        () => client.listAgentToolActions(agentId),
      ],
      ["GET /api/approvals", () => client.listAgentToolApprovals()],
      [
        "GET /api/operational-controls/capabilities",
        () => client.listOperationalCapabilities(),
      ],
      [
        "GET /api/dashboard/operations/summary",
        () => client.getOperationalSummary({ days: 7, tz: "UTC" }),
      ],
    ];

    for (const [endpoint, call] of cases) {
      stubJSON({
        protected_contract: "wrong",
        raw_value: "W3B_SECRET_CANARY",
      });
      const error = await call().catch((value) => value);
      expect(error).toBeInstanceOf(Error);
      expect(String(error)).not.toContain("W3B_SECRET_CANARY");
      expect(warn).toHaveBeenLastCalledWith(
        "API response failed protected schema validation",
        expect.objectContaining({ endpoint }),
      );
    }

    expect(JSON.stringify(warn.mock.calls)).not.toContain("W3B_SECRET_CANARY");
    expect(warn).toHaveBeenCalledTimes(cases.length);
  });
});
