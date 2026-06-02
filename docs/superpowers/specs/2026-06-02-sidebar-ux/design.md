# 单能力 Design

## 目标

- 把 workspace 导航改成桌面 hybrid 壳层，突出“任务执行与收件箱处理”这一主任务。
- 让运行中的番茄钟在所有主页面 header 可见，并可一键回到 `/pomodoro`。
- 保持移动端友好，沿用顶部 toolbar + 抽屉导航，而不是做第二套小屏信息架构。

## 非目标

- 不把桌面改成纯顶部导航。
- 不重做普通计时器的业务逻辑。
- 不新增后端接口、通知模型或番茄设置持久化字段。
- 不在本轮定义全新的 workspace 信息架构命名系统。

## 当前架构基线

- 当前入口：`apps/workspace/src/features/layout/components/dashboard-layout.tsx` `DashboardLayout`
- 当前核心逻辑：`apps/workspace/src/features/layout/components/app-sidebar.tsx` `AppSidebar` 负责分组导航；`apps/workspace/src/features/layout/components/desktop-workspace-header.tsx` `DesktopWorkspaceHeader` 负责桌面顶部状态栏；`apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx` `MobileWorkspaceToolbar` 负责移动端顶部条；`apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx` `PomodoroStatusPill` 负责 header 番茄状态呈现。
- 当前存储或状态：导航入口由 `apps/workspace/src/features/layout/navigation.ts` `navigationGroups` 与 `workspaceFooterNav` 提供；移动端开关由 `apps/workspace/src/components/ui/sidebar.tsx` `SidebarProvider` 与 `Sidebar` 维护；番茄 header 状态由 `usePomodoroQuery` 驱动。
- 当前 UI 或接口：桌面是常驻左侧栏，移动端是顶部条加抽屉，番茄页面只在 `/pomodoro` 内拥有完整页面级 timer。

### 代码证据

- `apps/workspace/src/features/layout/navigation.ts` `navigationGroups`：说明当前主导航已从单层列表改为分组模型。
- `apps/workspace/src/features/layout/components/app-sidebar.tsx` `AppSidebar`：说明 search、new issue 已从侧边栏移出，侧边栏 header 只保留 workspace switcher。
- `apps/workspace/src/features/layout/components/desktop-workspace-header.tsx` `DesktopWorkspaceHeader`：说明桌面 header 已成为全局动作和番茄状态承载层。
- `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx` `PomodoroStatusPill`：说明番茄模式当前依附于 header 状态入口。
- `apps/workspace/src/components/ui/sidebar.tsx` `Sidebar`：说明移动端抽屉式 sidebar 已存在。

## 缺口定义

- 当前侧边栏没有围绕主任务排序，用户要先跨过大量规划和工具入口，才能进入高频工作区。
- 当前番茄钟状态缺少全局可见性，只能在侧边栏 footer 或 `/pomodoro` 页面内感知。
- 当前移动端虽有抽屉基础，但没有与新的桌面分组、header 状态形成一致体验。
- 当前时间能力重复暴露，`Pomodoro`、`My Time`、`Team Time`、`Track time` 的层级不清晰。

## 方案与权衡

### 方案 A：纯顶部导航

- 做法：把一级导航全部移到顶部，侧边栏取消或仅保留可折叠菜单。
- 优点：界面更像文档类网站，视觉上更轻。
- 风险：当前入口过多，顶部会同时承载 workspace switcher、搜索、创建、页面标题和番茄状态，水平拥挤最严重；同时需要重写 shell 结构。

### 方案 B：hybrid 导航，推荐

- 做法：桌面保留左侧分组导航，顶部只承载页面标题、搜索、创建、运行中的番茄状态；移动端保持顶部 toolbar + drawer。
- 优点：与现有 `SidebarProvider` 架构一致，信息改造主要集中在分组与 header 状态层，落地成本可控。
- 风险：仍需处理 `Notifications` 并入 `Inbox` 与 `Agents` 降位后的发现性。

