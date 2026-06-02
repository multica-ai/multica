# 单能力 Spec

## 背景

当前产品已具备 issue、project、inbox、automation、time-tracking 等基础能力，但与 Linear Core + Advanced 的协作闭环仍有关键差距。需要先形成一个可执行的对齐设计基线，供后续实现 Agent 按同一范围推进。

## 范围

- 本次覆盖：
  - 对齐能力清单（缺失 + 明显弱于）
  - P0/P1/P2 优先级定义
  - 90 天执行顺序建议
  - 后续执行契约（以 design/tasks 为准）
- 本次不覆盖：
  - Enterprise 能力（SSO/SCIM/审计）
  - 具体代码实现与迁移执行

## 当前状态

当前系统已经覆盖基础任务管理与时间管理，但关键规划层、流程层与分析层仍不完整。尤其在归档生命周期、迭代对象、流程自动化规则化、任务流统计等能力上，与 Linear Core + Advanced 存在实质差距。

## 证据

- `server/pkg/db/queries/issue.sql` `DeleteIssue`：当前 issue 删除为物理删除，不是归档语义。
- `apps/workspace/src/router.tsx` `routeTree`：当前无 cycle、roadmap 相关路由入口。
- `apps/workspace/src/features/inbox/store.ts` `InboxState`：仅有 read/archive 基础动作，无 triage 动作集。
- `server/internal/automation/templates.go` `BuiltinTemplates`：固定模板列表，不是规则引擎。
- `apps/workspace/src/shared/types/issue.ts` `Issue`：无估算字段，无法承载 planning 估算能力。
- `server/internal/handler/time_entry.go` `GetTeamTimeStats`：只有工时聚合输出，无任务流效率指标。
- `server/internal/service/data_sync.go` `ManifestData`：导入导出只覆盖 issues。
- `apps/workspace/src/features/layout/components/dashboard-layout.tsx` `Cmd+K handler`：有全局搜索触发，无统一命令操作体系。

## 缺口

1. 缺归档生命周期与迭代对象，主流程闭环不完整。
2. 缺规划和分析核心能力，导致管理与复盘深度不足。
3. 缺规则化 triage/automation，协作效率明显弱于 Linear。

这些缺口会直接影响团队从“收敛任务”到“计划执行”再到“数据复盘”的完整体验。

## 交接说明

- 后续优先看：
  - `research.md`（证据链与边界）
  - `design.md`（推荐方案与契约）
  - `tasks.md`（切片和执行顺序）
- 进入实现前需要补充：
  - Cycle 与 Roadmap 的最终产品范围确认
  - 估算维度选型（points / minutes / 双轨）
