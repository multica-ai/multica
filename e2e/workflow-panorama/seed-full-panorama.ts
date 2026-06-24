/**
 * Full workflow panorama seed and repair helpers.
 */

import type { TestApiClient } from "../fixtures";

export interface PanoramaSeedAgent {
  id: string;
  name: string;
  description: string;
  runtime_mode: string;
  visibility: string;
  status: string;
  model: string;
  thinking_level: string;
  max_concurrent_tasks: number;
  is_builtin: boolean;
  plugin_id: string | null;
  instructions: string;
  ref: string;
}

export interface PanoramaSeedStage {
  id: string;
  name: string;
  sort_order: number;
  ref: string;
}

export interface PanoramaSeedNode {
  id: string;
  title: string;
  description: string;
  stageId: string | null;
  workerType: string;
  workerId: string | null;
  criticType: string;
  criticId: string | null;
  ref: string;
  agentRef: string | null;
  isCritic: boolean;
}

export interface PanoramaSeedEdge {
  id: string;
  sourceNodeId: string;
  targetNodeId: string;
  ref: string;
}

export interface FullPanoramaSeed {
  workflow: { id: string; title: string };
  stages: PanoramaSeedStage[];
  agents: PanoramaSeedAgent[];
  nodes: PanoramaSeedNode[];
  edges: PanoramaSeedEdge[];
}

interface AgentDef {
  ref: string;
  name: string;
  description: string;
  instructions: string;
  runtime_mode: string;
  visibility: string;
  model: string;
  thinking_level: string;
  max_concurrent_tasks: number;
  is_builtin: boolean;
}

interface StageDef {
  ref: string;
  name: string;
  sort_order: number;
}

interface NodeDef {
  ref: string;
  title: string;
  description: string;
  stageRef: string;
  position_x: number;
  position_y: number;
  workerType: "agent" | "human" | "squad";
  agentRef: string | null;
  criticType: "agent" | "human" | "squad" | "api" | "";
  criticAgentRef: string | null;
  isCriticNode: boolean;
  sort_order: number;
}

interface EdgeDef {
  ref: string;
  sourceRef: string;
  targetRef: string;
}

interface WorkflowSummary {
  id: string;
  title: string;
}

interface WorkflowNodeRecord {
  id: string;
  title: string;
  position_x: number;
  position_y: number;
}

interface WorkflowEdgeRecord {
  id: string;
  source_node_id: string;
  target_node_id: string;
}

export interface PanoramaRepairPlan {
  workflowId: string;
  workflowTitle: string;
  reason: string[];
  positionUpdates: Array<{ nodeId: string; title: string; position_x: number; position_y: number }>;
  missingEdges: Array<{ sourceTitle: string; targetTitle: string }>;
}

const AGENT_DEFS: AgentDef[] = [
  {
    ref: "brain-stormer",
    name: "Brain Stormer",
    description: "Collects and expands early-stage ideas.",
    instructions: "You are a creative brainstorming assistant. Help users explore ideas and generate innovative solutions.",
    runtime_mode: "cloud",
    visibility: "workspace",
    model: "claude-opus-4-8",
    thinking_level: "high",
    max_concurrent_tasks: 1,
    is_builtin: false,
  },
  {
    ref: "req-analyzer",
    name: "Requirement Analyzer",
    description: "Turns raw ideas into structured specifications.",
    instructions: "You are a requirements analyst. Transform raw ideas into structured specifications with clear acceptance criteria.",
    runtime_mode: "cloud",
    visibility: "private",
    model: "claude-sonnet-4-6",
    thinking_level: "medium",
    max_concurrent_tasks: 2,
    is_builtin: false,
  },
  {
    ref: "arch-designer",
    name: "Architecture Designer",
    description: "Designs and reviews technical architecture.",
    instructions: "You are a software architect. Design scalable, maintainable system architectures and review technical proposals.",
    runtime_mode: "cloud",
    visibility: "workspace",
    model: "claude-fable-5",
    thinking_level: "high",
    max_concurrent_tasks: 1,
    is_builtin: false,
  },
  {
    ref: "code-dev",
    name: "Code Developer",
    description: "Implements frontend, backend, and agent code.",
    instructions: "You are a full-stack developer. Write clean, well-tested code following the project conventions.",
    runtime_mode: "local",
    visibility: "workspace",
    model: "claude-sonnet-4-6",
    thinking_level: "medium",
    max_concurrent_tasks: 3,
    is_builtin: false,
  },
  {
    ref: "test-runner",
    name: "Test Runner",
    description: "Executes automated test and benchmark flows.",
    instructions: "You are a QA engineer. Write and execute comprehensive tests, report results with actionable feedback.",
    runtime_mode: "cloud",
    visibility: "workspace",
    model: "claude-haiku-4-5-20251001",
    thinking_level: "low",
    max_concurrent_tasks: 5,
    is_builtin: false,
  },
  {
    ref: "reviewer",
    name: "Code Reviewer",
    description: "Reviews quality, performance, and security.",
    instructions: "You are a code reviewer. Check for correctness, security, performance, and adherence to best practices.",
    runtime_mode: "cloud",
    visibility: "private",
    model: "claude-sonnet-4-6",
    thinking_level: "high",
    max_concurrent_tasks: 2,
    is_builtin: false,
  },
];

