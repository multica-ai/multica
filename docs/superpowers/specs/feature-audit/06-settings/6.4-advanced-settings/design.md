# 单能力 Design

## 目标

- 明确 6.4 的 ownership matrix，防止“高级设置”沦为杂项兜底页。
- 对五个条目给出是否进入当前阶段的明确判断。

## 非目标

- 不在当前阶段直接实现完整高级设置页。
- 不把时间追踪、workspace policy、开发诊断混成一个用户页签。
- 不允许执行 Agent 自行决定数据保留和调试权限策略。

## 当前架构基线

- 当前入口：无 advanced tab。
- 当前核心逻辑：`idle detection` 只在时间追踪缺口文档里存在；`autosave` 只以固定 `persist` 实现存在。
- 当前存储或状态：没有统一 advanced schema。
- 当前 UI 或接口：无。

### 代码证据

- `apps/workspace/src/features/settings/components/settings-page.tsx` `accountTabs`：当前无入口。
- `docs/superpowers/specs/feature-audit/02-time-management/2.1-time-tracking/spec.md` `缺口`：idle detection 属于时间管理缺口。
- `apps/workspace/src/features/issues/stores/draft-store.ts` `useIssueDraftStore`：自动保存存在但不可配置。

## 缺口定义

- 6.4 不是单一能力，而是五个 ownership 不同的条目被同一标题打包。
- 如果不先拆 ownership，任何实现都会把低优先级或产品边界问题错误前置。

## 方案与权衡

### 方案 A：做一个“高级设置”杂项页，把五项都塞进去

- 做法：直接补 tab 与表单。
- 优点：界面上看起来最完整。
- 风险：会把时间追踪、workspace policy、开发者开关、重置逻辑混在一起，后续难以维护。

### 方案 B：先做 ownership matrix，只允许“重置框架”在后续依赖完成后落地，推荐

- 做法：把五项拆成时间管理、编辑器持久化、workspace policy、开发诊断、设置重置五类；当前阶段仅保留文档与边界，不进入实现。
- 优点：符合仓库现状，也避免把明显未决的问题误做成当前目标。
- 风险：用户短期内看不到“高级设置页”，但这比错误实现更可控。

### 方案 C：全部降为开发配置

- 做法：把五项都从产品范围移除。
- 优点：最省事。
- 风险：`reset all settings` 这类真正可能进入产品的需求会被过度后置。

## 推荐方案

选择方案 B。

本轮把 6.4 明确标为 **P3 / 待产品决策**。五个子项里，只有“重置所有设置”在未来可能作为横向能力存在；其余条目都应先回到对应域确认 owner，再决定是否进入实现。

## 数据模型或状态模型

```text
advanced-ownership
├─ idle_detection -> time-tracking domain
├─ autosave_interval -> editor / draft domain
├─ data_retention -> workspace policy domain
├─ debug_mode -> diagnostics domain
└─ reset_all_settings -> cross-setting reset framework
```

- 当前阶段不新增统一 advanced settings table。
- `reset_all_settings` 依赖 6.1 / 6.2 / 6.3 先完成 schema 化。

## 接口契约

### 输入

- 暂无产品输入接口。
- 若未来进入实现，必须分别从对应 owner 域收敛字段。

### 输出

- 当前阶段输出是 ownership matrix 与低优先级结论。
- 不新增对外 API，不新增伪设置页。

## UI 或交互流程

### 页面交互流

```text
/settings
  -> 当前无 Advanced 入口
  -> 若未来重启
     -> 先按 owner 域拆解
     -> 再决定是否需要独立 Advanced 容器
```

### 状态机

```text
[undefined]
   -> [owner-mapped]
   -> [product-approved]
   -> [implementable]
```

### 数据变化流

```text
time-tracking spec / draft-store / workspace policy docs
   -> ownership matrix
   -> future per-domain settings schema
   -> optional reset framework
```

## 权限、边界条件、异常路径

- 谁可以使用：当前阶段无人可用，因为没有入口。
- 哪些输入非法：任何试图在本轮直接补 `data_retention`、`debug_mode` 字段的实现都视为越界。
- 失败时如何处理：若实现阶段发现需要 6.4 支撑，先回到文档更新 ownership，再继续。

## 实现约束

- 不要创建一个空壳 `AdvancedTab` 来伪装功能存在。
- 不要把 `idle detection` 直接从时间追踪域挪到 settings 域。
- 不要把 workspace policy 当成普通用户偏好。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 把杂项问题统一塞进一个页签 | 设计边界失真 | 先做 ownership matrix，当前不实现 |
| 误把 developer debug 开关暴露给普通用户 | 权限和支持成本上升 | 把 debug mode 标为待产品决策 |
| 没有统一 schema 就做 reset all | 重置行为不可预测 | 只有在 6.1/6.2/6.3 统一 schema 后再进入实现 |

## 验收检查

1. 6.4 被明确标注为 P3 / 待产品决策。
2. 五个条目都被映射到明确 owner 域。
3. 当前阶段不出现伪实现或空壳高级设置页。
4. 若后续需要实现，执行 Agent 能从 `tasks.md` 看到按域拆解的进入顺序。
