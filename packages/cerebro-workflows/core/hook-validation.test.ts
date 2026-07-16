import { describe, expect, it } from "vitest";
import { createHookDraft, HOOK_EVENT_OPTIONS } from "./hook-types";
import { fieldsForEvents, validateHook, validateHookStep } from "./hook-validation";

describe("workflow hook validation", () => {
  it("starts new hooks empty and guides the user to choose a trigger", () => {
    const draft = createHookDraft();
    expect(draft).toMatchObject({ name: "", events: [], bindings: [], conditions: [], actions: [] });
    expect(validateHookStep(draft, "trigger")).toEqual({ valid: false, message: "Choose at least one trigger." });
    expect(validateHook(draft).valid).toBe(false);
  });

  it("offers a field catalog for every supported trigger and rejects unknown fields", () => {
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
      actions: [{ type: "audit.record", label: "Record audit event", config: {} }],
    };
    expect(validateHookStep(hook, "filter")).toEqual({ valid: false, message: "Choose a field available for the selected trigger." });
  });
});
