import { createHookDraft, HOOK_EVENT_OPTIONS, HOOK_EVENT_TARGET_LABELS, type WorkflowHook } from "./hook-types";
import { validateHook, type HookStepKey } from "./hook-validation";
import { ACTION_CONFIGURATION, HOOK_ACTION_OPTIONS } from "./hook-action-config";
import { HOOK_FIELD_CATALOG } from "./hook-field-catalog.generated";

export { ACTION_CONFIGURATION, HOOK_ACTION_OPTIONS };
export type { ActionDefinition, ActionFieldDefinition } from "./hook-action-config";
export interface HookFieldDefinition {
  label: string;
  input: "text" | "number" | "boolean" | "select";
  options?: ReadonlyArray<{ value: string; label: string }>;
  sensitive?: boolean;
  known?: boolean;
}

// Field definitions come from the server-owned field manifest (via the
// generated catalog), so labels, input widgets, and sensitivity follow what
// the gate for an event actually supplies. Fields the editor does not know
// keep a generated label and are flagged unknown — honesty, not redaction.
interface CatalogField {
  readonly path: string;
  readonly label: string;
  readonly input: HookFieldDefinition["input"];
  readonly options?: ReadonlyArray<{ readonly value: string; readonly label: string }>;
  readonly sensitive?: boolean;
}
// The generated catalog is `as const`, which makes flatMap over its literal
// tuple types un-unifiable — read it through a loose shape once, then map.
type RawCatalog = { readonly common: readonly unknown[]; readonly events: ReadonlyArray<{ readonly fields: readonly unknown[] }> };
const rawCatalog = HOOK_FIELD_CATALOG as unknown as RawCatalog;
const catalogFields = [...rawCatalog.common, ...rawCatalog.events.flatMap((event) => event.fields)] as readonly CatalogField[];
const FIELD_DEFINITIONS: Record<string, HookFieldDefinition> = Object.fromEntries(
  catalogFields.map((field) => [field.path, { label: field.label, input: field.input, options: field.options, sensitive: field.sensitive, known: true as const }]),
);

export interface HookSummaryTarget { value: string; label: string }
export type HookSummaryDirectory = Partial<Record<"agent" | "member" | "model" | "issue" | "project" | "workflow" | "session" | "squad" | "skill" | "artifact" | "eval", readonly HookSummaryTarget[]>>;

export function fieldDefinition(field: string): HookFieldDefinition {
  return FIELD_DEFINITIONS[field] ?? {
    label: field.split(".").map((part) => part.replaceAll("_", " ")).join(" · ").replace(/^./, (value) => value.toUpperCase()),
    input: "text",
    known: false,
  };
}

export function triggerSummary(hook: WorkflowHook): string {
  const firstEvent = hook.events.at(0);
  return !firstEvent ? "Choose a trigger" : hook.events.length === 1 ? HOOK_EVENT_OPTIONS.find((option) => option.value === firstEvent)?.label ?? firstEvent : `${hook.events.length} triggers`;
}

function resolvedTarget(directory: HookSummaryDirectory, kind: keyof HookSummaryDirectory, value: string): string {
  return directory[kind]?.find((option) => option.value === value)?.label ?? "Unknown target";
}

export function scopeSummary(hook: WorkflowHook, directory: HookSummaryDirectory = {}): string {
  if (hook.bindings.length === 0) return "Choose what this applies to";
  return hook.bindings.map((binding) => binding.kind === "workspace" ? "This workspace" : resolvedTarget(directory, binding.kind, binding.value)).join(" or ");
}

export function filterSummary(hook: WorkflowHook): string {
  if (hook.conditions.length === 0) return "Every time";
  const mode = hook.condition_mode === "any" ? "Any" : "All";
  return `${mode}: ${hook.conditions.map(conditionSummary).join("; ")}`;
}

export function decisionSummary(hook: WorkflowHook): string {
  return ({ allow: "Guide (let it continue)", block: "Stop the action", modify: "Modify the action", require: "Require an outcome" })[hook.decision];
}

export function failModeSummary(hook: WorkflowHook): string {
  return ({ open: "Continue", closed: "Stop", warn: "Continue and log" })[hook.fail_mode];
}

export function stepSummary(hook: WorkflowHook, step: HookStepKey, directory: HookSummaryDirectory = {}): string {
  if (step === "when") return `${triggerSummary(hook)} · ${scopeSummary(hook, directory)} · ${filterSummary(hook)}`;
  if (step === "guide") return decisionSummary(hook);
  return hook.actions.length === 0 ? "Choose an action" : hook.actions.map((action) => actionSummary(action, directory)).join("; ");
}

export type HookStateFilter = "all" | "enforce" | "dry_run" | "off" | "managed";

