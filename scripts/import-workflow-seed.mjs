/**
 * 导入工作流种子测试数据
 *
 * 使用 API 将 e2e/seed-data/full-rd-workflow.ts 中的完整研发流程
 * 导入到正在运行的应用中。
 *
 * 依赖: BACKEND_URL (默认 http://localhost:8081)
 * 使用 kdemo648@gmail.com / demo111 进行认证和数据导入
 */

const BACKEND_URL = process.env.BACKEND_URL || "http://localhost:8081";
const API_BASE = `${BACKEND_URL}/api`;
const AUTH_BASE = `${BACKEND_URL}/auth`;

const E2E_EMAIL = "kdemo648@gmail.com";
const E2E_NAME = "kdemo648";
const DEV_CODE = "123456";

// ─────────────────────────────────────────────────────────────
// 种子数据 (从 e2e/seed-data/full-rd-workflow.ts 精简)
// ─────────────────────────────────────────────────────────────

const FULL_RD_WORKFLOW = {
  title: "全栈Web应用研发流程 v2.0",
  description:
    "覆盖从需求收集到生产发布的完整研发流程。整合 AI Agent 进行代码生成、代码评审和自动化测试，支持多人协作与质量门禁。",
  status: "active",
  max_retries: 3,
  is_template: true,
};

const SEED_STAGES = [
  { ref: "requirements", name: "需求分析", description: "收集、分析并确认产品需求，产出 PRD 文档和用户故事", sort_order: 0 },
  { ref: "design", name: "技术设计", description: "完成架构设计、接口定义和数据库建模，产出技术方案文档", sort_order: 1 },
  { ref: "implementation", name: "编码实现", description: "前后端及 AI Agent 模块的编码与集成联调", sort_order: 2 },
  { ref: "code-review", name: "代码评审", description: "AI 预审 + 人工评审双重质量保障", sort_order: 3 },
  { ref: "testing", name: "测试验证", description: "单元测试、集成测试、E2E 测试和性能基准测试", sort_order: 4 },
  { ref: "release", name: "发布上线", description: "", sort_order: 5 },
];

