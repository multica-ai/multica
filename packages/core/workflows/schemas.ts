import { z } from "zod";
import { parseWithFallback } from "../api/schema";
import type { WorkflowGraph, WorkflowCapabilities } from "../types/workflow";

const optionalText = z.string().nullish().transform((value) => value ?? undefined);
const nodeType = z.string().min(1);
const knownNodeWireFields = new Set(["id", "type", "label", "position", "agent_id", "instructions", "input_refs", "assignee", "config", "inputs", "outputs", "on_failure", "on_timeout", "max_retries"]);
function unknownNodeFields(node: Record<string, unknown>): Record<string, unknown> | undefined {
  const unknown = Object.fromEntries(Object.entries(node).filter(([key]) => !knownNodeWireFields.has(key)));
  return Object.keys(unknown).length ? unknown : undefined;
}
const workflowDefaultsSchema = z.object({
  retry: z.object({
    max_retries: z.number().int().optional(),
    initial_delay_seconds: z.number().int().optional(),
    backoff_multiplier: z.number().int().optional(),
    max_delay_seconds: z.number().int().optional(),
  }).optional(),
  max_retries: z.number().int().optional(),
  initial_delay_seconds: z.number().int().optional(),
  backoff_multiplier: z.number().int().optional(),
  max_delay_seconds: z.number().int().optional(),
  execution_timeout_seconds: z.number().int().optional(),
  queue_timeout_seconds: z.number().int().optional(),
  human_timeout_seconds: z.number().int().optional(),
  run_timeout_seconds: z.number().int().optional(),
  max_dispatches: z.number().int().optional(),
  max_active_execution_seconds: z.number().int().optional(),
}).transform((defaults) => ({
  maxRetries: defaults.max_retries ?? defaults.retry?.max_retries,
  initialDelaySeconds: defaults.initial_delay_seconds ?? defaults.retry?.initial_delay_seconds,
  backoffMultiplier: defaults.backoff_multiplier ?? defaults.retry?.backoff_multiplier,
  maxDelaySeconds: defaults.max_delay_seconds ?? defaults.retry?.max_delay_seconds,
  executionTimeoutSeconds: defaults.execution_timeout_seconds,
  queueTimeoutSeconds: defaults.queue_timeout_seconds,
  humanTimeoutSeconds: defaults.human_timeout_seconds,
  runTimeoutSeconds: defaults.run_timeout_seconds,
  maxDispatches: defaults.max_dispatches,
  maxActiveExecutionSeconds: defaults.max_active_execution_seconds,
})).optional();

const graphSchema = z.object({
  schema_version: z.number().int().positive().optional(),
  defaults: workflowDefaultsSchema,
  scopes: z.array(z.object({
    id: z.string(), type: z.literal("rework"), entry_node_id: z.string().optional(),
    exit_node_id: z.string().optional(), max_reworks: z.number().int().nonnegative().optional(),
  })).optional(),
  nodes: z.array(
    z.object({
      id: z.string().min(1),
      type: nodeType,
      label: z.string(),
      position: z.object({ x: z.number().finite(), y: z.number().finite() }),
      agent_id: optionalText,
      instructions: optionalText,
      input_refs: z.array(z.string()).nullish().transform((value) => value ?? []),
      assignee: z.object({ type: z.enum(["agent", "member"]), id: z.string().min(1) }).optional(),
      config: z.record(z.string(), z.unknown()).optional(),
      inputs: z.record(z.string(), z.unknown()).optional(),
      outputs: z.array(z.object({ key: z.string(), label: z.string().optional(), type: z.string().optional(), required: z.boolean().optional() })).optional(),
      on_failure: z.union([z.enum(["block", "route", "takeover"]), z.object({ action: z.enum(["block", "route", "takeover"]), assignee: z.object({ type: z.enum(["agent", "member"]), id: z.string() }).optional() })]).optional(),
      on_timeout: z.union([z.enum(["block", "route", "takeover"]), z.object({ action: z.enum(["block", "route", "takeover"]), assignee: z.object({ type: z.enum(["agent", "member"]), id: z.string() }).optional() })]).optional(),
      max_retries: z.number().int().nonnegative().optional(),
    }).passthrough().transform((node) => ({
      id: node.id, type: node.type, label: node.label, position: node.position,
      agentId: node.agent_id, instructions: node.instructions, inputRefs: node.input_refs,
      assignee: node.assignee, config: node.config, inputs: node.inputs, outputs: node.outputs,
      onFailure: node.on_failure, onTimeout: node.on_timeout, maxRetries: node.max_retries,
      unknownFields: unknownNodeFields(node),
    })),
  ),
  edges: z.array(z.object({
    id: z.string().min(1), source: z.string(), target: z.string(),
    kind: z.enum(["flow", "rework", "compensation"]).optional(),
    scope_id: z.string().optional(),
    source_port: z.string().optional(), target_port: z.string().optional(), label: z.string().optional(),
  }).transform((edge) => ({
    id: edge.id, source: edge.source, target: edge.target, kind: edge.kind,
    scopeId: edge.scope_id, sourcePort: edge.source_port, targetPort: edge.target_port, label: edge.label,
  }))),
}).transform((graph) => ({
  schemaVersion: graph.schema_version,
  defaults: graph.defaults,
  scopes: graph.scopes?.map((scope) => ({
    id: scope.id, type: scope.type, entryNodeId: scope.entry_node_id,
    exitNodeId: scope.exit_node_id, maxReworks: scope.max_reworks,
  })),
  nodes: graph.nodes,
  edges: graph.edges,
}));

