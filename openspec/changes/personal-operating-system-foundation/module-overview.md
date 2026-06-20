# Personal Operating System Foundation

## 目标与范围

本 change 把 Multica 从“AI-native issue tracker”收敛为能支撑个人知识管理、注意力管理、精力管理的执行闭环。

统一闭环：

```text
Issue
  -> Focus
  -> TimeEntry
  -> DailyReview
  -> DailyPlan
  -> Skill / Workspace Context
  -> Agent execution
```

## ASCII 状态机与数据流

### 总状态机

```text
                         +----------------------+
                         | Capture / Inbox      |
                         | issue, notification  |
                         +----------+-----------+
                                    |
                                    v
                         +----------------------+
                         | Plan                 |
                         | today / daily plan   |
                         +----------+-----------+
                                    |
                                    v
                         +----------------------+
                         | Focus                |
                         | flowtime / pomodoro  |
                         | quick_start          |
                         +----+-----------+-----+
                              |           |
                complete      |           | pause / abandon / break
                              v           v
                     +----------------+  +----------------------+
                     | TimeEntry      |  | FocusEvent           |
                     | actual work    |  | attention signals    |
                     +--------+-------+  +----------+-----------+
                              |                     |
                              +----------+----------+
                                         v
                         +----------------------+
                         | DailyReview          |
                         | output + energy      |
                         +----------+-----------+
                                    |
                                    v
                         +----------------------+
                         | DailyPlan            |
                         | next plan + capacity |
                         +----------+-----------+
                                    |
                                    v
                         +----------------------+
                         | Knowledge            |
                         | skill / context      |
                         +----------+-----------+
                                    |
                                    v
                         +----------------------+
                         | Agent Execution      |
                         | issue + context      |
                         +----------------------+
```

状态含义：

- `Capture / Inbox`：任务、通知、评论和 agent 事件进入注意力入口。
- `Plan`：用户决定今天或明天处理什么。
- `Focus`：用户进入当下执行状态。
- `TimeEntry`：只记录实际做过的工作。
- `FocusEvent`：记录开始阻力、中断、放弃和休息行为。
- `DailyReview`：把实际时间、任务结果、注意力事件和精力信号合并成复盘。
- `DailyPlan`：把复盘转成下一轮计划。
- `Knowledge`：把可复用背景沉淀到 Skill 或 Workspace Context。
- `Agent Execution`：agent 使用 issue、skill、workspace context 执行任务。

### Focus 子状态机

```text
                         +------+
                         | idle |
                         +--+---+
                            |
       start flowtime /     | start quick_start /
       start pomodoro       | start pomodoro preset
                            v
                      +-----------+
                      | focusing  |
                      +--+-----+--+
                         |     |
          pause          |     | complete
                         v     v
                     +--------+----------------+
                     | paused | break_suggested|
                     +---+----+--------+-------+
                         |             |
             resume      |             | start break
                         v             v
                      +-----------+  +----------+
                      | focusing  |  | breaking |
                      +-----+-----+  +----+-----+
                            |             |
                            |             | complete / skip
                            v             v
                       +----------+  +----------+
                       |abandoned |  | idle     |
                       +----------+  +----------+
```

Quick Start 补齐后的子流程：

```text
idle
  -> quick_start focusing
  -> two_minute_countdown
  -> quick_start_completed event
  -> continue_as_flowtime | complete | abandon
```

### 数据流向图

```text
Browser
  |
  | REST / WS
  v
Go API
  |
  +--> issue ------------------------------+
  |                                        |
  +--> focus_sessions --current state--+   |
  |                                    |   |
  +--> focus_events ----signals-------+---+--> ReviewService
  |                                        |       |
  +--> time_entry ------actual work--------+       v
  |                                            daily_review
  |                                                |
  +--> daily_plan <----- DailyPlanService <--------+
  |
  +--> skill -----------------------------+
  |                                       |
  +--> workspace.context -----------------+--> TaskService / Daemon
                                              |
                                              v
                                         Agent Prompt
```

数据边界：

- `focus_sessions` 保存当前运行态，不承担历史报表主表。
- `focus_events` 保存注意力事件和休息行为。
- `time_entry` 保存实际工作时间。
- `daily_review` 保存复盘草稿、确认状态和 Phase 1 energy signal。
- `daily_plan` 保存下一轮计划；Phase 1 仍是 Markdown，Phase 2 才结构化。
- `skill` 与 `workspace.context` 提供人和 agent 复用的知识。

### 页面交互图

