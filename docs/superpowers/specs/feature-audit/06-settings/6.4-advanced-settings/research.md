# 单能力 Research

## 调研目标

1. 确认高级设置当前是否有任何产品面或实现残片。
2. 拆分空闲检测、自动保存间隔、数据保留时长、调试模式、重置所有设置分别属于哪个域。
3. 判断哪些条目是低优先级、非当前阶段目标或待产品决策。

## 现状链路

1. 入口：`apps/workspace/src/features/settings/components/settings-page.tsx` 当前没有 advanced 页签。
2. 搜索结果：代码仓库没有 `idle detection`、`autosave interval`、`data retention`、`reset all settings` 等设置面实现。
3. 相关残片：时间追踪审计已把 idle detection 识别为时间管理缺口；issue 草稿持久化已存在固定 `persist` 逻辑，但没有可配置间隔。
4. 输出结果：6.4 不是“少一个页面”，而是多个未统一归属的问题集合。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/settings/components/settings-page.tsx` | `accountTabs` | 当前设置页没有 advanced 或同类入口。 |
| `docs/superpowers/specs/feature-audit/02-time-management/2.1-time-tracking/spec.md` | `缺口` | `idle detection` 已被识别为时间追踪能力缺口，不应直接视作通用高级设置已决需求。 |
| `apps/workspace/src/features/issues/stores/draft-store.ts` | `useIssueDraftStore` | 草稿自动保存目前依赖 `persist` 中间件，仓库没有“保存间隔”参数层。 |
| `apps/workspace/src/features/settings/components/workspace-tab.tsx` | `WorkspaceTab` | workspace 设置已单独承接共享配置，说明数据保留时长若落地，更接近 workspace policy。 |
| `product-overview.md` | `当前阶段的项目目标与展望` | 当前阶段强调任务、时间、项目、提醒与 AI 辅助，没有把 6.4 条目列入主轴。 |

## 空搜索证据

| 路径 | 符号 / 搜索关键词 | 结论 |
| --- | --- | --- |
| `apps/workspace/src`、`server` | `rg(idle detection|autosave interval|data retention|reset all settings)` | 未找到匹配，说明高级设置条目没有现成实现。 |
| `apps/workspace/src`、`server` | `rg(debug mode|reset all settings|调试模式|重置所有设置)` | 未找到匹配，说明调试模式与全量重置也没有现成实现。 |

## 数据模型或状态流

- `idle detection`：当前只在时间追踪审计中以缺口形式出现，没有状态模型。
- `autosave interval`：`draft-store.ts` 仅说明“已自动持久化”，没有可调间隔。
- `data retention`：仓库没有 retention policy 模型，更接近 workspace / backend policy。
- `debug mode`：当前没有设置页 debug flag，也未见用户级调试偏好。
- `reset all settings`：要想成立，前提是 6.1、6.2、6.3 都先有统一 schema。

## 边界条件

- 证据：`docs/superpowers/specs/feature-audit/02-time-management/2.1-time-tracking/spec.md` `缺口`；结论：idle detection 是时间管理域问题，不是单独高级设置就能闭环。
- 证据：`apps/workspace/src/features/issues/stores/draft-store.ts` `persist`；结论：自动保存目前是开发实现细节，不等于已经支持“自动保存间隔设置”。
- 证据：`apps/workspace/src/features/settings/components/workspace-tab.tsx` `WorkspaceTab`；结论：数据保留时长若进入产品，应受 workspace 权限边界控制。

## 未决问题

1. `数据保留时长` 是工作区管理员策略、系统运维策略，还是个人偏好；当前没有定论。
2. `调试模式` 面向谁：普通用户、管理员、开发者；当前没有产品叙述支持。
3. `重置所有设置` 是只重置本地偏好，还是连服务端 ntfy / workspace 配置一起清空；必须先明确分层。
