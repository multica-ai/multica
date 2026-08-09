import { api, parseWithFallback } from "@multica/core/api";
import { z } from "zod";
import { createHookDraft, type HookActionDraft, type HookEventType, type HookRun, type WorkflowHook } from "./hook-types";

const hookBindingSchema = z.object({ kind: z.enum(["workspace", "project", "workflow", "agent", "model", "issue", "session"]), id: z.string() });
const hookConditionSchema = z.object({ field: z.string(), op: z.string(), value: z.unknown().optional(), values: z.array(z.unknown()).optional() });
const hookActionSchema = z.object({ type: z.string(), config: z.record(z.string(), z.unknown()).optional().default({}) });
const hookHandlerSchema = z.object({ id: z.string().optional().default(""), decision: z.enum(["allow", "block", "modify", "require"]), requirement: z.string().optional().default(""), actions: z.array(hookActionSchema).optional().default([]) });
const hookLifecycleSchema = z.object({
  state: z.enum(["draft", "live", "live_with_draft", "off", "off_with_draft", "managed"]),
  live_policy_id: z.string().optional(),
  live_version: z.number().int().positive().optional(),
  draft_id: z.string().optional(),
  draft_series_id: z.string().optional(),
  draft_revision: z.number().int().positive().optional(),
  live_unchanged_by_draft: z.boolean().optional().default(false),
});
const hookJournalEventSchema = z.object({
  id: z.string(),
  event_id: z.string(),
  event_type: z.string(),
  schema_version: z.number().int(),
  occurred_at: z.string(),
  expires_at: z.string(),
});
const hookPolicySchema = z.object({
  id: z.string().optional(), family_id: z.string().optional(), draft_series_id: z.string().optional(), revision: z.number().int().positive().optional(),
  version: z.number().int().positive(), name: z.string(), description: z.string().optional().default(""),
  mode: z.enum(["off", "dry_run", "enforce", "managed"]), fail_mode: z.enum(["open", "closed", "warn"]),
  events: z.array(z.string()), bindings: z.array(hookBindingSchema), condition_mode: z.enum(["all", "any"]).optional().default("all"), conditions: z.array(hookConditionSchema).optional().default([]), handlers: z.array(hookHandlerSchema),
  observed_run_count: z.number().int().nonnegative().optional().default(0), can_publish: z.boolean().optional().default(false), updated_at: z.string().optional(), last_run_at: z.string().optional(),
  lifecycle: hookLifecycleSchema.optional(),
  compatible_events: z.array(hookJournalEventSchema).optional().default([]),
});
const hookPartialErrorSchema = z.object({
  record_id: z.string(),
  code: z.literal("hook_record_malformed"),
  request_id: z.string().optional(),
});
const hookListSchema = z.object({
  hooks: z.array(z.unknown()),
  partial_errors: z.array(hookPartialErrorSchema).optional().default([]),
});

type HookPolicyTransport = z.infer<typeof hookPolicySchema>;
export type HookPartialError = z.infer<typeof hookPartialErrorSchema>;
export interface HookListResult {
  hooks: WorkflowHook[];
  partial_errors: HookPartialError[];
}
export interface HookWriteTransport {
  name: string; description: string; mode: "dry_run"; fail_mode: WorkflowHook["fail_mode"];
  revision?: number;
  events: HookEventType[]; bindings: Array<{ kind: WorkflowHook["bindings"][number]["kind"]; id: string }>;
  condition_mode: WorkflowHook["condition_mode"];
  conditions: Array<{ field: string; op: string; value?: string; values?: string[] }>;
  handlers: Array<{ id: string; decision: WorkflowHook["decision"]; requirement: string; actions: HookActionDraft[] }>;
}

