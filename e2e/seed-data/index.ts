/**
 * e2e/seed-data — Workflow Stage Overview 种子数据
 *
 * 用于创建全量可验证的 Workflow 测试数据。
 *
 * ## 文件
 *
 * | 文件 | 用途 |
 * |------|------|
 * | `full-rd-workflow.ts` | TypeScript 种子数据 + 辅助函数 + 类型定义 |
 * | `full-rd-workflow.json` | JSON 格式，可直接用于 API/POST 调用 |
 *
 * ## 快速使用
 *
 * ### 在 E2E 测试中使用 (推荐)
 *
 * ```typescript
 * import { test, expect } from "../seed-workflow-overview";
 * import {
 *   SEED_STAGES,
 *   SEED_NODES,
 *   SEED_EDGES,
 *   FULL_RD_WORKFLOW,
 *   buildNodeIdMap,
 *   buildStageIdMap,
 *   resolveEdges,
 *   toCreateNodeRequest,
 * } from "../seed-data/full-rd-workflow";
 *
 * test("seed complete R&D workflow", async ({ seededApi }) => {
 *   // 1. Create workflow
 *   const wf = await seededApi.createWorkflow(FULL_RD_WORKFLOW.title);
 *
 *   // 2. Create stages
 *   const stages = [];
 *   for (const s of SEED_STAGES) {
 *     const created = await seededApi.createWorkflowStage(wf.id, s.name, s.sort_order);
 *     stages.push({ ...created, ref: s.ref });
 *   }
 *   const stageIdMap = buildStageIdMap(stages);
 *
 *   // 3. Create nodes
 *   const nodes = [];
 *   for (const n of SEED_NODES) {
 *     const stageId = n.stageRef ? stageIdMap.get(n.stageRef) ?? null : null;
 *     const created = await seededApi.createWorkflowNode(wf.id, {
 *       ...toCreateNodeRequest(n),
 *       stage_id: stageId,
 *     });
 *     nodes.push({ ...created, ref: n.ref });
 *   }
 *   const nodeIdMap = buildNodeIdMap(nodes);
 *
 *   // 4. Create edges
 *   for (const e of resolveEdges(SEED_EDGES, nodeIdMap)) {
 *     await seededApi.createWorkflowEdge(wf.id, e.source_node_id, e.target_node_id);
 *   }
 *
 *   // 5. Navigate to overview page and verify
 *   // ...
 * });
 * ```
 *
 * ### 使用 JSON 直接调用 API
 *
 * ```bash
 * # 创建 workflow
 * curl -X POST http://localhost:8080/api/workflows \
 *   -H "Content-Type: application/json" \
 *   -H "X-Workspace-ID: <ws-id>" \
 *   -d '{"title":"全栈Web应用研发流程 v2.0","description":"..."}'
 *
 * # 创建 stage (循环 6 个)
 * curl -X POST http://localhost:8080/api/workflows/<wf-id>/stages \
 *   -H "Content-Type: application/json" \
 *   -d '{"name":"需求分析","description":"...","sort_order":0}'
 * ```
 *
 * ## 验证覆盖矩阵
 *
 * 每行种子数据对应 spec 中需验证的具体功能点。
 * 详见 `full-rd-workflow.ts` 顶部注释。
 */

export {
  FULL_RD_WORKFLOW,
  SEED_STAGES,
  SEED_NODES,
  SEED_EDGES,
  SEED_STATS,
  buildNodeIdMap,
  buildStageIdMap,
  resolveEdges,
  toCreateNodeRequest,
} from "./full-rd-workflow";

export type {
  SeedStage,
  SeedNode,
  SeedEdge,
  ResolvedStage,
  ResolvedNode,
  ResolvedEdge,
  CreateNodeWithStageRequest,
} from "./full-rd-workflow";