const SEED_NODES = [
  // Stage 1: 需求分析
  { ref: "req-collect", stageRef: "requirements", title: "需求收集", description: "从产品经理和客户处收集原始需求，整理为需求清单", position_x: 100, position_y: 200, sort_order: 0, worker_type: "human", critic_type: "human" },
  { ref: "req-review", stageRef: "requirements", title: "需求评审", description: "技术负责人和产品负责人共同评审需求可行性和优先级", position_x: 400, position_y: 200, sort_order: 1, worker_type: "squad", critic_type: "agent" },
  { ref: "req-confirm", stageRef: "requirements", title: "需求确认", description: "最终确认需求范围、验收标准和排期计划", position_x: 700, position_y: 200, sort_order: 2, worker_type: "human", critic_type: "human" },

  // Stage 2: 技术设计
  { ref: "arch-design", stageRef: "design", title: "架构设计", description: "确定系统整体架构、技术选型和模块划分", position_x: 100, position_y: 200, sort_order: 0, worker_type: "agent", critic_type: "human" },
  { ref: "api-design", stageRef: "design", title: "接口设计", description: "定义 RESTful API 和 WebSocket 事件规范", position_x: 400, position_y: 70, sort_order: 1, worker_type: "agent", critic_type: "agent" },
  { ref: "db-design", stageRef: "design", title: "数据库设计", description: "设计数据库表结构、索引策略和迁移方案", position_x: 400, position_y: 330, sort_order: 2, worker_type: "agent", critic_type: "human" },
  { ref: "design-review", stageRef: "design", title: "设计评审", description: "团队集体评审技术方案，识别风险和优化点", position_x: 700, position_y: 200, sort_order: 3, worker_type: "squad", critic_type: "human" },

  // Stage 3: 编码实现
  { ref: "frontend-dev", stageRef: "implementation", title: "前端开发", description: "实现 UI 组件、页面路由和状态管理", position_x: 100, position_y: 70, sort_order: 0, worker_type: "agent", critic_type: "agent" },
  { ref: "backend-dev", stageRef: "implementation", title: "后端开发", description: "实现 API handler、业务逻辑和数据访问层", position_x: 100, position_y: 330, sort_order: 1, worker_type: "agent", critic_type: "api", critic_api_url: "https://api.validator.internal/v1/lint" },
  { ref: "agent-dev", stageRef: "implementation", title: "AI Agent 开发", description: "开发 Workflow Agent 节点逻辑和 Prompt 工程", position_x: 450, position_y: 200, sort_order: 2, worker_type: "agent", critic_type: "agent" },
  { ref: "db-migration", stageRef: "implementation", title: "数据库迁移", description: "编写和执行数据库 migration 脚本", position_x: 100, position_y: 460, sort_order: 3, worker_type: "human", critic_type: "agent" },
  { ref: "integration", stageRef: "implementation", title: "集成联调", description: "前后端联调、Agent 集成验证和端到端流程测试", position_x: 750, position_y: 200, sort_order: 4, worker_type: "squad", critic_type: "human" },

  // Stage 4: 代码评审
  { ref: "ai-preview", stageRef: "code-review", title: "AI 预审", description: "AI Agent 自动扫描代码，检测常见问题、安全漏洞和代码异味", position_x: 100, position_y: 200, sort_order: 0, worker_type: "agent", critic_type: "human" },
  { ref: "human-review", stageRef: "code-review", title: "人工评审", description: "团队成员对代码进行全面评审，关注业务逻辑和架构一致性", position_x: 400, position_y: 200, sort_order: 1, worker_type: "human", critic_type: "human" },
  { ref: "review-fix", stageRef: "code-review", title: "评审修正", description: "根据评审意见修复代码问题，重新提交评审", position_x: 700, position_y: 200, sort_order: 2, worker_type: "agent", critic_type: "agent" },

  // Stage 5: 测试验证
  { ref: "unit-test", stageRef: "testing", title: "单元测试", description: "编写和执行单元测试，确保代码覆盖率达到标准", position_x: 100, position_y: 200, sort_order: 0, worker_type: "agent", critic_type: "agent" },
  { ref: "integration-test", stageRef: "testing", title: "集成测试", description: "验证模块间接口调用、数据流和状态转换的正确性", position_x: 400, position_y: 70, sort_order: 1, worker_type: "agent", critic_type: "human" },
  { ref: "e2e-test", stageRef: "testing", title: "E2E 测试", description: "使用 Playwright 执行端到端用户流程测试", position_x: 400, position_y: 330, sort_order: 2, worker_type: "agent", critic_type: "human" },
  { ref: "perf-test", stageRef: "testing", title: "性能测试", description: "执行负载测试和性能基准对比，确保服务 SLA", position_x: 700, position_y: 200, sort_order: 3, worker_type: "agent", critic_type: "api", critic_api_url: "https://perf.internal/v1/benchmark/validate" },

  // Stage 6: 发布上线
  { ref: "pre-release", stageRef: "release", title: "预发布验证", description: "在 staging 环境验证完整功能，执行冒烟测试", position_x: 150, position_y: 200, sort_order: 0, worker_type: "squad", critic_type: "human" },
  { ref: "prod-release", stageRef: "release", title: "生产发布", description: "执行生产环境发布，监控系统健康指标和业务指标", position_x: 550, position_y: 200, sort_order: 1, worker_type: "human", critic_type: "api", critic_api_url: "https://monitor.internal/v1/health/validate" },

  // 未分组节点
  { ref: "tech-research", stageRef: null, title: "技术调研", description: "调研新技术栈、第三方服务和开源方案的技术可行性", position_x: 150, position_y: 200, sort_order: 0, worker_type: "agent", critic_type: "human" },
  { ref: "doc-update", stageRef: null, title: "文档更新", description: "更新 API 文档、用户手册和运维 Runbook", position_x: 500, position_y: 200, sort_order: 1, worker_type: "human", critic_type: "human" },
];

