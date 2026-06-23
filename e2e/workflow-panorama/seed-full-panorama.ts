/**
 * 全量 Workflow Panorama 测试数据
 *
 * 覆盖设计文档要求的 Stage → Node(Plugin) → Agent 三层完整数据链路。
 *
 * 数据覆盖:
 * - 6 个 Stage (含空阶段)
 * - 6 个 Agent (不同 runtime_mode, visibility, model, status)
 * - 19 个 Node (含 worker 节点和 critic 节点)
 * - 5 条跨阶段 Edge (数据流)
 *
 * @spec docs/superpowers/specs/2026-06-23-workflow-panorama-design.md
 */

import type { TestApiClient } from "../fixtures";

// ─────────────────────────────────────────────────────────────
// 类型定义
// ─────────────────────────────────────────────────────────────

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
  agentRef: string | null; // references PanoramaSeedAgent.ref
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

// ─────────────────────────────────────────────────────────────
// Agent 定义
// ─────────────────────────────────────────────────────────────

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

const AGENT_DEFS: AgentDef[] = [
  {
    ref: "brain-stormer",
    name: "Brain Stormer",
    description: "头脑风暴 Agent，负责收集和发散初始需求",
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
    description: "需求分析 Agent，负责结构化需求和生成规格文档",
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
    description: "架构设计 Agent，负责技术方案设计和架构评审",
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
    description: "编码实现 Agent，负责前后端和 Agent 模块开发",
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
    description: "测试执行 Agent，负责自动化测试和性能基准",
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
    description: "代码评审 Agent，负责代码质量审查和安全审计",
    instructions: "You are a code reviewer. Check for correctness, security, performance, and adherence to best practices.",
    runtime_mode: "cloud",
    visibility: "private",
    model: "claude-sonnet-4-6",
    thinking_level: "high",
    max_concurrent_tasks: 2,
    is_builtin: false,
  },
];

// ─────────────────────────────────────────────────────────────
// Stage 定义
// ─────────────────────────────────────────────────────────────

interface StageDef {
  ref: string;
  name: string;
  sort_order: number;
}

const STAGE_DEFS: StageDef[] = [
  { ref: "intake", name: "需求接入", sort_order: 1 },
  { ref: "analysis", name: "需求分析", sort_order: 2 },
  { ref: "design", name: "技术设计", sort_order: 3 },
  { ref: "implementation", name: "编码实现", sort_order: 4 },
  { ref: "testing", name: "测试验证", sort_order: 5 },
  { ref: "release", name: "发布上线", sort_order: 6 },
];

// ─────────────────────────────────────────────────────────────
// Node 定义 (每个 node = 一个 plugin 卡片)
// ─────────────────────────────────────────────────────────────

interface NodeDef {
  ref: string;
  title: string;
  description: string;
  stageRef: string;
  workerType: "agent" | "human" | "squad";
  agentRef: string | null; // null = unconfigured worker
  criticType: "agent" | "human" | "squad" | "api" | "";
  criticAgentRef: string | null;
  isCriticNode: boolean;
}

/**
 * 匹配设计文档的 node 布局:
 *
 * Stage 1 需求接入: brainstorming, session-context, using-specdeveloper
 * Stage 2 需求分析: requirement-analysis, system-requirement + critics: aireq-evaluator, sysreq-evaluator
 * Stage 3 技术设计: architecture-design, api-design, db-design
 * Stage 4 编码实现: frontend-dev, backend-dev, agent-dev, integration
 * Stage 5 测试验证: unit-test, e2e-test, perf-test
 * Stage 6 发布上线: pre-release-check, production-deploy
 */
