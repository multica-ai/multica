export type HookMode = "off" | "dry_run" | "enforce" | "managed";
export type HookFailMode = "open" | "closed" | "warn";
export type HookDecision = "allow" | "block" | "modify" | "require";
export type HookScopeKind = "workspace" | "project" | "workflow" | "agent" | "model" | "issue" | "session";

export type HookEventType =
  | "before.session.start"
  | "after.session.start"
  | "before.session.end"
  | "after.session.end"
  | "before.prompt.assemble"
  | "before.tool.call"
  | "after.tool.call"
  | "on.tool.failure"
  | "before.task.complete"
  | "before.agent.stop"
  | "before.subagent.start"
  | "after.subagent.stop"
  | "on.error"
  | "on.task.failure"
  | "before.wakeup.create"
  | "on.wakeup.fire_failure"
  | "before.issue.status_change"
  | "before.message.send"
  | "after.workflow.step_completed";

export interface HookCondition {
  field: string;
  operator: string;
  value: string;
  conjunction?: "AND" | "OR";
}

export interface HookBinding {
  kind: HookScopeKind;
  value: string;
}

export interface HookActionDraft {
  type: string;
  label: string;
  config: Record<string, unknown>;
}

export interface WorkflowHook {
  id?: string;
  version: number;
  name: string;
  description: string;
  mode: HookMode;
  fail_mode: HookFailMode;
  events: HookEventType[];
  bindings: HookBinding[];
  conditions: HookCondition[];
  decision: HookDecision;
  requirement: string;
  actions: HookActionDraft[];
  baseline_run_count: number;
  can_publish?: boolean;
  last_run_at?: string;
}

export interface HookRun {
  id: string;
  created_at: string;
  source: string;
  matched_steps: string[];
  decision: HookDecision;
  would_action: string;
  side_effects: false;
  latency_ms: number;
}

export const HOOK_EVENT_OPTIONS: ReadonlyArray<{ value: HookEventType; label: string; advanced?: boolean }> = [
  { value: "before.task.complete", label: "Before task completes" },
  { value: "before.wakeup.create", label: "Before wakeup is created" },
  { value: "before.issue.status_change", label: "Before issue status changes" },
  { value: "before.message.send", label: "Before message is sent" },
  { value: "before.tool.call", label: "Before tool call", advanced: true },
  { value: "after.workflow.step_completed", label: "After workflow step completes" },
  { value: "before.session.start", label: "Before session starts", advanced: true },
  { value: "after.session.start", label: "After session starts", advanced: true },
  { value: "before.session.end", label: "Before session ends", advanced: true },
  { value: "after.session.end", label: "After session ends", advanced: true },
  { value: "before.prompt.assemble", label: "Before prompt is assembled", advanced: true },
  { value: "after.tool.call", label: "After tool call", advanced: true },
  { value: "on.tool.failure", label: "When a tool fails", advanced: true },
  { value: "before.agent.stop", label: "Before agent stops", advanced: true },
  { value: "before.subagent.start", label: "Before subagent starts", advanced: true },
  { value: "after.subagent.stop", label: "After subagent stops", advanced: true },
  { value: "on.error", label: "When an error occurs", advanced: true },
];

export function createHookDraft(): WorkflowHook {
  return {
    version: 1,
    name: "",
    description: "",
    mode: "dry_run",
    fail_mode: "warn",
    events: [],
    bindings: [],
    conditions: [],
    decision: "allow",
    requirement: "",
    actions: [],
    baseline_run_count: 0,
    can_publish: false,
  };
}