function fromTransport(policy: HookPolicyTransport): WorkflowHook {
	const fallback = createHookDraft();
	const handler = policy.handlers[0] ?? { id: "", decision: fallback.decision, requirement: fallback.requirement, actions: [] };
  return {
    id: policy.id, family_id: policy.family_id, draft_series_id: policy.draft_series_id, revision: policy.revision,
    version: policy.version, name: policy.name, description: policy.description,
		mode: policy.mode, fail_mode: policy.fail_mode === "open" ? "warn" : policy.fail_mode,
    events: policy.events.filter((event): event is HookEventType => typeof event === "string") as HookEventType[],
    bindings: policy.bindings.map((binding) => ({ kind: binding.kind, value: binding.id })),
    condition_mode: policy.condition_mode,
    conditions: policy.conditions.map((condition, index) => ({ field: condition.field, operator: condition.op, value: condition.values?.map(String).join(", ") ?? (condition.value === undefined ? "" : String(condition.value)), conjunction: index < policy.conditions.length - 1 ? "AND" : undefined })),
    decision: handler.decision, requirement: handler.requirement,
    actions: handler.actions.map((action) => ({ type: action.type, label: humaniseAction(action.type), config: action.config })),
    baseline_run_count: policy.observed_run_count,
	can_publish: policy.can_publish,
    last_run_at: policy.last_run_at,
    lifecycle: policy.lifecycle ?? legacyLifecycle(policy),
    compatible_events: policy.compatible_events.map((event) => ({ ...event, event_type: event.event_type as HookEventType })),
  };
}

export function parseHookResponse(raw: unknown): WorkflowHook {
  const parsed = parseWithFallback<HookPolicyTransport | null>(raw, hookPolicySchema, null, { endpoint: "workflowHook" });
  if (!parsed) throw new Error("Hook response is malformed");
  return fromTransport(parsed);
}

export function parseHookListResponse(raw: unknown): HookListResult {
  const parsed = parseWithFallback<z.infer<typeof hookListSchema> | null>(raw, hookListSchema, null, { endpoint: "workflowHooks" });
  if (!parsed) throw new Error("Hook list response is malformed");
  const hooks: WorkflowHook[] = [];
  const partialErrors = [...parsed.partial_errors];
  parsed.hooks.forEach((record, index) => {
    const policy = parseWithFallback<HookPolicyTransport | null>(record, hookPolicySchema, null, { endpoint: `workflowHooks[${index}]` });
    if (policy) {
      hooks.push(fromTransport(policy));
      return;
    }
    const recordID = record && typeof record === "object" && typeof (record as { id?: unknown }).id === "string"
      ? (record as { id: string }).id
      : `record-${index + 1}`;
    partialErrors.push({ record_id: recordID, code: "hook_record_malformed" });
  });
  return { hooks, partial_errors: partialErrors };
}

export function toHookTransport(hook: WorkflowHook): HookWriteTransport {
  return {
    name: hook.name, description: hook.description, mode: "dry_run", fail_mode: hook.fail_mode === "open" ? "warn" : hook.fail_mode,
    revision: hook.revision,
    events: hook.events, bindings: hook.bindings.map((binding) => ({ kind: binding.kind, id: binding.value })),
    condition_mode: hook.condition_mode,
    conditions: hook.conditions.map((condition) => {
      const base = { field: condition.field, op: condition.operator };
      if (condition.operator === "not_in") {
        return { ...base, values: condition.value.split(",").map((value) => value.trim()).filter(Boolean) };
      }
      return condition.value === "" ? base : { ...base, value: condition.value };
    }),
    handlers: [{ id: "primary", decision: hook.decision, requirement: hook.requirement, actions: hook.actions }],
  };
}