export const WorkflowGraphSchema = graphSchema;
export const WorkflowSchema = z.object({
  id: z.string().min(1), workspace_id: z.string().min(1), name: z.string(),
  description: z.string().nullish().transform((value) => value ?? ""),
  graph: graphSchema, revision: z.number().int().positive(),
  can_undo: z.boolean().optional().default(false), can_redo: z.boolean().optional().default(false),
  owner_id: optionalText, draft_revision: z.number().int().optional(), published_release_id: optionalText,
  archived_at: optionalText, created_at: z.string(), updated_at: z.string(),
  last_edit_error: optionalText, last_edit_message_id: optionalText,
  upgrade_issues: z.array(z.object({ code: z.string(), node_id: z.string().optional(), edge_id: z.string().optional(), field: z.string().optional(), message: z.string() })).optional(),
}).transform((value) => ({
  id: value.id, workspaceId: value.workspace_id, ownerId: value.owner_id, name: value.name, description: value.description,
  graph: value.graph, revision: value.revision, draftRevision: value.draft_revision, publishedReleaseId: value.published_release_id, archivedAt: value.archived_at,
  canUndo: value.can_undo, canRedo: value.can_redo,
  createdAt: value.created_at, updatedAt: value.updated_at,
  lastEditError: value.last_edit_error, lastEditMessageId: value.last_edit_message_id,
  upgradeIssues: value.upgrade_issues?.map((item) => ({ code: item.code, nodeId: item.node_id, edgeId: item.edge_id, field: item.field, message: item.message })),
}));
export const WorkflowListSchema = z.object({ workflows: z.array(WorkflowSchema) });

export const WorkflowTemplateSchema = z.object({
  id: z.string().min(1), version: z.number().int().positive(), name: z.string(), description: z.string(),
  graph: graphSchema,
  bindings: z.array(z.object({ key: z.string(), kind: z.enum(["agent", "member"]), node_id: z.string(), required: z.boolean() }))
    .transform((items) => items.map((item) => ({ key: item.key, kind: item.kind, nodeId: item.node_id, required: item.required }))),
});
export const WorkflowTemplateListSchema = z.object({ templates: z.array(WorkflowTemplateSchema) });

