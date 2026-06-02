# 单能力 Tasks

## 实现目标

把 workspace 导航升级为 desktop hybrid shell，并让运行中的番茄钟成为跨页面可见的 header 状态，同时保证移动端抽屉导航与桌面信息架构一致。

## 前置依赖

- 设计决策已关闭：桌面采用 hybrid，移动端采用顶部 toolbar + drawer。
- 设计决策已关闭：运行中的番茄钟进入 header，导航中时间能力只保留一个一级入口。
- 若执行阶段需要保留 `Notifications` 独立一级入口，必须先回写 `design.md`。

## 任务切片

### Task 1 - Done

- 目标：重构导航定义与分组模型。
- 文件：
  - `apps/workspace/src/features/layout/navigation.ts`
  - `apps/workspace/src/features/layout/navigation.test.ts`
  - `apps/workspace/src/features/layout/components/app-sidebar.tsx`
- 改动：
  - 把单层 `primaryNav` 改为面向 `Focus`、`Planning`、`Tools`、`Workspace` 的分组结构。
  - 删除或降级重复入口，特别是 `Notifications` 与时间能力的重复暴露。
- 完成定义：
  - 桌面侧边栏按分组渲染。
  - 第一组固定服务任务执行与收件箱处理。
  - 时间能力只保留一个一级导航入口。
- 验证方式：
  - 运行前端 typecheck。
  - 手动检查桌面侧边栏分组顺序和 active 态。

### Task 2 - Done

- 目标：把运行中的番茄钟上提到 header。
- 文件：
  - `apps/workspace/src/features/layout/components/dashboard-layout.tsx`
  - `apps/workspace/src/features/layout/components/desktop-workspace-header.tsx`
  - `apps/workspace/src/features/layout/components/desktop-workspace-header.test.tsx`
  - `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx`
  - `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.test.tsx`
  - `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.tsx`
  - `apps/workspace/src/features/time-tracking/components/PomodoroStatusPill.test.tsx`
  - `apps/workspace/src/features/time-tracking/lib/pomodoro-display.ts`
- 改动：
  - 在桌面与移动端 header 中新增番茄状态 pill。
  - 复用现有番茄状态来源，禁止创建第二个 live timer 实例。
  - footer 中移除番茄钟的高可见主暴露。
- 完成定义：
  - 番茄钟运行时，任意主页面 header 都能看到状态 pill。
  - 点击 pill 返回 `/pomodoro`。
  - `/pomodoro` 页面本身不出现重复计时实例。
- 验证方式：
  - 补组件或 hook 测试，覆盖 idle / work / break 三种状态。
  - 手动验证桌面和移动端 header。

### Task 3 - Done

- 目标：对齐移动端抽屉导航与桌面分组。
- 文件：
  - `apps/workspace/src/features/layout/components/mobile-workspace-toolbar.tsx`
  - `apps/workspace/src/features/layout/components/app-sidebar.tsx`
- 改动：
  - 保持顶部 toolbar + drawer 模式。
  - 在抽屉中沿用桌面分组顺序和低频项位置。
  - 确保番茄状态 pill 不遮挡页面标题和主要操作。
- 完成定义：
  - 移动端抽屉信息架构与桌面一致。
  - 主内容优先，没有常驻窄侧栏。
- 验证方式：
  - 手动检查窄屏断点。
  - 必要时补响应式组件测试。

### Task 4 - Done

- 目标：清理重复入口并回写设计资产。
- 文件：
  - `apps/workspace/src/features/layout/navigation.ts`
  - `docs/superpowers/specs/2026-06-02-sidebar-ux/*.md`
- 改动：
  - 删除已确定降级的重复导航项。
  - 用最终实现回写 spec / research / design 中的证据与验收状态。
- 完成定义：
  - 代码与设计文档一致。
  - 不存在“设计说合并，代码还双入口”的分叉状态。
- 验证方式：
  - 人工复核文档路径、符号名、验收项。
  - 运行相关前端校验命令。

## 执行顺序说明

先重构导航定义，再接入 header 番茄状态，随后对齐移动端，最后做重复入口清理和文档回写。

这样排序可以先稳定信息架构，再处理跨壳层状态展示。否则如果先做 header 状态，很容易在旧导航结构下产生二次返工。

## 回写要求

- 已更新本目录 `research.md` 的现状链路和关键证据。
- 已更新 `design.md` 中的风险状态与验收结果。
- `Agents` 最终位于 `Workspace` 分组；`Notifications` 不作为一级入口展示，相关路由由 `Inbox` active/title 承接。