const STAGE_DEFS: StageDef[] = [
  { ref: "intake", name: "需求接入", sort_order: 0 },
  { ref: "analysis", name: "需求分析", sort_order: 1 },
  { ref: "design", name: "技术设计", sort_order: 2 },
  { ref: "implementation", name: "编码实现", sort_order: 3 },
  { ref: "testing", name: "测试验证", sort_order: 4 },
  { ref: "release", name: "发布上线", sort_order: 5 },
];

const NODE_DEFS: NodeDef[] = [
  { ref: "brainstorming", title: "brainstorming", description: "Brainstorming plugin", stageRef: "intake", position_x: 120, position_y: 120, workerType: "agent", agentRef: "brain-stormer", criticType: "", criticAgentRef: null, isCriticNode: false, sort_order: 0 },
  { ref: "session-context", title: "session-context", description: "Session context manager", stageRef: "intake", position_x: 420, position_y: 120, workerType: "agent", agentRef: "brain-stormer", criticType: "", criticAgentRef: null, isCriticNode: false, sort_order: 1 },
  { ref: "using-specdeveloper", title: "using-specdeveloper", description: "Spec developer helper", stageRef: "intake", position_x: 720, position_y: 120, workerType: "agent", agentRef: "req-analyzer", criticType: "", criticAgentRef: null, isCriticNode: false, sort_order: 2 },
  { ref: "requirement-analysis", title: "requirement-analysis", description: "Requirement analysis plugin", stageRef: "analysis", position_x: 120, position_y: 360, workerType: "agent", agentRef: "req-analyzer", criticType: "agent", criticAgentRef: "reviewer", isCriticNode: false, sort_order: 0 },
  { ref: "system-requirement", title: "system-requirement", description: "System requirement spec", stageRef: "analysis", position_x: 420, position_y: 360, workerType: "agent", agentRef: "req-analyzer", criticType: "agent", criticAgentRef: "reviewer", isCriticNode: false, sort_order: 1 },
  { ref: "aireq-evaluator", title: "aireq-evaluator", description: "AI requirement evaluator", stageRef: "analysis", position_x: 120, position_y: 540, workerType: "agent", agentRef: null, criticType: "agent", criticAgentRef: "reviewer", isCriticNode: true, sort_order: 2 },
  { ref: "sysreq-evaluator", title: "sysreq-evaluator", description: "System requirement evaluator", stageRef: "analysis", position_x: 420, position_y: 540, workerType: "agent", agentRef: null, criticType: "agent", criticAgentRef: "reviewer", isCriticNode: true, sort_order: 3 },
  { ref: "architecture-design", title: "architecture-design", description: "Architecture design", stageRef: "design", position_x: 120, position_y: 780, workerType: "agent", agentRef: "arch-designer", criticType: "human", criticAgentRef: null, isCriticNode: false, sort_order: 0 },
  { ref: "api-design", title: "api-design", description: "API design", stageRef: "design", position_x: 420, position_y: 780, workerType: "agent", agentRef: "arch-designer", criticType: "human", criticAgentRef: null, isCriticNode: false, sort_order: 1 },
  { ref: "db-design", title: "db-design", description: "Database design", stageRef: "design", position_x: 720, position_y: 780, workerType: "agent", agentRef: "arch-designer", criticType: "human", criticAgentRef: null, isCriticNode: false, sort_order: 2 },
  { ref: "frontend-dev", title: "frontend-dev", description: "Frontend implementation", stageRef: "implementation", position_x: 120, position_y: 1020, workerType: "agent", agentRef: "code-dev", criticType: "agent", criticAgentRef: "reviewer", isCriticNode: false, sort_order: 0 },
  { ref: "backend-dev", title: "backend-dev", description: "Backend implementation", stageRef: "implementation", position_x: 420, position_y: 1020, workerType: "agent", agentRef: "code-dev", criticType: "agent", criticAgentRef: "reviewer", isCriticNode: false, sort_order: 1 },
  { ref: "agent-dev", title: "agent-dev", description: "Agent implementation", stageRef: "implementation", position_x: 720, position_y: 1020, workerType: "agent", agentRef: "code-dev", criticType: "agent", criticAgentRef: "reviewer", isCriticNode: false, sort_order: 2 },
  { ref: "integration", title: "integration", description: "Integration coordination", stageRef: "implementation", position_x: 1020, position_y: 1020, workerType: "squad", agentRef: null, criticType: "human", criticAgentRef: null, isCriticNode: false, sort_order: 3 },
  { ref: "unit-test", title: "unit-test", description: "Unit testing", stageRef: "testing", position_x: 120, position_y: 1260, workerType: "agent", agentRef: "test-runner", criticType: "agent", criticAgentRef: "reviewer", isCriticNode: false, sort_order: 0 },
  { ref: "e2e-test", title: "e2e-test", description: "E2E testing", stageRef: "testing", position_x: 420, position_y: 1260, workerType: "agent", agentRef: "test-runner", criticType: "agent", criticAgentRef: "reviewer", isCriticNode: false, sort_order: 1 },
  { ref: "perf-test", title: "perf-test", description: "Performance benchmarking", stageRef: "testing", position_x: 720, position_y: 1260, workerType: "agent", agentRef: "test-runner", criticType: "api", criticAgentRef: null, isCriticNode: false, sort_order: 2 },
  { ref: "pre-release-check", title: "pre-release-check", description: "Pre-release check", stageRef: "release", position_x: 120, position_y: 1500, workerType: "squad", agentRef: null, criticType: "human", criticAgentRef: null, isCriticNode: false, sort_order: 0 },
  { ref: "production-deploy", title: "production-deploy", description: "Production deploy", stageRef: "release", position_x: 420, position_y: 1500, workerType: "agent", agentRef: "code-dev", criticType: "human", criticAgentRef: null, isCriticNode: false, sort_order: 1 },
];