const runStatus = z.enum(["queued", "running", "waiting", "blocked", "cancelling", "compensating", "compensation_blocked", "succeeded", "failed", "cancelled"]);
const nodeRunStatus = z.enum(["pending", "ready", "queued", "running", "retry_wait", "waiting_human", "blocked", "cancelling", "succeeded", "failed", "cancelled", "skipped"]);
const workflowNodeRunSchema = z.object({
  node_id: z.string(), status: nodeRunStatus, generation: z.number().int().positive().optional(), activation_id: optionalText, scope_instance_id: optionalText,
  issue_id: optionalText, task_id: optionalText, attempt: z.number().int().nonnegative(), automatic_retries_used: z.number().int().nonnegative().optional(), retry_at: optionalText,
  output: z.string().nullish().transform((value) => value ?? ""), error: z.string().nullish().transform((value) => value ?? ""),
  reason_code: optionalText, output_id: optionalText, work_item_id: optionalText,
}).transform((node) => ({
  nodeId: node.node_id, status: node.status, generation: node.generation, activationId: node.activation_id, scopeInstanceId: node.scope_instance_id, issueId: node.issue_id, taskId: node.task_id,
  attempt: node.attempt, automaticRetriesUsed: node.automatic_retries_used, retryAt: node.retry_at, output: node.output, error: node.error,
  reasonCode: node.reason_code, outputId: node.output_id, workItemId: node.work_item_id,
}));
export const WorkflowRunSchema = z.object({
  id: z.string().min(1), workflow_id: z.string().min(1), workflow_revision: z.number().int().positive(),
  graph: graphSchema, input: z.string(), status: runStatus, issue_id: z.string(),
  created_at: z.string(), updated_at: z.string(), output: optionalText, error: optionalText,
  mode: z.enum(["production", "test", "simulation"]).optional(), release_id: optionalText,
  state_revision: z.number().int().nonnegative().optional(), reason_code: optionalText,
  owner_id: optionalText, dispatch_count: z.number().int().nonnegative().optional(), active_execution_ms: z.number().int().nonnegative().optional(), deadline_at: optionalText, finished_at: optionalText,
  rework_counts: z.record(z.string(), z.number().int().nonnegative()).optional(),
  input_values: z.record(z.string(), z.unknown()).optional(), current_activations: z.array(z.string()).optional(), pending_work_items: z.array(z.string()).optional(),
  allowed_actions: z.array(z.string()).optional(),
  nodes: z.array(workflowNodeRunSchema),
}).transform((run) => ({
  id: run.id, workflowId: run.workflow_id, workflowRevision: run.workflow_revision,
  graph: run.graph, input: run.input, status: run.status, issueId: run.issue_id,
  createdAt: run.created_at, updatedAt: run.updated_at, nodes: run.nodes, output: run.output, error: run.error,
  mode: run.mode, releaseId: run.release_id, stateRevision: run.state_revision, reasonCode: run.reason_code,
  ownerId: run.owner_id, dispatchCount: run.dispatch_count, activeExecutionMs: run.active_execution_ms, deadlineAt: run.deadline_at, finishedAt: run.finished_at, allowedActions: run.allowed_actions,
  reworkCounts: run.rework_counts, inputValues: run.input_values, currentActivations: run.current_activations, pendingWorkItems: run.pending_work_items,
}));
export const WorkflowRunListSchema = z.object({ runs: z.array(WorkflowRunSchema) });

const workflowNodeSchema = z.object({
  id: z.string(), type: nodeType, label: z.string(), position: z.object({ x: z.number(), y: z.number() }),
  agent_id: optionalText, instructions: optionalText, input_refs: z.array(z.string()).nullish(),
  assignee: z.object({ type: z.enum(["agent", "member"]), id: z.string() }).optional(), config: z.record(z.string(), z.unknown()).optional(),
  inputs: z.record(z.string(), z.unknown()).optional(), outputs: z.array(z.object({ key: z.string(), label: z.string().optional(), type: z.string().optional(), required: z.boolean().optional() })).optional(),
  on_failure: z.union([z.enum(["block", "route", "takeover"]), z.object({ action: z.enum(["block", "route", "takeover"]), assignee: z.object({ type: z.enum(["agent", "member"]), id: z.string() }).optional() })]).optional(),
  on_timeout: z.union([z.enum(["block", "route", "takeover"]), z.object({ action: z.enum(["block", "route", "takeover"]), assignee: z.object({ type: z.enum(["agent", "member"]), id: z.string() }).optional() })]).optional(),
  max_retries: z.number().int().optional(),
}).passthrough();
const workflowNodeDefinitionSchema = workflowNodeSchema.transform((node) => ({
  id: node.id, type: node.type, label: node.label, position: node.position, agentId: node.agent_id, instructions: node.instructions,
  inputRefs: node.input_refs ?? [], assignee: node.assignee, config: node.config, inputs: node.inputs, outputs: node.outputs,
  onFailure: node.on_failure, onTimeout: node.on_timeout, maxRetries: node.max_retries,
  unknownFields: unknownNodeFields(node),
}));
export const WorkflowRunNodeDetailSchema = z.object({
  run_id: z.string(), node_id: z.string(), activation_id: optionalText, generation: z.number().int().positive().optional(),
  definition: workflowNodeDefinitionSchema, current: workflowNodeRunSchema, attempts: z.array(workflowNodeRunSchema),
  outputs: z.array(z.object({
    id: z.string(), activation_id: z.string(), schema: z.unknown(), values: z.unknown(), artifact_refs: z.array(z.unknown()),
    content_hash: optionalText, superseded_at: optionalText, created_at: z.string(),
  }).transform((output) => ({
    id: output.id, activationId: output.activation_id, schema: output.schema, values: output.values,
    artifactRefs: output.artifact_refs, contentHash: output.content_hash, supersededAt: output.superseded_at, createdAt: output.created_at,
  }))),
}).transform((detail) => ({
  runId: detail.run_id, nodeId: detail.node_id, activationId: detail.activation_id, generation: detail.generation,
  definition: detail.definition, current: detail.current, attempts: detail.attempts, outputs: detail.outputs,
}));

