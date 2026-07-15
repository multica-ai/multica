import { describe, expect, it } from "vitest";
import { createHookDraft } from "./hook-types";
import { parseHookListResponse, parseHookResponse, parseHookRunsResponse, toHookTransport } from "./hook-api";

describe("workflow hook API compatibility", () => {
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
      bindings: [{ kind: "model", id: "gpt-5.6" }],
      conditions: [{ field: "attempt", op: "lt", value: 3 }],
      handlers: [{ id: "h1", decision: "block", requirement: "Choose one", actions: [{ type: "continuation.require" }] }],
      observed_run_count: 4,
      can_publish: true,
    });
    expect(parsed.binding).toEqual({ kind: "model", value: "gpt-5.6" });
    expect(parsed.conditions[0]).toEqual(expect.objectContaining({ operator: "lt", value: "3" }));
    expect(parsed.baseline_run_count).toBe(4);
  });

  it("always writes a dry-run transport policy", () => {
    const transport = toHookTransport({ ...createHookDraft(), mode: "enforce" });
    expect(transport.mode).toBe("dry_run");
    expect(transport.events).toEqual(["before.task.complete"]);
    expect(transport.bindings[0]).toEqual({ kind: "model", id: "gpt-5.6" });
  });

  it("maps persisted explanatory runs to the Test and history view", () => {
    const [run] = parseHookRunsResponse({ runs: [{
      id: "run-1", created_at: "2026-07-15T08:00:00Z", policy_id: "policy-1", policy_version: 2,
      event: { event_id: "event-1", event_type: "before.task.complete" },
      source_scope: { kind: "model", id: "gpt-5.6" }, latency_ms: 12,
      result: { decision: "allow", would_decision: "block", matches: [{ policy_id: "policy-1", version: 2, handler_id: "primary", source_scope: { kind: "model", id: "gpt-5.6" }, decision: "block", dry_run: true }], action_results: [{ type: "audit.record", status: "would_run" }] },
    }] });
    expect(run).toEqual(expect.objectContaining({ source: "before.task.complete · model gpt-5.6", matched_steps: ["Trigger", "Scope", "Filter", "Decision", "Action"], decision: "block", side_effects: false, latency_ms: 12 }));
  });
});
