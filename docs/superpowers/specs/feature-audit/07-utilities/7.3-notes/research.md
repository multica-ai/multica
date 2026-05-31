# 单能力 Research

## 调研目标

1. 确认仓库中现有“笔记”残片到底落在哪里。
2. 判断 7.3 是否已具备独立笔记域基础，还是只有番茄 note 的输入与落库残片。
3. 为番茄 note 到独立笔记域的迁移边界提供证据。

## 现状链路

1. 入口：`apps/workspace/src/router.tsx` 当前没有 notes 路由。
2. 前端残片：`apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` 在番茄完成弹层里维护 `noteInputValue`。
3. 请求模型：`apps/workspace/src/shared/types/pomodoro.ts` `CompletePomodoroBody` 包含 `note?: string`。
4. 服务端落库：`server/internal/handler/pomodoro.go` `completePomodoro` 把 `note` 映射为 `time_entry.description`；为空时回退 `"Pomodoro 专注"`。
5. 数据模型：`server/pkg/db/generated/models.go` `TimeEntry` 只有 `Description`，没有 note 实体。
6. 输出结果：7.3 当前只存在“番茄 note 写入时间记录描述”的残片，不存在独立笔记域、页面、搜索或数据模型。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/router.tsx` | `routeTree` | 应用中没有 notes 页面入口。 |
| `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` | `noteInputValue` | 番茄完成流程确实已有 note 输入。 |
| `apps/workspace/src/shared/types/pomodoro.ts` | `CompletePomodoroBody` | 番茄完成请求携带 note 字段。 |
| `apps/workspace/src/features/time-tracking/hooks/use-pomodoro.ts` | `completePomodoro` | 前端会把 note 一起提交给完成接口。 |
| `server/internal/handler/pomodoro.go` | `completePomodoro` | 服务端把 note 写入 `time_entry.description`，不是写入独立 note 表。 |
| `server/pkg/db/generated/models.go` | `TimeEntry` | 数据库当前只有时间记录描述字段，没有 note domain。 |
| `product-overview.md` | `MMF-2` | 知识管理被放到后续增强，独立笔记域不是当前阶段主轴。 |

## 空搜索证据

| 路径 | 符号 / 搜索关键词 | 结论 |
| --- | --- | --- |
| `apps/workspace/src`、`server` | `rg(global note|note entity|notes page|note search|markdown note)` | 未找到匹配，说明没有独立笔记实体、页面或搜索能力。 |
| `apps/workspace/src`、`server` | `rg(path: "notes"|bookmark|bookmarks)` | 未找到匹配，说明 notes / bookmarks 都没有路由级支撑。 |

## 数据模型或状态流

- 当前状态流：番茄弹层输入 `note` -> `CompletePomodoroBody.note` -> `completePomodoro` -> `time_entry.description`。
- 当前事实源：在独立笔记域不存在之前，番茄 note 的唯一持久化事实源就是 `time_entry.description`。
- 当前缺口：没有 note id、note source、note backlink、note search、note list。

## 边界条件

- 证据：`server/internal/handler/pomodoro.go` `completePomodoro`；结论：历史番茄 note 已经和时间记录描述合并存储，迁移时不能直接丢弃旧 description。
- 证据：`product-overview.md` `MMF-2`；结论：独立知识/笔记域应降级，当前阶段只适合定义迁移边界。
- 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：没有现成导航容器承接 notes 子系统。

## 未决问题

1. 未来独立笔记域是围绕 issue / time entry 的工作笔记，还是通用知识笔记；当前产品未定。
2. 历史 `time_entry.description` 中哪些值应回填成 note：仅用户显式输入，还是包括默认 `"Pomodoro 专注"` 文案；必须先定迁移规则。
3. 笔记是否需要 Markdown、全文检索、标签；当前没有任何实现证据，不应自行补 scope。
