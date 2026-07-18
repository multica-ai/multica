import { createHookDraft, HOOK_EVENT_OPTIONS, type HookEventType, type WorkflowHook } from "./hook-types";
import type { HookStepKey } from "./hook-validation";

export interface HookFieldDefinition {
  label: string;
  input: "text" | "number" | "boolean" | "select";
  options?: ReadonlyArray<{ value: string; label: string }>;
}

export interface ActionFieldDefinition {
  key: string;
  label: string;
  input: "text" | "textarea" | "number" | "datetime-local" | "checkbox" | "target" | "select";
  required?: boolean;
  target?: "agent" | "member" | "issue" | "workflow" | "skill" | "squad" | "artifact";
  help?: string;
  options?: ReadonlyArray<{ value: string; label: string }>;
}

export interface ActionDefinition {
  label: string;
  description: string;
  fields: readonly ActionFieldDefinition[];
}

const statusOptions = ["Backlog", "Todo", "In progress", "In review", "Done", "Blocked", "Cancelled"].map((label) => ({ value: label.toLowerCase().replaceAll(" ", "_"), label }));

const FIELD_DEFINITIONS: Record<string, HookFieldDefinition> = {
  "issue.status": { label: "Issue status", input: "select", options: statusOptions },
  "issue.previous_status": { label: "Previous issue status", input: "select", options: statusOptions },
  "actor.type": { label: "Actor type", input: "select", options: [{ value: "member", label: "Member" }, { value: "agent", label: "Agent" }, { value: "system", label: "System" }] },
  attempt: { label: "Attempt number", input: "number" },
  hook_depth: { label: "Hook depth", input: "number" },
  continuation: { label: "Continuation registered", input: "boolean" },
  "message.body": { label: "Message text", input: "text" },
  "tool.name": { label: "Tool name", input: "text" },
  "workflow.step": { label: "Workflow step", input: "text" },
  model: { label: "Model", input: "text" },
};

export function fieldDefinition(field: string): HookFieldDefinition {
  return FIELD_DEFINITIONS[field] ?? {
    label: field.split(".").map((part) => part.replaceAll("_", " ")).join(" · ").replace(/^./, (value) => value.toUpperCase()),
    input: field.endsWith(".id") ? "text" : "text",
  };
}

