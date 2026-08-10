import { describe, expect, it } from "vitest";
import { createHookDraft, HOOK_EVENT_OPTIONS } from "./hook-types";
import { fieldsForEvents, validateHook, validateHookStep } from "./hook-validation";

describe("workflow hook validation", () => {
  it("starts new hooks empty and guides the user to choose a trigger", () => {
    const draft = createHookDraft();
    expect(draft).toMatchObject({ name: "", events: [], bindings: [], conditions: [], actions: [] });
    expect(validateHookStep(draft, "when")).toEqual({ valid: false, message: "Choose at least one trigger." });
    expect(validateHook(draft).valid).toBe(false);
  });

  // The catalog is the picker's suggestions, not the set of legal fields: the
  // server accepts any non-empty name, and the platform's own hooks filter on
  // fields the catalog never listed. Only an empty field is an error.
  it("offers a field catalog for every supported trigger and accepts a field outside it", () => {
    for (const event of HOOK_EVENT_OPTIONS) {
      expect(fieldsForEvents([event.value]).length, event.value).toBeGreaterThan(0);
    }
    const hook = {
      ...createHookDraft(),
      name: "Guard completion",
      events: ["before.task.complete" as const],
      bindings: [{ kind: "workspace" as const, value: "" }],
      conditions: [{ field: "made.up", operator: "eq", value: "x" }],
      decision: "block" as const,
      requirement: "Add a continuation",
      actions: [{ type: "audit.record", label: "Record audit event", config: { event: "hook.test" } }],
    };
    expect(validateHookStep(hook, "when")).toEqual({ valid: true });
    expect(validateHookStep({ ...hook, conditions: [{ field: "  ", operator: "eq", value: "x" }] }, "when")).toEqual({ valid: false, message: "Choose a filter field." });
  });

  it("requires both plain-language contract lines before publish", () => {
    const hook = Object.assign(createHookDraft(), {
      name: "Guard completion",
      events: ["before.task.complete" as const],
      bindings: [{ kind: "workspace" as const, value: "" }],
      decision: "require" as const,
      requirement: "Add a continuation",
      actions: [{ type: "audit.record", label: "Record audit event", config: { event: "hook.test" } }],
    });

    expect(validateHook(hook)).toEqual({ valid: false, message: "Explain the rule in plain language." });
  });
});
