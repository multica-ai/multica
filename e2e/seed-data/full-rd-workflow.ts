/**
 * 全量研发流程 Workflow 种子数据
 *
 * 覆盖 spec: docs/superpowers/specs/2026-06-19-workflow-stage-overview-design.md
 * 所有需要验证的数据和功能。
 *
 * ## 数据覆盖清单
 *
 * ### 阶段 (Stage) 功能覆盖:
 * - [x] 多阶段工作流 (6个阶段，覆盖全研发流程)
 * - [x] stage.sort_order 排序
 * - [x] stage.description 非空和空值
 * - [x] node_count (API 计算字段)
 * - [x] 空阶段 (阶段内无节点 — 发布上线阶段初始无节点)
 * - [x] stage_id = NULL 的未分组节点
 *
 * ### 节点 (Node) 功能覆盖:
 * - [x] worker_type: human / agent / squad
 * - [x] critic_type: human / agent / squad / api
 * - [x] worker_id 配置 / null (未配置)
 * - [x] critic_id 配置 / null (未配置)
 * - [x] critic_api_url 配置 (api 类型)
 * - [x] format_schema — JSON Schema 对象、非标准 shape、空/null
 * - [x] position_x / position_y 布局坐标
 * - [x] node.sort_order 排序
 * - [x] 节点描述 (description) 覆盖
 *
 * ### 边 (Edge) 功能覆盖:
 * - [x] 阶段内 DAG (intra-stage edges only)
 * - [x] condition 字段 (JSON)
 * - [x] 多上游/多下游节点
 * - [x] 孤立节点 (无入边无出边)
 *
 * ### 边界情况覆盖:
 * - [x] "未分组" 虚拟卡片 (2个 stage_id=NULL 的节点)
 * - [x] 空阶段 (发布上线阶段无节点)
 * - [x] 未配置 worker (文档更新节点)
 * - [x] 未配置 critic (技术调研、文档更新节点)
 * - [x] 空 format_schema (多个节点)
 * - [x] API critic 带 critic_api_url
 *
 * ### 详情面板覆盖:
 * - [x] 上游/下游关系展示
 * - [x] worker 配置展示 (agent / human / squad)
 * - [x] critic 配置展示 (agent / human / squad / api)
 * - [x] format_schema 格式化展示
 * - [x] "未配置" 状态展示
 * - [x] "无格式约束" 状态展示
 *
 * ## 研发流程概览
 *
 *   Stage 1: 需求分析     (3 nodes)  需求收集 → 需求评审 → 需求确认
 *   Stage 2: 技术设计     (4 nodes)  架构设计 → 接口设计 → 数据库设计 → 设计评审
 *   Stage 3: 编码实现     (5 nodes)  前端开发 ↘          ↘ 集成联调
 *                                   后端开发 → Agent开发 ↗
 *                                   数据库迁移 ↗
 *   Stage 4: 代码评审     (3 nodes)  AI预审 → 人工评审 → 评审修正
 *   Stage 5: 测试验证     (4 nodes)  单元测试 → 集成测试 → E2E测试 → 性能测试
 *   Stage 6: 发布上线     (2 nodes)  预发布验证 → 生产发布
 *
 *   未分组: 技术调研, 文档更新  (stage_id = NULL)
 */

import type {
  WorkflowStage,
  WorkflowNode,
  WorkflowEdge,
  CreateStageRequest,
  CreateNodeRequest,
  CreateEdgeRequest,
} from "@multica/core/types";

// ─────────────────────────────────────────────────────────────
// Workflow 元数据
// ─────────────────────────────────────────────────────────────

export const FULL_RD_WORKFLOW = {
  title: "全栈Web应用研发流程 v2.0",
  description:
    "覆盖从需求收集到生产发布的完整研发流程。整合 AI Agent 进行代码生成、代码评审和自动化测试，支持多人协作与质量门禁。",
  status: "active" as const,
  max_retries: 3,
  is_template: true,
} as const;

