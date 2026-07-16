import type { HookEventType, WorkflowHook } from "./hook-types";
import { ACTION_CONFIGURATION } from "./hook-ux";

export type HookStepKey = "trigger" | "scope" | "filter" | "decision" | "action" | "failure";
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
    case "trigger":
      return hook.events.length > 0 ? { valid: true } : { valid: false, message: "Choose at least one trigger." };
    case "scope":
      if (hook.bindings.length === 0) return { valid: false, message: "Choose at least one scope." };
      return hook.bindings.every((binding) => binding.kind === "workspace" || binding.value.trim().length > 0)
        ? { valid: true }
        : { valid: false, message: "Choose a named target for every scope." };
    case "filter": {
      if (hook.conditions.length === 0) return { valid: false, message: "Add at least one condition." };
      const fields = fieldsForEvents(hook.events);
      if (hook.conditions.some((condition) => !fields.includes(condition.field))) {
        return { valid: false, message: "Choose a field available for the selected trigger." };
      }
      if (hook.conditions.some((condition) => !condition.operator || (!["exists", "not_exists"].includes(condition.operator) && !condition.value.trim()))) {
        return { valid: false, message: "Complete every condition." };
      }
      return { valid: true };
    }
    case "decision":
      return hook.decision ? { valid: true } : { valid: false, message: "Choose a decision." };
    case "action":
      if (["block", "require"].includes(hook.decision) && !hook.requirement.trim()) return { valid: false, message: "Describe the required outcome." };
      if (hook.actions.length === 0) return { valid: false, message: "Add at least one action." };
      return hook.actions.every(actionIsConfigured) ? { valid: true } : { valid: false, message: "Complete every action." };
    case "failure":
      return hook.fail_mode ? { valid: true } : { valid: false, message: "Choose failure behavior." };
  }
}

function actionIsConfigured(action: WorkflowHook["actions"][number]): boolean {
  const definition = ACTION_CONFIGURATION[action.type];
  if (!definition) return false;
  return definition.fields.filter((field) => field.required).every((field) => {
    const value = action.config[field.key];
    return typeof value === "boolean" ? true : String(value ?? "").trim().length > 0;
  });
}

export function validateHook(hook: WorkflowHook): HookValidationResult {
  if (!hook.name.trim()) return { valid: false, message: "Name this hook." };
  for (const step of ["trigger", "scope", "filter", "decision", "action", "failure"] as const) {
    const result = validateHookStep(hook, step);
    if (!result.valid) return result;
  }
  return { valid: true };
}
