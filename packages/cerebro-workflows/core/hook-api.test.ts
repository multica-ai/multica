import { describe, expect, it } from "vitest";
import { createHookDraft } from "./hook-types";
import { parseActiveHookRulesResponse, parseHookListResponse, parseHookResponse, parseHookRunsResponse, toHookTransport } from "./hook-api";

describe("workflow hook API compatibility", () => {
  it("parses the agent-scoped active-rules response", () => {
    expect(parseActiveHookRulesResponse({ rules: [{
      id: "rule-1", name: "Require a next step",
      contract_rule: "Runs must leave a visible next step.",
      contract_satisfy: "Register a continuation before stopping.",
      events: ["before.task.complete"],
      scope: { kind: "agent", value: "agent-1" },
    }] })).toEqual({ rules: [expect.objectContaining({ id: "rule-1", name: "Require a next step" })] });
  });

  it("round-trips the plain-language contract", () => {
    const parsed = parseHookResponse({
      id: "contract", version: 1, name: "Guard completion", mode: "dry_run", fail_mode: "warn",
      contract_rule: "An unfinished issue needs a continuation.",
      contract_satisfy: "Create a wakeup or mark the issue blocked.",
      events: [], bindings: [], conditions: [], handlers: [],
    });

    expect(parsed).toEqual(expect.objectContaining({
      contract_rule: "An unfinished issue needs a continuation.",
      contract_satisfy: "Create a wakeup or mark the issue blocked.",
    }));
    expect(toHookTransport(parsed)).toEqual(expect.objectContaining({
      contract_rule: "An unfinished issue needs a continuation.",
      contract_satisfy: "Create a wakeup or mark the issue blocked.",
    }));
  });

  it("defaults legacy policies to all conditions and round-trips any", () => {
    const legacy = parseHookResponse({
      id: "legacy", version: 1, name: "Legacy", mode: "dry_run", fail_mode: "warn",
      events: [], bindings: [], conditions: [], handlers: [],
    });
    expect(legacy.condition_mode).toBe("all");

    const any = parseHookResponse({
      id: "any", version: 1, name: "Any", mode: "dry_run", fail_mode: "warn", condition_mode: "any",
      events: [], bindings: [], conditions: [], handlers: [],
    });
    expect(any.condition_mode).toBe("any");
    expect(toHookTransport(any).condition_mode).toBe("any");
  });

  it("serializes list filters as trimmed values instead of one comma string", () => {
    const transport = toHookTransport({
      ...createHookDraft(),
      conditions: [{ field: "issue.status", operator: "not_in", value: "done, cancelled,  in_review  " }],
    });

    expect(transport.conditions).toEqual([{
      field: "issue.status",
      op: "not_in",
      values: ["done", "cancelled", "in_review"],
    }]);
  });

  it("keeps an incomplete draft editable after save and reload", () => {
    const parsed = parseHookResponse({
      id: "draft-1", version: 1, name: "", description: "", mode: "dry_run", fail_mode: "warn",
      events: [], bindings: [], conditions: [], handlers: [], observed_run_count: 0,
    });
    expect(parsed).toEqual(expect.objectContaining({ id: "draft-1", events: [], bindings: [], conditions: [], actions: [] }));
  });

  it("preserves stable family identity and separate Live and Draft lifecycle", () => {
    const parsed = parseHookResponse({
      id: "draft-r3",
      family_id: "family-1",
      draft_series_id: "draft-series-1",
      revision: 3,
      version: 5,
      name: "Draft changes",
      mode: "dry_run",
      fail_mode: "warn",
      events: [],
      bindings: [],
      conditions: [],
      handlers: [],
      lifecycle: {
        state: "live_with_draft",
        live_policy_id: "live-v4",
        live_version: 4,
        draft_id: "draft-r3",
        draft_series_id: "draft-series-1",
        draft_revision: 3,
        live_unchanged_by_draft: true,
      },
    });

    expect(parsed).toEqual(expect.objectContaining({
      id: "draft-r3",
      family_id: "family-1",
      draft_series_id: "draft-series-1",
      revision: 3,
      lifecycle: expect.objectContaining({
        state: "live_with_draft",
        live_policy_id: "live-v4",
        live_version: 4,
        live_unchanged_by_draft: true,
      }),
    }));
    expect(toHookTransport(parsed).revision).toBe(3);
  });

  it("retires the legacy silent failure mode at the API boundary", () => {
    const parsed = parseHookResponse({
      id: "draft-1", version: 1, name: "Legacy hook", description: "", mode: "dry_run", fail_mode: "open",
      events: [], bindings: [], conditions: [], handlers: [], observed_run_count: 0,
    });

    expect(parsed.fail_mode).toBe("warn");
    expect(toHookTransport(parsed).fail_mode).toBe("warn");
  });

  it("blocks a malformed detail response instead of creating an editable blank Draft", () => {
    expect(() => parseHookResponse({ id: 42 })).toThrow("Hook response is malformed");
  });

  it("keeps valid list records and reports each malformed record", () => {
    const result = parseHookListResponse({
      hooks: [
        {
          id: "valid-hook", version: 1, name: "Valid", mode: "dry_run", fail_mode: "warn",
          events: [], bindings: [], conditions: [], handlers: [],
        },
        { id: "broken-hook", version: "one" },
      ],
      partial_errors: [{ record_id: "server-broken", code: "hook_record_malformed", request_id: "request-1" }],
    });

    expect(result.hooks).toHaveLength(1);
    expect(result.hooks[0]?.id).toBe("valid-hook");
    expect(result.partial_errors).toEqual([
      { record_id: "server-broken", code: "hook_record_malformed", request_id: "request-1" },
      { record_id: "broken-hook", code: "hook_record_malformed" },
    ]);
  });

  it("maps the shared backend policy contract to the editor contract", () => {
    const parsed = parseHookResponse({
      id: "11111111-1111-1111-1111-111111111111",
      version: 2,
      name: "Require continuation",
      description: "No silent stop",
      mode: "dry_run",
      fail_mode: "closed",
      events: ["before.task.complete"],
      bindings: [{ kind: "model", id: "claude-opus-4-6" }],
      conditions: [{ field: "attempt", op: "lt", value: 3 }],
      handlers: [{ id: "h1", decision: "block", requirement: "Choose one", actions: [{ type: "continuation.require" }] }],
      observed_run_count: 4,
      pass_count_7d: 18,
      block_count_7d: 3,
      can_publish: true,
      updated_at: "2026-07-15T08:00:00Z",
      last_run_at: "2026-07-16T09:30:00Z",
    });
    expect(parsed.bindings).toEqual([{ kind: "model", value: "claude-opus-4-6" }]);
    expect(parsed.conditions[0]).toEqual(expect.objectContaining({ operator: "lt", value: "3" }));
    expect(parsed.baseline_run_count).toBe(4);
    expect(parsed.pass_count_7d).toBe(18);
    expect(parsed.block_count_7d).toBe(3);
    expect(parsed.last_run_at).toBe("2026-07-16T09:30:00Z");
  });

  it("always writes a dry-run transport policy", () => {
    const transport = toHookTransport({
      ...createHookDraft(),
      mode: "enforce",
      events: ["before.task.complete", "before.agent.stop"],
      bindings: [
        { kind: "agent", value: "agent-1" },
        { kind: "issue", value: "issue-1" },
      ],
    });
    expect(transport.mode).toBe("dry_run");
    expect(transport.events).toEqual(["before.task.complete", "before.agent.stop"]);
    expect(transport.bindings).toEqual([
      { kind: "agent", id: "agent-1" },
      { kind: "issue", id: "issue-1" },
    ]);
  });

  it("preserves every persisted trigger and binding", () => {
    const parsed = parseHookResponse({
      id: "11111111-1111-1111-1111-111111111111",
      version: 1,
      name: "Multiple targets",
      mode: "dry_run",
      fail_mode: "warn",
      events: ["before.task.complete", "before.agent.stop"],
      bindings: [{ kind: "agent", id: "agent-1" }, { kind: "issue", id: "issue-1" }],
      conditions: [],
      handlers: [{ id: "primary", decision: "allow", actions: [] }],
    });
    expect(parsed.events).toEqual(["before.task.complete", "before.agent.stop"]);
    expect(parsed.bindings).toEqual([
      { kind: "agent", value: "agent-1" },
      { kind: "issue", value: "issue-1" },
    ]);
  });

  it("maps persisted explanatory runs to the Test and history view", () => {
    const [run] = parseHookRunsResponse({ runs: [{
      id: "run-1", created_at: "2026-07-15T08:00:00Z", policy_id: "policy-1", policy_version: 2,
      event: { event_id: "event-1", event_type: "before.task.complete" },
      source_scope: { kind: "model", id: "claude-opus-4-6" }, fail_mode: "warn", latency_ms: 12,
      result: { decision: "allow", would_decision: "block", matched_conditions: [{ field: "issue.status", op: "eq", value: "in_review" }], requirements: ["Add delivery evidence"], matches: [{ policy_id: "policy-1", version: 2, handler_id: "primary", source_scope: { kind: "model", id: "claude-opus-4-6" }, decision: "block", dry_run: true }], action_results: [{ type: "audit.record", status: "would_run" }] },
    }] });
    expect(run).toEqual(expect.objectContaining({ policy_id: "policy-1", policy_version: 2, source_scope: { kind: "model", id: "claude-opus-4-6" }, matched_conditions: ["issue.status eq in_review"], fail_mode: "warn", remediation: ["Add delivery evidence"], source: "before.task.complete · model claude-opus-4-6", matched_steps: ["Trigger", "Scope", "Filter", "Decision", "Action"], decision: "block", side_effects: false, latency_ms: 12 }));
  });
});