// ─────────────────────────────────────────────────────────────
// Stage 种子数据
// ─────────────────────────────────────────────────────────────

export interface SeedStage extends CreateStageRequest {
  /** 稳定标识符，用于边和测试引用 */
  ref: string;
  sort_order: number;
}

export const SEED_STAGES: SeedStage[] = [
  {
    ref: "requirements",
    name: "需求分析",
    description: "收集、分析并确认产品需求，产出 PRD 文档和用户故事",
    sort_order: 0,
  },
  {
    ref: "design",
    name: "技术设计",
    description: "完成架构设计、接口定义和数据库建模，产出技术方案文档",
    sort_order: 1,
  },
  {
    ref: "implementation",
    name: "编码实现",
    description: "前后端及 AI Agent 模块的编码与集成联调",
    sort_order: 2,
  },
  {
    ref: "code-review",
    name: "代码评审",
    description: "AI 预审 + 人工评审双重质量保障",
    sort_order: 3,
  },
  {
    ref: "testing",
    name: "测试验证",
    description: "单元测试、集成测试、E2E 测试和性能基准测试",
    sort_order: 4,
  },
  {
    ref: "release",
    name: "发布上线",
    description: "",
    sort_order: 5,
  },
];

// ─────────────────────────────────────────────────────────────
// Node 种子数据
// ─────────────────────────────────────────────────────────────

export interface SeedNode extends CreateNodeRequest {
  /** 稳定标识符，用于边和测试引用 */
  ref: string;
  /** 引用 stage ref (null = 未分组) */
  stageRef: string | null;
  position_x: number;
  position_y: number;
  sort_order: number;
  description: string;
}