export const WorkflowEventSchema = z.object({
  id: z.string().min(1), workspace_id: z.string().min(1), run_id: z.string().min(1),
  sequence: z.number().int().nonnegative(), event_type: z.string().min(1), actor_type: z.string().min(1),
  actor_id: optionalText, payload: z.record(z.string(), z.unknown()).nullish().transform((value) => value ?? {}),
  created_at: z.string(),
}).transform((event) => ({
  id: event.id, workspaceId: event.workspace_id, runId: event.run_id, sequence: event.sequence,
  eventType: event.event_type, actorType: event.actor_type, actorId: event.actor_id,
  payload: event.payload, createdAt: event.created_at,
}));
export const WorkflowEventListSchema = z.object({
  events: z.array(WorkflowEventSchema), next_after_sequence: z.number().int().nonnegative(),
}).transform((value) => ({ events: value.events, nextAfterSequence: value.next_after_sequence }));

export const WorkflowWorkItemSchema = z.object({
  id: z.string().min(1), run_id: z.string().min(1), workflow_id: z.string().min(1), activation_id: optionalText, node_id: z.string().min(1),
  kind: z.enum(["task", "review", "recovery"]), assignee_id: z.string().min(1),
  form_snapshot: z.record(z.string(), z.unknown()), input_snapshot: z.record(z.string(), z.unknown()),
  output_values: z.record(z.string(), z.unknown()), decision: optionalText, feedback: optionalText,
  version: z.number().int().positive(), status: z.enum(["open", "closed", "expired"]),
  due_at: optionalText, submitted_by: optionalText, submitted_at: optionalText,
}).transform((item) => ({
  id: item.id, runId: item.run_id, workflowId: item.workflow_id, activationId: item.activation_id, nodeId: item.node_id, kind: item.kind, assigneeId: item.assignee_id,
  formSnapshot: item.form_snapshot, inputSnapshot: item.input_snapshot, outputValues: item.output_values,
  decision: item.decision, feedback: item.feedback, version: item.version, status: item.status,
  dueAt: item.due_at, submittedBy: item.submitted_by, submittedAt: item.submitted_at,
}));
export const WorkflowWorkItemListSchema = z.object({ work_items: z.array(WorkflowWorkItemSchema) });
export const WorkflowChatSessionSchema = z.object({ session_id: z.string().min(1) })
  .transform((value) => ({ sessionId: value.session_id }));

export const WorkflowValidationSchema = z.object({
  valid: z.boolean(),
  errors: z.array(z.object({ code: z.string(), node_id: z.string().optional(), edge_id: z.string().optional(), field: z.string().optional(), message: z.string() }))
    .transform((items) => items.map((item) => ({ code: item.code, nodeId: item.node_id, edgeId: item.edge_id, field: item.field, message: item.message }))),
  warnings: z.array(z.object({ code: z.string(), node_id: z.string().optional(), edge_id: z.string().optional(), field: z.string().optional(), message: z.string() }))
    .transform((items) => items.map((item) => ({ code: item.code, nodeId: item.node_id, edgeId: item.edge_id, field: item.field, message: item.message }))),
  content_hash: z.string().optional(),
}).transform((value) => ({ valid: value.valid, errors: value.errors, warnings: value.warnings, contentHash: value.content_hash }));

const workflowValidationDetailSchema = z.object({ code: z.string(), node_id: z.string().optional(), edge_id: z.string().optional(), field: z.string().optional(), message: z.string() })
  .transform((item) => ({ code: item.code, nodeId: item.node_id, edgeId: item.edge_id, field: item.field, message: item.message }));