export const HOOK_STATE_FILTERS: ReadonlyArray<{ value: HookStateFilter; label: string }> = [
  { value: "all", label: "All" },
  { value: "enforce", label: "Enforced" },
  { value: "dry_run", label: "Dry run" },
  { value: "off", label: "Off" },
  { value: "managed", label: "Managed" },
];

// One vocabulary, used by the list badge, the legend, and the editor header.
// Every word here answers the only question a reader has: does this stop or
// change my run right now? Nothing else may invent its own phrasing.
export const HOOK_STATE_MEANING: Record<Exclude<HookStateFilter, "all">, string> = {
  enforce: "Live. It runs on every matching event and can stop, change, or start work.",
  dry_run: "Watching only. It sees real events and records what it would have done, and it never stops, changes, or starts anything.",
  off: "Not running. It stays saved, and nothing reaches it.",
  managed: "Live and owned by the workspace. It runs like Enforced, and it cannot be edited or published here.",
};

// A hook that is live can still carry an unpublished edit. The edit is what the
// editor shows, so the list must say so — otherwise a red draft error reads as
// "this hook is broken" when the live version is enforcing perfectly.
export interface HookDraftState {
  present: boolean;
  publishable: boolean;
  blocker?: string;
  overLive: boolean;
}

export function hookDraftState(hook: WorkflowHook): HookDraftState {
  const present = Boolean(hook.lifecycle.draft_id) || (!hook.family_id && hook.mode === "dry_run");
  if (!present) return { present: false, publishable: true, overLive: false };
  const validation = validateHook(hook);
  return { present: true, publishable: validation.valid, blocker: validation.message, overLive: Boolean(hook.live) };
}

// What the hook does to a run right now — the LIVE decision when an unpublished
// draft is sitting on top of it, never the draft's.
export function enforcedDecisionSummary(hook: WorkflowHook): string {
  return hook.live ? decisionSummary({ ...hook, decision: hook.live.decision }) : decisionSummary(hook);
}

export interface HookFilterState {
  query: string;
  state: HookStateFilter;
  trigger: string;
  decision: string;
  scope: string;
  action: string;
  attention: boolean;
}

export const EMPTY_HOOK_FILTERS: HookFilterState = { query: "", state: "all", trigger: "all", decision: "all", scope: "all", action: "all", attention: false };

export interface HookFilterOption { value: string; label: string }
export interface HookFilterOptions {
  trigger: HookFilterOption[];
  decision: HookFilterOption[];
  scope: HookFilterOption[];
  action: HookFilterOption[];
}

// Options are derived from the hooks on screen, so every value that exists is
// filterable and no filter ever offers an empty result.
export function hookFilterOptions(hooks: readonly WorkflowHook[]): HookFilterOptions {
  const collect = (values: Iterable<[string, string]>): HookFilterOption[] =>
    [...new Map(values)].map(([value, label]) => ({ value, label })).sort((left, right) => left.label.localeCompare(right.label));
  return {
    trigger: collect(hooks.flatMap((hook) => hook.events.map((event): [string, string] => [event, HOOK_EVENT_OPTIONS.find((option) => option.value === event)?.label ?? event]))),
    decision: collect(hooks.map((hook): [string, string] => [hook.live?.decision ?? hook.decision, enforcedDecisionSummary(hook)])),
    scope: collect(hooks.flatMap((hook) => hook.bindings.map((binding): [string, string] => [binding.kind, binding.kind === "workspace" ? "This workspace" : binding.kind.replace(/^./, (letter) => letter.toUpperCase())]))),
    action: collect(hooks.flatMap((hook) => hook.actions.map((action): [string, string] => [action.type, ACTION_CONFIGURATION[action.type]?.label ?? action.type]))),
  };
}

export function filterHooks(hooks: readonly WorkflowHook[], filters: HookFilterState, directory: HookSummaryDirectory = {}): WorkflowHook[] {
  return hooks.filter((hook) => {
    if (filters.state !== "all" && hookStateKey(hook) !== filters.state) return false;
    if (filters.trigger !== "all" && !hook.events.includes(filters.trigger as WorkflowHook["events"][number])) return false;
    if (filters.decision !== "all" && (hook.live?.decision ?? hook.decision) !== filters.decision) return false;
    if (filters.scope !== "all" && !hook.bindings.some((binding) => binding.kind === filters.scope)) return false;
    if (filters.action !== "all" && !hook.actions.some((action) => action.type === filters.action)) return false;
    if (filters.attention && hookDraftState(hook).publishable) return false;
    return hookMatchesQuery(hook, filters.query, directory);
  });
}

