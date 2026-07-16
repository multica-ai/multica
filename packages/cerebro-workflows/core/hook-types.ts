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
  policy_id: string;
  policy_version: number;
  source_scope: { kind: string; id: string };
  matched_conditions: string[];
  matched_steps: string[];
  decision: HookDecision;
  would_action: string;
  fail_mode: HookFailMode;
  remediation: string[];
  side_effects: false;
  latency_ms: number;
  event?: Record<string, unknown>;
}

export const HOOK_EVENT_OPTIONS: ReadonlyArray<{ value: HookEventType; label: string; description: string; advanced?: boolean }> = [
  { value: "before.task.complete", label: "Before task completes", description: "Check work immediately before a task is marked complete." },
  { value: "on.task.failure", label: "When a task fails", description: "React after a task ends with an error." },
  { value: "before.wakeup.create", label: "Before wakeup is created", description: "Check a scheduled wakeup before it is saved." },
  { value: "on.wakeup.fire_failure", label: "When a wakeup fails", description: "React when a scheduled wakeup cannot start its work." },
  { value: "before.issue.status_change", label: "Before issue status changes", description: "Check an issue before its status is changed." },
  { value: "before.message.send", label: "Before message is sent", description: "Check an outgoing message before delivery." },
  { value: "after.workflow.step_completed", label: "After workflow step completes", description: "React after a workflow finishes one step." },
  { value: "before.tool.call", label: "Before tool call", description: "Check a tool request before it runs.", advanced: true },
  { value: "before.session.start", label: "Before session starts", description: "Check a session before it starts.", advanced: true },
  { value: "after.session.start", label: "After session starts", description: "React after a session starts.", advanced: true },
  { value: "before.session.end", label: "Before session ends", description: "Check a session before it ends.", advanced: true },
  { value: "after.session.end", label: "After session ends", description: "React after a session ends.", advanced: true },
  { value: "before.prompt.assemble", label: "Before prompt is assembled", description: "Check context before the final prompt is assembled.", advanced: true },
  { value: "after.tool.call", label: "After tool call", description: "React after a tool returns successfully.", advanced: true },
  { value: "on.tool.failure", label: "When a tool fails", description: "React when a tool returns an error.", advanced: true },
  { value: "before.agent.stop", label: "Before agent stops", description: "Check an agent immediately before it stops.", advanced: true },
  { value: "before.subagent.start", label: "Before subagent starts", description: "Check delegated work before it starts.", advanced: true },
  { value: "after.subagent.stop", label: "After subagent stops", description: "React after delegated work stops.", advanced: true },
  { value: "on.error", label: "When an error occurs", description: "React to an unhandled workflow error.", advanced: true },
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
