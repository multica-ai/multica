# 模块级设计总览

## 目标与范围

- 目标：把 `/pomodoro` 从“倒计时 + 结果回顾”提升为“可直接承载专注上下文、声音控制和完成记录”的主工作台。
- 本次包含：
  - `/pomodoro` 页内高频声音控制
  - 番茄启动后即可编辑的专注上下文草稿
  - 非阻塞式完成记录
  - 用可搜索的 issue picker 替换手输 issue ID
  - 页面内当前轮次上下文摘要
- 本次明确不包含：
  - sidebar compact 版重构
  - 后端接口和数据库 schema 变更
  - 已完成 pomodoro 记录的二次编辑
  - 历史统计、成就、复杂图表扩展

## 能力列表

| 能力 | 当前状态 | 优先级 | 备注 |
| --- | --- | --- | --- |
| 页内高频声音控制 | 未开始 | P1 | 仅前移 `tick`、`white noise`、`volume` |
| 运行中上下文草稿 | 未开始 | P1 | 启动后可编辑 `issue / note / labels` |
| 非阻塞式完成记录 | 未开始 | P1 | 完成时优先提交草稿，不强制补填 |
| Issue picker 替换 prompt | 未开始 | P1 | 复用现有 picker 模式 |
| 当前轮次摘要 | 未开始 | P2 | 展示本轮草稿，不做复杂统计 |

## 当前状态基线

### `/pomodoro` 页面承载面

- 证据：`apps/workspace/src/router.tsx` `pomodoroRoute`
- 当前行为：`/pomodoro` 路由直接挂载 `PomodoroPage`。
- 当前缺口：页面是明确的产品入口，但没有承载高频设置和运行中上下文编辑。

### 页面结构

- 证据：`apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx` `PomodoroPage`
- 当前行为：页面只渲染 `PomodoroTimer`、`PomodoroTodaySummary`、`PomodoroRecentSessions`。
- 当前缺口：缺少页内声音控制、当前轮次摘要、运行中可编辑的 capture 面板。

### 完成记录流程

- 证据：`apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `PomodoroTimer`
- 当前行为：page 版 `Quick capture` 只有在 `completionFlow` 存在时才渲染可编辑控件，而 `completionFlow` 只在 work phase 结束后被打开。
- 当前缺口：记录是后置补记，不是运行中草稿；完成时流程偏阻塞。

### 声音配置入口

- 证据：`apps/workspace/src/features/settings/components/pomodoro-settings-tab.tsx` `PomodoroSettingsTab`
- 当前行为：`sound_enabled`、`tick_enabled`、`volume`、`white_noise` 只在设置页配置。
- 当前缺口：用户在专注现场调整声音必须离开 `/pomodoro`。

### 声音能力基线

- 证据：`apps/workspace/src/features/time-tracking/hooks/use-sound-system.ts` `useSoundSystem`
- 当前行为：已经支持 `playTick`、`playStartTick`、`playWorkComplete`、`playBreakComplete`、`startWhiteNoise`、`updateWhiteNoiseVolume`。
- 当前缺口：能力层足够，但 UI 层入口分散。

### 可复用 issue picker

- 证据：`apps/workspace/src/features/time-tracking/components/TimeEntryCreateSheet.tsx` `IssuePicker`
- 当前行为：仓库里已存在可搜索的 issue picker 实现，可复用 `PropertyPicker + useIssuesListQuery + useIssueStore`。
- 当前缺口：番茄当前仍使用 `window.prompt` 手输 issue ID。

## 非目标

- 不把完整 Pomodoro Settings 面板搬进 `/pomodoro`
- 不修改 `CompletePomodoroBody` 接口结构
- 不扩展新的 worklog/草稿后端模型
- 不让 compact 版和 page 版在这一轮同时重构

## 优先级与推进顺序

1. 先做运行中上下文草稿和 issue picker
2. 再做完成时自动带草稿的非阻塞记录
3. 再做 `/pomodoro` 页内轻量声音控制
4. 最后补当前轮次摘要和相关验证

排序原因：
- 上下文前置决定记录质量，是核心行为
- 非阻塞完成依赖草稿存在，否则会降低记录率
- 页内声音控制是高频优化，但不应先于主流程
- 摘要展示依赖前面状态模型稳定

## 共享约束

- 共享数据约束：继续复用 `CompletePomodoroBody` 的 `issue_id`、`note`、`label_ids`
- 共享权限约束：仅使用现有用户自身 pomodoro 与 issue 查询能力，不新增权限面
- 共享交互约束：page 版允许运行中编辑，compact 版本轮不扩展
- 共享状态约束：`lastCompletedDraft` 仅用于 page 版只读摘要，不代表可回写后端
- 共享技术约束：优先复用现有 `usePomodoroSettings`、`useSoundSystem`、`PropertyPicker`

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| 草稿状态与完成状态不同步 | 可能提交错误 issue/note | 在 `PomodoroTimer` 内定义单一 draft source of truth |
| page 与 compact 行为不一致 | 用户心智混乱 | 在 design 中明确本轮只优化 page 版 |
| 完成时自动开始 break 与补记同时发生 | 容易出现时序 bug | 先定义完成后的状态顺序，再实现 |
| issue picker 复用范围不清 | 容易复制粘贴出第二套组件 | 明确复用 `PropertyPicker` 模式，不引入第三套选择器 |
| 完成失败后缺少恢复路径 | 用户可能卡在 00:00 | 设计中显式定义重试完成或 reset 的恢复分支 |

## 回写规则

- 实现后更新本 change 下的 `research.md` 和 `design.md`，补充最终落地差异
- 若 page 版范围被扩大到 compact 版，必须先更新 `design.md` 和 `tasks.md`