### 方案 C：最小改动，仅改视觉层级

- 做法：不动导航信息架构，只通过分隔线、缩进、文案微调优化侧边栏。
- 优点：改动最小。
- 风险：根因没有被处理，任务执行、规划和工具仍混在一起，番茄钟也无法成为全局状态。

## 推荐方案

选择方案 B。

原因有三点：

1. 它顺着当前 `SidebarProvider + AppSidebar + MobileWorkspaceToolbar` 的壳层结构演进，不需要为纯顶部导航重写 shell。
2. 它能把“运行中的番茄钟要进 header”这一新约束放到最适合的位置，同时避免复制第二个 live timer 实例。
3. 它对移动端最自然，现有抽屉模式可以直接复用同一套分组，而不需要再设计一套底部 tab 或小屏侧栏逻辑。

## 数据模型或状态模型

- 导航分组模型：
  - `Focus`：`Inbox`、`My Work`、`Issues`
  - `Planning`：`Projects`、`Board`、`Backlog`、`Today`、`Upcoming`、`Calendar`
  - `Tools`：`Pomodoro`
  - `Workspace`：`Settings`、`Log out`
- header 番茄状态模型：
  - `idle`：header 不显示番茄状态 pill
  - `running-work`：显示剩余时间、工作阶段标记，点击跳转 `/pomodoro`
  - `running-break`：显示剩余时间、休息阶段标记，点击跳转 `/pomodoro`
- 关键约束：
  - 时间能力在导航中只允许一个一级入口
  - header 只显示番茄状态，不承担完整控制面板
  - `Notifications` 并入 `Inbox` 的信息架构，不再保留第二个一级入口

## 接口契约

### 输入

- 用户在桌面端点击左侧分组导航。
- 用户在任意主页面的 header 中看到番茄状态 pill 并点击返回 `/pomodoro`。
- 用户在移动端通过顶部按钮打开抽屉导航。

### 输出

- 桌面端侧边栏按分组渲染，低频项从主导航移出。
- 运行中的番茄钟在 header 中显示状态和剩余时间。
- 移动端抽屉与桌面沿用同一分组顺序。
- 错误场景：如果番茄状态读取失败，header 不显示错误形态的 pill，只回退为无状态。

## UI 或交互流程

### ASCII 线框

#### 桌面端，推荐的 hybrid 壳层

```text
+--------------------------------------------------------------------------------------------------+
| Page title                          Search                 New issue        [🍅 Focus 18:24]     |
+---------------------------+----------------------------------------------------------------------+
| Workspace switcher        |                                                                      |
|                           |                                                                      |
| Focus                     |                                                                      |
| > Inbox                   |                 Main content area                                     |
|   My Work                 |                 current page, list, board, etc.                       |
|   Issues                  |                                                                      |
|                           |                                                                      |
| Planning                  |                                                                      |
|   Projects                |                                                                      |
|   Board                   |                                                                      |
|   Backlog                 |                                                                      |
|   Today                   |                                                                      |
|   Upcoming                |                                                                      |
|   Calendar                |                                                                      |
|                           |                                                                      |
| Tools                     |                                                                      |
|   Pomodoro                |                                                                      |
|                           |                                                                      |
| Workspace                 |                                                                      |
|   Settings                |                                                                      |
|   Log out                 |                                                                      |
+---------------------------+----------------------------------------------------------------------+
```

设计意图：

- 左侧只做导航分组，不再塞全局状态。
- 顶部只承载页面级全局动作和运行中的番茄钟状态。
- `Inbox / My Work / Issues` 固定成为第一视线层。

#### 移动端，顶部 toolbar + drawer