const EDGE_DEFS: EdgeDef[] = [
  // Intra-stage: Intake
  { ref: "e-intake-1", sourceRef: "brainstorming", targetRef: "session-context" },
  { ref: "e-intake-2", sourceRef: "session-context", targetRef: "using-specdeveloper" },
  // Intra-stage: Analysis
  { ref: "e-analysis-1", sourceRef: "requirement-analysis", targetRef: "system-requirement" },
  // Intra-stage: Implementation
  { ref: "e-implementation-1", sourceRef: "frontend-dev", targetRef: "backend-dev" },
  { ref: "e-implementation-2", sourceRef: "backend-dev", targetRef: "agent-dev" },
  { ref: "e-implementation-3", sourceRef: "agent-dev", targetRef: "integration" },
  // Intra-stage: Release
  { ref: "e-release-1", sourceRef: "pre-release-check", targetRef: "production-deploy" },
  // Cross-stage edges
  { ref: "e-cross-1", sourceRef: "using-specdeveloper", targetRef: "requirement-analysis" },
  { ref: "e-cross-2", sourceRef: "system-requirement", targetRef: "architecture-design" },
  { ref: "e-cross-3", sourceRef: "db-design", targetRef: "frontend-dev" },
  { ref: "e-cross-4", sourceRef: "integration", targetRef: "unit-test" },
  { ref: "e-cross-5", sourceRef: "perf-test", targetRef: "pre-release-check" },
];

