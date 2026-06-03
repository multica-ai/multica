# Linear Core + Advanced 对齐差距设计（Multica）

## 1. 目标与范围

- 目标：识别 Multica 相对 Linear Core + Advanced 的能力差距。
- 输出口径：按模块分组，覆盖“缺失 + 明显弱于”两类差距，并标注 P0/P1/P2。
- 本文只做能力对齐评估，不进入代码实现。

## 2. 评估基线

### 2.1 对齐范围

- Core：Issue、Project、Triage、基础计划流程。
- Advanced：Roadmap、Insights、Workflow Automation 等增强能力。

### 2.2 判定标准

- 缺失：无可用数据模型或 API 或入口。
- 弱于：能力存在，但闭环、可配置性或规模能力明显短于 Linear。

### 2.3 优先级标准

- P0：不补齐会阻断主流程闭环。
- P1：主流程可用但效率或可管理性明显落后。
- P2：增强项，短期不阻断核心流程。

## 3. 差距清单（按模块）

| 模块 | 功能项 | 当前状态 | 证据（文件 + 函数/组件） | 差距说明 | 优先级 |
| --- | --- | --- | --- | --- | --- |
| Issue 生命周期与视图 | 归档生命周期（可恢复归档） | 缺失 | `server/pkg/db/queries/issue.sql` · `DeleteIssue` 仍为物理删除；`ListIssues` 无 `archived_at` 语义 | Linear 将归档作为独立生命周期能力，当前删除即丢失，不可恢复 | P0 |
| Project/Cycle/Roadmap | Cycle/Iteration 管理 | 缺失 | `apps/workspace/src/router.tsx` 路由列表无 cycle/sprint 入口；`server/pkg/db/queries/project.sql` 无 cycle 相关实体查询；仓库无 cycle/sprint 相关文件（全局 glob） | 缺少迭代计划核心对象，无法形成 Linear 的项目节奏管理闭环 | P0 |
| Project/Cycle/Roadmap | Roadmap 视图与里程碑时间轴 | 缺失 | `apps/workspace/src/router.tsx` 与 `features/layout/navigation.ts` 无 roadmap 入口；`shared/types/project.ts` 仅有项目基础字段 | Linear Advanced 的跨项目规划和时间轴能力当前无承载面 | P1 |
| Triage 与 Inbox | Inbox 分拣能力（批量 triage、snooze、规则分流） | 弱于 | `apps/workspace/src/features/inbox/store.ts` 仅有 `markRead/archive/archiveAll` 等基础动作 | 当前 Inbox 更接近通知列表，缺少 Linear 式的高效 triage 动作集 | P1 |
| Workflow Automation | 通用规则引擎（条件 + 动作编排） | 弱于 | `server/internal/automation/templates.go` 明确“fixed list of named templates, not a rule engine”；`automation.sql` 仅按 `template_id` 开关；`automation-tab.tsx` 仅模板开关与手动运行 | 当前是模板开关，不是可组合规则系统 | P1 |
| Estimation & Planning | 工作量估算（Story points / estimated time） | 缺失 | `apps/workspace/src/shared/types/issue.ts` 无估算字段；全局搜索 `estimated_minutes/story points` 无代码实现 | 缺估算会影响计划、容量与优先级判断 | P1 |
| Insights/Analytics | 任务流指标（lead time/cycle time/throughput） | 缺失 | `server/pkg/db/queries/time_entry.sql` 仅有 `SumTimeEntriesByUserInWorkspace` 和 `SumTimeEntriesByProjectInWorkspace`；`server/internal/handler/time_entry.go` · `GetTeamTimeStats` 返回 `by_user/by_project` | 当前统计集中在工时聚合，缺 Linear Advanced 的任务流效率指标 | P1 |
| Integrations & Sync | 统一导入导出覆盖多实体 | 弱于 | `server/internal/service/data_sync.go` · `ManifestData` 仅 `Issues []ManifestIssue`；`BuildExportManifest` 仅导出 issue | 导入导出链路已存在，但覆盖范围与生态集成能力仍明显不足 | P2 |
| Collaboration UX | 命令面板与快捷操作体系 | 弱于 | `features/layout/components/dashboard-layout.tsx` 仅注册 `Cmd/Ctrl+K` 打开搜索；`settings-page.tsx` 无快捷键管理配置入口 | 有全局搜索快捷键，但缺 Linear 式统一命令操作面板与可管理快捷体系 | P2 |

## 4. Top P0（先补齐）

1. 归档生命周期（替代物理删除）。
2. Cycle/Iteration 对象与视图。

## 5. 90 天建议顺序（P0 → P1）

### Phase 1（0-30 天）

1. P0: 归档生命周期改造（数据模型、API、列表视图、恢复动作）。
2. P0: Cycle 基础对象（schema、CRUD、Issue 关联、列表入口）。

### Phase 2（31-60 天）

1. P1: Triage 能力补齐（批量 triage、snooze、分流动作）。
2. P1: Automation 从模板开关升级为“条件 + 动作”规则模型最小集。
3. P1: 估算字段与计划页最小闭环。

### Phase 3（61-90 天）

1. P1: Insights 首批任务流指标（throughput、lead time、cycle time）。
2. P1: Roadmap 轻量版（按项目和时间窗口展示）。
3. P2: 命令面板与快捷键管理统一化。

## 6. 风险与边界

- 当前项目是 AI-native 任务系统，和 Linear 目标并不完全重叠，建议只对齐“协作效率核心面”，避免复制无差异功能。
- 自动化能力如果直接追求全量规则引擎，复杂度会迅速上升，建议先做最小规则闭环。
- 统计能力应以服务端聚合为主，避免客户端基于分页数据拼装全局指标。
