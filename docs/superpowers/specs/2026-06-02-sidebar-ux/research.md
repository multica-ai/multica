# 单能力 Research

## 调研目标

1. 确认当前 workspace 壳层、侧边栏和移动端导航分别由哪些组件控制。
2. 确认番茄钟与普通计时当前如何在 UI 中暴露。
3. 确认移动端是否已有可复用的抽屉模式，而不是从零设计。
4. 确认顶部导航是否适合成为桌面主导航。

## 现状链路

1. 入口：`apps/workspace/src/features/layout/components/dashboard-layout.tsx` `DashboardLayout` 把登录后的页面包在 `SidebarProvider` 中，并同时渲染 `AppSidebar`、`SidebarInset` 与 `MobileWorkspaceToolbar`。
2. 导航定义：`apps/workspace/src/features/layout/navigation.ts` `navigationGroups` 和 `workspaceFooterNav` 提供分组导航与 footer 入口。`isWorkspaceNavActive` 负责 active 判定，`getWorkspacePageTitle` 负责 shell 标题解析。
3. 桌面端输出：`apps/workspace/src/features/layout/components/app-sidebar.tsx` `AppSidebar` 使用 `navigationGroups` 渲染 `Focus`、`Planning`、`Tools`、`Workspace` 分组，footer 只渲染 `workspaceFooterNav`。
4. 桌面 header：`apps/workspace/src/features/layout/components/desktop-workspace-header.tsx` `DesktopWorkspaceHeader` 承载页面标题、全局搜索、新建 issue 和 `PomodoroStatusPill`。
5. 移动端输出：`apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx` `MobileWorkspaceToolbar` 在 `md:hidden` 下显示页面标题、`PomodoroStatusPill` 和新建 issue，通过 `SidebarTrigger` 打开抽屉导航。
6. 抽屉机制：`apps/workspace/src/components/ui/sidebar.tsx` `Sidebar` 在 `isMobile` 为真时使用 `Sheet` 渲染 sidebar 内容。
7. 计时链路：`apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx` `PomodoroStatusPill` 读取 `usePomodoroQuery`，只在运行中显示阶段与剩余时间并链接到 `/pomodoro`。
8. 番茄主页面：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage` 在 `/pomodoro` 路由内渲染大尺寸 `PomodoroTimer variant="page"`，header pill 只作为返回入口。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/layout/components/dashboard-layout.tsx` | `DashboardLayout` | 当前已是侧边栏壳层，桌面改成纯顶部导航不属于微调，而是 shell 级改造。 |
| `apps/workspace/src/features/layout/navigation.ts` | `navigationGroups` | 当前导航已按 `Focus`、`Planning`、`Tools`、`Workspace` 分组，时间能力只保留 `Pomodoro` 一个入口。 |
| `apps/workspace/src/features/layout/components/app-sidebar.tsx` | `AppSidebar` | 当前 sidebar header 只保留 workspace switcher，footer 只保留 settings / logout。 |
| `apps/workspace/src/features/layout/components/desktop-workspace-header.tsx` | `DesktopWorkspaceHeader` | 桌面 header 当前承载页面标题、搜索、新建 issue 和番茄状态 pill。 |
| `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx` | `PomodoroStatusPill` | 番茄状态已从 sidebar footer 主暴露上提为 shell header 状态。 |
| `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` | `PomodoroPage` | `/pomodoro` 页面已有独立番茄主视图，说明 header 状态更适合做全局跳转入口，而不是复制完整番茄 UI。 |
| `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx` | `MobileWorkspaceToolbar` | 移动端顶部工具栏当前显示页面标题、番茄状态和新建 issue。 |
| `apps/workspace/src/components/ui/sidebar.tsx` | `Sidebar` | 移动端 sidebar 已由 `Sheet` 承载，抽屉式导航是现成模式。 |

## 数据模型或状态流

- 导航状态：
  - 读取点：`navigation.ts` 中的 `navigationGroups`、`workspaceFooterNav`
  - 消费点：`AppSidebar`
  - 激活规则：`isWorkspaceNavActive`
  - 标题规则：`getWorkspacePageTitle`
- 壳层状态：
  - `SidebarProvider` 管理 `open`、`openMobile`、`isMobile`
  - `SidebarTrigger` 在移动端控制抽屉显示
- 番茄状态：
  - `PomodoroStatusPill` 读取 `usePomodoroQuery()` 的当前 session
  - `getPomodoroRemainingSeconds`、`getPomodoroHeaderLabel`、`formatPomodoroTimer` 负责 header 展示计算
  - `/pomodoro` 路由内通过 `PomodoroPage` 挂载页面级 `PomodoroTimer`

## 边界条件

- 顶部导航边界：当前 header 已经需要承载 workspace switcher、搜索和创建动作。如果再承载一整排一级导航，水平空间会被快速耗尽，桌面信息密度更差。
- 重复状态边界：`GlobalTimerWidget` 已对 `/pomodoro` 路由做双实例规避，说明番茄状态不能简单复制第二个 live timer 组件。
- 移动端边界：小屏主内容优先，导航只能放在抽屉内，不能把桌面常驻列宽原样搬过去。
- 术语边界：`Notifications` 与 `Inbox` 都指向消息处理心智，本轮若同时保留为两个一级入口，会继续制造重复。

## 已关闭问题

1. `Agents` 已保留在 `Workspace` 分组，作为团队运行时相关入口。
2. `Notifications` 不再作为一级入口展示，`/`、`/inbox`、`/notifications` 均解析为 `Inbox` active/title。
3. header 中番茄状态仅显示阶段和剩余时间并跳转 `/pomodoro`，不承载暂停、停止等完整控制面板。