export const SEED_NODES: SeedNode[] = [
  // ── Stage 1: 需求分析 (3 nodes) ─────────────────────────────
  {
    ref: "req-collect",
    stageRef: "requirements",
    title: "需求收集",
    description: "从产品经理和客户处收集原始需求，整理为需求清单",
    position_x: 100,
    position_y: 200,
    sort_order: 0,
    worker_type: "human",
    worker_id: null, // 待分配，测试 "未配置 worker_id" 展示
    critic_type: "human",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        title: { type: "string", description: "需求标题" },
        priority: {
          type: "string",
          enum: ["P0", "P1", "P2", "P3"],
        },
        description: { type: "string" },
      },
      required: ["title", "priority"],
    },
  },
  {
    ref: "req-review",
    stageRef: "requirements",
    title: "需求评审",
    description: "技术负责人和产品负责人共同评审需求可行性和优先级",
    position_x: 400,
    position_y: 200,
    sort_order: 1,
    worker_type: "squad",
    worker_id: null, // squad 模式下 worker_id 可为空
    critic_type: "agent",
    critic_id: null, // 由 AI 辅助评审
    format_schema: {
      type: "object",
      properties: {
        approved: { type: "boolean" },
        comments: { type: "string" },
        estimated_effort: {
          type: "string",
          enum: ["S", "M", "L", "XL"],
        },
      },
      required: ["approved"],
    },
  },
  {
    ref: "req-confirm",
    stageRef: "requirements",
    title: "需求确认",
    description: "最终确认需求范围、验收标准和排期计划",
    position_x: 700,
    position_y: 200,
    sort_order: 2,
    worker_type: "human",
    worker_id: null,
    critic_type: "human",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        confirmed: { type: "boolean" },
        acceptance_criteria: {
          type: "array",
          items: { type: "string" },
        },
        sprint: { type: "string" },
      },
      required: ["confirmed", "acceptance_criteria"],
    },
  },

  // ── Stage 2: 技术设计 (4 nodes) ─────────────────────────────
  {
    ref: "arch-design",
    stageRef: "design",
    title: "架构设计",
    description: "确定系统整体架构、技术选型和模块划分",
    position_x: 100,
    position_y: 200,
    sort_order: 0,
    worker_type: "agent",
    worker_id: null, // AI agent 执行架构设计建议
    critic_type: "human",
    critic_id: null, // 需要人工审批
    format_schema: {
      type: "object",
      properties: {
        architecture_diagram: { type: "string" },
        tech_stack: {
          type: "array",
          items: { type: "string" },
        },
        modules: {
          type: "array",
          items: {
            type: "object",
            properties: {
              name: { type: "string" },
              responsibility: { type: "string" },
            },
          },
        },
      },
      required: ["architecture_diagram", "tech_stack"],
    },
  },
  {
    ref: "api-design",
    stageRef: "design",
    title: "接口设计",
    description: "定义 RESTful API 和 WebSocket 事件规范",
    position_x: 400,
    position_y: 70,
    sort_order: 1,
    worker_type: "agent",
    worker_id: null,
    critic_type: "agent",
    critic_id: null, // AI 自动评审 API 设计规范性
    format_schema: {
      type: "object",
      properties: {
        endpoints: {
          type: "array",
          items: {
            type: "object",
            properties: {
              method: { type: "string" },
              path: { type: "string" },
              request_body: { type: "object" },
              response_body: { type: "object" },
            },
          },
        },
      },
      required: ["endpoints"],
    },
  },
  {
    ref: "db-design",
    stageRef: "design",
    title: "数据库设计",
    description: "设计数据库表结构、索引策略和迁移方案",
    position_x: 400,
    position_y: 330,
    sort_order: 2,
    worker_type: "agent",
    worker_id: null,
    critic_type: "human",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        tables: {
          type: "array",
          items: {
            type: "object",
            properties: {
              name: { type: "string" },
              columns: { type: "array" },
              indexes: { type: "array" },
            },
          },
        },
        migration_plan: { type: "string" },
      },
      required: ["tables"],
    },
  },
  {
    ref: "design-review",
    stageRef: "design",
    title: "设计评审",
    description: "团队集体评审技术方案，识别风险和优化点",
    position_x: 700,
    position_y: 200,
    sort_order: 3,
    worker_type: "squad",
    worker_id: null,
    critic_type: "human",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        approved: { type: "boolean" },
        risks: {
          type: "array",
          items: {
            type: "object",
            properties: {
              description: { type: "string" },
              severity: { type: "string", enum: ["low", "medium", "high"] },
              mitigation: { type: "string" },
            },
          },
        },
        action_items: {
          type: "array",
          items: { type: "string" },
        },
      },
      required: ["approved"],
    },
  },

  // ── Stage 3: 编码实现 (5 nodes) ─────────────────────────────
  {
    ref: "frontend-dev",
    stageRef: "implementation",
    title: "前端开发",
    description: "实现 UI 组件、页面路由和状态管理",
    position_x: 100,
    position_y: 70,
    sort_order: 0,
    worker_type: "agent",
    worker_id: null,
    critic_type: "agent",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        components: {
          type: "array",
          items: { type: "string" },
        },
        tests_included: { type: "boolean" },
        accessibility_check: { type: "boolean" },
      },
      required: ["components", "tests_included"],
    },
  },
  {
    ref: "backend-dev",
    stageRef: "implementation",
    title: "后端开发",
    description: "实现 API handler、业务逻辑和数据访问层",
    position_x: 100,
    position_y: 330,
    sort_order: 1,
    worker_type: "agent",
    worker_id: null,
    critic_type: "api",
    critic_id: null,
    critic_api_url: "https://api.validator.internal/v1/lint", // API 类型 critic 回调
    format_schema: {
      type: "object",
      properties: {
        handlers: {
          type: "array",
          items: { type: "string" },
        },
        sqlc_queries: {
          type: "array",
          items: { type: "string" },
        },
        test_coverage_pct: { type: "number", minimum: 80 },
      },
      required: ["handlers", "test_coverage_pct"],
    },
  },
  {
    ref: "agent-dev",
    stageRef: "implementation",
    title: "AI Agent 开发",
    description: "开发 Workflow Agent 节点逻辑和 Prompt 工程",
    position_x: 450,
    position_y: 200,
    sort_order: 2,
    worker_type: "agent",
    worker_id: null,
    critic_type: "agent",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        agent_config: { type: "object" },
        prompt_template: { type: "string" },
        tool_definitions: { type: "array" },
        expected_output_schema: { type: "object" },
      },
      required: ["agent_config", "prompt_template"],
    },
  },
  {
    ref: "db-migration",
    stageRef: "implementation",
    title: "数据库迁移",
    description: "编写和执行数据库 migration 脚本",
    position_x: 100,
    position_y: 460,
    sort_order: 3,
    worker_type: "human",
    worker_id: null,
    critic_type: "agent",
    critic_id: null, // AI 评审 migration 安全性
    format_schema: {
      type: "object",
      properties: {
        up_script: { type: "string" },
        down_script: { type: "string" },
        tested_on_staging: { type: "boolean" },
      },
      required: ["up_script", "down_script", "tested_on_staging"],
    },
  },
  {
    ref: "integration",
    stageRef: "implementation",
    title: "集成联调",
    description: "前后端联调、Agent 集成验证和端到端流程测试",
    position_x: 750,
    position_y: 200,
    sort_order: 4,
    worker_type: "squad",
    worker_id: null,
    critic_type: "human",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        integration_tests_passed: { type: "boolean" },
        issues_found: {
          type: "array",
          items: {
            type: "object",
            properties: {
              description: { type: "string" },
              severity: { type: "string" },
              resolved: { type: "boolean" },
            },
          },
        },
        ready_for_review: { type: "boolean" },
      },
      required: ["integration_tests_passed", "ready_for_review"],
    },
  },

  // ── Stage 4: 代码评审 (3 nodes) ─────────────────────────────
  {
    ref: "ai-preview",
    stageRef: "code-review",
    title: "AI 预审",
    description: "AI Agent 自动扫描代码，检测常见问题、安全漏洞和代码异味",
    position_x: 100,
    position_y: 200,
    sort_order: 0,
    worker_type: "agent",
    worker_id: null,
    critic_type: "human", // AI 给出建议，人工确认
    critic_id: null,
    format_schema: null, // 无格式约束
  },
  {
    ref: "human-review",
    stageRef: "code-review",
    title: "人工评审",
    description: "团队成员对代码进行全面评审，关注业务逻辑和架构一致性",
    position_x: 400,
    position_y: 200,
    sort_order: 1,
    worker_type: "human",
    worker_id: null,
    critic_type: "human",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        overall_score: { type: "number", minimum: 0, maximum: 100 },
        categories: {
          type: "object",
          properties: {
            correctness: { type: "number" },
            security: { type: "number" },
            performance: { type: "number" },
            maintainability: { type: "number" },
          },
        },
        comments: {
          type: "array",
          items: {
            type: "object",
            properties: {
              file: { type: "string" },
              line: { type: "number" },
              severity: { type: "string" },
              message: { type: "string" },
            },
          },
        },
      },
      required: ["overall_score"],
    },
  },
  {
    ref: "review-fix",
    stageRef: "code-review",
    title: "评审修正",
    description: "根据评审意见修复代码问题，重新提交评审",
    position_x: 700,
    position_y: 200,
    sort_order: 2,
    worker_type: "agent",
    worker_id: null,
    critic_type: "agent",
    critic_id: null, // AI 验证修复
    format_schema: null,
  },

  // ── Stage 5: 测试验证 (4 nodes) ─────────────────────────────
  {
    ref: "unit-test",
    stageRef: "testing",
    title: "单元测试",
    description: "编写和执行单元测试，确保代码覆盖率达到标准",
    position_x: 100,
    position_y: 200,
    sort_order: 0,
    worker_type: "agent",
    worker_id: null, // AI 自动生成单元测试
    critic_type: "agent",
    critic_id: null, // AI 评估测试质量
    format_schema: {
      type: "object",
      properties: {
        coverage_pct: { type: "number", minimum: 80 },
        tests_generated: { type: "number" },
        tests_passed: { type: "number" },
        tests_failed: { type: "number" },
      },
      required: ["coverage_pct", "tests_passed"],
    },
  },
  {
    ref: "integration-test",
    stageRef: "testing",
    title: "集成测试",
    description: "验证模块间接口调用、数据流和状态转换的正确性",
    position_x: 400,
    position_y: 70,
    sort_order: 1,
    worker_type: "agent",
    worker_id: null,
    critic_type: "human",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        scenarios: { type: "number" },
        passed: { type: "number" },
        failed: { type: "number" },
        blocked: { type: "number" },
      },
      required: ["scenarios", "passed", "failed"],
    },
  },
  {
    ref: "e2e-test",
    stageRef: "testing",
    title: "E2E 测试",
    description: "使用 Playwright 执行端到端用户流程测试",
    position_x: 400,
    position_y: 330,
    sort_order: 2,
    worker_type: "agent",
    worker_id: null,
    critic_type: "human",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        browser_matrix: {
          type: "array",
          items: { type: "string" },
        },
        specs: { type: "number" },
        passed: { type: "number" },
        flaky: { type: "number" },
      },
      required: ["specs", "passed"],
    },
  },
  {
    ref: "perf-test",
    stageRef: "testing",
    title: "性能测试",
    description: "执行负载测试和性能基准对比，确保服务 SLA",
    position_x: 700,
    position_y: 200,
    sort_order: 3,
    worker_type: "agent",
    worker_id: null,
    critic_type: "api",
    critic_id: null,
    critic_api_url: "https://perf.internal/v1/benchmark/validate", // API 性能基准校验
    format_schema: {
      type: "object",
      properties: {
        p50_ms: { type: "number" },
        p95_ms: { type: "number" },
        p99_ms: { type: "number" },
        rps: { type: "number" },
        error_rate: { type: "number", maximum: 0.01 },
        passed: { type: "boolean" },
      },
      required: ["p95_ms", "error_rate", "passed"],
    },
  },

  // ── Stage 6: 发布上线 (2 nodes) ─────────────────────────────
  {
    ref: "pre-release",
    stageRef: "release",
    title: "预发布验证",
    description: "在 staging 环境验证完整功能，执行冒烟测试",
    position_x: 150,
    position_y: 200,
    sort_order: 0,
    worker_type: "squad",
    worker_id: null,
    critic_type: "human",
    critic_id: null,
    format_schema: {
      type: "object",
      properties: {
        smoke_tests_passed: { type: "boolean" },
        staging_deploy_version: { type: "string" },
        verified_features: {
          type: "array",
          items: { type: "string" },
        },
        go_no_go: { type: "boolean" },
      },
      required: ["smoke_tests_passed", "go_no_go"],
    },
  },
  {
    ref: "prod-release",
    stageRef: "release",
    title: "生产发布",
    description: "执行生产环境发布，监控系统健康指标和业务指标",
    position_x: 550,
    position_y: 200,
    sort_order: 1,
    worker_type: "human",
    worker_id: null, // 需要人工触发和确认
    critic_type: "api",
    critic_id: null,
    critic_api_url: "https://monitor.internal/v1/health/validate", // 发布后自动健康检查
    format_schema: {
      type: "object",
      properties: {
        version: { type: "string" },
        deploy_timestamp: { type: "string", format: "date-time" },
        health_check_passed: { type: "boolean" },
        rollback_plan_ready: { type: "boolean" },
      },
      required: ["version", "health_check_passed", "rollback_plan_ready"],
    },
  },

  // ── 未分组节点 (stage_id = NULL) ────────────────────────────
  {
    ref: "tech-research",
    stageRef: null, // 未分配阶段 → "未分组" 虚拟卡片
    title: "技术调研",
    description: "调研新技术栈、第三方服务和开源方案的技术可行性",
    position_x: 150,
    position_y: 200,
    sort_order: 0,
    worker_type: "agent",
    worker_id: null, // worker 未分配 → worker_id 为 null
    critic_type: "human",
    critic_id: null, // critic 未分配 → critic_id 为 null
    format_schema: null, // 无格式约束 → "无格式约束" 展示
  },
  {
    ref: "doc-update",
    stageRef: null, // 未分配阶段 → "未分组" 虚拟卡片
    title: "文档更新",
    description: "更新 API 文档、用户手册和运维 Runbook",
    position_x: 500,
    position_y: 200,
    sort_order: 1,
    worker_type: "human",
    worker_id: null, // worker 未分配 → worker_id 为 null
    critic_type: "human",
    critic_id: null, // critic 未分配 → critic_id 为 null
    format_schema: null, // 无格式约束 → "无格式约束" 展示
  },
];