export const ACTION_CONFIGURATION: Record<string, ActionDefinition> = {
  "member.notify": { label: "Notify member", description: "Send a clear notification to a named member.", fields: [
    { key: "member_id", label: "Member", input: "target", target: "member", required: true },
    { key: "title", label: "Title", input: "text", required: true },
    { key: "message", label: "Message", input: "textarea", required: true },
  ] },
  "agent.dispatch": { label: "Start agent", description: "Start a named agent with this hook context.", fields: [{ key: "agent_id", label: "Agent", input: "target", target: "agent", required: true }] },
  "squad.dispatch": { label: "Start squad", description: "Start a squad through its lead agent.", fields: [
    { key: "squad_id", label: "Squad", input: "target", target: "squad", required: true },
    { key: "agent_id", label: "Lead agent", input: "target", target: "agent", required: true },
  ] },
  "skill.run": { label: "Run skill", description: "Run a named skill, optionally with a specific agent.", fields: [
    { key: "skill_name", label: "Skill", input: "target", target: "skill", required: true },
    { key: "agent_id", label: "Agent", input: "target", target: "agent" },
  ] },
  "judge.gate": { label: "Judge gate", description: "Ask a judge agent to decide against a written rubric.", fields: [
    { key: "agent_id", label: "Judge agent", input: "target", target: "agent", required: true },
    { key: "rubric", label: "Rubric", input: "textarea", required: true },
  ] },
  "wakeup.create": { label: "Create wakeup", description: "Schedule a single future wakeup.", fields: [
    { key: "fire_at", label: "Wake up at", input: "datetime-local", required: true },
    { key: "agent_id", label: "Agent", input: "target", target: "agent" },
    { key: "issue_id", label: "Issue", input: "target", target: "issue" },
    { key: "prompt", label: "Prompt", input: "textarea", required: true },
  ] },
  "wakeup.cancel": { label: "Cancel wakeup", description: "Cancel one existing wakeup by its runtime value.", fields: [{ key: "wakeup_id", label: "Wakeup ID", input: "text", required: true }] },
  "session.handoff": { label: "Start Handoff", description: "Pass the work and its state to another agent session.", fields: [
    { key: "target", label: "Target agent", input: "target", target: "agent", required: true },
    { key: "plan_ref", label: "Plan reference", input: "target", target: "artifact", help: "Optional when the hook does not follow a saved plan." },
    { key: "start_new", label: "Start new session now", input: "checkbox" },
    { key: "summary", label: "Summary", input: "textarea", required: true },
    { key: "done", label: "Done", input: "textarea", required: true },
    { key: "remaining", label: "Remaining", input: "textarea", required: true },
    { key: "max_depth", label: "Maximum Handoff depth", input: "number", required: true },
  ] },
  "task.retry": { label: "Repeat current step", description: "Retry the task that produced the event.", fields: [{ key: "task_id", label: "Task ID", input: "text", required: true }] },
  "task.cancel": { label: "Cancel task", description: "Cancel the task that produced the event.", fields: [{ key: "task_id", label: "Task ID", input: "text", required: true }] },
  "artifact.create_or_update": { label: "Create or update artifact", description: "Write a reusable document from the hook outcome.", fields: [
    { key: "artifact_id", label: "Existing artifact", input: "target", target: "artifact" },
    { key: "title", label: "Title", input: "text", required: true },
    { key: "kind", label: "Type", input: "select", required: true, options: ["report", "plan", "decision", "diagram", "note"].map((value) => ({ value, label: value.charAt(0).toUpperCase() + value.slice(1) })) },
    { key: "body", label: "Content", input: "textarea", required: true },
  ] },
  "workflow.activate": { label: "Start workflow", description: "Start a named workflow.", fields: [{ key: "workflow_id", label: "Workflow", input: "target", target: "workflow", required: true }] },
  "workflow.pause": { label: "Pause workflow", description: "Pause a named workflow.", fields: [{ key: "workflow_id", label: "Workflow", input: "target", target: "workflow", required: true }] },
  "workflow.resume": { label: "Resume workflow", description: "Resume a named workflow.", fields: [{ key: "workflow_id", label: "Workflow", input: "target", target: "workflow", required: true }] },
  "workflow.stop": { label: "Stop workflow", description: "Stop a named workflow.", fields: [{ key: "workflow_id", label: "Workflow", input: "target", target: "workflow", required: true }] },
  "approval.require": { label: "Require approval", description: "Pause until a person approves the described capability.", fields: [
    { key: "capability", label: "Capability", input: "text", required: true },
    { key: "resource", label: "Resource", input: "text", required: true },
    { key: "reason", label: "Reason", input: "textarea", required: true },
  ] },
  "audit.record": { label: "Record audit event", description: "Add a named event to the audit trail.", fields: [
    { key: "event", label: "Event name", input: "text", required: true },
    { key: "message", label: "Details", input: "textarea" },
  ] },
  "metric.increment": { label: "Increment metric", description: "Increase a named operational metric.", fields: [
    { key: "name", label: "Metric name", input: "text", required: true },
    { key: "amount", label: "Amount", input: "number", required: true },
  ] },
};

const eventPhrase = (event?: HookEventType) => {
  if (event === "before.task.complete") return "before a task completes";
  if (event === "on.task.failure") return "when a task fails";
  if (event === "on.wakeup.fire_failure") return "when a wakeup fails";
  const label = HOOK_EVENT_OPTIONS.find((option) => option.value === event)?.label ?? "the selected event occurs";
  return label.replace(/^Before /, "before ").replace(/^After /, "after ").replace(/^When /, "when ").replace(/^the selected/, "when the selected");
};

export function triggerSummary(hook: WorkflowHook): string {
  const firstEvent = hook.events.at(0);
  return !firstEvent ? "Choose a trigger" : hook.events.length === 1 ? HOOK_EVENT_OPTIONS.find((option) => option.value === firstEvent)?.label ?? firstEvent : `${hook.events.length} triggers`;
}

export function scopeSummary(hook: WorkflowHook): string {
  const firstBinding = hook.bindings.at(0);
  return !firstBinding ? "Choose what this applies to" : hook.bindings.length === 1 ? `1 ${firstBinding.kind === "session" ? "chat or session" : firstBinding.kind}` : `${hook.bindings.length} scopes`;
}

export function filterSummary(hook: WorkflowHook): string {
  return hook.conditions.length === 0 ? "Every time" : hook.conditions.length === 1 ? "1 filter" : `${hook.conditions.length} filters`;
}

export function decisionSummary(hook: WorkflowHook): string {
  return ({ allow: "Guide (let it continue)", block: "Stop the action", modify: "Modify the action", require: "Require an outcome" })[hook.decision];
}

export function failModeSummary(hook: WorkflowHook): string {
  return ({ open: "Continue", closed: "Stop", warn: "Continue and log" })[hook.fail_mode];
}

export function stepSummary(hook: WorkflowHook, step: HookStepKey): string {
  const firstAction = hook.actions.at(0);
  if (step === "when") return `${triggerSummary(hook)} · ${scopeSummary(hook)} · ${filterSummary(hook)}`;
  if (step === "guide") return decisionSummary(hook);
  return !firstAction ? "Choose an action" : hook.actions.length === 1 ? ACTION_CONFIGURATION[firstAction.type]?.label ?? firstAction.label : `${hook.actions.length} actions`;
}

