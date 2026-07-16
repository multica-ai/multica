import { api, parseWithFallback } from "@multica/core/api";
import { z } from "zod";
import { createHookDraft, type HookActionDraft, type HookEventType, type HookRun, type WorkflowHook } from "./hook-types";

const hookBindingSchema = z.object({ kind: z.enum(["workspace", "project", "workflow", "agent", "model", "issue", "session"]), id: z.string() });
const hookConditionSchema = z.object({ field: z.string(), op: z.string(), value: z.unknown().optional(), values: z.array(z.unknown()).optional() });
const hookActionSchema = z.object({ type: z.string(), config: z.record(z.string(), z.unknown()).optional().default({}) });
const hookHandlerSchema = z.object({ id: z.string().optional().default(""), decision: z.enum(["allow", "block", "modify", "require"]), requirement: z.string().optional().default(""), actions: z.array(hookActionSchema).optional().default([]) });
const hookPolicySchema = z.object({
  id: z.string().optional(), version: z.number().int().positive(), name: z.string(), description: z.string().optional().default(""),
  mode: z.enum(["off", "dry_run", "enforce", "managed"]), fail_mode: z.enum(["open", "closed", "warn"]),
  events: z.array(z.string()), bindings: z.array(hookBindingSchema), conditions: z.array(hookConditionSchema).optional().default([]), handlers: z.array(hookHandlerSchema),
  observed_run_count: z.number().int().nonnegative().optional().default(0), can_publish: z.boolean().optional().default(false), updated_at: z.string().optional(),
});
const hookListSchema = z.object({ hooks: z.array(hookPolicySchema) });

type HookPolicyTransport = z.infer<typeof hookPolicySchema>;
export interface HookWriteTransport {
  name: string; description: string; mode: "dry_run"; fail_mode: WorkflowHook["fail_mode"];
  events: HookEventType[]; bindings: Array<{ kind: WorkflowHook["bindings"][number]["kind"]; id: string }>;
  conditions: Array<{ field: string; op: string; value?: string }>;
  handlers: Array<{ id: string; decision: WorkflowHook["decision"]; requirement: string; actions: HookActionDraft[] }>;
}

function fromTransport(policy: HookPolicyTransport): WorkflowHook {
	const fallback = createHookDraft();
	const handler = policy.handlers[0] ?? { id: "", decision: fallback.decision, requirement: fallback.requirement, actions: [] };
  return {
    id: policy.id, version: policy.version, name: policy.name, description: policy.description,
		mode: policy.mode, fail_mode: policy.fail_mode,
    events: policy.events.filter((event): event is HookEventType => typeof event === "string") as HookEventType[],
    bindings: policy.bindings.map((binding) => ({ kind: binding.kind, value: binding.id })),
    conditions: policy.conditions.map((condition, index) => ({ field: condition.field, operator: condition.op, value: condition.values?.map(String).join(", ") ?? (condition.value === undefined ? "" : String(condition.value)), conjunction: index < policy.conditions.length - 1 ? "AND" : undefined })),
    decision: handler.decision, requirement: handler.requirement,
    actions: handler.actions.map((action) => ({ type: action.type, label: humaniseAction(action.type), config: action.config })),
    baseline_run_count: policy.observed_run_count,
	can_publish: policy.can_publish,
    last_run_at: policy.updated_at,
  };
}

export function parseHookResponse(raw: unknown): WorkflowHook {
  const parsed = parseWithFallback(raw, hookPolicySchema, null, { endpoint: "workflowHook" });
  return parsed ? fromTransport(parsed) : createHookDraft();
}

export function parseHookListResponse(raw: unknown): WorkflowHook[] {
  const parsed = parseWithFallback(raw, hookListSchema, { hooks: [] }, { endpoint: "workflowHooks" });
  return parsed.hooks.map(fromTransport);
}

export function toHookTransport(hook: WorkflowHook): HookWriteTransport {
  return {
    name: hook.name, description: hook.description, mode: "dry_run", fail_mode: hook.fail_mode,
    events: hook.events, bindings: hook.bindings.map((binding) => ({ kind: binding.kind, id: binding.value })),
    conditions: hook.conditions.map((condition) => ({ field: condition.field, op: condition.operator, ...(condition.value === "" ? {} : { value: condition.value }) })),
    handlers: [{ id: "primary", decision: hook.decision, requirement: hook.requirement, actions: hook.actions }],
  };
}

export async function fetchWorkflowHooks(): Promise<WorkflowHook[]> {
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
export async function testWorkflowHook(id: string, event: Record<string, unknown>): Promise<{ side_effects: false; result: unknown }> {
  return api.cerebroRequest(`/api/cerebro/workflow-hooks/${encodeURIComponent(id)}/test`, { method: "POST", body: JSON.stringify(event) });
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
    }];
  });
}

function asObject(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function humaniseAction(type: string) { return type.split(".").map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(" "); }