export async function fetchWorkflowHooks(): Promise<HookListResult> {
  return parseHookListResponse(await api.cerebroRequest<unknown>("/api/cerebro/workflow-hooks"));
}
export async function fetchWorkflowHook(id: string): Promise<WorkflowHook> {
  return parseHookResponse(await api.cerebroRequest<unknown>(`/api/cerebro/workflow-hooks/${encodeURIComponent(id)}`));
}
export async function createWorkflowHook(hook: WorkflowHook): Promise<WorkflowHook> {
  return parseHookResponse(await api.cerebroRequest<unknown>("/api/cerebro/workflow-hooks", { method: "POST", body: JSON.stringify(toHookTransport(hook)) }));
}
export async function updateWorkflowHook(id: string, hook: WorkflowHook): Promise<WorkflowHook> {
  return parseHookResponse(await api.cerebroRequest<unknown>(`/api/cerebro/workflow-hooks/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(toHookTransport(hook)) }));
}
export async function publishWorkflowHook(id: string): Promise<WorkflowHook> {
  return parseHookResponse(await api.cerebroRequest<unknown>(`/api/cerebro/workflow-hooks/${encodeURIComponent(id)}/publish`, { method: "POST" }));
}
export async function disableWorkflowHook(id: string): Promise<WorkflowHook> {
  return parseHookResponse(await api.cerebroRequest<unknown>(`/api/cerebro/workflow-hooks/${encodeURIComponent(id)}/disable`, { method: "POST" }));
}
export async function discardWorkflowHookDraft(id: string): Promise<WorkflowHook> {
  return parseHookResponse(await api.cerebroRequest<unknown>(`/api/cerebro/workflow-hooks/${encodeURIComponent(id)}/draft`, { method: "DELETE" }));
}
export async function deleteWorkflowHook(id: string): Promise<void> {
  await api.cerebroRequest(`/api/cerebro/workflow-hooks/${encodeURIComponent(id)}`, { method: "DELETE" });
}
export async function testWorkflowHook(id: string, eventId: string, revision: number): Promise<{ side_effects: false; tested_revision: number; retained_event_id: string; result: unknown }> {
  return api.cerebroRequest(`/api/cerebro/workflow-hooks/${encodeURIComponent(id)}/test`, {
    method: "POST",
    body: JSON.stringify({ event_id: eventId, revision }),
  });
}
export async function fetchWorkflowHookRuns(id: string): Promise<HookRun[]> {
  return parseHookRunsResponse(await api.cerebroRequest<unknown>(`/api/cerebro/workflow-hooks/${encodeURIComponent(id)}/runs`));
}

export function parseHookRunsResponse(raw: unknown): HookRun[] {
  const records = raw && typeof raw === "object" && Array.isArray((raw as { runs?: unknown }).runs)
    ? (raw as { runs: Array<Record<string, unknown>> }).runs
    : [];
  return records.flatMap((record) => {
    const event = asObject(record.event);
    const result = asObject(record.result);
    const sourceScope = asObject(record.source_scope);
    const matches = Array.isArray(result.matches) ? result.matches : [];
    const actions = Array.isArray(result.action_results) ? result.action_results : [];
    const conditions = Array.isArray(result.matched_conditions) ? result.matched_conditions : [];
    const remediation = Array.isArray(result.requirements) ? result.requirements.map(String) : [];
    const decision = String(result.would_decision || result.decision || "allow") as WorkflowHook["decision"];
    const eventType = String(event.event_type || "Unknown event");
    const scopeKind = String(sourceScope.kind || "workspace");
    const scopeID = String(sourceScope.id || "");
    return [{
      id: String(record.id || event.event_id || ""),
      created_at: String(record.created_at || ""),
      policy_id: String(record.policy_id || ""),
      policy_version: Number(record.policy_version || 0),
      source_scope: { kind: scopeKind, id: scopeID },
      matched_conditions: conditions.map((condition) => {
        const item = asObject(condition);
        const value = item.values ?? item.value;
        const rendered = Array.isArray(value) ? value.join(", ") : String(value ?? "");
        return [item.field, item.op, rendered].filter(Boolean).join(" ");
      }),
      source: `${eventType} · ${scopeKind}${scopeID ? ` ${scopeID}` : ""}`,
      matched_steps: matches.length > 0 ? ["Trigger", "Scope", "Filter", "Decision", ...(actions.length > 0 ? ["Action"] : [])] : [],
      decision,
      would_action: actions.length > 0 ? actions.map((action) => String(asObject(action).type || "action")).join(", ") : String((Array.isArray(result.requirements) ? result.requirements[0] : "No action") || "No action"),
      fail_mode: (["open", "closed", "warn"].includes(String(record.fail_mode)) ? String(record.fail_mode) : "open") as WorkflowHook["fail_mode"],
      remediation,
      side_effects: false as const,
      latency_ms: Number(record.latency_ms || 0),
      event,
    }];
  });
}

function asObject(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function humaniseAction(type: string) { return type.split(".").map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(" "); }

function legacyLifecycle(policy: HookPolicyTransport): WorkflowHook["lifecycle"] {
  const state = policy.mode === "dry_run" ? "draft" : policy.mode === "off" ? "off" : policy.mode === "managed" ? "managed" : "live";
  return {
    state,
    live_policy_id: policy.mode === "dry_run" ? undefined : policy.id,
    live_version: policy.mode === "dry_run" ? undefined : policy.version,
    draft_id: policy.mode === "dry_run" ? policy.id : undefined,
    draft_revision: policy.mode === "dry_run" ? policy.revision : undefined,
    live_unchanged_by_draft: false,
  };
}