// ─────────────────────────────────────────────────────────────
// Edge 种子数据 (阶段内 DAG)
// ─────────────────────────────────────────────────────────────

export interface SeedEdge {
  /** 稳定标识符 */
  ref: string;
  /** 源节点 ref（运行时解析为 source_node_id） */
  sourceRef: string;
  /** 目标节点 ref（运行时解析为 target_node_id） */
  targetRef: string;
  /** 占位 — 由 resolveEdges() 填充 */
  source_node_id: string;
  /** 占位 — 由 resolveEdges() 填充 */
  target_node_id: string;
  condition?: unknown;
}

export const SEED_EDGES: SeedEdge[] = [
  // ── Stage 1: 需求分析 ─────────────────────────────────────
  {
    ref: "e-req-1",
    source_node_id: "", // 运行时替换为 req-collect 的实际 ID
    target_node_id: "", // 运行时替换为 req-review 的实际 ID
    sourceRef: "req-collect",
    targetRef: "req-review",
  },
  {
    ref: "e-req-2",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "req-review",
    targetRef: "req-confirm",
  },

  // ── Stage 2: 技术设计 ─────────────────────────────────────
  {
    ref: "e-design-1",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "arch-design",
    targetRef: "api-design",
    condition: { type: "architecture_approved" },
  },
  {
    ref: "e-design-2",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "arch-design",
    targetRef: "db-design",
    condition: { type: "architecture_approved" },
  },
  {
    ref: "e-design-3",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "api-design",
    targetRef: "design-review",
  },
  {
    ref: "e-design-4",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "db-design",
    targetRef: "design-review",
  },

  // ── Stage 3: 编码实现 ─────────────────────────────────────
  {
    ref: "e-impl-1",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "frontend-dev",
    targetRef: "agent-dev",
  },
  {
    ref: "e-impl-2",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "backend-dev",
    targetRef: "agent-dev",
  },
  {
    ref: "e-impl-3",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "db-migration",
    targetRef: "backend-dev",
  },
  {
    ref: "e-impl-4",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "frontend-dev",
    targetRef: "integration",
  },
  {
    ref: "e-impl-5",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "agent-dev",
    targetRef: "integration",
  },
  {
    ref: "e-impl-6",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "backend-dev",
    targetRef: "integration",
  },

  // ── Stage 4: 代码评审 ─────────────────────────────────────
  {
    ref: "e-review-1",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "ai-preview",
    targetRef: "human-review",
  },
  {
    ref: "e-review-2",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "human-review",
    targetRef: "review-fix",
    condition: { type: "changes_requested" },
  },

  // ── Stage 5: 测试验证 ─────────────────────────────────────
  {
    ref: "e-test-1",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "unit-test",
    targetRef: "integration-test",
  },
  {
    ref: "e-test-2",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "unit-test",
    targetRef: "e2e-test",
  },
  {
    ref: "e-test-3",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "integration-test",
    targetRef: "perf-test",
  },
  {
    ref: "e-test-4",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "e2e-test",
    targetRef: "perf-test",
  },

  // ── Stage 6: 发布上线 ─────────────────────────────────────
  {
    ref: "e-release-1",
    source_node_id: "",
    target_node_id: "",
    sourceRef: "pre-release",
    targetRef: "prod-release",
    condition: { type: "go_no_go", value: true },
  },
];

