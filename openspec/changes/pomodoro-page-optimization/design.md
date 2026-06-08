# 单能力 Design

## 目标

- 让 `/pomodoro` 成为专注主工作台，而不是单纯倒计时页面
- 让用户在番茄启动后即可建立本轮上下文
- 让 work phase 完成时能够自动带上当前草稿完成记录
- 让高频声音控制留在 `/pomodoro` 页面内完成
- 让完成后的交互更顺，不强制先补记再休息

## 非目标

- 不重构 compact sidebar 版交互
- 不新增后端接口、数据库字段或新的 pomodoro 草稿模型
- 不支持已完成 pomodoro 记录的二次编辑
- 不扩展历史统计、成就、目标系统
- 不把完整设置页嵌入 `/pomodoro`

## 当前架构基线

- 当前入口：`apps/workspace/src/router.tsx` `pomodoroRoute`
- 当前核心逻辑：`apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `PomodoroTimer`
- 当前存储或状态：`PomodoroSession` 走 React Query，`PomodoroSettings` 走 `localStorage`
- 当前 UI 或接口：
  - `/pomodoro` 页面：`PomodoroPage`
  - 完成提交接口：`CompletePomodoroBody`
  - 设置页入口：`PomodoroSettingsTab`

### 代码证据

- `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`：说明 page 当前信息架构
- `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `PomodoroTimer`：说明 capture 逻辑与完成流程
- `apps/workspace/src/features/time-tracking/hooks/use-pomodoro.ts` `useCompletePomodoroMutation`：说明当前完成接口契约
- `apps/workspace/src/features/time-tracking/hooks/use-sound-system.ts` `useSoundSystem`：说明声音能力已可复用
- `apps/workspace/src/features/time-tracking/components/TimeEntryCreateSheet.tsx` `IssuePicker`：说明 issue picker 现有模式

## 缺口定义

- `/pomodoro` 缺少高频设置入口
- 缺少“运行中草稿”作为当前轮次上下文
- 完成后流程依赖 `completionFlow` 阻塞补记
- issue 绑定方式低效且不一致
- 页面缺少当前轮次摘要，用户看不到“这轮专注正在记录什么”

## 方案与权衡

### 方案 A：保留完成后补记模型，只把声音面板搬到 `/pomodoro`

- 做法：维持 `completionFlow` 结构，仅在页面右侧或下方增加声音面板
- 优点：改动小，几乎不触碰完成逻辑
- 风险：主问题未解，记录仍然是后置补记，用户体验提升有限

### 方案 B：引入运行中草稿模型，完成时自动带草稿，并把声音控制前移

- 做法：
  - 在 `PomodoroTimer` page 版引入独立 draft state
  - 启动后即可编辑 `issue / note / labels`
  - work phase 完成时自动读取 draft 调用 `completePomodoro`
  - 完成后仅提供轻量补充反馈，不再强制阻塞
  - 在 `/pomodoro` 页面中加入轻量声音控制
- 优点：直接解决上下文时机、完成流畅度和声音入口三个核心问题
- 风险：page 版状态复杂度上升，需要清楚定义 reset / pause / complete 时的草稿行为

## 推荐方案

选择方案 B。

原因：
- 它直接修复最核心的时机问题，而不是只优化表层入口
- 不需要改后端接口，仍可复用 `CompletePomodoroBody`
- 草稿状态可以限制在 page 版 `PomodoroTimer` 内部，不必扩散到全局 store
- issue picker、label picker、声音设置都已有复用基础，风险可控

不选方案 A，因为它只能改善“设置跳转”，不能解决“上下文建立过晚”和“完成后阻塞”。

## 数据模型或状态模型

新增 page 版本地草稿状态：

```ts
interface PomodoroCaptureDraft {
  issueId: string | null;
  note: string;
  labelIds: string[];
}

interface CompletedDraftSummary {
  issueId: string | null;
  note: string;
  labelIds: string[];
  completedAt: string;
}
```

状态约束：

- 初始值为空草稿
- 启动后即可编辑
- pause 不清空草稿
- work phase 成功完成后，将当前草稿快照写入 `lastCompletedDraft`，然后清空活动草稿
- reset 清空草稿
- break / long break 阶段只展示 `lastCompletedDraft` 摘要，不继续编辑本轮草稿
- 完成请求失败时，活动草稿保持不变，`lastCompletedDraft` 不更新
- page 版新增的草稿状态与摘要状态只允许存在于 `variant === "page"` 的交互分支，compact 版不接入

