export type KnownWorkflowNodeType = "start" | "agent" | "human_task" | "human_review" | "condition" | "parallel" | "merge" | "end";
export type WorkflowNodeType = KnownWorkflowNodeType | (string & {});

export interface WorkflowAssignee {
  type: "agent" | "member";
  id: string;
}

export interface WorkflowOutputField {
  key: string;
  label?: string;
  type?: string;
  required?: boolean;
}

export interface WorkflowDefaults {
  maxRetries?: number;
  initialDelaySeconds?: number;
  backoffMultiplier?: number;
  maxDelaySeconds?: number;
  executionTimeoutSeconds?: number;
  queueTimeoutSeconds?: number;
  humanTimeoutSeconds?: number;
  runTimeoutSeconds?: number;
  maxDispatches?: number;
  maxActiveExecutionSeconds?: number;
}

export interface WorkflowScope {
  id: string;
  type: "rework";
  entryNodeId?: string;
  exitNodeId?: string;
  maxReworks?: number;
}

export interface WorkflowNode {
  id: string;
  type: WorkflowNodeType;
  label: string;
  position: { x: number; y: number };
  agentId?: string;
  instructions?: string;
  inputRefs?: string[];
  assignee?: WorkflowAssignee;
  config?: Record<string, unknown>;
  inputs?: Record<string, unknown>;
  outputs?: WorkflowOutputField[];
  onFailure?: "block" | "route" | "takeover" | { action: "block" | "route" | "takeover"; assignee?: WorkflowAssignee };
  onTimeout?: "block" | "route" | "takeover" | { action: "block" | "route" | "takeover"; assignee?: WorkflowAssignee };
  maxRetries?: number;
  unknownFields?: Record<string, unknown>;
}

export interface WorkflowEdge {
  id: string;
  source: string;
  target: string;
  kind?: "flow" | "rework" | "compensation";
  scopeId?: string;
  sourcePort?: string;
  targetPort?: string;
  label?: string;
}

export interface WorkflowGraph {
  schemaVersion?: number;
  defaults?: WorkflowDefaults;
  scopes?: WorkflowScope[];
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
}

export interface WorkflowDraft {
  name: string;
  description: string;
  graph: WorkflowGraph;
}

export interface WorkflowTemplateBinding {
  key: string;
  kind: "agent" | "member";
  nodeId: string;
  required: boolean;
}

export interface WorkflowTemplate {
  id: string;
  version: number;
  name: string;
  description: string;
  graph: WorkflowGraph;
  bindings: WorkflowTemplateBinding[];
}

export interface Workflow extends WorkflowDraft {
  id: string;
  workspaceId: string;
  ownerId?: string;
  revision: number;
  draftRevision?: number;
  publishedReleaseId?: string;
  archivedAt?: string;
  canUndo: boolean;
  canRedo: boolean;
  createdAt: string;
  updatedAt: string;
  lastEditError?: string;
  lastEditMessageId?: string;
  upgradeIssues?: WorkflowValidationDetail[];
}

export type WorkflowRunStatus = "queued" | "running" | "waiting" | "blocked" | "cancelling" | "compensating" | "compensation_blocked" | "succeeded" | "failed" | "cancelled";
export type WorkflowNodeRunStatus = "pending" | "ready" | "queued" | "running" | "retry_wait" | "waiting_human" | "blocked" | "cancelling" | "succeeded" | "failed" | "cancelled" | "skipped";

export interface WorkflowNodeRun {
  nodeId: string;
  status: WorkflowNodeRunStatus;
  generation?: number;
  activationId?: string;
  scopeInstanceId?: string;
  issueId?: string;
  taskId?: string;
  attempt: number;
  automaticRetriesUsed?: number;
  retryAt?: string;
  output: string;
  error: string;
  reasonCode?: string;
  outputId?: string;
  workItemId?: string;
}

export interface WorkflowRun {
  id: string;
  workflowId: string;
  workflowRevision: number;
  graph: WorkflowGraph;
  input: string;
  status: WorkflowRunStatus;
  issueId: string;
  createdAt: string;
  updatedAt: string;
  nodes: WorkflowNodeRun[];
  output?: string;
  error?: string;
  mode?: "production" | "test" | "simulation";
  releaseId?: string;
  stateRevision?: number;
  reasonCode?: string;
  ownerId?: string;
  dispatchCount?: number;
  activeExecutionMs?: number;
  deadlineAt?: string;
  reworkCounts?: Record<string, number>;
  finishedAt?: string;
  inputValues?: Record<string, unknown>;
  currentActivations?: string[];
  pendingWorkItems?: string[];
  allowedActions?: string[];
}