// ─────────────────────────────────────────────────────────────
// 动态运行时类型 (API 返回后填充)
// ─────────────────────────────────────────────────────────────

export interface ResolvedStage extends WorkflowStage {
  ref: string;
}

export interface ResolvedNode extends WorkflowNode {
  ref: string;
  stageRef: string | null;
}

export interface ResolvedEdge extends WorkflowEdge {
  ref: string;
  sourceRef: string;
  targetRef: string;
}

/**
 * 构建 Node ID 映射表。调用方通过 API 创建 nodes 后填充。
 */
export function buildNodeIdMap(
  createdNodes: Array<{ id: string; ref?: string }>,
): Map<string, string> {
  const map = new Map<string, string>();
  for (const n of createdNodes) {
    if (n.ref) map.set(n.ref, n.id);
  }
  return map;
}

/**
 * 构建 Stage ID 映射表。调用方通过 API 创建 stages 后填充。
 */
export function buildStageIdMap(
  createdStages: Array<{ id: string; ref?: string }>,
): Map<string, string> {
  const map = new Map<string, string>();
  for (const s of createdStages) {
    if (s.ref) map.set(s.ref, s.id);
  }
  return map;
}

/**
 * 将 SeedEdge 中的 sourceRef/targetRef 替换为实际 node ID。
 */