const NODE_DEFS: NodeDef[] = [
  // ── Stage 1: 需求接入 (3 worker nodes) ───────────────────────
  {
    ref: "brainstorming",
    title: "brainstorming",
    description: "头脑风暴插件，收集初始需求",
    stageRef: "intake",
    workerType: "agent",
    agentRef: "brain-stormer",
    criticType: "",
    criticAgentRef: null,
    isCriticNode: false,
  },
  {
    ref: "session-context",
    title: "session-context",
    description: "会话上下文管理",
    stageRef: "intake",
    workerType: "agent",
    agentRef: "brain-stormer",
    criticType: "",
    criticAgentRef: null,
    isCriticNode: false,
  },
  {
    ref: "using-specdeveloper",
    title: "using-specdeveloper",
    description: "规格开发辅助",
    stageRef: "intake",
    workerType: "agent",
    agentRef: "req-analyzer",
    criticType: "",
    criticAgentRef: null,
    isCriticNode: false,
  },

  // ── Stage 2: 需求分析 (2 worker + 2 critic nodes) ────────────
  {
    ref: "requirement-analysis",
    title: "requirement-analysis",
    description: "需求分析插件",
    stageRef: "analysis",
    workerType: "agent",
    agentRef: "req-analyzer",
    criticType: "agent",
    criticAgentRef: "reviewer",
    isCriticNode: false,
  },
  {
    ref: "aireq-evaluator",
    title: "aireq-evaluator",
    description: "AI需求评估器",
    stageRef: "analysis",
    workerType: "agent",
    agentRef: null,
    criticType: "agent",
    criticAgentRef: "reviewer",
    isCriticNode: true,
  },
  {
    ref: "system-requirement",
    title: "system-requirement",
    description: "系统需求规格",
    stageRef: "analysis",
    workerType: "agent",
    agentRef: "req-analyzer",
    criticType: "agent",
    criticAgentRef: "reviewer",
    isCriticNode: false,
  },
  {
    ref: "sysreq-evaluator",
    title: "sysreq-evaluator",
    description: "系统需求评估器",
    stageRef: "analysis",
    workerType: "agent",
    agentRef: null,
    criticType: "agent",
    criticAgentRef: "reviewer",
    isCriticNode: true,
  },

  // ── Stage 3: 技术设计 (3 worker nodes) ───────────────────────
  {
    ref: "architecture-design",
    title: "architecture-design",
    description: "系统架构设计",
    stageRef: "design",
    workerType: "agent",
    agentRef: "arch-designer",
    criticType: "human",
    criticAgentRef: null,
    isCriticNode: false,
  },
  {
    ref: "api-design",
    title: "api-design",
    description: "API 接口设计",
    stageRef: "design",
    workerType: "agent",
    agentRef: "arch-designer",
    criticType: "human",
    criticAgentRef: null,
    isCriticNode: false,
  },
  {
    ref: "db-design",
    title: "db-design",
    description: "数据库模型设计",
    stageRef: "design",
    workerType: "agent",
    agentRef: "arch-designer",
    criticType: "human",
    criticAgentRef: null,
    isCriticNode: false,
  },

  // ── Stage 4: 编码实现 (4 worker nodes) ───────────────────────
  {
    ref: "frontend-dev",
    title: "frontend-dev",
    description: "前端开发",
    stageRef: "implementation",
    workerType: "agent",
    agentRef: "code-dev",
    criticType: "agent",
    criticAgentRef: "reviewer",
    isCriticNode: false,
  },
  {
    ref: "backend-dev",
    title: "backend-dev",
    description: "后端开发",
    stageRef: "implementation",
    workerType: "agent",
    agentRef: "code-dev",
    criticType: "agent",
    criticAgentRef: "reviewer",
    isCriticNode: false,
  },
  {
    ref: "agent-dev",
    title: "agent-dev",
    description: "AI Agent 开发",
    stageRef: "implementation",
    workerType: "agent",
    agentRef: "code-dev",
    criticType: "agent",
    criticAgentRef: "reviewer",
    isCriticNode: false,
  },
  {
    ref: "integration",
    title: "integration",
    description: "集成联调",
    stageRef: "implementation",
    workerType: "squad",
    agentRef: null,
    criticType: "human",
    criticAgentRef: null,
    isCriticNode: false,
  },

  // ── Stage 5: 测试验证 (3 worker nodes) ───────────────────────
  {
    ref: "unit-test",
    title: "unit-test",
    description: "单元测试",
    stageRef: "testing",
    workerType: "agent",
    agentRef: "test-runner",
    criticType: "agent",
    criticAgentRef: "reviewer",
    isCriticNode: false,
  },
  {
    ref: "e2e-test",
    title: "e2e-test",
    description: "E2E自动化测试",
    stageRef: "testing",
    workerType: "agent",
    agentRef: "test-runner",
    criticType: "agent",
    criticAgentRef: "reviewer",
    isCriticNode: false,
  },
  {
    ref: "perf-test",
    title: "perf-test",
    description: "性能基准测试",
    stageRef: "testing",
    workerType: "agent",
    agentRef: "test-runner",
    criticType: "api",
    criticAgentRef: null,
    isCriticNode: false,
  },

  // ── Stage 6: 发布上线 (2 worker nodes) ───────────────────────
  {
    ref: "pre-release-check",
    title: "pre-release-check",
    description: "预发布验证",
    stageRef: "release",
    workerType: "squad",
    agentRef: null,
    criticType: "human",
    criticAgentRef: null,
    isCriticNode: false,
  },
  {
    ref: "production-deploy",
    title: "production-deploy",
    description: "生产发布",
    stageRef: "release",
    workerType: "agent",
    agentRef: "code-dev",
    criticType: "human",
    criticAgentRef: null,
    isCriticNode: false,
  },
];

