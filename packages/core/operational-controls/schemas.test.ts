import { describe, expect, it } from "vitest";
import {
  AgentToolActionEventSchema,
  AgentToolActionListResponseSchema,
  AgentToolApprovalSchema,
  AgentToolApprovalListResponseSchema,
  AgentToolPolicySchema,
  OperationalCapabilityListResponseSchema,
  OperationalControlsChangedPayloadSchema,
  OperationalSummarySchema,
  ReplaceAgentToolPolicyRequestSchema,
} from "./schemas";

const digest = `sha256:${"a".repeat(64)}`;
const ids = {
  workspace: "11111111-1111-4111-8111-111111111111",
  agent: "22222222-2222-4222-8222-222222222222",
  task: "33333333-3333-4333-8333-333333333333",
  invocation: "44444444-4444-4444-8444-444444444444",
  approval: "55555555-5555-4555-8555-555555555555",
  event: "66666666-6666-4666-8666-666666666666",
};

const rule = {
  transport_kind: "managed_mcp",
  server_key: "linear",
  tool_name: "list_issues",
  schema_digest: digest,
  effect: "require_approval",
} as const;

describe("operational control schemas", () => {
  it("accepts a configured exact-match policy and an explicit unconfigured policy", () => {
    expect(
      AgentToolPolicySchema.parse({
        configured: true,
        revision: 3,
        status: "active",
        policy_digest: digest,
        default_effect: "deny",
        rules: [rule],
      }),
    ).toMatchObject({ configured: true, revision: 3, rules: [rule] });

    expect(
      AgentToolPolicySchema.parse({ configured: false, rules: [] }),
    ).toEqual({ configured: false, rules: [] });
  });

  it("rejects policy identities that are not exact metadata", () => {
    expect(() =>
      ReplaceAgentToolPolicyRequestSchema.parse({
        expected_revision: 0,
        rules: [{ ...rule, tool_name: "*" }],
      }),
    ).toThrow();
    expect(() =>
      ReplaceAgentToolPolicyRequestSchema.parse({
        expected_revision: -1,
        rules: [rule],
      }),
    ).toThrow();
    expect(() =>
      ReplaceAgentToolPolicyRequestSchema.parse({
        expected_revision: 0,
        rules: [{ ...rule, schema_digest: "sha256:not-a-digest" }],
      }),
    ).toThrow();
  });

  it("accepts metadata-only action events and refuses raw value fields", () => {
    const event = {
      id: ids.event,
      workspace_id: ids.workspace,
      agent_id: ids.agent,
      task_id: ids.task,
      invocation_id: ids.invocation,
      approval_request_id: ids.approval,
      transport_kind: "managed_mcp",
      server_key: "linear",
      tool_name: "list_issues",
      schema_digest: digest,
      coverage_kind: "managed_mcp",
      event_type: "succeeded",
      argument_bytes: 42,
      result_bytes: 84,
      duration_ms: 12,
      outcome_code: "succeeded",
      created_at: "2026-08-29T12:00:00Z",
    };

    expect(AgentToolActionEventSchema.parse(event)).toEqual(event);
    expect(
      AgentToolActionListResponseSchema.parse({ items: [event] }),
    ).toMatchObject({ items: [event] });
    expect(() =>
      AgentToolActionEventSchema.parse({
        ...event,
        arguments: { access_token: "do-not-log" },
      }),
    ).toThrow();
    expect(() =>
      AgentToolActionEventSchema.parse({
        ...event,
        provider_body: "do-not-log",
      }),
    ).toThrow();
  });

  it("models the complete approval state machine without raw arguments or notes", () => {
    const approval = {
      id: ids.approval,
      workspace_id: ids.workspace,
      agent_id: ids.agent,
      task_id: ids.task,
      invocation_id: ids.invocation,
      transport_kind: "managed_mcp",
      server_key: "linear",
      tool_name: "list_issues",
      schema_digest: digest,
      policy_revision: 3,
      schema_field_names: ["team_id"],
      argument_bytes: 42,
      status: "pending",
      requested_at: "2026-08-29T12:00:00Z",
      expires_at: "2026-08-29T12:05:00Z",
    };

    for (const status of [
      "pending",
      "approved",
      "consumed",
      "denied",
      "expired",
      "cancelled",
    ]) {
      expect(
        AgentToolApprovalSchema.parse({ ...approval, status }).status,
      ).toBe(status);
    }
    expect(
      AgentToolApprovalListResponseSchema.parse({ items: [approval] }).items,
    ).toHaveLength(1);
    expect(() =>
      AgentToolApprovalSchema.parse({
        ...approval,
        arguments: { team_id: "secret-customer-value" },
      }),
    ).toThrow();
    expect(() =>
      AgentToolApprovalSchema.parse({ ...approval, note: "raw summary" }),
    ).toThrow();
  });

  it("requires transport and provider-family scoping for capabilities", () => {
    const response = {
      capabilities: [
        {
          name: "managed_mcp.tool_invocation",
          transport_kind: "managed_mcp",
          provider_family: "openai_compatible",
          supported: true,
        },
      ],
    };
    expect(OperationalCapabilityListResponseSchema.parse(response)).toEqual(
      response,
    );
    expect(() =>
      OperationalCapabilityListResponseSchema.parse({
        capabilities: [{ name: "tool_invocation", supported: true }],
      }),
    ).toThrow();
    expect(() =>
      OperationalCapabilityListResponseSchema.parse({
        capabilities: [
          {
            ...response.capabilities[0],
            supported: false,
            offline_reason: "Bearer W3B_SECRET_CANARY",
          },
        ],
      }),
    ).toThrow();
  });

  it("accepts bounded operational aggregates and rejects raw detail fields", () => {
    const summary = {
      workspace_id: ids.workspace,
      days: 7,
      timezone: "America/New_York",
      generated_at: "2026-08-29T12:00:00Z",
      pending: 1,
      approved: 2,
      denied: 1,
      expired: 0,
      failed: 1,
      intercepted_invocation_count: 10,
      declaration_only_count: 2,
      median_decision_time_ms: 1500,
      configured_agent_capability_gaps: 3,
    };

    expect(OperationalSummarySchema.parse(summary)).toEqual(summary);
    expect(() =>
      OperationalSummarySchema.parse({
        ...summary,
        raw_results: ["secret"],
      }),
    ).toThrow();
  });

  it("validates the workspace-only realtime payload", () => {
    expect(
      OperationalControlsChangedPayloadSchema.parse({
        workspace_id: ids.workspace,
      }),
    ).toEqual({ workspace_id: ids.workspace });
    expect(() =>
      OperationalControlsChangedPayloadSchema.parse({
        workspace_id: ids.workspace,
        agent_id: ids.agent,
      }),
    ).toThrow();
    expect(() =>
      OperationalControlsChangedPayloadSchema.parse({ workspace_id: "bad" }),
    ).toThrow();
  });
});
