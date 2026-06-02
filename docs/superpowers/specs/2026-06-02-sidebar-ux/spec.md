# 单能力 Spec

## 背景

当前 workspace 侧边栏承载了过多同层级入口，任务执行、项目规划、时间工具和消息入口被放在一个连续列表里。用户在“处理任务与收件箱”为主任务时，需要先跨过大量不相关入口。与此同时，番茄钟运行状态只在侧边栏 footer 里可见，无法成为跨页面的全局状态。

这次设计的触发点有三个：

1. 需要提高桌面端侧边栏的可扫描性和主任务聚焦能力。
2. 需要让运行中的番茄钟进入 header，跨主页面可见。
3. 需要保证移动端仍然友好，不把桌面常驻侧边栏生硬搬到小屏。

## 范围

- 本次覆盖：
  - 桌面端 hybrid 导航结构
  - 侧边栏分组与入口优先级
  - header 中的番茄钟运行状态
  - 移动端 toolbar + drawer 的映射策略
- 本次不覆盖：
  - 纯顶部导航重做
  - 后端数据模型或时间接口改造
  - 新增通知系统或收件箱业务规则
  - 普通计时器的业务逻辑改写

## 当前状态

- 桌面壳层采用 `SidebarProvider + AppSidebar + SidebarInset` 的侧边栏布局。
- 主导航当前由 `navigationGroups` 驱动，按 `Focus`、`Planning`、`Tools`、`Workspace` 分组。
- 侧边栏 footer 只保留 `Settings` 与 `Log out`，不再承担番茄钟高可见状态。
- 桌面 header 已由 `DesktopWorkspaceHeader` 承载页面标题、搜索、新建 issue 和 `PomodoroStatusPill`。
- 移动端已有顶部 toolbar 和抽屉式 sidebar，并通过 `getWorkspacePageTitle` 与同一套分组策略对齐。

## 证据

- `apps/workspace/src/features/layout/components/dashboard-layout.tsx` `DashboardLayout`：桌面壳层当前围绕侧边栏容器组织，改纯顶部导航会触及整个 shell。
- `apps/workspace/src/features/layout/navigation.ts` `navigationGroups`：当前把任务、规划、工具、工作区入口拆成稳定分组。
- `apps/workspace/src/features/layout/components/app-sidebar.tsx` `AppSidebar`：侧边栏当前渲染分组导航，footer 渲染 `workspaceFooterNav`。
- `apps/workspace/src/features/layout/components/desktop-workspace-header.tsx` `DesktopWorkspaceHeader`：桌面 header 当前挂载页面标题、搜索、新建 issue 和 `PomodoroStatusPill`。
- `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx` `PomodoroStatusPill`：运行中的番茄状态通过 header pill 暴露。
- `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx` `MobileWorkspaceToolbar`：移动端当前采用顶部 toolbar，而不是常驻侧边栏，并显示当前页面标题与番茄状态。
- `apps/workspace/src/components/ui/sidebar.tsx` `Sidebar`：移动端 sidebar 已经通过 `Sheet` 以抽屉形式显示。

## 已关闭缺口

1. 已由 `navigationGroups` 提供按主任务优先级组织的导航分组。
2. 已由 `PomodoroStatusPill` 提供跨主页面可见的番茄钟 header 状态。
3. 已由 `getWorkspacePageTitle` 与 `AppSidebar` 分组渲染提供桌面和移动端一致的导航映射规则。
4. 已收口重复入口：`Notifications` 不作为一级入口展示，时间能力在导航中只保留 `Pomodoro`。

## 目标结构预览

### 桌面端，hybrid 壳层

```text
+--------------------------------------------------------------------------------------------------+
| Page title                          Search                 New issue        [🍅 Focus 18:24]     |
+---------------------------+----------------------------------------------------------------------+
| Workspace switcher        |                                                                      |
|                           |                                                                      |
| Focus                     |                 Main content area                                     |
| > Inbox                   |                 current page, list, board, etc.                       |
|   My Work                 |                                                                      |
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

- 左侧只承担导航分组，不再承载运行中的番茄钟状态。
- 顶部承载页面标题、搜索、创建入口和运行中的番茄钟状态 pill。
- `Inbox / My Work / Issues` 固定为第一组，服务主任务“任务执行与收件箱处理”。

### 移动端，toolbar + drawer

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

- 移动端继续让主内容优先，导航通过顶部按钮打开抽屉。
- header 中的番茄状态压缩为小尺寸 pill，保证标题和主操作仍然可读。
- drawer 与桌面沿用同一套分组，避免出现两套心智模型。

## 交接说明

- 进入设计前，优先阅读 `research.md` 中的当前链路和边界条件。
- 进入实现前，必须遵循 `design.md` 中的 hybrid 结构，不允许执行阶段自行改成纯顶部导航。
- 实现已落地在这些文件：
  - `apps/workspace/src/features/layout/components/app-sidebar.tsx`
  - `apps/workspace/src/features/layout/components/desktop-workspace-header.tsx`
  - `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx`
  - `apps/workspace/src/features/layout/navigation.ts`
  - `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx`
  - `apps/workspace/src/features/time-tracking/lib/pomodoro-display.ts`
