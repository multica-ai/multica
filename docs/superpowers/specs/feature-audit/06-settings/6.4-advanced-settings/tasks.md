# 单能力 Tasks

## 实现目标

当前不直接实现 6.4；先把五个条目的 owner 域、进入条件和未来文件范围固定下来，作为后续执行合同。

## 前置依赖

- 产品先确认 `data retention`、`debug mode`、`reset all settings` 的权限边界。
- 6.1、6.2、6.3 至少完成统一 schema 设计，才能讨论 reset framework。

## 任务切片

### Task 1

- 目标：收敛 idle detection 的 owner 域。
- 文件：`apps/workspace/src/features/time-tracking/`、`server/internal/handler/`、`docs/superpowers/specs/feature-audit/02-time-management/`
- 改动：在时间追踪域补 idle monitor 设计，而不是放进通用高级设置。
- 完成定义：`idle detection` 有独立时间管理实现方案。
- 验证方式：对应时间追踪文档与测试计划更新。

### Task 2

- 目标：收敛 autosave interval 的 owner 域。
- 文件：`apps/workspace/src/features/issues/stores/draft-store.ts`、编辑器相关 store / hook
- 改动：先确认草稿保存是否真的需要“间隔配置”，再决定是否从固定 persist 扩展为可调策略。
- 完成定义：明确这是产品需求还是实现细节。
- 验证方式：设计评审通过，文档更新。

### Task 3

- 目标：收敛 data retention 的 owner 域。
- 文件：`server/pkg/db/queries/`、`server/internal/handler/`、`apps/workspace/src/features/settings/components/workspace-tab.tsx`
- 改动：如进入实现，应按 workspace admin policy 设计，而不是个人设置。
- 完成定义：有清晰的管理员入口、后端策略与回收规则。
- 验证方式：接口设计评审 + 后端验证计划。

### Task 4

- 目标：评估 debug mode 与 reset framework。
- 文件：`apps/workspace/src/features/settings/`、`apps/workspace/src/shared/`、相关服务端偏好文件
- 改动：只有在统一 schema 存在后，才允许实现 reset；debug mode 需单独确定面向对象。
- 完成定义：不再存在“高级设置兜底项”。
- 验证方式：设计文档与任务拆分完成。

### Task 5

- 目标：回写审计台账。
- 文件：`docs/superpowers/specs/feature-audit/06-settings/6.4-advanced-settings/spec.md`、`docs/superpowers/specs/feature-audit/06-settings/overview.md`
- 改动：更新低优先级标注、owner 域与状态。
- 完成定义：文档准确反映“待产品决策”的现实。
- 验证方式：人工复核。

## 执行顺序说明

必须先拆 owner，再决定是否有实现。6.4 当前不允许跳过决策门直接编码。

## 回写要求

- 若未来任一子项进入实现，先更新本 `design.md` 的 owner 判断。
- 只在状态真实变化时回写 `spec.md` 和模块 `overview.md`。
- 不得把 6.4 的未决内容静默转移到其他模块实现后不回写文档。