const SEED_EDGES = [
  // Stage 1: 需求分析
  { sourceRef: "req-collect", targetRef: "req-review" },
  { sourceRef: "req-review", targetRef: "req-confirm" },

  // Stage 2: 技术设计
  { sourceRef: "arch-design", targetRef: "api-design" },
  { sourceRef: "arch-design", targetRef: "db-design" },
  { sourceRef: "api-design", targetRef: "design-review" },
  { sourceRef: "db-design", targetRef: "design-review" },

  // Stage 3: 编码实现
  { sourceRef: "frontend-dev", targetRef: "agent-dev" },
  { sourceRef: "backend-dev", targetRef: "agent-dev" },
  { sourceRef: "db-migration", targetRef: "backend-dev" },
  { sourceRef: "frontend-dev", targetRef: "integration" },
  { sourceRef: "agent-dev", targetRef: "integration" },
  { sourceRef: "backend-dev", targetRef: "integration" },

  // Stage 4: 代码评审
  { sourceRef: "ai-preview", targetRef: "human-review" },
  { sourceRef: "human-review", targetRef: "review-fix" },

  // Stage 5: 测试验证
  { sourceRef: "unit-test", targetRef: "integration-test" },
  { sourceRef: "unit-test", targetRef: "e2e-test" },
  { sourceRef: "integration-test", targetRef: "perf-test" },
  { sourceRef: "e2e-test", targetRef: "perf-test" },

  // Stage 6: 发布上线
  { sourceRef: "pre-release", targetRef: "prod-release" },
];

// ─────────────────────────────────────────────────────────────
// 辅助函数
// ─────────────────────────────────────────────────────────────

let token = null;
let workspaceSlug = null;

async function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function authFetch(path, init = {}) {
  const headers = { "Content-Type": "application/json", ...init.headers };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  if (workspaceSlug) headers["X-Workspace-Slug"] = workspaceSlug;
  const url = `${API_BASE}${path}`;
  console.log(`  ${init.method || "GET"} ${url}`);
  const res = await fetch(url, { ...init, headers });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`HTTP ${res.status}: ${text}`);
  }
  return res.json();
}

// ─────────────────────────────────────────────────────────────
// 认证流程
// ─────────────────────────────────────────────────────────────

