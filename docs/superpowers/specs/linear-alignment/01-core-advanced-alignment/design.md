# 单能力 Design

## 目标

给出 Multica 对齐 Linear Core + Advanced 的可执行设计基线，明确补齐顺序、接口方向、数据模型增量和验收标准，供执行 Agent 直接落地。

## 非目标

- 不实现 Enterprise 范围（SSO/SCIM/审计）。
- 不在本轮改造与对齐目标无关的 AI-native 特性。
- 不一次性上线所有能力，采用分阶段推进。

## 当前架构基线

- 当前入口：`primaryNav` + `routeTree`，无 cycle/roadmap 独立入口。
- 当前核心逻辑：issue/project/inbox/automation 分散在各 feature + handler。
- 当前存储或状态：issue/project/time_entry/automation_rule 已有，缺 cycle/roadmap/estimate 等核心结构。
- 当前 UI 或接口：有 inbox、automation、data import/export、team-time 基础页面与 API。

### 代码证据

- `apps/workspace/src/router.tsx` `routeTree`：无 cycle/roadmap 路由。
- `server/pkg/db/queries/issue.sql` `DeleteIssue`：删除语义非归档。
- `server/internal/automation/templates.go` `BuiltinTemplates`：固定模板模型。
- `server/internal/handler/time_entry.go` `GetTeamTimeStats`：统计维度有限。

## 缺口定义

1. 主流程缺口：无归档生命周期、无 cycle，无法形成标准执行闭环。
2. 计划缺口：无估算与 roadmap，难以进行团队计划管理。
3. 效率缺口：triage/automation 规则化不足，协作效率劣于 Linear。
4. 复盘缺口：缺任务流指标体系，难以数据驱动优化。

## 方案与权衡

### 方案 A：大爆炸一次性对齐

- 做法：并行实现归档、cycle、roadmap、automation、insights、estimation。
- 优点：理论上上线速度快。
- 风险：跨域改动过大，失败成本高，回归风险高。

### 方案 B：分阶段纵向切片（推荐）

- 做法：先补 P0 主流程，再补 P1 计划与效率，再补 P2 体验增强。
- 优点：每阶段都可交付可验证成果，风险可控。
- 风险：整体完成周期更长。

### 方案 C：仅补 UI 层“感知对齐”

- 做法：优先补页面和入口，后补数据模型和能力。
- 优点：短期可见度高。
- 风险：形成空壳功能，后续返工成本高。

## 推荐方案

选择方案 B。原因是该方案保持“先能力闭环再体验增强”的顺序，符合当前代码结构分层和稳定性要求。方案 A 风险过高，方案 C 会导致行为与展示不一致。

## 数据模型或状态模型

- 新增字段（建议）
  - issue: `archived_at`, `estimate_points`（或 `estimate_minutes`）, `cycle_id`
  - cycle: `id`, `workspace_id`, `name`, `starts_at`, `ends_at`, `state`
  - roadmap_item: `id`, `workspace_id`, `project_id`, `target_date`, `status`
- 状态变化
  - issue 生命周期增加 `archived` 语义，删除与归档分离。
  - cycle 增加 active/closed 状态，issue 可绑定 cycle。
- 关键约束
  - 所有新实体必须强制 `workspace_id` 边界。
  - 归档不等于完成，统计口径需区分。

## 接口契约

### 输入

- Cycle API：创建/更新/关闭 cycle，校验时间区间合法性。
- Issue API：支持 archive/unarchive、estimate、cycle 关联更新。
- Insights API：按时间窗口返回 throughput/lead-time/cycle-time 聚合。

### 输出

- 统一返回结构延续现有 JSON 风格，错误继续采用 4xx/5xx + message。
- 统计接口必须显式返回口径字段，避免前后端解释不一致。

## UI 或交互流程

1. 用户从导航进入新能力（Cycle / Roadmap / Insights）。
2. 用户先看到当前工作区范围内数据与空状态引导。
3. 用户进行计划动作（分配 cycle、设置估算、执行 triage）。
4. 用户在 Insights 查看执行结果与效率指标。

### ASCII 流程图

```text
+-------------------+      +-------------------+      +-------------------+
| Issue Lifecycle   | ---> | Cycle Planning    | ---> | Execution/Triage  |
| (archive/estimate)|      | (assign cycle)    |      | (rules + inbox)   |
+-------------------+      +-------------------+      +-------------------+
          |                           |                           |
          v                           v                           v
+-----------------------------------------------------------------------+
|                  Insights Aggregation (throughput/lead/cycle time)    |
+-----------------------------------------------------------------------+
                                  |
                                  v
                        +----------------------+
                        | Roadmap Visibility   |
                        | (project milestones) |
                        +----------------------+
```

## 权限、边界条件、异常路径

- 谁可以使用
  - 保持 workspace 成员权限体系，管理员可管理规划配置，普通成员按权限读写。
- 哪些输入非法
  - cycle 时间区间反转、估算负值、跨 workspace 关联都应拒绝。
- 失败时如何处理
  - 与现有 handler 一致，返回明确错误，不做静默降级。

## 实现约束

- 执行阶段不能自行扩大到 Enterprise 范围。
- 必须复用现有 `handler/query/store/router` 结构，不新增平行框架。
- 禁止引入临时双写、一次性兼容层和不可回收 feature flag。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 归档和删除语义混淆 | 数据丢失或行为不一致 | 明确新增 archive/unarchive 接口与 UI 动作 |
| cycle/roadmap 范围膨胀 | 交付延迟 | 先交付 cycle MVP，再扩 roadmap |
| 自动化规则过度设计 | 技术复杂度失控 | 首版限制为单条件 + 固定动作集合 |
| 指标口径不一致 | 数据不可用 | 在 API 响应中返回口径与窗口信息 |

## 验收检查

1. 用户可以完成归档与恢复，且列表行为一致。
2. 用户可以创建 cycle 并把 issue 关联到 cycle。
3. 系统可返回并展示基础任务流指标（throughput/lead time/cycle time）。
4. triage/automation 首版规则可用并可验证触发。
5. 相关文档与模块总览状态已同步回写。
