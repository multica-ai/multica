# 单能力 Design

## 目标

建立 Focus Mode 的主入口、当前状态模型和上下文契约，让 Pomodoro、Flowtime、反拖延启动都成为 Focus Mode 的 preset，而不是各自维护独立状态。

## 非目标

- 不在本能力中实现 Flowtime 动态休息算法。
- 不在本能力中实现 break 事件聚合报表。
- 不删除旧 Pomodoro API。
- 不改造 legacy `worklog`。

## 当前架构基线

- 当前入口：`apps/workspace/src/router.tsx` `pomodoroRoute`
- 当前核心逻辑：`apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `PomodoroTimer`
- 当前存储或状态：`server/migrations/041_pomodoro_session.up.sql` `pomodoro_sessions`
- 当前 UI 或接口：`/api/pomodoro/current/start/pause/complete/reset`

### 代码证据

- `apps/workspace/src/router.tsx` `pomodoroRoute`：说明现有 `/pomodoro` 入口。
- `apps/workspace/src/features/layout/navigation.ts` `navigationGroups`：说明导航命名仍是 Pomodoro。
- `server/migrations/041_pomodoro_session.up.sql` `pomodoro_sessions`：说明当前 session 状态只覆盖 Pomodoro。
- `server/internal/handler/time_entry.go` `CreateTimeEntry`：说明普通 live timer 已有独立运行态。
- `server/internal/handler/pomodoro.go` `CompletePomodoro`：说明 Pomodoro 历史写入 `time_entry`。

## 缺口定义

- 入口命名不支持 Focus Mode。
- 当前状态模型不支持 mode、preset、commitment、reason、suggested break。
- 普通 timer 与 Focus 缺少互斥规则。
- 当前专注上下文不是一等数据。

## 方案与权衡

### 方案 A：扩展 `pomodoro_sessions`

- 做法：在 `pomodoro_sessions` 增加 mode、reason、commitment 等字段。
- 优点：迁移少，旧 API 改动小。
- 风险：Pomodoro phase 会继续约束 Flowtime 和 break flow，状态机会快速变混乱。

### 方案 B：新增 `focus_sessions`，历史继续落 `time_entry`

- 做法：新增 Focus 当前状态表，`time_entry` 继续作为专注历史来源。
- 优点：清晰分离“当前流程状态”和“历史时间记录”，Pomodoro 可作为 preset。
- 风险：需要迁移 UI 和新增 API。

## 推荐方案

选择方案 B。

原因：
- Focus Mode 不是 Pomodoro 的小扩展，而是更高层的专注工作台。
- `time_entry` 已经是统一时间历史主线，新增 `focus_sessions` 不会再造历史表。
- Pomodoro 可以保留为 `mode = pomodoro` 或 `preset = pomodoro_25_5`，不会阻塞 Flowtime。

## 数据模型或状态模型

新增 `focus_sessions`：

```text
id uuid pk
workspace_id uuid not null
user_id uuid not null
mode text not null -- pomodoro | flowtime | quick_start
phase text not null -- idle | focusing | paused | break_suggested | breaking | completed | abandoned
preset text null -- pomodoro_25_5 | flowtime_default | two_minute_start
issue_id uuid null
description text null
commitment_text text null
label_ids jsonb not null default '[]'
started_at timestamptz null
paused_at timestamptz null
elapsed_focus_seconds int not null default 0
suggested_break_seconds int null
status_reason text null
reason_note text null
created_at timestamptz not null
updated_at timestamptz not null
```

约束：

- `UNIQUE(user_id, workspace_id)` 保证每个 workspace 只有一个当前 Focus session。
- `mode` 首轮允许：`pomodoro`, `flowtime`, `quick_start`。
- `phase` 首轮允许：`idle`, `focusing`, `paused`, `break_suggested`, `breaking`, `completed`, `abandoned`。
- `label_ids` 是当前草稿上下文，完成写 `time_entry` 后再同步到 `time_entry_to_label`。

## 接口契约

### 输入

新增 API：

- `GET /api/focus/current`
- `POST /api/focus/start`
- `POST /api/focus/pause`
- `POST /api/focus/resume`
- `POST /api/focus/complete`
- `POST /api/focus/abandon`
- `PATCH /api/focus/current`

`POST /api/focus/start` body：

```json
{
  "mode": "flowtime",
  "preset": "flowtime_default",
  "issue_id": "optional uuid",
  "description": "optional text",
  "commitment_text": "optional text",
  "label_ids": [],
  "timer_conflict_action": "stop_existing"
}
```

`timer_conflict_action`：

- `stop_existing`
- `convert_existing`
- `cancel`

### 输出

所有当前状态接口返回：

```json
{
  "session": {
    "id": "uuid",
    "mode": "flowtime",
    "phase": "focusing",
    "preset": "flowtime_default",
    "issue_id": null,
    "description": null,
    "commitment_text": null,
    "label_ids": [],
    "elapsed_focus_seconds": 0,
    "suggested_break_seconds": null,
    "started_at": "iso",
    "updated_at": "iso"
  }
}
```

错误：

- `409 timer_conflict`：已有 ordinary running timer 且未提供处理策略。
- `400 invalid_focus_mode`：mode 不合法。
- `400 invalid_focus_phase`：当前 phase 不允许该操作。

## UI 或交互流程

1. 用户从导航进入 `Focus`。
2. `/focus` 页面显示当前 session 面板、模式选择、issue picker、note/labels、下一步承诺。
3. 如果用户访问 `/pomodoro`，路由重定向到 `/focus`。
4. 用户点击 Start：
   - 无 ordinary running timer：直接开始 Focus。
   - 有 ordinary running timer：弹出确认，选择 stop existing 或 cancel。
5. Focus 开始后，全局状态 pill 显示当前模式、已专注时长或剩余时间。

## 权限、边界条件、异常路径

- 只有 workspace member 可使用 Focus API。
- 无 issue、空 note、空 label 允许。
- 已有普通 timer 时不得静默启动 Focus。
- Focus session 已在 `focusing` 时重复 start 返回当前 session 或 409，执行阶段需二选一并写入 tests。

## 实现约束

- 不把 Flowtime 字段加到 `pomodoro_sessions`。
- 不向 `worklog` 写入 Focus 数据。
- 不保留 `window.prompt` 作为 Focus issue 绑定入口。
- 前端路由新增 `/focus`，`/pomodoro` 保留重定向。
- 代码注释必须是英文。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 新旧 Pomodoro 状态并存 | UI 显示冲突 | Focus 页面只读 `focus_sessions`，旧 Pomodoro 页面迁移到重定向 |
| 普通 timer 冲突处理不清 | 双计时 | API 使用 `timer_conflict_action` 强制显式处理 |
| `label_ids` 存 JSONB 与 label 表不同步 | 完成时写标签失败 | 完成时逐个校验 workspace label，失败时返回 400 |

## 验收检查

1. `/focus` 可访问，导航显示 Focus。
2. `/pomodoro` 会进入 Focus 页面。
3. 用户可以用 issue、note、labels、commitment 启动 Focus。
4. 有 ordinary running timer 时，启动 Focus 不会静默双跑。
5. Focus completion 后产生 `time_entry`，不产生 `worklog`。
6. 后端和前端类型检查通过。