export function describeHook(hook: WorkflowHook): string {
  const trigger = eventPhrase(hook.events[0]);
  const scope = hook.bindings.length === 0 ? "the selected work" : hook.bindings.some((binding) => binding.kind === "workspace") ? "this workspace" : hook.bindings.map((binding) => binding.kind === "session" ? "chat or session" : binding.kind).join(" or ");
  const condition = hook.conditions.length === 0 ? "every time" : `when ${hook.conditions.length === 1 ? "its condition matches" : `${hook.conditions.length} conditions match`}`;
  const decision = ({ allow: "let it continue with guidance", block: "stop it", modify: "modify it", require: "require the stated outcome" })[hook.decision];
  const actions = hook.actions.length === 0 ? "take no follow-up action" : hook.actions.map((action) => {
    const label = (ACTION_CONFIGURATION[action.type]?.label ?? action.label).toLowerCase();
    return label === "notify member" ? "notify a member" : label;
  }).join(" and ");
  return `This hook runs ${trigger} for ${scope}, ${condition}, will ${decision}, and will ${actions}.`;
}

function templateHook(patch: Partial<WorkflowHook>): WorkflowHook {
  return { ...createHookDraft(), ...patch };
}

export const HOOK_TEMPLATES: ReadonlyArray<{ id: string; title: string; description: string; hook: WorkflowHook }> = [
  { id: "scratch", title: "Start from scratch", description: "Build a hook one step at a time.", hook: createHookDraft() },
  { id: "require-continuation", title: "Require a continuation before completion", description: "Stop tasks from ending without a clear next step.", hook: templateHook({ name: "Require a continuation", description: "Protects work from ending without a registered continuation.", events: ["before.task.complete"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "continuation", operator: "eq", value: "false" }], decision: "block", requirement: "Register a valid continuation before completing the task.", actions: [{ type: "audit.record", label: "Record audit event", config: { event: "continuation_required" } }] }) },
  { id: "notify-task-failure", title: "Notify someone when a task fails", description: "Send an actionable notification after a task failure.", hook: templateHook({ name: "Notify on task failure", events: ["on.task.failure"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "attempt", operator: "gte", value: "1" }], decision: "allow", actions: [{ type: "member.notify", label: "Notify member", config: { title: "Task failed", message: "Review the failed task and choose the next action." } }] }) },
  { id: "wakeup-failure", title: "Record wakeup failures", description: "Keep a visible audit trail when a wakeup cannot fire.", hook: templateHook({ name: "Record wakeup failures", events: ["on.wakeup.fire_failure"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "attempt", operator: "gte", value: "1" }], decision: "allow", actions: [{ type: "audit.record", label: "Record audit event", config: { event: "wakeup_fire_failed" } }] }) },
  { id: "judge-completion", title: "Judge work before completion", description: "Require a judge agent to check a written rubric.", hook: templateHook({ name: "Judge completion", events: ["before.task.complete"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "issue.status", operator: "eq", value: "in_review" }], decision: "require", requirement: "The judge must approve the completion evidence.", actions: [{ type: "judge.gate", label: "Judge gate", config: { rubric: "Approve only when the outcome and verification evidence are complete." } }] }) },
  { id: "handoff-stalled", title: "Handoff stalled work", description: "Pass repeatedly failing work to a fresh agent session.", hook: templateHook({ name: "Handoff stalled work", events: ["on.task.failure"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "attempt", operator: "gte", value: "2" }], decision: "require", requirement: "Continue in a fresh session with full state.", actions: [{ type: "session.handoff", label: "Start Handoff", config: { start_new: true, summary: "The task is stalled after repeated failures.", done: "Preserve completed work and evidence.", remaining: "Diagnose the recurring failure and continue.", max_depth: 2 } }] }) },
  { id: "think-before-comment", title: "Think before an agent comments", description: "Before a message is sent, a judge confirms the agent considered a wakeup, a tag, or a handoff.", hook: templateHook({ name: "Think before comment", description: "Requires a judge to confirm the agent thought about next steps before sending a message.", events: ["before.message.send"], bindings: [{ kind: "workspace", value: "" }], conditions: [], decision: "require", requirement: "Before sending, confirm the agent considered whether to set a wakeup, tag a person, or hand off immediately.", actions: [{ type: "judge.gate", label: "Judge gate", config: { rubric: "Approve the message only if it shows the agent considered: (1) whether a wakeup should be scheduled before waiting on anything, (2) whether a person or agent should be tagged or looped in, and (3) whether an immediate handoff would be better than commenting. Otherwise return what the agent must address first." } }] }) },
];
