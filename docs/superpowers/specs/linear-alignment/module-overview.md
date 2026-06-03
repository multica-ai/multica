# 模块级设计总览

## 目标与范围

- 目标是在 Multica 内补齐与 Linear Core + Advanced 对齐所需的关键能力。
- 当前阶段优先服务个人与小团队使用场景，先补齐信息流入口与任务生命周期出口。
- 本轮包含能力缺口定义、优先级、推进顺序和执行交接规则。
- 本轮不包含 Enterprise 范围，例如 SSO、SCIM、审计合规与组织级权限治理。

## 能力列表

| 能力 | 当前状态 | 优先级 | 备注 |
| --- | --- | --- | --- |
| 1. Issue 归档生命周期 | 已实现 | P0 | 已支持 archive/restore、默认隐藏归档 issue、归档视图入口与批量归档 |
| 2. Triage Inbox 手动分拣闭环 | 已实现 | P0 | 已支持 handled/dismissed/snooze 语义、批量 triage 与到期 snooze 回流 |
| 3. Cycle/Iteration 管理 | 缺失 | Deferred | 当前个人/小团队阶段暂缓，不作为本轮 P0 |
| 4. Workflow Automation 规则引擎 | 部分完成 | P1 | 仅模板开关，不支持条件+动作编排 |
| 5. Estimation & Planning | 缺失 | P1 | 无估算字段，无容量计划能力 |
| 6. Insights/Analytics | 缺失 | P1 | 只有工时聚合，无任务流指标 |
| 7. Roadmap 规划面 | 缺失 | P1 | 无 roadmap 入口与时间轴模型 |
| 8. Integrations & Sync 深化 | 部分完成 | P2 | 已有导入导出，但仅覆盖 issue |
| 9. Collaboration UX（命令面板/快捷操作） | 部分完成 | P2 | 有 Cmd+K 搜索，无统一命令操作体系 |

## 当前状态基线

### 1) Issue / View 基线

- 证据：`server/pkg/db/queries/issue.sql` `DeleteIssue`
- 当前行为：删除 issue 走物理删除。
- 当前缺口：缺少归档、恢复、归档视图契约。

### 2) Project 与计划基线

- 证据：`apps/workspace/src/shared/types/project.ts` `Project`
- 当前行为：项目字段仅覆盖基础信息与状态。
- 当前缺口：缺 cycle、roadmap、里程碑语义。

### 3) Inbox/Triage 基线

- 证据：`apps/workspace/src/features/inbox/store.ts` `InboxState` action 集合
- 证据：`server/pkg/db/queries/inbox.sql` `ListInboxItems` / `ArchiveInboxItem`
- 当前行为：支持 read/archive/批量 read/archive，并默认排除 `archived = true` 的 inbox item。
- 当前缺口：缺 handled/dismissed/snooze 的 triage 分拣语义，read 与处理完成没有分离。

### 4) Automation 基线

- 证据：`server/internal/automation/templates.go` `BuiltinTemplates`
- 当前行为：固定模板列表，按模板开关启停。
- 当前缺口：缺条件表达、动作编排、规则管理。

### 5) Data Sync 基线

- 证据：`server/internal/service/data_sync.go` `ManifestData`
- 当前行为：统一 manifest 仅包含 issues。
- 当前缺口：导入导出覆盖范围不足，生态集成能力不足。

### 6) Analytics 基线

- 证据：`server/internal/handler/time_entry.go` `GetTeamTimeStats`
- 当前行为：返回 by_user / by_project 工时聚合。
- 当前缺口：缺 throughput、lead time、cycle time 等任务流指标。

## 非目标

- 不在本轮直接实现全部对齐能力。
- 不允许执行 Agent 在实现阶段引入未在设计包声明的产品范围扩展。
- 不在本轮改写与 Linear 无关的 AI-native 差异化能力。
- 不在当前 P0 中实现 Cycle/Iteration、Roadmap、Insights 或 Automation 规则引擎。

## 优先级与推进顺序

1. 先完成 P0：Issue 归档生命周期、Inbox Triage 手动分拣闭环。
2. 再完成 P1：Automation 规则化、估算、Insights、Roadmap。
3. 最后推进 P2：集成深度、命令面板体验增强。
4. Cycle/Iteration 保持 Deferred，等团队形成固定迭代节奏后再重新设计。
5. 排序依据是当前阶段先管好任务出口与信息流入口，再提升计划、复盘和体验能力。

## 共享约束

- 共享数据约束：继续以 `issue` 为任务主实体，不新增平行任务实体。
- 共享权限约束：沿用 workspace 成员权限体系，新增能力不得绕过 workspace 边界。
- 共享交互约束：主导航需保持可理解性，避免把规划、执行、统计全部混入同一路径。
- 共享技术约束：优先扩展现有 handler/query/store/router，不引入一次性兼容层。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| 未先补归档就推进视图与统计 | 指标和操作语义不一致 | 将归档能力固定为 P0 先决条件 |
| Inbox 继续只做 read/archive | 小团队输入流仍然混乱 | 将 handled/dismissed/snooze 固定为 P0 |
| 当前阶段过早引入 Cycle | 增加计划模型成本但收益不稳定 | 将 Cycle 标记为 Deferred，等团队形成固定迭代节奏后再设计 |
| 自动化直接上全量规则引擎 | 架构复杂度快速上升 | 先交付最小可用规则模型 |
| 统计基于前端分页拼装 | 指标失真 | 统一走服务端聚合契约 |

## 回写规则

- 任一能力进入实现前，执行 Agent 必须先读取对应能力目录的 `design.md` 与 `tasks.md`。
- 实现完成后，先回写对应能力 `spec.md` 的缺口状态，再回写本 `module-overview.md` 的状态表。
- 若实现阶段改变产品边界，先更新设计文档，再继续实现。