```text
+----------------------------------------------------------------------------------+
| [≡]  Inbox                                      [+]              [🍅 18:24]      |
+----------------------------------------------------------------------------------+
|                                                                                  |
|                               Main content area                                  |
|                                                                                  |
+----------------------------------------------------------------------------------+

Drawer
+----------------------------------+
| Workspace switcher               |
|                                  |
| Focus                            |
| > Inbox                          |
|   My Work                        |
|   Issues                         |
|                                  |
| Planning                         |
|   Projects                       |
|   Board                          |
|   Backlog                        |
|   Today                          |
|   Upcoming                       |
|   Calendar                       |
|                                  |
| Tools                            |
|   Pomodoro                       |
|                                  |
| Workspace                        |
|   Settings                       |
|   Log out                        |
+----------------------------------+
```

设计意图：

- 小屏继续让主内容优先，导航放进 drawer。
- header 里的番茄状态缩成 pill，不遮挡标题和主操作。
- drawer 与桌面沿用同一套分组，避免双重心智模型。

1. 用户进入任意主页面。
2. 桌面端看到精简后的左侧三段导航，顶部看到页面标题、搜索、创建和番茄状态位置。
3. 如果番茄钟正在运行，header 出现状态 pill，点击后进入 `/pomodoro`。
4. 如果用户在移动端，顶部 toolbar 保持页面标题与创建入口，左上按钮打开抽屉，抽屉内沿用与桌面一致的分组。
5. 用户在 `Inbox / My Work / Issues` 间切换时，不再被时间工具与规划视图打断。

## 权限、边界条件、异常路径

- 谁可以使用：所有登录用户。
- 哪些输入非法：无效导航项或无法识别的番茄状态一律不渲染 header pill。
- 失败时如何处理：
  - 番茄状态获取失败时，header 回退到无状态，不额外制造错误 UI。
  - 移动端 drawer 打开失败时，保留当前页面内容，不阻断主流程。
- 边界条件：
  - `/pomodoro` 页面本身已经渲染页面级 timer，header 状态是回路入口，不是第二个主视图。
  - 普通计时器仍可存在于时间管理域内，但不再和番茄钟共同占用主导航注意力。

## 实现约束

- 执行阶段不能自行把 hybrid 改成纯顶部导航。
- 必须复用现有 `SidebarProvider`、`SidebarTrigger`、`AppSidebar`、`MobileWorkspaceToolbar` 与番茄相关 hooks / 组件。
- 禁止通过新建第二个番茄倒计时实例来实现 header 状态。
- 禁止保留 `Pomodoro` 导航、header pill、footer 番茄入口三处并行高可见暴露。
- 若需要保留 `Notifications` 路由兼容，必须先更新设计文档说明兼容范围。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| `Inbox` 与 `Notifications` 收口后，用户短期不适应 | 高 | 已保留未读 badge，`/`、`/inbox`、`/notifications` 均映射为 `Inbox` |
| header 番茄状态与 sidebar footer 状态并存 | 高 | 已把番茄高可见状态上提到 `PomodoroStatusPill`，sidebar footer 只保留 settings / logout |
| `Agents` 降位后影响团队功能发现性 | 中 | 已接受折中：`Agents` 放入 `Workspace` 分组，仍保留一级 sidebar 可见性 |
| 移动端抽屉过长 | 中 | 已保持与桌面一致的分组语义，移动端 toolbar 只显示页面标题、pill 和主动作 |

## 验收检查

1. 已验收：桌面主导航按 `Focus`、`Planning`、`Tools`、`Workspace` 分组，主任务相关入口位于第一组。
2. 已验收：时间能力在导航中只保留 `Pomodoro` 1 个一级入口。
3. 已验收：运行中的番茄钟通过 `PomodoroStatusPill` 在桌面和移动端 header 可见，并可点击回到 `/pomodoro`。
4. 已验收：移动端通过顶部 toolbar 打开抽屉，抽屉分组与桌面一致。
5. 已验收：侧边栏 footer 不再同时承担番茄钟的高可见主入口。