async function login() {
  console.log("\n=== 认证 ===");

  // 1. send-code
  console.log(`  发送验证码到 ${E2E_EMAIL}...`);
  const sendRes = await fetch(`${AUTH_BASE}/send-code`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email: E2E_EMAIL }),
  });
  if (!sendRes.ok) {
    const text = await sendRes.text();
    // 如果已注册过可能触发限流，等待后重试
    if (sendRes.status === 429) {
      console.log("  触发限流，等待 3 秒后重试...");
      await sleep(3000);
      const retryRes = await fetch(`${AUTH_BASE}/send-code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: E2E_EMAIL }),
      });
      if (!retryRes.ok) throw new Error(`send-code retry failed: ${retryRes.status} ${await retryRes.text()}`);
    } else {
      throw new Error(`send-code failed: ${sendRes.status} ${text}`);
    }
  }

  // 2. verify-code (使用 Dev Code 123456)
  console.log(`  验证码: ${DEV_CODE}`);
  const verifyRes = await fetch(`${AUTH_BASE}/verify-code`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email: E2E_EMAIL, code: DEV_CODE }),
  });
  if (!verifyRes.ok) {
    throw new Error(`verify-code failed: ${verifyRes.status} ${await verifyRes.text()}`);
  }
  const data = await verifyRes.json();
  token = data.token;
  console.log(`  ✓ 登录成功 (token: ${token.slice(0, 20)}...)`);

  // 3. 更新用户名
  if (data.user?.name !== E2E_NAME) {
    await authFetch("/me", { method: "PATCH", body: JSON.stringify({ name: E2E_NAME }) });
    console.log("  ✓ 用户名已更新");
  }

  // 4. 获取 E2E workspace
  const workspaces = await authFetch("/workspaces");
  const ws = workspaces.find((w) => w.slug === "demo111") || workspaces[0];
  if (!ws) throw new Error("没有找到可用工作区");
  workspaceSlug = ws.slug;
  console.log(`  ✓ 工作区: ${ws.name} (${ws.slug})`);

  return ws;
}

// ─────────────────────────────────────────────────────────────
// 数据导入
// ─────────────────────────────────────────────────────────────

async function importData() {
  console.log("\n=== 导入工作流数据 ===");

  // 1. 创建 Workflow
  console.log("\n--- 创建 Workflow ---");
  const workflow = await authFetch("/workflows", {
    method: "POST",
    body: JSON.stringify(FULL_RD_WORKFLOW),
  });
  console.log(`  ✓ Workflow: ${workflow.title} (id: ${workflow.id})`);

  // 2. 创建 Stages
  console.log("\n--- 创建 Stages ---");
  const stageMap = new Map();
  for (const s of SEED_STAGES) {
    const stage = await authFetch(`/workflows/${workflow.id}/stages`, {
      method: "POST",
      body: JSON.stringify({
        name: s.name,
        description: s.description,
        sort_order: s.sort_order,
      }),
    });
    stageMap.set(s.ref, stage.id);
    console.log(`  ✓ Stage: ${s.name} (${stage.id.slice(0, 8)}...)`);
  }

  // 3. 创建 Nodes
  console.log("\n--- 创建 Nodes ---");
  const nodeMap = new Map();
  for (const n of SEED_NODES) {
    const body = {
      title: n.title,
      description: n.description,
      position_x: n.position_x,
      position_y: n.position_y,
      worker_type: n.worker_type,
      critic_type: n.critic_type,
    };
    if (n.critic_api_url) body.critic_api_url = n.critic_api_url;
    if (n.format_schema) body.format_schema = n.format_schema;

    const node = await authFetch(`/workflows/${workflow.id}/nodes`, {
      method: "POST",
      body: JSON.stringify(body),
    });
    nodeMap.set(n.ref, node.id);
    console.log(`  ✓ Node: ${n.title} (${node.id.slice(0, 8)}...)`);

    // 分配到 Stage
    if (n.stageRef) {
      const stageId = stageMap.get(n.stageRef);
      if (stageId) {
        await authFetch(`/workflows/${workflow.id}/nodes/${node.id}/stage`, {
          method: "PUT",
          body: JSON.stringify({ stage_id: stageId }),
        });
      }
    }
  }

  // 4. 创建 Edges
  console.log("\n--- 创建 Edges ---");
  for (const e of SEED_EDGES) {
    const sourceId = nodeMap.get(e.sourceRef);
    const targetId = nodeMap.get(e.targetRef);
    if (!sourceId || !targetId) {
      console.warn(`  ⚠ 跳过边 ${e.sourceRef} -> ${e.targetRef} (节点不存在)`);
      continue;
    }
    await authFetch(`/workflows/${workflow.id}/edges`, {
      method: "POST",
      body: JSON.stringify({
        source_node_id: sourceId,
        target_node_id: targetId,
      }),
    });
    console.log(`  ✓ Edge: ${e.sourceRef} -> ${e.targetRef}`);
  }

  console.log("\n=== 导入完成 ===");
  console.log(`Workflow: ${workflow.id}`);
  console.log(`Stages: ${SEED_STAGES.length}`);
  console.log(`Nodes: ${SEED_NODES.length}`);
  console.log(`Edges: ${SEED_EDGES.length}`);
  console.log(`URL: http://localhost:3000/tasks/${workspaceSlug}/workflows/${workflow.id}`);
}

// ─────────────────────────────────────────────────────────────
// 运行
// ─────────────────────────────────────────────────────────────

async function main() {
  try {
    await login();
    await importData();
  } catch (err) {
    console.error(`\n❌ 导入失败: ${err.message}`);
    process.exit(1);
  }
}

main();
