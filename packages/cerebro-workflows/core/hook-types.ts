export type HookMode = "off" | "dry_run" | "enforce" | "managed";
export type HookFailMode = "open" | "closed" | "warn";
export type HookConditionMode = "all" | "any";
export type HookDecision = "allow" | "block" | "modify" | "require";
export type HookScopeKind = "workspace" | "project" | "workflow" | "agent" | "model" | "issue" | "session";
export type HookLifecycleState = "draft" | "live" | "live_with_draft" | "off" | "off_with_draft" | "managed";

export interface HookLifecycle {
  state: HookLifecycleState;
  live_policy_id?: string;
  live_version?: number;
  draft_id?: string;
  draft_series_id?: string;
  draft_revision?: number;
  live_unchanged_by_draft: boolean;
}

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
  | "before.issue.assigned"
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

// Symbolic action targets, resolved per event by the server. Mirrors
// HookEventTargetLabels in server/internal/cerebro/workflows/hook_actions.go.
export const HOOK_EVENT_TARGET_LABELS: Record<string, string> = {
  "event.agent": "The agent that triggered this hook",
  "event.task": "The task that produced this event",
};

// What is actually enforcing right now. Present only when the hook body above
// is an unpublished Draft, so the list can stop describing a draft as live.
export interface HookLive {
  policy_id: string;
  version: number;
  name: string;
  decision: HookDecision;
  requirement?: string;
  fail_mode: HookFailMode;
}

export interface WorkflowHook {
  id?: string;
  family_id?: string;
  draft_series_id?: string;
  revision?: number;
  version: number;
  name: string;
  description: string;
  contract_rule: string;
  contract_satisfy: string;
  mode: HookMode;
  fail_mode: HookFailMode;
  events: HookEventType[];
  bindings: HookBinding[];
  condition_mode: HookConditionMode;
  conditions: HookCondition[];
  decision: HookDecision;
  requirement: string;
  actions: HookActionDraft[];
  baseline_run_count: number;
  pass_count_7d: number;
  block_count_7d: number;
  can_publish?: boolean;
  last_run_at?: string;
  lifecycle: HookLifecycle;
  compatible_events?: HookJournalEvent[];
  live?: HookLive;
}

// One row of the workspace-wide hook history.
export interface HookRunSummary {
  id: string;
  family_id: string;
  hook_name: string;
  policy_version: number;
  event_type: string;
  agent_id: string;
  issue_id: string;
  decision: HookDecision;
  would_decision?: HookDecision;
  enforced: boolean;
  requirements: string[];
  timed_out: boolean;
  latency_ms: number;
  created_at: string;
}

export interface HookJournalEvent {
  id: string;
  event_id: string;
  event_type: HookEventType;
  schema_version: number;
  occurred_at: string;
  expires_at: string;
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
  // A run recorded from a Dry run or a replayed test changed nothing. A live
  // run did. Calling every row a test was the old panel's worst lie.
  dry_run: boolean;
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
  { value: "before.issue.assigned", label: "Before an issue is handed to an agent", description: "Check an issue before work is started for the assigned agent." },
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
    contract_rule: "",
    contract_satisfy: "",
    mode: "dry_run",
    fail_mode: "warn",
    events: [],
    bindings: [],
    condition_mode: "all",
    conditions: [],
    decision: "allow",
    requirement: "",
    actions: [],
    baseline_run_count: 0,
    pass_count_7d: 0,
    block_count_7d: 0,
    can_publish: false,
    lifecycle: { state: "draft", live_unchanged_by_draft: false },
    compatible_events: [],
  };
}