export interface WorkflowRunNodeDetail {
  runId: string;
  nodeId: string;
  activationId?: string;
  generation?: number;
  definition: WorkflowNode;
  current: WorkflowNodeRun;
  attempts: WorkflowNodeRun[];
  outputs: WorkflowRunNodeOutput[];
}

export interface WorkflowRunNodeOutput {
  id: string;
  activationId: string;
  schema: unknown;
  values: unknown;
  artifactRefs: unknown[];
  contentHash?: string;
  supersededAt?: string;
  createdAt: string;
}

export interface WorkflowEvent {
  id: string;
  workspaceId: string;
  runId: string;
  sequence: number;
  eventType: string;
  actorType: string;
  actorId?: string;
  payload: Record<string, unknown>;
  createdAt: string;
}

export interface WorkflowValidationDetail {
  code: string;
  nodeId?: string;
  edgeId?: string;
  field?: string;
  message: string;
}

export interface WorkflowValidationResult {
  valid: boolean;
  errors: WorkflowValidationDetail[];
  warnings: WorkflowValidationDetail[];
  contentHash?: string;
}

export interface WorkflowUpgradeResult {
  sourceSchemaVersion: number;
  targetSchemaVersion: number;
  graph: WorkflowGraph;
  autoConvertible: boolean;
  issues: WorkflowValidationDetail[];
  warnings: WorkflowValidationDetail[];
}

export interface WorkflowCapabilities {
  graphSchemaVersions: number[];
  engineVersions: number[];
  nodeTypes: WorkflowNodeType[];
  limits: { maxNodes: number; maxEdges: number; maxNesting: number };
}

export interface WorkflowRelease {
  id: string;
  workflowId: string;
  versionNumber: number;
  draftRevision: number;
  graphSchemaVersion: number;
  planVersion: number;
  contentHash: string;
  notes?: string;
  createdBy: string;
  createdAt: string;
  published: boolean;
}

export interface CreateWorkflowRequest {
  name: string;
  description?: string;
  templateId?: string;
  graph?: WorkflowGraph;
  idempotencyKey?: string;
}

export interface UpdateWorkflowRequest extends WorkflowDraft {
  expectedRevision: number;
  idempotencyKey?: string;
}

export interface StartWorkflowRunRequest {
  input?: string;
  inputValues?: Record<string, unknown>;
  expectedRevision: number;
  idempotencyKey: string;
  releaseId?: string;
}

export interface WorkflowRunOwnerUpdateRequest {
  expectedStateRevision: number;
  ownerId: string;
  reason?: string;
  idempotencyKey?: string;
}

export type WorkflowNodeResolution = "confirmed_not_executed" | "confirmed_completed" | "confirmed_stopped";

export type WorkflowTestRunMode = "simulation" | "test";

export interface WorkflowSimulationFixture {
  output?: string;
  values?: Record<string, unknown>;
  action?: "submit" | "approve" | "rework";
}

export interface StartWorkflowTestRunRequest {
  mode: WorkflowTestRunMode;
  inputValues: Record<string, unknown>;
  draftRevision: number;
  idempotencyKey: string;
  fixtures?: Record<string, WorkflowSimulationFixture>;
}

export interface WorkflowWorkItem {
  id: string;
  runId: string;
  workflowId: string;
  activationId?: string;
  nodeId: string;
  kind: "task" | "review" | "recovery";
  assigneeId: string;
  formSnapshot: Record<string, unknown>;
  inputSnapshot: Record<string, unknown>;
  outputValues: Record<string, unknown>;
  decision?: string;
  feedback?: string;
  version: number;
  status: "open" | "closed" | "expired";
  dueAt?: string;
  submittedBy?: string;
  submittedAt?: string;
}

export interface SubmitWorkflowWorkItemRequest {
  expectedStateRevision: number;
  expectedItemVersion: number;
  idempotencyKey: string;
  action: "submit" | "approve" | "rework";
  values?: Record<string, unknown>;
  feedback?: string;
}

export interface SendWorkflowChatMessageRequest {
  sessionId: string;
  content: string;
  selectedNodeId?: string;
  expectedRevision: number;
  idempotencyKey?: string;
}
