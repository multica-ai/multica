# 单能力 Spec

## 背景

反拖延不是另一个独立 timer，而是 Focus Mode 的启动辅助。用户常见问题不是不知道要计时，而是卡在“下一步不明确、任务太大、状态低、回避、被打断”。首轮目标是降低启动阻力，同时记录足够轻量的原因，方便后续分析。

## 范围

- 本次覆盖：
  - 2 分钟启动 preset
  - 下一步承诺 `commitment_text`
  - 启动阻力原因 `resistance_reason`
  - 可选原因备注 `resistance_note`
  - 暂停原因 `pause_reason`
  - 放弃原因 `abandon_reason`
  - 原因事件写入 `focus_events`
- 本次不覆盖：
  - AI 自动识别拖延原因
  - 长问卷式情绪记录
  - 复杂个人分析报表
  - 系统级拦截或提醒

## 当前状态

- 普通 timer 只有 description 输入。
- Pomodoro start 不需要 commitment，也不记录启动原因。
- Reset/abandon 没有原因。
- Pause 没有原因。

## 证据

- `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` `StartTimerBar`：普通 timer 只输入 description 并 start。
- `server/internal/handler/pomodoro.go` `StartPomodoro`：Pomodoro start 只启动或恢复 session。
- `server/internal/handler/pomodoro.go` `PausePomodoro`：pause 只累计 elapsed 并设为 paused。
- `server/internal/handler/pomodoro.go` `ResetPomodoro`：reset 不记录原因。
- `server/migrations/041_pomodoro_session.up.sql` `pomodoro_sessions`：没有 commitment 或 reason 字段。

## 缺口

1. 启动阻力缺口  
系统不知道用户为什么没有开始或为什么需要 2 分钟启动。

2. 下一步承诺缺口  
用户没有被引导把任务缩小到下一步动作。

3. 暂停/放弃分析缺口  
系统无法区分正常暂停、外部打断、低能量、任务不清晰等原因。

4. 交互负担风险  
如果原因填写太重，会加剧拖延。

## 交接说明

- 本能力依赖 `focus_events`。
- 原因填写必须轻量可选，不能阻塞核心 start/pause/abandon。
- 2 分钟启动完成后可进入 break suggestion 或转为普通 Flowtime，执行阶段需按 design 选择。