// The list groups a hook by what it actually does to a run right now, not by
// its lifecycle wording: "Enforced · Draft changes" still enforces.
export function hookStateKey(hook: WorkflowHook): Exclude<HookStateFilter, "all"> {
  const state = hook.family_id ? hook.lifecycle.state : hook.mode;
  if (state === "managed") return "managed";
  if (state === "dry_run" || state === "draft") return "dry_run";
  if (state === "off" || state === "off_with_draft") return "off";
  return "enforce";
}

// Search covers everything the row shows — including the generated summary —
// so "the hook that comments on my issue" is findable without opening each one.
export function hookMatchesQuery(hook: WorkflowHook, query: string, directory: HookSummaryDirectory = {}): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") return true;
  return [hook.name, hook.contract_rule, hook.contract_satisfy, hook.description, describeHook(hook, directory)]
    .some((field) => (field ?? "").toLowerCase().includes(needle));
}

export function describeHook(hook: WorkflowHook, directory: HookSummaryDirectory = {}): string {
  const conditions = hook.conditions.length === 0
    ? "if no conditions"
    : `if ${hook.condition_mode === "any" ? "any" : "all"} ${hook.conditions.map(conditionSummary).join("; ")}`;
  const actions = hook.actions.length === 0 ? "No follow-up action" : hook.actions.map((action) => actionSummary(action, directory)).join("; ");
  return `When ${triggerSummary(hook)} for ${scopeSummary(hook, directory)}, ${conditions}, ${decisionSummary(hook)}, then ${actions}.`;
}

const conditionOperators: Record<string, string> = {
  eq: "is",
  not_in: "is not one of",
  exists: "exists",
  not_exists: "does not exist",
  starts_with: "starts with",
  gte: "is at least",
  lt: "is below",
};

function conditionSummary(condition: WorkflowHook["conditions"][number]): string {
  const definition = fieldDefinition(condition.field);
  const operator = conditionOperators[condition.operator] ?? condition.operator;
  const unknown = definition.known === false ? " (unknown field)" : "";
  if (condition.operator === "exists" || condition.operator === "not_exists") return `${definition.label} ${operator}${unknown}`;
  // Known fields follow the manifest's sensitivity. Unknown fields fall back
  // to the conservative free-text pattern — they are marked unknown, never
  // silently redacted the way `<redacted>` used to borrow security semantics
  // for fields the editor simply did not recognise (FIR-4797, FIR-4933).
  const sensitive = definition.known ? Boolean(definition.sensitive) : /(?:^|\.)(?:body|content|id|prompt|reason|rubric|secret|token)(?:$|\.)/i.test(condition.field);
  const value = sensitive ? "<redacted>" : humanConditionValue(condition.value, definition);
  return `${definition.label} ${operator} ${value}${unknown}`;
}

function humanConditionValue(value: string, definition: HookFieldDefinition): string {
  if (definition.input === "boolean") return value === "true" ? "Yes" : value === "false" ? "No" : value;
  if (definition.options) {
    return value.split(",").map((item) => definition.options?.find((option) => option.value === item.trim())?.label ?? item.trim()).join(", ");
  }
  return value;
}

function actionSummary(action: WorkflowHook["actions"][number], directory: HookSummaryDirectory): string {
  const definition = ACTION_CONFIGURATION[action.type];
  if (!definition) return action.label || "Unknown action";
  const details = definition.fields.flatMap((field) => {
    const value = action.config[field.key];
    if (value === undefined || value === null || value === "" || field.summary === "omit") return [];
    if (field.summary === "redacted") return [`${field.label}: <redacted>`];
    if (field.summary === "target") return [`${field.label}: ${HOOK_EVENT_TARGET_LABELS[String(value)] ?? resolvedTarget(directory, field.target ?? "agent", String(value))}`];
    const optionLabel = field.options?.find((option) => option.value === String(value))?.label;
    const displayed = field.input === "checkbox" ? value === true ? "Yes" : "No" : optionLabel ?? String(value);
    return [`${field.label}: ${displayed}`];
  });
  return details.length > 0 ? `${definition.label} — ${details.join("; ")}` : definition.label;
}

function templateHook(patch: Partial<WorkflowHook>): WorkflowHook {
  return {
    ...createHookDraft(),
    contract_rule: patch.description ?? patch.name ?? "",
    contract_satisfy: patch.requirement ?? "Complete the configured actions.",
    ...patch,
  };
}

