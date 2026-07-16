import { describe, expect, it } from "vitest";
import { createHookDraft } from "./hook-types";
import { parseHookListResponse, parseHookResponse, parseHookRunsResponse, toHookTransport } from "./hook-api";

describe("workflow hook API compatibility", () => {
  it("keeps an incomplete draft editable after save and reload", () => {
    const parsed = parseHookResponse({
      id: "draft-1", version: 1, name: "", description: "", mode: "dry_run", fail_mode: "warn",
      events: [], bindings: [], conditions: [], handlers: [], observed_run_count: 0,
    });
    expect(parsed).toEqual(expect.objectContaining({ id: "draft-1", events: [], bindings: [], conditions: [], actions: [] }));
  });

  it("falls back safely when an installed client receives malformed data", () => {
    expect(parseHookListResponse({ hooks: null })).toEqual([]);
    expect(parseHookResponse({ id: 42 })).toEqual(createHookDraft());
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
      can_publish: true,
      updated_at: "2026-07-15T08:00:00Z",
      last_run_at: "2026-07-16T09:30:00Z",
    });
    expect(parsed.bindings).toEqual([{ kind: "model", value: "claude-opus-4-6" }]);
    expect(parsed.conditions[0]).toEqual(expect.objectContaining({ operator: "lt", value: "3" }));
    expect(parsed.baseline_run_count).toBe(4);
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
