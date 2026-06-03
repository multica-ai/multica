# 单能力 Research

## 调研目标

1. 确认当前系统哪些能力已经覆盖 Linear Core + Advanced 的主流程。
2. 明确哪些能力是缺失，哪些是弱于但已有基础。
3. 给出后续设计和实现必须遵守的边界条件。

## 现状链路

1. 入口
   - 主导航由 `apps/workspace/src/features/layout/navigation.ts` `primaryNav` 定义，当前包含 issues/projects/backlog/today/upcoming/my-time/team-time/pomodoro 等入口。
   - 路由由 `apps/workspace/src/router.tsx` `routeTree` 定义，未见 cycle/roadmap 路由。
2. 数据流
   - Issue 与 Project 由后端 SQL 查询驱动，核心在 `server/pkg/db/queries/issue.sql` 与 `server/pkg/db/queries/project.sql`。
   - Inbox 在前端 store 层做读写状态管理，核心在 `apps/workspace/src/features/inbox/store.ts`。
3. 状态更新
   - Issue 删除通过 `DeleteIssue` 直接删除数据库记录。
   - Automation 启停通过 `automation_rule` 表按 `template_id` 开关状态。
4. 输出结果
   - 时间统计通过 `GetTeamTimeStats` 输出 `by_user` 与 `by_project`。
   - 数据导出通过 `BuildExportManifest` 输出仅包含 issues 的 manifest。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/layout/navigation.ts` | `primaryNav` | 没有 cycle/roadmap 导航入口 |
| `apps/workspace/src/router.tsx` | `routeTree` | 没有 cycle/roadmap 路由 |
| `server/pkg/db/queries/issue.sql` | `DeleteIssue` | 当前是物理删除而非归档 |
| `server/pkg/db/queries/project.sql` | `Project` 相关 query | 项目层无 cycle/roadmap 数据模型支持 |
| `apps/workspace/src/features/inbox/store.ts` | `InboxState` actions | Inbox 动作为基础 read/archive，triage 动作不足 |
| `server/internal/automation/templates.go` | `BuiltinTemplates` | 自动化是固定模板，不是规则引擎 |
| `server/pkg/db/queries/automation.sql` | `UpsertAutomationRule` | 自动化状态仅按模板开关 |
| `apps/workspace/src/shared/types/issue.ts` | `Issue` | 缺估算字段 |
| `server/internal/handler/time_entry.go` | `GetTeamTimeStats` | 仅有工时聚合，无任务流指标 |
| `server/internal/service/data_sync.go` | `ManifestData` | 导入导出仅覆盖 issues |
| `apps/workspace/src/features/layout/components/dashboard-layout.tsx` | `Cmd+K handler` | 仅全局搜索触发，无统一命令动作体系 |

## 数据模型或状态流

- 核心字段
  - `Issue`：status/priority/project_id 等存在，缺 `archived_at`、估算、cycle 关联。
  - `Project`：基础字段存在，缺 roadmap/里程碑信息。
  - `AutomationRule`：只有 `template_id` + `enabled`，缺条件表达和动作定义。
- 状态如何变化
  - issue 删除直接物理删除。
  - inbox 仅前端维护 read/archive 交互状态。
  - automation 由模板启停切换。
- 写入点
  - issue/project/automation 在后端 handler + query。
  - inbox 状态在前端 store + API 更新。
- 读取点
  - 路由页面读取 query/store，汇总成 UI。
  - team-time 统计读取 time_entry 聚合接口。

## 边界条件

- 权限边界
  - 系统以 workspace 维度做权限与数据隔离，新增能力需保留该边界。
- 空状态
  - roadmap/cycle 当前不存在，新增时必须定义空态与引导。
- 错误路径
  - 当前 handler 普遍以 4xx/5xx 返回错误，新增接口需保持一致错误语义。
- 多租户边界
  - query 层广泛使用 `workspace_id` 过滤，新增实体必须延续该约束。

## 未决问题

1. 估算能力采用 points、minutes，还是双轨并存。
2. Roadmap 首版是否仅覆盖项目级里程碑，还是直接支持跨项目聚合。
3. Automation 首版规则模型是否支持多条件组合（AND/OR），还是仅单条件 MVP。
