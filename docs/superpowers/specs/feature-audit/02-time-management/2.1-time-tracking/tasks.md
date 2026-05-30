# 2.1 工时追踪执行任务

## 1. 实现目标

- 在不改写 time entry 统计模型的前提下，实现自动暂停与恢复草稿。

## 2. 前置依赖

- 无硬依赖，但 2.3 会复用本能力暴露的生命周期事件。

## 3. 任务切片

### 切片 A：补前端 idle monitor 与本地草稿

- 目标文件 / 目录：
  - `apps/workspace/src/features/time-tracking/hooks/`
  - `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`
  - `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx`
- 完成定义：
  - idle 监听、warning、auto-paused、paused_draft 都落地。
- 验证方式：
  - 手动验证 idle -> warning -> auto-paused -> resume。

### 切片 B：复用 stop/start mutation

- 目标文件 / 目录：
  - `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts`
  - `apps/workspace/src/features/time-tracking/hooks/use-time-entry-actions.ts`
- 完成定义：
  - 自动暂停复用 stop。
  - 恢复复用 start。
- 验证方式：
  - 检查自动暂停后统计与手动停止一致。

### 切片 C：暴露共享生命周期事件

- 目标文件 / 目录：
  - `apps/workspace/src/features/time-tracking/`
- 完成定义：
  - 2.3 可订阅 `timer:auto-paused` 或等价事件。
- 验证方式：
  - 联动测试提醒系统可收到事件。

## 4. 回写要求

1. 若自动暂停最终改成真实 paused state，先回写本设计与 `overview.md`。
2. 实现完成后回写 `spec.md` 状态。