const NODE_DEF_BY_REF = new Map(NODE_DEFS.map((node) => [node.ref, node]));
const CANONICAL_NODE_TITLES = new Set(NODE_DEFS.map((node) => node.title));
const CANONICAL_EDGE_KEYS = new Set(EDGE_DEFS.map((edge) => `${edge.sourceRef}->${edge.targetRef}`));

export async function seedFullPanoramaWorkflow(api: TestApiClient): Promise<FullPanoramaSeed> {
  const workflow = await api.createWorkflow(`全栈研发全景图(E2E) ${Date.now()}`);

  const stageMap = new Map<string, PanoramaSeedStage>();
  for (const stageDef of STAGE_DEFS) {
    const created = await api.createWorkflowStage(workflow.id, stageDef.name, stageDef.sort_order);
    stageMap.set(stageDef.ref, {
      id: created.id,
      name: stageDef.name,
      sort_order: stageDef.sort_order,
      ref: stageDef.ref,
    });
  }

  const runtimesData = await api.listRuntimes();
  const runtimes: Array<{ id: string; mode?: string; runtime_mode?: string }> =
    Array.isArray(runtimesData) ? runtimesData : runtimesData?.items ?? runtimesData?.runtimes ?? [];
  if (runtimes.length === 0) {
    throw new Error("No runtimes available. Ensure at least one runtime exists in the workspace.");
  }

  const modeOf = (runtime: { mode?: string; runtime_mode?: string }) => runtime.runtime_mode ?? runtime.mode ?? "";
  const cloudRuntime = runtimes.find((runtime) => modeOf(runtime) === "cloud") ?? runtimes[0]!;
  const localRuntime = runtimes.find((runtime) => modeOf(runtime) === "local") ?? runtimes[0]!;

  const existingAgents: Array<{ id: string; name: string }> = (await api.listAgents({ include_archived: true })) ?? [];
  const existingAgentByName = new Map(existingAgents.map((agent) => [agent.name.toLowerCase(), agent.id]));

  const agentMap = new Map<string, PanoramaSeedAgent>();
  for (const agentDef of AGENT_DEFS) {
    const runtimeId = agentDef.runtime_mode === "local" ? localRuntime.id : cloudRuntime.id;
    const existingId = existingAgentByName.get(agentDef.name.toLowerCase());
    let created: { id: string; status?: string; plugin_id?: string | null };
    if (existingId) {
      created = await api.getAgent(existingId);
    } else {
      created = await api.createAgent({
        name: agentDef.name,
        description: agentDef.description,
        instructions: agentDef.instructions,
        runtime_id: runtimeId,
        visibility: agentDef.visibility,
        model: agentDef.model,
        thinking_level: agentDef.thinking_level,
        max_concurrent_tasks: agentDef.max_concurrent_tasks,
      });
      existingAgentByName.set(agentDef.name.toLowerCase(), created.id);
    }

    agentMap.set(agentDef.ref, {
      id: created.id,
      name: agentDef.name,
      description: agentDef.description,
      runtime_mode: agentDef.runtime_mode,
      visibility: agentDef.visibility,
      status: created.status ?? "idle",
      model: agentDef.model,
      thinking_level: agentDef.thinking_level,
      max_concurrent_tasks: agentDef.max_concurrent_tasks,
      is_builtin: agentDef.is_builtin,
      plugin_id: created.plugin_id ?? null,
      instructions: agentDef.instructions,
      ref: agentDef.ref,
    });
  }

  const nodeMap = new Map<string, PanoramaSeedNode>();
  for (const nodeDef of NODE_DEFS) {
    const stage = stageMap.get(nodeDef.stageRef)!;
    const workerId = nodeDef.agentRef ? agentMap.get(nodeDef.agentRef)?.id ?? null : null;
    const criticId = nodeDef.criticAgentRef ? agentMap.get(nodeDef.criticAgentRef)?.id ?? null : null;
    const criticType = nodeDef.isCriticNode ? "agent" : nodeDef.criticType;

    const created = await api.createWorkflowNode(workflow.id, {
      title: nodeDef.title,
      description: nodeDef.description,
      stage_id: stage.id,
      position_x: nodeDef.position_x,
      position_y: nodeDef.position_y,
      worker_type: nodeDef.workerType,
      worker_id: workerId,
      critic_type: criticType || "human",
      critic_id: criticId,
    });

    nodeMap.set(nodeDef.ref, {
      id: created.id,
      title: nodeDef.title,
      description: nodeDef.description,
      stageId: stage.id,
      workerType: nodeDef.workerType,
      workerId,
      criticType: criticType || "human",
      criticId,
      ref: nodeDef.ref,
      agentRef: nodeDef.agentRef,
      isCritic: nodeDef.isCriticNode,
    });
  }

  for (const nodeDef of NODE_DEFS) {
    if (!nodeDef.isCriticNode || !nodeDef.agentRef) continue;
    const node = nodeMap.get(nodeDef.ref)!;
    const workerId = agentMap.get(nodeDef.agentRef)?.id;
    if (workerId && !node.workerId) {
      await api.updateWorkflowNode(workflow.id, node.id, { worker_id: workerId });
      node.workerId = workerId;
    }
  }

  // Set sort_order for all nodes (CreateNodeRequest doesn't support sort_order)
  for (const nodeDef of NODE_DEFS) {
    const node = nodeMap.get(nodeDef.ref)!;
    await api.updateWorkflowNode(workflow.id, node.id, { sort_order: nodeDef.sort_order });
  }

  const edgeMap = new Map<string, PanoramaSeedEdge>();
  for (const edgeDef of EDGE_DEFS) {
    const sourceNode = nodeMap.get(edgeDef.sourceRef)!;
    const targetNode = nodeMap.get(edgeDef.targetRef)!;
    const created = await api.createWorkflowEdge(workflow.id, sourceNode.id, targetNode.id);
    edgeMap.set(edgeDef.ref, {
      id: created.id,
      sourceNodeId: sourceNode.id,
      targetNodeId: targetNode.id,
      ref: edgeDef.ref,
    });
  }

  return {
    workflow,
    stages: Array.from(stageMap.values()).sort((a, b) => a.sort_order - b.sort_order),
    agents: Array.from(agentMap.values()),
    nodes: Array.from(nodeMap.values()),
    edges: Array.from(edgeMap.values()),
  };
}