// ─────────────────────────────────────────────────────────────
// Edge 定义 (跨阶段数据流)
// ─────────────────────────────────────────────────────────────

interface EdgeDef {
  ref: string;
  sourceRef: string;
  targetRef: string;
}

const EDGE_DEFS: EdgeDef[] = [
  // Stage 1 → Stage 2: 需求接入输出流转到需求分析
  { ref: "e-intake-analysis", sourceRef: "using-specdeveloper", targetRef: "requirement-analysis" },
  // Stage 2 → Stage 3: 需求分析输出流转到技术设计
  { ref: "e-analysis-design", sourceRef: "system-requirement", targetRef: "architecture-design" },
  // Stage 3 → Stage 4: 技术设计输出流转到编码实现
  { ref: "e-design-implementation", sourceRef: "api-design", targetRef: "backend-dev" },
  // Stage 4 → Stage 5: 编码实现输出流转到测试验证
  { ref: "e-implementation-testing", sourceRef: "integration", targetRef: "e2e-test" },
  // Stage 5 → Stage 6: 测试验证输出流转到发布上线
  { ref: "e-testing-release", sourceRef: "perf-test", targetRef: "pre-release-check" },
];

// ─────────────────────────────────────────────────────────────
// 主 Seed 函数
// ─────────────────────────────────────────────────────────────

/**
 * 创建全量 Panorama 测试数据。
 *
 * 流程:
 * 1. 创建 Workflow
 * 2. 创建 6 个 Stage
 * 3. 获取 Runtime → 创建 6 个 Agent
 * 4. 创建 19 个 Node (含 worker_id 和 critic_id 链接)
 * 5. 创建 5 条跨阶段 Edge
 * 6. 解决 worker_id/critic_id 映射
 *
 * 返回结构化的 seed 数据供测试断言使用。
 */