```text
                 +----------------+
                 | Inbox          |
                 | triage/snooze  |
                 +-------+--------+
                         |
                         | open issue / start focus
                         v
+---------+      +-------+--------+      +----------------+
| Today   +----->| Issue Detail   +----->| Start Focus    |
| list    |      | description    |      | dialog/action  |
+----+----+      | comments       |      +-------+--------+
     |           +-------+--------+              |
     |                   |                       v
     |                   |               +---------------+
     |                   +-------------->| Focus Page    |
     |                                   | current focus |
     |                                   +-------+-------+
     |                                           |
     | complete / abandon / break                |
     v                                           v
+----+----------------+                 +----------------+
| My Time             |<----------------| Time Entry     |
| timer/history       |                 | actual work    |
| review/plan panels  |                 +----------------+
+----+----------------+
     |
     | generate / confirm review
     v
+----+----------------+
| Daily Review        |
| energy check-in     |
+----+----------------+
     |
     | feed next plan
     v
+----+----------------+
| Daily Plan          |
| high-energy work    |
| low-energy fallback |
+----+----------------+
     |
     | reusable learning
     v
+----+----------------+
| Skills / Context    |
| team knowledge      |
+----+----------------+
     |
     | injected into task context
     v
+----+----------------+
| Agent Execution     |
| issue + knowledge   |
+---------------------+
```

页面交互约束：

- `Start Focus` 必须是可复用 action，不把入口逻辑散落在每个页面。
- `Issue Detail` 是任务上下文最完整的入口，必须支持直接启动 Focus。
- `Today` 是计划执行入口，必须支持从列表快速启动 Focus。
- `My Time` 是实际记录和复盘入口，必须同时解释普通 timer 与 Focus 的关系。
- `Daily Review` 不应变成重表单；energy check-in 必须可选。
- `Skills / Context` 是沉淀结果，不应打断执行闭环。

本次包含：

- Phase 0：修正 OpenSpec 与代码状态漂移，让后续实现有可信入口。
- Phase 1：建立最小可用闭环，让用户能从任务进入专注、记录实际消耗、复盘精力、生成下一轮计划，并让 agent 读取 workspace context。

本次明确不包含：

- 独立 Wiki / Doc 系统。
- RAG、全文知识检索、双向知识图谱。
- 系统级勿扰、网站拦截、浏览器插件。
- 完整 timeboxing / 计划日历实现。
- 复杂精力算法、医疗化健康建议或自动诊断。

## 能力列表

| 能力 | 当前状态 | 优先级 | 备注 |
| --- | --- | --- | --- |
| Spec reverse sync | 已完成 | P0 | Focus/Flowtime 文档已回写当前实现状态 |
| Knowledge loop | P0 已完成 | P0 | Workspace context 已接通 agent；Skill visibility/search 后置 |
| Attention loop | P1 已完成 | P1 | Focus 入口、quick start 闭环、review signals 已接通 |
| Energy loop | P1 已完成 | P1 | Daily review energy check-in 与 energy-aware plan 已接通 |
| Structured planning | 暂停 | - | 会引入平行执行对象；后续必须重新写 spec 并证明 issue 模型不足 |

## 当前状态基线

### 产品定位

- 证据：`README.md` `What is Multica?`
- 当前行为：产品对外定位是把 coding agents 作为 teammate，围绕 issue 分配、执行、评论和状态变化。
- 当前缺口：README 尚未表达个人知识、注意力、精力管理闭环。

- 证据：`openspec/project.md` `Product Overview`
- 当前行为：`issue` 是 canonical persisted work item，`agent` 是 AI worker，`workspace` 是 tenant boundary。
- 当前缺口：知识、专注、精力需要围绕现有 issue/agent/workspace 模型扩展，不能引入平行工作项模型。

### Spec reverse sync

- 证据：`openspec/changes/focus-mode-flowtime/module-overview.md` `能力列表`
- 当前行为：Focus Mode Core、Flowtime Session、Break Flow、Anti-Procrastination Start 已回写到部分实现或已实现状态。
- 当前缺口：后续只保留 history/reporting 等非 Phase 1 能力。

- 证据：`server/internal/handler/focus.go` `StartFocus` / `CompleteFocus` / `transitionFocusBreak`
- 当前行为：后端已经支持 Focus 开始、完成、暂停、放弃、休息开始、休息跳过、休息完成。
- 当前缺口：无 Phase 1 阻塞缺口。

### Knowledge loop

- 证据：`server/internal/handler/skill.go` `SkillResponse` / `CreateSkill` / `UpdateSkill` / `ImportSkill`
- 当前行为：Skill 已支持名称、描述、正文、配置、文件和外部导入。
- 当前缺口：Skill 仍偏 agent 指令库，缺 team visibility 和搜索。

- 证据：`apps/workspace/src/features/search/use-search-results.ts` `useSearchResults`
- 当前行为：全局搜索聚合 issues、projects、members。
- 当前缺口：不包含 skills 或 team knowledge。

- 证据：`server/internal/daemon/execenv/context.go` `renderIssueContext`
- 当前行为：agent 执行上下文包含 issue、trigger、quick start、agent skills 和非空 `workspace.context`。
- 当前缺口：Skill visibility 和 knowledge search 后置。

### Attention loop

- 证据：`apps/workspace/src/router.tsx` `focusRoute` / `pomodoroRoute`
- 当前行为：`/focus` 渲染 FocusPage，`/pomodoro` 重定向到 `/focus`。
- 当前缺口：Focus 已接入 issue、today、my-time；inbox 入口后置。