export function getPanoramaRepairPlan(
  workflow: WorkflowSummary,
  nodes: WorkflowNodeRecord[],
  edges: WorkflowEdgeRecord[],
): PanoramaRepairPlan | null {
  if (!isPanoramaSeedWorkflow(nodes)) {
    return null;
  }

  const reasons: string[] = [];
  if (nodes.every((node) => node.position_x === 0 && node.position_y === 0)) {
    reasons.push("all-node-positions-are-0-0");
  }

  const nodeByTitle = new Map(nodes.map((node) => [node.title, node]));
  const positionUpdates = NODE_DEFS
    .map((nodeDef) => {
      const node = nodeByTitle.get(nodeDef.title);
      if (!node) return null;
      if (node.position_x === nodeDef.position_x && node.position_y === nodeDef.position_y) {
        return null;
      }
      return {
        nodeId: node.id,
        title: node.title,
        position_x: nodeDef.position_x,
        position_y: nodeDef.position_y,
      };
    })
    .filter((item): item is NonNullable<typeof item> => item !== null);

  if (positionUpdates.length > 0 && !reasons.includes("all-node-positions-are-0-0")) {
    reasons.push("canonical-node-positions-mismatch");
  }

  const edgeKeys = new Set(
    edges
      .map((edge) => {
        const sourceTitle = nodes.find((node) => node.id === edge.source_node_id)?.title;
        const targetTitle = nodes.find((node) => node.id === edge.target_node_id)?.title;
        return sourceTitle && targetTitle ? `${sourceTitle}->${targetTitle}` : null;
      })
      .filter((key): key is string => key !== null),
  );

  const missingEdges = EDGE_DEFS
    .map((edgeDef) => {
      const sourceTitle = NODE_DEF_BY_REF.get(edgeDef.sourceRef)!.title;
      const targetTitle = NODE_DEF_BY_REF.get(edgeDef.targetRef)!.title;
      const key = `${sourceTitle}->${targetTitle}`;
      return edgeKeys.has(key) ? null : { sourceTitle, targetTitle };
    })
    .filter((item): item is NonNullable<typeof item> => item !== null);

  if (missingEdges.length > 0) {
    reasons.push("missing-canonical-edges");
  }

  if (reasons.length === 0) {
    return null;
  }

  return {
    workflowId: workflow.id,
    workflowTitle: workflow.title,
    reason: reasons,
    positionUpdates,
    missingEdges,
  };
}