export async function seedFullPanoramaWorkflow(
  api: TestApiClient,
): Promise<FullPanoramaSeed> {
  // ── 1. 创建 Workflow ──
  const workflow = await api.createWorkflow(
    "全栈研发全景图 (E2E) " + Date.now(),
  );

  // ── 2. 创建 Stages ──
  const stageMap = new Map<string, PanoramaSeedStage>();
  for (const sd of STAGE_DEFS) {
    const created = await api.createWorkflowStage(workflow.id, sd.name, sd.sort_order);
    stageMap.set(sd.ref, {
      id: created.id,
      name: sd.name,
      sort_order: sd.sort_order,
      ref: sd.ref,
    });
  }
  const stages = Array.from(stageMap.values()).sort((a, b) => a.sort_order - b.sort_order);

  // ── 3. 获取 Runtime 并创建 Agents ──
  const runtimesData = await api.listRuntimes();
  const runtimes: Array<{ id: string; mode: string }> =
    runtimesData?.items ?? runtimesData?.runtimes ?? [];
  if (runtimes.length === 0) {
    throw new Error(
      "No runtimes available. Ensure at least one runtime exists in the workspace.",
    );
  }
  // Pick first available runtime for each mode, fallback to first runtime
  const cloudRuntime =
    runtimes.find((r) => r.mode === "cloud") ?? runtimes[0]!;
  const localRuntime =
    runtimes.find((r) => r.mode === "local") ?? runtimes[0]!;

  const agentMap = new Map<string, PanoramaSeedAgent>();
  for (const ad of AGENT_DEFS) {
    const runtimeId = ad.runtime_mode === "local" ? localRuntime.id : cloudRuntime.id;
    const created = await api.createAgent({
      name: ad.name,
      description: ad.description,
      instructions: ad.instructions,
      runtime_id: runtimeId,
      visibility: ad.visibility,
      model: ad.model,
      thinking_level: ad.thinking_level,
      max_concurrent_tasks: ad.max_concurrent_tasks,
    });
    agentMap.set(ad.ref, {
      id: created.id,
      name: ad.name,
      description: ad.description,
      runtime_mode: ad.runtime_mode,
      visibility: ad.visibility,
      status: created.status ?? "idle",
      model: ad.model,
      thinking_level: ad.thinking_level,
      max_concurrent_tasks: ad.max_concurrent_tasks,
      is_builtin: ad.is_builtin,
      plugin_id: created.plugin_id ?? null,
      instructions: ad.instructions,
      ref: ad.ref,
    });
  }
  const agents = Array.from(agentMap.values());

  // ── 4. 创建 Nodes (含 worker_id / critic_id 链接) ──
  const nodeMap = new Map<string, PanoramaSeedNode>();

  for (const nd of NODE_DEFS) {
    const stage = stageMap.get(nd.stageRef)!;
    const workerId = nd.agentRef ? agentMap.get(nd.agentRef)?.id ?? null : null;
    const criticId = nd.criticAgentRef ? agentMap.get(nd.criticAgentRef)?.id ?? null : null;
    const criticType = nd.isCriticNode ? "agent" : nd.criticType;

    const created = await api.createWorkflowNode(workflow.id, {
      title: nd.title,
      description: nd.description,
      stage_id: stage.id,
      worker_type: nd.workerType,
      worker_id: workerId,
      critic_type: criticType || "human",
      critic_id: criticId,
    });

    nodeMap.set(nd.ref, {
      id: created.id,
      title: nd.title,
      description: nd.description,
      stageId: stage.id,
      workerType: nd.workerType,
      workerId: workerId,
      criticType: criticType || "human",
      criticId: criticId,
      ref: nd.ref,
      agentRef: nd.agentRef,
      isCritic: nd.isCriticNode,
    });
  }

  // For critic nodes, we also need to set their worker_id if they have one
  for (const nd of NODE_DEFS) {
    if (nd.isCriticNode && nd.agentRef) {
      const node = nodeMap.get(nd.ref)!;
      const workerId = agentMap.get(nd.agentRef)?.id;
      if (workerId && !node.workerId) {
        await api.updateWorkflowNode(workflow.id, node.id, { worker_id: workerId });
        node.workerId = workerId;
      }
    }
  }

  const nodes = Array.from(nodeMap.values());

  // ── 5. 创建跨阶段 Edges ──
  const edgeMap = new Map<string, PanoramaSeedEdge>();
  for (const ed of EDGE_DEFS) {
    const sourceNode = nodeMap.get(ed.sourceRef)!;
    const targetNode = nodeMap.get(ed.targetRef)!;
    const created = await api.createWorkflowEdge(workflow.id, sourceNode.id, targetNode.id);
    edgeMap.set(ed.ref, {
      id: created.id,
      sourceNodeId: sourceNode.id,
      targetNodeId: targetNode.id,
      ref: ed.ref,
    });
  }
  const edges = Array.from(edgeMap.values());

  // ── 6. 返回结构化数据 ──
  return {
    workflow,
    stages,
    agents,
    nodes,
    edges,
  };
}

// ─────────────────────────────────────────────────────────────
// 统计信息 (供测试断言使用)
// ─────────────────────────────────────────────────────────────

export const FULL_PANORAMA_STATS = {
  totalStages: 6,
  totalAgents: 6,
  totalNodes: 19,
  totalEdges: 5,
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
    // reviewer is also cloud
    cloudAgents: ["brain-stormer", "req-analyzer", "arch-designer", "test-runner", "reviewer"],
    localAgents: ["code-dev"],
  },
} as const;