export const HOOK_TEMPLATES: ReadonlyArray<{ id: string; title: string; description: string; hook: WorkflowHook }> = [
  { id: "scratch", title: "Start from scratch", description: "Build a hook one step at a time.", hook: createHookDraft() },
  { id: "require-continuation", title: "Require a continuation before completion", description: "Stop tasks from ending without a clear next step.", hook: templateHook({ name: "Require a continuation", description: "Protects work from ending without a registered continuation.", events: ["before.task.complete"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "continuation.present", operator: "eq", value: "false" }], decision: "block", requirement: "Register a valid continuation before completing the task.", actions: [{ type: "audit.record", label: "Record audit event", config: { event: "continuation_required" } }] }) },
  // The attempt cap is load-bearing: without it a failing retry re-triggers the
  // same hook and the pair loops. Keep a condition on every recipe that starts
  // work in response to a failure.
  { id: "instruct-retry", title: "Tell the agent to try again instead of stopping it", description: "The run continues; on the first failure only, the same agent is started again with an instruction naming what to fix.", hook: templateHook({ name: "Instruct instead of stop", description: "Guides the agent that triggered the hook instead of blocking its work.", contract_rule: "A first failed attempt must be retried with the reason stated, not silently stopped.", contract_satisfy: "Read the instruction, fix what it names, and finish the work.", events: ["on.task.failure"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "attempt", operator: "lt", value: "2" }], decision: "allow", actions: [{ type: "agent.dispatch", label: "Instruct an agent", config: { agent_id: "event.agent", prompt: "Your last attempt stopped early. Read the failure reason on this issue, fix what it names, and continue the work." } }] }) },
  { id: "notify-task-failure", title: "Notify someone when a task fails", description: "Send an actionable notification after a task failure.", hook: templateHook({ name: "Notify on task failure", events: ["on.task.failure"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "attempt", operator: "gte", value: "1" }], decision: "allow", actions: [{ type: "member.notify", label: "Notify member", config: { title: "Task failed", message: "Review the failed task and choose the next action." } }] }) },
  { id: "no-silent-failure", title: "No silent task failures", description: "When a run exhausts its retries, post the reason and next step on the issue and mark it blocked.", hook: templateHook({ name: "No silent task failures", description: "Guarantees a failed run leaves a visible trail: a comment with the failure reason and next step, and a blocked status so the issue cannot die quietly.", events: ["on.task.failure"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "failure.attempt", operator: "gte", value: "$event.failure.max_attempts" }], decision: "allow", actions: [{ type: "issue.comment", label: "Comment on issue", config: { body: "This run stopped before it could finish (reason: {{task.failure_reason}}, attempt {{task.attempt}} of {{task.max_attempts}}).\n\nNext step: review the run log for this task, then restart the work or reassign it. The issue is marked blocked until someone acts." } }, { type: "issue.status", label: "Set issue status", config: { status: "blocked" } }] }) },
  { id: "wakeup-failure", title: "Record wakeup failures", description: "Keep a visible audit trail when a wakeup cannot fire.", hook: templateHook({ name: "Record wakeup failures", events: ["on.wakeup.fire_failure"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "attempt", operator: "gte", value: "1" }], decision: "allow", actions: [{ type: "audit.record", label: "Record audit event", config: { event: "wakeup_fire_failed" } }] }) },
  { id: "judge-completion", title: "Judge work before completion", description: "Require a judge agent to check a written rubric.", hook: templateHook({ name: "Judge completion", events: ["before.task.complete"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "issue.status", operator: "eq", value: "in_review" }], decision: "require", requirement: "The judge must approve the completion evidence.", actions: [{ type: "judge.gate", label: "Judge gate", config: { rubric: "Approve only when the outcome and verification evidence are complete." } }] }) },
  { id: "handoff-stalled", title: "Handoff stalled work", description: "Pass repeatedly failing work to a fresh agent session.", hook: templateHook({ name: "Handoff stalled work", events: ["on.task.failure"], bindings: [{ kind: "workspace", value: "" }], conditions: [{ field: "attempt", operator: "gte", value: "2" }], decision: "require", requirement: "Continue in a fresh session with full state.", actions: [{ type: "session.handoff", label: "Start Handoff", config: { start_new: true, summary: "The task is stalled after repeated failures.", done: "Preserve completed work and evidence.", remaining: "Diagnose the recurring failure and continue.", max_depth: 2 } }] }) },
  { id: "think-before-comment", title: "Think before an agent comments", description: "Before a message is sent, a judge confirms the agent considered a wakeup, a tag, or a handoff.", hook: templateHook({ name: "Think before comment", description: "Requires a judge to confirm the agent thought about next steps before sending a message.", events: ["before.message.send"], bindings: [{ kind: "workspace", value: "" }], conditions: [], decision: "require", requirement: "Before sending, confirm the agent considered whether to set a wakeup, tag a person, or hand off immediately.", actions: [{ type: "judge.gate", label: "Judge gate", config: { rubric: "Approve the message only if it shows the agent considered: (1) whether a wakeup should be scheduled before waiting on anything, (2) whether a person or agent should be tagged or looped in, and (3) whether an immediate handoff would be better than commenting. Otherwise return what the agent must address first." } }] }) },
];