export async function repairPanoramaWorkflow(
  api: TestApiClient,
  workflow: WorkflowSummary,
  dryRun = false,
): Promise<PanoramaRepairPlan | null> {
  const nodeResponse = await api.listWorkflowNodes(workflow.id);
  const edgeResponse = await api.listWorkflowEdges(workflow.id);
  const nodes: WorkflowNodeRecord[] = nodeResponse?.nodes ?? [];
  const edges: WorkflowEdgeRecord[] = edgeResponse?.edges ?? [];

  const plan = getPanoramaRepairPlan(workflow, nodes, edges);
  if (!plan || dryRun) {
    return plan;
  }

  const nodeByTitle = new Map(nodes.map((node) => [node.title, node]));

  for (const update of plan.positionUpdates) {
    await api.updateWorkflowNode(workflow.id, update.nodeId, {
      position_x: update.position_x,
      position_y: update.position_y,
    });
  }

  for (const edge of plan.missingEdges) {
    const sourceNode = nodeByTitle.get(edge.sourceTitle);
    const targetNode = nodeByTitle.get(edge.targetTitle);
    if (!sourceNode || !targetNode) {
      continue;
    }
    await api.createWorkflowEdge(workflow.id, sourceNode.id, targetNode.id);
  }

  return plan;
}

function isPanoramaSeedWorkflow(nodes: WorkflowNodeRecord[]) {
  if (nodes.length !== NODE_DEFS.length) {
    return false;
  }

  const titles = new Set(nodes.map((node) => node.title));
  if (titles.size !== CANONICAL_NODE_TITLES.size) {
    return false;
  }

  for (const title of Array.from(CANONICAL_NODE_TITLES)) {
    if (!titles.has(title)) {
      return false;
    }
  }

  return true;
}

export const FULL_PANORAMA_STATS = {
  totalStages: 6,
  totalAgents: 6,
  totalNodes: 19,
  totalEdges: 12,
  nodeBreakdown: {
    intake: { nodeCount: 3, criticCount: 0 },
    analysis: { nodeCount: 4, criticCount: 2 },
    design: { nodeCount: 3, criticCount: 0 },
    implementation: { nodeCount: 4, criticCount: 0 },
    testing: { nodeCount: 3, criticCount: 0 },
    release: { nodeCount: 2, criticCount: 0 },
  },
  agentBreakdown: {
    cloud: 4,
    local: 1,
    cloudAgents: ["brain-stormer", "req-analyzer", "arch-designer", "test-runner", "reviewer"],
    localAgents: ["code-dev"],
  },
} as const;

export const PANORAMA_CANONICAL_TITLES = Array.from(CANONICAL_NODE_TITLES);
export const PANORAMA_CANONICAL_EDGE_KEYS = Array.from(CANONICAL_EDGE_KEYS);