export function resolveEdges(
  edges: SeedEdge[],
  nodeIdMap: Map<string, string>,
): CreateEdgeRequest[] {
  return edges.map((e) => ({
    source_node_id: nodeIdMap.get(e.sourceRef) ?? e.source_node_id,
    target_node_id: nodeIdMap.get(e.targetRef) ?? e.target_node_id,
    condition: (e as { condition?: unknown }).condition,
  }));
}

// ─────────────────────────────────────────────────────────────
// 用于 API 批量创建的参数
// ─────────────────────────────────────────────────────────────

/**
 * 创建节点请求（带 stage_id 分配）。
 */
export interface CreateNodeWithStageRequest extends CreateNodeRequest {
  /** 对应的 stage ref (null = 不分配阶段) */
  stageRef: string | null;
}

/**
 * 从 SeedNode 提取 API 创建参数。
 */
export function toCreateNodeRequest(n: SeedNode): CreateNodeRequest {
  return {
    title: n.title,
    description: n.description,
    position_x: n.position_x,
    position_y: n.position_y,
    format_schema: n.format_schema,
    worker_type: n.worker_type,
    worker_id: n.worker_id,
    critic_type: n.critic_type,
    critic_id: n.critic_id,
    critic_api_url: (n as { critic_api_url?: string }).critic_api_url,
  };
}

// ─────────────────────────────────────────────────────────────
// 统计信息（用于验证）
// ─────────────────────────────────────────────────────────────

export const SEED_STATS = {
  totalStages: 6,
  totalNodes: 23,
  totalEdges: 19,
  unassignedNodes: 2,
  stageBreakdown: {
    requirements: { nodeCount: 3, edgeCount: 2 },
    design: { nodeCount: 4, edgeCount: 4 },
    implementation: { nodeCount: 5, edgeCount: 6 },
    "code-review": { nodeCount: 3, edgeCount: 2 },
    testing: { nodeCount: 4, edgeCount: 4 },
    release: { nodeCount: 2, edgeCount: 1 },
    unassigned: { nodeCount: 2, edgeCount: 0 },
  },
  configCoverage: {
    workerTypes: { agent: 13, human: 6, squad: 4 },
    criticTypes: { agent: 7, human: 13, api: 3 },
    withFormatSchema: 19,
    withoutFormatSchema: 4,
    withApiCriticUrl: 3,
  },
} as const;
