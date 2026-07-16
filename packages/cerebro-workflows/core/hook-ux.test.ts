import { describe, expect, it } from "vitest";
import { createHookDraft, HOOK_EVENT_OPTIONS } from "./hook-types";
import {
  ACTION_CONFIGURATION,
  HOOK_TEMPLATES,
  describeHook,
  fieldDefinition,
  stepSummary,
} from "./hook-ux";

describe("Hooks UX contract", () => {
  it("offers every supported event with plain-language copy and advanced metadata", () => {
    expect(HOOK_EVENT_OPTIONS.map((option) => option.value)).toEqual(expect.arrayContaining([
      "on.task.failure",
      "on.wakeup.fire_failure",
      "before.session.start",
      "on.error",
    ]));
    expect(HOOK_EVENT_OPTIONS.every((option) => option.description.length > 0)).toBe(true);
  });

  it("defines typed configuration for every visible action", () => {
    const required = [
      "member.notify", "wakeup.create", "wakeup.cancel", "approval.require",
      "metric.increment", "artifact.create_or_update", "task.retry", "task.cancel",
      "audit.record", "session.handoff", "judge.gate",
    ];
    expect(required.every((type) => (ACTION_CONFIGURATION[type]?.fields.length ?? 0) > 0)).toBe(true);
    expect(ACTION_CONFIGURATION["wakeup.create"]?.fields.find((field) => field.key === "fire_at")?.input).toBe("datetime-local");
    expect(ACTION_CONFIGURATION["task.retry"]?.fields.find((field) => field.key === "task_id")?.required).toBe(true);
  });

  it("uses human labels and typed value metadata for filter fields", () => {
    expect(fieldDefinition("issue.status")).toMatchObject({ label: "Issue status", input: "select" });
    expect(fieldDefinition("attempt")).toMatchObject({ label: "Attempt number", input: "number" });
    expect(fieldDefinition("continuation")).toMatchObject({ label: "Continuation registered", input: "boolean" });
  });

  it("provides useful starter recipes plus a scratch option", () => {
    expect(HOOK_TEMPLATES.length).toBeGreaterThanOrEqual(5);
    expect(HOOK_TEMPLATES.length).toBeLessThanOrEqual(7);
    expect(HOOK_TEMPLATES.some((template) => template.id === "scratch")).toBe(true);
    expect(HOOK_TEMPLATES.filter((template) => template.id !== "scratch").every((template) => template.hook.events.length > 0)).toBe(true);
  });

  it("keeps chain and overview summaries in sync with the current draft", () => {
    const hook = {
      ...createHookDraft(),
      name: "Protect completed work",
      events: ["before.task.complete" as const],
      bindings: [{ kind: "issue" as const, value: "issue-1" }],
      conditions: [{ field: "issue.status", operator: "eq", value: "done" }],
      decision: "block" as const,
      requirement: "Add a continuation",
      actions: [{ type: "member.notify", label: "Notify member", config: { member_id: "member-1" } }],
    };
    expect(stepSummary(hook, "trigger")).toBe("Before task completes");
    expect(stepSummary(hook, "scope")).toBe("1 issue");
    expect(stepSummary(hook, "decision")).toBe("Block the action");
    expect(describeHook(hook)).toContain("before a task completes");
    expect(describeHook(hook)).toContain("notify a member");
  });
});