export const WorkflowUpgradeSchema = z.object({
  source_schema_version: z.number().int(), target_schema_version: z.number().int(), graph: graphSchema,
  auto_convertible: z.boolean(), issues: z.array(workflowValidationDetailSchema), warnings: z.array(workflowValidationDetailSchema),
}).transform((value) => ({
  sourceSchemaVersion: value.source_schema_version, targetSchemaVersion: value.target_schema_version, graph: value.graph,
  autoConvertible: value.auto_convertible, issues: value.issues, warnings: value.warnings,
}));

export const WorkflowCapabilitiesSchema = z.object({
  graph_schema_versions: z.array(z.number().int()), engine_versions: z.array(z.number().int()),
  node_types: z.array(z.string()), limits: z.object({ max_nodes: z.number().int(), max_edges: z.number().int(), max_nesting: z.number().int() }),
}).transform((value) => ({
  graphSchemaVersions: value.graph_schema_versions,
  engineVersions: value.engine_versions,
  nodeTypes: value.node_types as WorkflowCapabilities["nodeTypes"],
  limits: { maxNodes: value.limits.max_nodes, maxEdges: value.limits.max_edges, maxNesting: value.limits.max_nesting },
}));

export const WorkflowReleaseSchema = z.object({
  id: z.string().min(1), workflow_id: z.string().min(1), version_number: z.number().int().positive(),
  draft_revision: z.number().int().positive(), graph_schema_version: z.number().int().positive(),
  plan_version: z.number().int().positive(), content_hash: z.string(), notes: optionalText,
  created_by: z.string().min(1), created_at: z.string(), published: z.boolean(),
}).transform((release) => ({
  id: release.id, workflowId: release.workflow_id, versionNumber: release.version_number,
  draftRevision: release.draft_revision, graphSchemaVersion: release.graph_schema_version,
  planVersion: release.plan_version, contentHash: release.content_hash, notes: release.notes,
  createdBy: release.created_by, createdAt: release.created_at, published: release.published,
}));
export const WorkflowReleaseListSchema = z.object({ releases: z.array(WorkflowReleaseSchema) });

// Do not acknowledge a malformed write as a successful save or navigation.
export function parseWorkflowResponse<T>(raw: unknown, schema: z.ZodType<T>, endpoint: string): T {
  const parsed = parseWithFallback<T | null>(raw, schema, null, { endpoint });
  if (parsed === null) throw new Error("Invalid workflow response");
  return parsed;
}

export function workflowGraphToWire(graph: WorkflowGraph) {
  return {
    schema_version: graph.schemaVersion,
    defaults: graph.defaults ? {
      max_retries: graph.defaults.maxRetries,
      initial_delay_seconds: graph.defaults.initialDelaySeconds,
      backoff_multiplier: graph.defaults.backoffMultiplier,
      max_delay_seconds: graph.defaults.maxDelaySeconds,
      execution_timeout_seconds: graph.defaults.executionTimeoutSeconds,
      queue_timeout_seconds: graph.defaults.queueTimeoutSeconds,
      human_timeout_seconds: graph.defaults.humanTimeoutSeconds,
      run_timeout_seconds: graph.defaults.runTimeoutSeconds,
      max_dispatches: graph.defaults.maxDispatches,
      max_active_execution_seconds: graph.defaults.maxActiveExecutionSeconds,
    } : undefined,
    scopes: graph.scopes?.map((scope) => ({
      id: scope.id, type: scope.type, entry_node_id: scope.entryNodeId,
      exit_node_id: scope.exitNodeId, max_reworks: scope.maxReworks,
    })),
    nodes: graph.nodes.map((node) => ({
      ...(node.unknownFields ?? {}),
      id: node.id, type: node.type, label: node.label, position: node.position,
      agent_id: node.agentId, instructions: node.instructions, input_refs: node.inputRefs ?? [],
      assignee: node.assignee, config: node.config, inputs: node.inputs, outputs: node.outputs,
      on_failure: node.onFailure, on_timeout: node.onTimeout, max_retries: node.maxRetries,
    })),
    edges: graph.edges.map((edge) => ({
      id: edge.id, source: edge.source, target: edge.target, kind: edge.kind, scope_id: edge.scopeId,
      source_port: edge.sourcePort, target_port: edge.targetPort, label: edge.label,
    })),
  };
}
