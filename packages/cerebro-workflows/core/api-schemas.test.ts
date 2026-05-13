import { describe, expect, it } from "vitest";
import { parseWithFallback } from "@multica/core/api";
import {
  EMPTY_REGENERATE_INBOUND_TOKEN,
  EMPTY_WORKFLOW,
  EMPTY_WORKFLOWS_LIST,
  EMPTY_WORKFLOW_RUNS_LIST,
  regenerateInboundTokenSchema,
  workflowRunsListSchema,
  workflowSchema,
  workflowsListSchema,
} from "./api-schemas";

// These tests pin the boundary defense CLAUDE.md "API Response Compatibility"
// mandates: every endpoint method must feed the response through a zod schema
// AND survive a malformed body without throwing into React. Each test below
// stands in for the kind of contract drift incidents the repo has hit before
// (#2143 / #2147 / #2192) — the workflow editor is the next surface that
// would white-screen on drift, so we lock in the safe-fallback behavior here.

describe("workflowSchema", () => {
  it("accepts a full phase-3 workflow row", () => {
    const ok = workflowSchema.safeParse({
      id: "wf-1",
      workspace_id: "ws-1",
      name: "WF",
      enabled: true,
      trigger_type: "webhook_inbound",
      trigger_config: {},
      conditions: [],
      action_type: "create_sub_issue",
      action_config: {},
      editor_mode: "form",
      created_by_id: "u-1",
      created_by_type: "member",
      created_at: "2026-05-13T08:00:00Z",
      updated_at: "2026-05-13T08:00:00Z",
      inbound_webhook_token: "abc",
      inbound_signing_secret_set: true,
      outbound_webhook_secret_set: false,
    });
    expect(ok.success).toBe(true);
    if (ok.success) {
      expect(ok.data.inbound_webhook_token).toBe("abc");
      expect(ok.data.inbound_signing_secret_set).toBe(true);
    }
  });

  it("defaults the phase-3 fields when the backend omits them", () => {
    // Mimics a server still on the phase-2 schema — no token, no _set bools.
    const ok = workflowSchema.safeParse({
      id: "wf-1",
      workspace_id: "ws-1",
      name: "WF",
      enabled: true,
      trigger_type: "status_changed",
      trigger_config: {},
      conditions: [],
      action_type: "set_status",
      action_config: {},
      editor_mode: "form",
      created_by_id: "u-1",
      created_by_type: "member",
      created_at: "2026-05-13T08:00:00Z",
      updated_at: "2026-05-13T08:00:00Z",
    });
    expect(ok.success).toBe(true);
    if (ok.success) {
      expect(ok.data.inbound_webhook_token).toBeUndefined();
      expect(ok.data.inbound_signing_secret_set).toBe(false);
      expect(ok.data.outbound_webhook_secret_set).toBe(false);
    }
  });

  it("passes a future trigger_type through unchanged (enum drift downgrades, not crashes)", () => {
    const ok = workflowSchema.safeParse({
      id: "wf-1",
      workspace_id: "ws-1",
      name: "WF",
      enabled: true,
      // Pretend the server added a new trigger in phase 4.
      trigger_type: "future_trigger_type",
      trigger_config: {},
      conditions: [],
      action_type: "set_status",
      action_config: {},
      editor_mode: "form",
      created_by_id: "u-1",
      created_by_type: "member",
      created_at: "2026-05-13T08:00:00Z",
      updated_at: "2026-05-13T08:00:00Z",
    });
    // Schema is lenient on enums by design — the UI's switch statements have
    // `default` branches that render a generic fallback.
    expect(ok.success).toBe(true);
  });
});

describe("workflowsListSchema (parseWithFallback wiring)", () => {
  it("returns the empty fallback on a null body", () => {
    const out = parseWithFallback(
      null,
      workflowsListSchema,
      EMPTY_WORKFLOWS_LIST,
      { endpoint: "listCerebroWorkflows" },
    );
    expect(out).toEqual({ workflows: [] });
  });

  it("returns the empty fallback when 'workflows' is the wrong type", () => {
    const out = parseWithFallback(
      { workflows: "definitely-not-an-array" },
      workflowsListSchema,
      EMPTY_WORKFLOWS_LIST,
      { endpoint: "listCerebroWorkflows" },
    );
    expect(out.workflows).toEqual([]);
  });
});

describe("workflowRunsListSchema (parseWithFallback wiring)", () => {
  it("defaults runs to an empty array when omitted", () => {
    const out = parseWithFallback(
      {},
      workflowRunsListSchema,
      EMPTY_WORKFLOW_RUNS_LIST,
      { endpoint: "listCerebroWorkflowRuns" },
    );
    expect(out.runs).toEqual([]);
  });
});

describe("regenerateInboundTokenSchema (parseWithFallback wiring)", () => {
  it("defaults missing fields to empty strings (one-time reveal still renders)", () => {
    const out = parseWithFallback(
      {},
      regenerateInboundTokenSchema,
      EMPTY_REGENERATE_INBOUND_TOKEN,
      { endpoint: "regenerateCerebroInboundToken" },
    );
    expect(out.inbound_webhook_token).toBe("");
    expect(out.inbound_webhook_url).toBe("");
  });

  it("returns the freshly-minted token + url on a happy response", () => {
    const out = parseWithFallback(
      {
        inbound_webhook_token: "tok-xyz",
        inbound_webhook_url: "https://app/api/cerebro/workflows/webhook/tok-xyz",
      },
      regenerateInboundTokenSchema,
      EMPTY_REGENERATE_INBOUND_TOKEN,
      { endpoint: "regenerateCerebroInboundToken" },
    );
    expect(out.inbound_webhook_token).toBe("tok-xyz");
    expect(out.inbound_webhook_url).toContain("/webhook/tok-xyz");
  });
});

describe("EMPTY_WORKFLOW", () => {
  it("has the shape every form-fragment reads — safe to render unchanged", () => {
    // The form reads these fields directly without defaulting. If the schema
    // fallback ever drops one, the editor white-screens on render.
    expect(EMPTY_WORKFLOW.editor_mode).toBe("form");
    expect(EMPTY_WORKFLOW.trigger_config).toEqual({});
    expect(EMPTY_WORKFLOW.action_config).toEqual({});
    expect(EMPTY_WORKFLOW.inbound_signing_secret_set).toBe(false);
    expect(EMPTY_WORKFLOW.outbound_webhook_secret_set).toBe(false);
  });
});
