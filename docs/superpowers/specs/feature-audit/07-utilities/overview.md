# 模块级设计总览

## 目标与范围

- 本轮覆盖 `07-utilities` 下的三个二级能力：`7.1 屏蔽列表`、`7.2 书签`、`7.3 笔记`。
- 本轮只补设计包与模块总览，不进入代码实现，不改动其他模块或 `dashboard.md`。
- 本轮重点判断哪些能力仍停留在产品边界假设，哪些已经有番茄 note 残片可作为后续起点。

## 能力列表

| 能力 | 当前状态 | 优先级 | 备注 |
| --- | --- | --- | --- |
| 7.1 屏蔽列表 | 无实现 | P3 | 明显属于系统级行为假设，当前阶段非目标 |
| 7.2 书签 | 无实现 | P3 | 仍是产品边界假设，若重启应收窄成“个人保存视图/对象书签” |
| 7.3 笔记 | 仅有残片 | P2 | 现有残片只存在于番茄完成时写入 `time_entry.description` |

## 当前状态基线

### 通用基线

- 证据：`apps/workspace/src/router.tsx` `settingsRoute` / `pomodoroRoute` / `myTimeRoute`；结论：工作区应用当前有设置、番茄、时间视图等入口，但没有 utilities 聚合入口，也没有 notes / bookmarks / blocklist 路由。
- 证据：`product-overview.md` `当前阶段的项目目标与展望`；结论：当前阶段主轴是任务、时间、项目、提醒与 AI 协作，不是工具集扩展。

### 7.1 屏蔽列表

- 证据：`apps/workspace/src`、`server` `rg(blocklist|website block|app block|custom block rule)`；结论：代码与后端均未找到屏蔽列表实现。
- 证据：`product-overview.md` `当前阶段的项目目标与展望`；结论：产品主轴没有系统级网站/应用阻断能力描述，因此 7.1 仍是边界假设。

### 7.2 书签

- 证据：`apps/workspace/src`、`server` `rg(bookmark|bookmarks)`；结论：仓库没有书签实体、路由或 API。
- 证据：`apps/workspace/src/features/issues/stores/view-store.ts` `useIssueViewStore`；结论：现有最接近“书签”起点的是个人本地持久化视图筛选，但它仍不是可分享或可管理的书签域。

### 7.3 笔记

- 证据：`apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `noteInputValue`；结论：前端已有番茄 note 输入框。
- 证据：`apps/workspace/src/shared/types/pomodoro.ts` `CompletePomodoroBody`；结论：番茄完成请求已携带 `note` 字段。
- 证据：`server/internal/handler/pomodoro.go` `completePomodoro`；结论：当前 note 会落到 `time_entry.description`，并没有独立 note 实体。
- 证据：`server/pkg/db/generated/models.go` `TimeEntry`；结论：数据库模型只有 `Description`，没有 note domain。
- 证据：`product-overview.md` `MMF-2`；结论：知识管理明确写成“后续再增强”，说明独立笔记域不在当前阶段主轴。

## 非目标

- 不把 7.1、7.2 误判成“只差一个前端页面”的轻量能力。
- 不把 7.3 的番茄 note 残片直接等同为“已有完整笔记系统”。
- 不在 utilities 模块内引入超出当前产品边界的系统守护、知识库或通用收藏中心。

## 优先级与推进顺序

1. **先写清 7.3 笔记迁移边界**：因为已有番茄 note 残片，是唯一有真实代码起点的能力。
2. **再保留 7.2 书签的收敛方向**：若未来需要，应优先收敛到个人保存视图 / 对象书签，而不是泛化收藏站。
3. **最后继续挂起 7.1 屏蔽列表**：系统级阻断能力对 daemon、权限、平台兼容都更敏感，当前不应进入实现。

## 共享约束

- 证据：`apps/workspace/src/router.tsx` `routeTree`；结论：utilities 模块当前没有一层现成路由容器，因此任一能力都不能假设已有独立导航层。
- 证据：`product-overview.md` `MMF-2`；结论：知识管理仍属后续增强，utilities 里的笔记与书签都不能擅自扩展成完整知识库产品。
- 证据：`server/internal/handler/pomodoro.go` `completePomodoro`；结论：7.3 的现有 note 残片必须尊重时间记录作为当前事实源，迁移前不能直接改写历史描述字段。
- 证据：`apps/workspace/src`、`server` `rg(blocklist|website block|app block|custom block rule)`；结论：7.1 若重启，必然依赖新运行时或 daemon 能力，不能假设纯 Web 即可完成。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| 7.1 缺系统级运行时 | 纯前端实现无法真正屏蔽网站/应用 | 明确标记为非当前阶段目标 |
| 7.2 缺对象模型 | 很容易做成边界模糊的杂项收藏箱 | 若未来重启，先收窄到“个人保存视图/对象书签” |
| 7.3 现有 note 只有时间记录描述 | 迁移时容易破坏工作日志语义 | 先定义 note 与 `time_entry.description` 的迁移边界 |
| 产品主轴不在 utilities | 容易占用当前阶段开发容量 | 模块内显式标低优先级与进入条件 |

## 回写规则

- 若未来 7.3 从番茄 note 残片演进为独立域，必须先更新 `7.3-notes/design.md` 与本 `overview.md`，再进入实现。
- 若 7.1 或 7.2 获得产品确认，先回写优先级与边界，再开始任务拆分。
- utilities 模块回写只限本模块文档，不改 `dashboard.md`。
