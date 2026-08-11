import type { HookEventType, WorkflowHook } from "./hook-types";
import { ACTION_CONFIGURATION } from "./hook-action-config";
import { HOOK_FIELD_CATALOG } from "./hook-field-catalog.generated";

export type HookStepKey = "when" | "guide" | "actions";
export interface HookValidationResult { valid: boolean; message?: string }

// Suggestions come from the server-owned field manifest (via the generated
// catalog): every field listed here is one the gate for that event actually
// supplies, and every field the platform's own managed hooks use is listed.
const COMMON_FIELDS = HOOK_FIELD_CATALOG.common.map((field) => field.path);
const EVENT_FIELDS: Record<string, readonly string[]> = Object.fromEntries(
  HOOK_FIELD_CATALOG.events.map((event) => [event.type, event.fields.map((field) => field.path)]),
);

export function fieldsForEvents(events: readonly HookEventType[]): string[] {
  return [...new Set(events.flatMap((event) => [...COMMON_FIELDS, ...(EVENT_FIELDS[event] ?? [])]))].sort();
}

export function validateHookStep(hook: WorkflowHook, step: HookStepKey): HookValidationResult {
  switch (step) {
    case "when": {
      // "When" combines the trigger, the scope it applies to, and any optional filters.
      if (hook.events.length === 0) return { valid: false, message: "Choose at least one trigger." };
      if (hook.bindings.length === 0) return { valid: false, message: "Choose what this applies to." };
      if (!hook.bindings.every((binding) => binding.kind === "workspace" || binding.value.trim().length > 0)) {
        return { valid: false, message: "Choose a named target for every scope." };
      }
      // Filters are optional — no conditions means the hook runs every time.
      // The field list below is the picker's suggestions, NOT the set of legal
      // fields: the server accepts any non-empty field name, and the platform's
      // own hooks use fields it never listed (message.agent_authored,
      // chain.active). Rejecting those printed a red error on hooks that were
      // enforcing perfectly (FIR-4797).
      if (hook.conditions.some((condition) => !condition.field.trim())) {
        return { valid: false, message: "Choose a filter field." };
      }
      if (hook.conditions.some((condition) => !condition.operator || (!["exists", "not_exists"].includes(condition.operator) && !condition.value.trim()))) {
        return { valid: false, message: "Complete every filter." };
      }
      return { valid: true };
    }
    case "guide":
      if (!hook.decision) return { valid: false, message: "Choose whether to guide or enforce." };
      if (["block", "require"].includes(hook.decision) && !hook.requirement.trim()) return { valid: false, message: "Describe what the agent must do." };
      return { valid: true };
    case "actions":
      // Mirrors the server: a Stop or Require decision already tells the agent
      // what to do, so it needs no action on top. Only a hook that merely
      // guides has to run something to have any effect at all.
      if (hook.actions.length === 0 && !["block", "require"].includes(hook.decision)) {
        return { valid: false, message: "Add an action, or choose Stop or Require an outcome so the agent is told what to do." };
      }
      for (const action of hook.actions) {
        const message = actionConfigurationError(action);
        if (message) return { valid: false, message };
      }
      return hook.fail_mode ? { valid: true } : { valid: false, message: "Choose what happens if the check cannot run." };
  }
}

function actionConfigurationError(action: WorkflowHook["actions"][number]): string | undefined {
  const definition = ACTION_CONFIGURATION[action.type];
  if (!definition) return "Choose a supported action.";
  const missing = definition.fields.filter((field) => field.required).find((field) => {
    const value = action.config[field.key];
    return typeof value !== "boolean" && String(value ?? "").trim().length === 0;
  });
  return missing ? `Choose ${missing.label} for ${definition.label}.` : undefined;
}

export function validateHook(hook: WorkflowHook): HookValidationResult {
  if (!hook.name.trim()) return { valid: false, message: "Name this hook." };
  if (!hook.contract_rule.trim()) return { valid: false, message: "Explain the rule in plain language." };
  if (!hook.contract_satisfy.trim()) return { valid: false, message: "Explain how to satisfy the rule." };
  for (const step of ["when", "guide", "actions"] as const) {
    const result = validateHookStep(hook, step);
    if (!result.valid) return result;
  }
  return { valid: true };
}
