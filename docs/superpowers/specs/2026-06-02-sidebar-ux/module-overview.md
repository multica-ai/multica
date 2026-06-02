# 模块级设计总览

## 目标与范围

- 目标：重组 workspace 壳层导航，降低侧边栏认知负担，同时把运行中的番茄钟提升为跨页面可见的 header 状态。
- 本次包含：桌面端 hybrid 导航结构、移动端抽屉导航映射、Pomodoro header 状态入口、侧边栏分组与低频项归位。
- 本次明确不包含：纯顶部导航重写、后端接口改动、时间跟踪数据模型改造、全量信息架构重命名。

## 能力列表

| 能力 | 当前状态 | 优先级 | 备注 |
| --- | --- | --- | --- |
| 侧边栏信息架构重组 | 待设计落地 | P1 | 把任务执行、规划视图、工具从同层堆叠改成分组导航 |
| Header 番茄钟状态 | 待设计落地 | P1 | 运行中时跨主页面可见，点击回到 `/pomodoro` |
| 移动端导航映射 | 待设计落地 | P1 | 保持顶部 toolbar + drawer，不照搬桌面常驻侧边栏 |

## 当前状态基线

### 侧边栏信息架构重组

- 证据：`apps/workspace/src/features/layout/navigation.ts` `primaryNav`
- 当前行为：`Issues`、`Agents`、`Projects`、`Board`、`Backlog`、`Today`、`Upcoming`、`My Work`、`My Time`、`Team Time`、`Pomodoro`、`Calendar`、`Notifications` 全部处于同一层级。
- 当前缺口：任务执行、规划视图、时间工具和消息入口混排，侧边栏没有体现主任务优先级。

### Header 番茄钟状态

- 证据：`apps/workspace/src/features/layout/components/app-sidebar.tsx` `AppSidebar`
- 当前行为：全局计时入口位于 `SidebarFooter` 中，通过 `GlobalTimerWidget` 暴露。
- 当前缺口：运行中的番茄钟没有进入 header，跨页面可见性不足。

### 移动端导航映射

- 证据：`apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx` `MobileWorkspaceToolbar`
- 当前行为：移动端已有顶部工具栏加抽屉式侧边栏。
- 当前缺口：移动端虽已有壳层基础，但菜单分组和番茄状态没有与桌面重构后的结构保持一致。

## 非目标

- 不把桌面导航整体改成纯顶部导航。
- 不新增 `Notifications` 与 `Inbox` 两套并行消息主入口。
- 不在本轮引入新的计时器运行时实例或双写番茄状态。

## 优先级与推进顺序

1. 先定义桌面端 hybrid 壳层与分组规则。
2. 再定义 header 中番茄钟状态的呈现与行为。
3. 最后把同一套分组和状态映射到移动端抽屉与顶部工具栏。

这样排序，是因为桌面信息架构决定移动端映射方式，而 header 状态又依赖桌面和移动端都共用同一套番茄状态语义。

## 共享约束

- 共享交互约束：运行中的番茄钟必须在所有主页面 header 可见。
- 共享信息架构约束：时间能力在导航中只能保留一个入口。
- 共享技术约束：必须复用现有 `SidebarProvider`、`AppSidebar`、`MobileWorkspaceToolbar`、`GlobalTimerWidget` 与 `PomodoroTimer`，不能通过第二个计时器实例硬拼状态。
- 共享移动端约束：移动端保持主内容优先，导航通过顶部按钮打开抽屉。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| `Notifications` 并入 `Inbox` 后的可发现性下降 | 用户可能找不到消息入口 | 保留清晰文案、未读 badge，并在切换期避免同时保留第二个一级导航 |
| `GlobalTimerWidget` 与 header 番茄状态重复 | 容易出现两个运行中状态源 | 由设计明确 header 只承载番茄状态展示，sidebar footer 去掉番茄模式的主暴露 |
| 移动端直接照搬桌面分组文案 | 抽屉过长，首屏信息密度过高 | 保留同一分组语义，但在移动端压缩辅助说明和低频入口 |

## 回写规则

- 实现后回写本目录下 `research.md`、`design.md`、`tasks.md` 的完成状态。
- 若实现中发现需要保留 `Notifications` 独立一级入口，必须先更新 `design.md` 再改代码。
- 若执行阶段决定改成纯顶部导航，必须新开设计，不允许在本设计包上直接扩 scope。