- 证据：`apps/workspace/src/features/time-tracking/pages/FocusPage.tsx` `FocusPage` / `modeOptions`
- 当前行为：前端已有 Flowtime、Pomodoro、2 min start、next step、start friction、pause/abandon reason UI，并支持 quick start 2 分钟倒计时后继续 Flowtime。
- 当前缺口：无 Phase 1 阻塞缺口。

### Energy loop

- 证据：`server/internal/service/review.go` `GenerateReviewDraft`
- 当前行为：Daily Review 从当天 time entries、done issues、open issues、focus signals 生成 Markdown 草稿，并在确认时保存 energy level、energy note、recovery need。
- 当前缺口：不做复杂精力算法。

- 证据：`server/internal/service/daily_plan.go` `GeneratePlanDraft`
- 当前行为：Daily Plan 从 open issues、昨日 confirmed review、focus signals 生成明日 Markdown 计划，并输出精力安排和低精力备选。
- 当前缺口：不新增结构化计划项。

- 证据：`server/internal/handler/focus.go` `validFocusReason`
- 当前行为：Focus reason 已包含 `low_energy`。
- 当前缺口：`low_energy` 已进入 review/plan 语境；后续可做报表，不在 Phase 1。

### Structured planning

- 证据：`specs/issue-energy-loop/PRODUCT.md` `Product Hard Rules`
- 当前行为：`issue` 是当前版本唯一可执行对象，daily plan 只能作为 Markdown 草稿、AI 摘要或轻量记录存在。
- 当前缺口：无 Phase 1/P2 实现入口；结构化 planner 方向已暂停。

- 证据：`specs/issue-energy-loop/TECH.md` `Runtime Object Matrix`
- 当前行为：Daily Plan、Daily Review、Focus 和 Time Entry 都不能承载平行执行语义。
- 当前缺口：如果未来重新设计结构化 planning，必须先更新 product spec、tech spec 和运行时对象矩阵。

## 非目标

- 不把知识管理扩展成独立 Wiki。
- 不在 Phase 1 做 RAG 或 AI 主动检索知识库。
- 不把 `worklog` 重新作为新时间记录主线。
- 不在 Phase 1 新增完整 timeboxing 数据模型。
- 不要求执行 Agent 自行扩展本文档未定义的数据库字段、路由或 UI 面。

## 优先级与推进顺序

1. Spec reverse sync。
   - 原因：当前 OpenSpec 与代码状态漂移，继续按旧文档执行会产生重复实现。
2. Knowledge P0：workspace context 注入 agent。
   - 原因：成本低，直接提升 agent 执行质量。
3. Attention P1：Focus 入口贯穿 issue/today/my-time，并补 quick start 闭环。
   - 原因：注意力管理的关键是降低开始工作的摩擦。
4. Energy P1：Daily Review 增加 energy check-in，Daily Plan 使用 energy/focus signals。
   - 原因：先采集信号，再做计划建议，不提前上复杂算法。
5. Knowledge P1：Skill visibility + search。
   - 原因：让知识被人找到，但不阻塞最小执行闭环。
6. Structured planning 已暂停。
   - 原因：当前产品规则要求 issue 是唯一可执行对象，不能继续沿结构化计划项或计划块路线推进。

## 共享约束

- `issue` 仍是核心工作对象，不新增平行 task model 或平行执行对象。
- 不新增结构化计划项、计划块或任何能独立启动/完成执行的计划对象。
- `time_entry` 是 actual work 的主线；`worklog` 保持 legacy issue-bound duration model。
- `Skill` 和 `workspace.context` 是 Phase 1 的知识载体。
- 所有数据继续以 `workspace_id` 为租户边界。
- 个人计划、复盘、精力信号默认绑定 user，不默认团队公开。
- 代码实现必须先读取对应 spec 和本 change 的 `tasks.md`。
- 如果实现发现需要改变数据模型，先回写 spec，再改代码。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| OpenSpec 与代码继续漂移 | 执行 Agent 可能重复实现 Focus | Phase 0 先修 reverse sync |
| 知识管理范围膨胀 | 变成 Wiki/RAG 项目 | Phase 1 只做 workspace context 和 Skill 语义 |
| Focus 与普通 timer 冲突 | 产生重复 time entry | 复用 `timer_conflict_action`，UI 解释 Start Focus 与 Start Timer 的区别 |
| Energy check-in 增加负担 | 用户跳过复盘 | 所有 energy 字段可选，优先从已有 focus reason 和 break event 推导 |
| Agent context 过载 | prompt 噪音增加 | Phase 1 只注入 workspace context 摘要，不注入全文历史 |

## 回写规则

- Phase 0 完成后，必须更新 `focus-mode-flowtime` 的当前状态和剩余缺口。
- 任一 Phase 1 能力实现后，必须回写本 change 对应 spec 的“当前状态”。
- 如果实现新增数据库表或字段，必须同步更新 spec 的数据模型说明。
- 如果实现放弃本 change 的推荐顺序，必须先更新本文件“优先级与推进顺序”。
