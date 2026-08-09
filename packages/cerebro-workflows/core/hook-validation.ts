import type { HookEventType, WorkflowHook } from "./hook-types";
import { ACTION_CONFIGURATION } from "./hook-ux";

export type HookStepKey = "when" | "guide" | "actions";
export interface HookValidationResult { valid: boolean; message?: string }

const COMMON_FIELDS = ["actor.type", "actor.id", "issue.id", "project.id", "workflow.id", "agent.id", "model", "session.id", "attempt", "hook_depth"];
const EVENT_FIELDS: Record<HookEventType, readonly string[]> = {
  "before.session.start": ["session.id", "agent.id", "model"],
  "after.session.start": ["session.id", "agent.id", "model"],
  "before.session.end": ["session.id", "agent.id", "issue.status"],
  "after.session.end": ["session.id", "agent.id", "issue.status"],
  "before.prompt.assemble": ["session.id", "prompt", "model"],
  "before.tool.call": ["tool.name", "tool.input", "session.id"],
  "after.tool.call": ["tool.name", "tool.output", "session.id"],
  "on.tool.failure": ["tool.name", "error", "session.id"],
  "before.task.complete": ["task.id", "issue.status", "continuation", "attempt"],
  "before.agent.stop": ["agent.id", "issue.status", "continuation"],
  "before.subagent.start": ["agent.id", "parent_agent.id", "issue.id"],
  "after.subagent.stop": ["agent.id", "parent_agent.id", "issue.id"],
  "on.error": ["error", "error.stage", "session.id"],
  "on.task.failure": ["task.id", "error", "attempt"],
  "before.wakeup.create": ["wakeup.fire_at", "wakeup.agent_id", "issue.id"],
  "on.wakeup.fire_failure": ["wakeup.id", "error", "attempt"],
  "before.issue.status_change": ["issue.status", "issue.previous_status", "issue.id"],
  "before.message.send": ["message.channel_id", "message.body", "actor.id"],
  "after.workflow.step_completed": ["workflow.id", "workflow.step", "issue.id"],
};

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
      const fields = fieldsForEvents(hook.events);
      if (hook.conditions.some((condition) => !fields.includes(condition.field))) {
        return { valid: false, message: "Choose a filter field available for the selected trigger." };
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
      if (hook.actions.length === 0) return { valid: false, message: "Add at least one action." };
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
  for (const step of ["when", "guide", "actions"] as const) {
    const result = validateHookStep(hook, step);
    if (!result.valid) return result;
  }
  return { valid: true };
}
