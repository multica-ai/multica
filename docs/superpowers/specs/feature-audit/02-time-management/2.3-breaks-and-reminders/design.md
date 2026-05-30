# 2.3 休息与提醒设计

## 1. 目标

1. 支持周期性休息提醒、任务截止提醒、桌面通知和声音提醒。
2. 统一工时追踪与番茄钟的提醒生命周期。
3. 明确时间管理域与通用通知系统的边界。

## 2. 非目标

- 本阶段不做跨设备推送。
- 本阶段不做服务端常驻 scheduler。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` · `PomodoroTimer` | 2.2 已有阶段切换和音效逻辑。 |
| `apps/workspace/src/features/time-tracking/hooks/use-sound-system.ts` · `useSoundSystem` | 本地声音投递已可复用。 |
| `apps/workspace/src/features/settings/components/notifications-tab.tsx` · `TYPE_GROUPS` | 通用通知系统没有时间提醒类别。 |

## 4. 缺口定义

- 缺提醒触发注册表。
- 缺浏览器桌面通知与截止提醒链路。
- 缺把工时计时和番茄钟统一纳入同一提醒总线。

## 5. 方案与权衡

### 方案 A：每个功能自己做提醒

优点：局部实现快。  
缺点：running timer、pomodoro、deadline 会重复建计时器，状态冲突大。

### 方案 B：时间管理域维护统一 reminder registry，delivery 复用本地能力，推荐

优点：能统一生命周期；便于未来接入通知偏好系统。  
缺点：需要先抽 reminder service。

## 6. 推荐方案

采用方案 B：在时间管理域内新增 `reminder registry`。  
触发归时间管理域负责：

- running timer 的休息提醒
- 2.1 自动暂停事件
- pomodoro phase 切换提醒
- issue 截止时间提醒

投递层优先复用：

- `useSoundSystem` 的音效
- 浏览器 Notification API
- toast / in-app banner

远端 ntfy 推送列为**低优先级**，原因是 `notifications-tab` 当前没有 time reminder 类型。

## 7. 数据模型或状态模型

- `reminder_preferences`
  - `break_interval_minutes`
  - `deadline_offsets_minutes[]`
  - `desktop_enabled`
  - `sound_enabled`
- `reminder_instance`
  - `source_type`（timer / pomodoro / deadline）
  - `fire_at`
  - `status`（armed / fired / dismissed / snoozed）

## 8. 接口契约

- 前端本地 reminder service 暴露：
  - `registerReminderSource`
  - `armReminder`
  - `dismissReminder`
  - `snoozeReminder`
- future extension：把 time reminder 类型映射进通知偏好系统

## 9. UI 或交互流程

- 在时间页或设置页增加提醒偏好。
- 计时中达到休息阈值，触发桌面通知、音效、toast。
- 任务临近截止时，按用户设置提前量触发提醒。

### 页面交互流

```text
用户开启提醒
  -> registry 注册 running timer / pomodoro / deadline 源
  -> 到达 fire_at
  -> 触发声音 + 桌面通知 + toast
  -> 用户 dismiss / snooze
```

### 状态机

```text
disabled
  -> armed
armed
  -> fired
fired
  -> dismissed
  -> snoozed
snoozed
  -> armed
```

### 数据变化流

```text
Running Timer / Pomodoro / Deadline Query
  -> Reminder Registry
  -> Sound + Notification + Toast
  -> 用户 dismiss/snooze
  -> Reminder Registry 更新状态
```

## 10. 权限、边界条件、异常路径

- 浏览器拒绝通知权限时，回退到声音与 in-app toast。
- 2.1 自动暂停属于高优先级事件，触发时应取消同一计时段的普通休息提醒。
- 远端 ntfy 推送被标为低优先级，因为现有 `notifications-tab` 与 `notification_preferences` 都没有时间提醒类型，强行纳入会扩 scope。

## 11. 实现约束

- 所有提醒源必须通过统一 registry 调度。
- 同一时间段不得重复弹出多个同类型提醒。

## 12. 风险与对策

- 风险：多源提醒互相打架。  
  对策：统一 registry，按 source_type 和优先级去重。
- 风险：用户关闭通知权限后无感知。  
  对策：首轮降级到 toast + sound，并在设置页提示权限状态。

## 13. 验收检查

1. 可配置休息提醒间隔。
2. 可配置截止提醒提前量。
3. 触发提醒时支持桌面通知、声音和 toast 降级。
4. pomodoro 与 running timer 共用同一 registry。
5. ntfy 推送被明确标为低优先级扩展。
