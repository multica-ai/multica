# 2.1 工时追踪设计

## 1. 目标

1. 在现有工时主链路上补自动暂停。
2. 不破坏既有时间统计口径。
3. 为 2.3 的休息提醒共享同一计时生命周期。

## 2. 非目标

- 不引入后台守护进程。
- 不重构 time entry 为“多段 pause/resume”复杂模型。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts` · `useTimeTracking` | 已有 start/stop/current，可承接自动暂停。 |
| `apps/workspace/src/shared/types/time-entry.ts` · `TimeEntry` | 没有 paused 字段，不适合做原地暂停。 |
| `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx` · `GlobalTimerWidget` | 已有全局停止入口，适合承接自动暂停提示与恢复。 |

## 4. 缺口定义

- 缺浏览器 idle 监听。
- 缺自动暂停后的恢复体验。
- 缺把自动暂停暴露给 2.3 提醒系统的共享事件。

## 5. 方案与权衡

### 方案 A：引入真正的 paused 状态

优点：语义直观。  
缺点：与 `time_entries` 现有区间模型冲突，需要重写统计口径。

### 方案 B：自动停止当前段并保存恢复草稿，推荐

优点：复用现有 stop 语义；历史统计不变。  
缺点：用户看到的是“自动暂停=自动停止+恢复入口”。

## 6. 推荐方案

采用方案 B：前端 idle 监听在达到阈值后调用现有 stop 路径，同时把最近一条 running timer 的 issue、描述、标签等上下文写入本地 `paused draft`；`GlobalTimerWidget` 和时间页显示“恢复上一段工作” CTA。

## 7. 数据模型或状态模型

- 服务端：time entry 仍为连续区间。
- 前端本地：
  - `idle_state`：active / warning / auto_paused
  - `paused_draft`：最近一次自动暂停的恢复上下文

## 8. 接口契约

- 复用 `stopTimeEntry`
- 新增前端 idle monitor hook
- 对 2.3 暴露 `timer:auto-paused` 事件

## 9. UI 或交互流程

- 当前计时达到 idle 阈值前先弹 warning。
- 用户未恢复活动则自动停止并展示“恢复计时”按钮。

### 页面交互流

```text
开始计时
  -> 浏览器进入 idle
  -> 弹出即将自动暂停提示
  -> 仍 idle
  -> stopTimeEntry
  -> 保存 paused_draft
  -> GlobalTimerWidget 显示“恢复”
```

### 状态机

```text
running
  -> idle_warning
  -> auto_paused
auto_paused
  -> resumed
  -> dismissed
```

### 数据变化流

```text
Idle Monitor
  -> stopTimeEntryMutation
  -> time entry closed on server
  -> paused_draft saved locally
  -> GlobalTimerWidget / MyTimePage
  -> 用户点击恢复
  -> startTimeEntryMutation
```

## 10. 权限、边界条件、异常路径

- 若浏览器不支持 idle API，则退化为页面可见性 + 用户输入事件监测。
- warning 阶段用户有任何活动都应取消自动暂停。
- 自动暂停被标记为客户端特性；跨设备同步恢复不是当前阶段目标，原因是 `notification_preferences` 现有体系也未覆盖 time reminder 类型。

## 11. 实现约束

- 不修改历史 time entry 的已记录时长。
- 自动暂停与用户手动停止共用同一停止接口和缓存失效策略。

## 12. 风险与对策

- 风险：误暂停。  
  对策：先 warning，再自动停止；阈值可配置。
- 风险：恢复体验割裂。  
  对策：恢复 CTA 同时出现在 `GlobalTimerWidget` 与 `MyTimePage`。

## 13. 验收检查

1. running timer 在 idle 超时后会自动停止。
2. 自动停止后可一键恢复相同上下文的下一段计时。
3. 自动暂停不改变既有时长统计口径。
4. 2.3 可复用 `timer:auto-paused` 事件。