## 接口契约

### 输入

- 不新增后端接口
- work phase 完成时仍调用：
  - `api.completePomodoro({ issue_id, note, label_ids, long_break_after })`
- issue picker 读取现有 issue 列表：
  - `useIssuesListQuery`
  - `useIssueStore`

### 输出

- 成功：
  - session 正常切换到下一 phase
  - 本轮草稿被提交，快照写入 `lastCompletedDraft`，活动草稿清空
  - UI 提示本轮已记录到何处，并在 break 阶段展示只读摘要
- 失败：
  - 保留当前草稿
  - 显示错误 toast
  - timer 保持在 work phase 的可恢复完成态，不进入 break / long break
  - 页面提供显式 `Retry completion` 或等价重试入口

## UI 或交互流程

1. 用户进入 `/pomodoro`
2. 页面显示：
   - 大时钟和主控制按钮
   - 轻量 `Sound` 面板：`Tick Sound`、`White Noise`、`Volume`
   - `Quick capture` 草稿区：`Issue`、`Note`、`Labels`
   - `Current focus` 摘要区
3. 用户点击 `Start`
4. 番茄运行中，用户可随时编辑草稿
5. work phase 完成时：
   - 系统直接带当前草稿调用 `completePomodoro`
   - 请求成功后，若配置了 auto-start break，则 break 正常开始
   - UI 通过 toast 或轻量 inline 提示本轮已记录，并展示只读完成摘要
6. 若完成请求失败：
   - timer 停在 `00:00` 的 work phase 完成态
   - 保留当前草稿
   - 用户可点击 `Retry completion` 重试，或 `Reset` 放弃本轮
   - 在成功前，不进入 break，也不清空草稿

## 权限、边界条件、异常路径

- 谁可以使用：
  - 当前 workspace 下已进入 `/pomodoro` 的普通成员
- 非法输入：
  - 无 issue：允许
  - 空 note：允许
  - 空 labels：允许
- 失败处理：
  - 完成请求失败：保留草稿，停留在 work phase 完成态，显示重试入口
  - issue 列表未加载：issue picker 允许空态展示
  - 声音设置读取失败：回落到默认 `PomodoroSettings`

## 实现约束

- 不允许新增后端接口或 schema
- 必须复用现有 `CompletePomodoroBody`
- 必须优先复用现有 `PropertyPicker` 模式实现 issue picker
- 不允许引入与 compact 版共享的全局草稿 store，避免无必要扩散
- 必须将 page-only 新交互限制在 `variant === "page"` 分支，或抽离 page 专属子组件承载
- 不允许保留 `window.prompt` 作为主交互

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 草稿清空时机不对 | 用户可能丢失本轮信息 | 只在 work phase 成功完成或 reset 时清空 |
| auto-start break 与完成提交顺序冲突 | 可能出现重复完成或错误 phase | 保持 `fireComplete` 为唯一完成入口，break 启动在成功回调之后 |
| 复用 issue picker 时引入过重依赖 | 页面体积和复杂度增加 | 只复用 `PropertyPicker` 模式，不直接搬整个 sheet |
| page 与 compact 行为差异增大 | 用户在 sidebar 中预期不一致 | 在本轮设计中明确 page-only，compact 后续单独设计 |
| 完成请求失败后 timer 卡死 | 用户无法恢复本轮记录 | 定义 `Retry completion` 恢复路径，成功前不进入 break |

## 验收检查

1. 用户进入 `/pomodoro` 后，无需跳转设置页即可调整 `Tick Sound`、`White Noise`、`Volume`
2. 用户启动番茄后，立即可以选择 issue、填写 note、设置 labels
3. work phase 完成时，当前草稿会通过现有 `completePomodoro` 接口一并提交
4. 关联 issue 不再使用 `window.prompt`
5. 完成后不会强制用户先补记再进入休息
6. 完成请求失败时，用户可以在 `00:00` 的 work phase 完成态重试，不会丢失草稿
7. 至少通过 page 版组件测试和番茄现有测试的回归验证
