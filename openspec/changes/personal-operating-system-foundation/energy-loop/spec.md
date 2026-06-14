# Energy Loop Spec

## 背景

当前产品已有 time entry、daily review、daily plan、focus reason，但这些信号还没有形成精力管理闭环。V1 的目标不是复杂算法，而是先让精力信号进入复盘和计划。

## 范围

Phase 1 包含：

- Daily Review 记录轻量 energy check-in。
- Daily Plan 使用 energy/focus/review signals 调整建议。
- Focus low energy reason 进入 review/plan 语境。

Phase 1 不包含：

- 医疗或健康诊断。
- 自动识别用户精力。
- 复杂预测模型。
- 完整 structured planner。

## 当前状态

- 证据：`server/migrations/038_daily_review.up.sql` `daily_review`
- 当前行为：review 存储 `draft_content`、`status`、`generated_by`，并通过迁移补充可选 `energy_level`、`energy_note`、`recovery_need`。
- 当前缺口：无 Phase 1 阻塞缺口。

- 证据：`server/internal/service/review.go` `GenerateReviewDraft`
- 当前行为：review 根据 time entries、done issues、open issues 和 focus signals 生成 Markdown，确认时保存 energy check-in。
- 当前缺口：无 Phase 1 阻塞缺口。

- 证据：`server/migrations/039_daily_plan.up.sql` `daily_plan`
- 当前行为：plan 存储 `draft_content` 和 `top_issue_ids`。
- 当前缺口：没有 `daily_plan_item`、energy demand、capacity；这些属于 structured planning 后置。

- 证据：`server/internal/service/daily_plan.go` `GeneratePlanDraft`
- 当前行为：plan 使用 open issues、昨日 confirmed review 和 focus signals，并生成精力安排/低精力 fallback。
- 当前缺口：无 Phase 1 阻塞缺口。

- 证据：`server/internal/handler/focus.go` `validFocusReason`
- 当前行为：Focus reason 支持 `low_energy`。
- 当前缺口：低精力已被复盘和计划消费；复杂趋势报表后置。

## Phase 1 已关闭缺口

1. 用户已有轻量方式记录每日精力。
2. Focus 中的低精力原因已沉淀到 Daily Review。
3. Daily Plan 已根据精力状态调整任务建议。
4. 休息跳过/完成行为已进入后续计划语境。

## 推荐功能切片

### E1. Energy Check-in

目标：建立最小精力信号。

当前状态：已完成。

推荐字段：

- `energy_level`：1 到 5，可选。
- `energy_note`：自由文本，可选。
- `recovery_need`：布尔或轻量枚举，可选。

完成定义：

- Daily Review 可记录这些字段。
- 不填写不阻塞 review 生成或确认。

验证记录：

- `cd server && go test ./internal/handler ./internal/service`
- `pnpm typecheck`

### E2. Focus Signals In Review

目标：Daily Review 能解释今日精力消耗。

当前状态：已完成。

最小行为：

- Review draft 汇总 focus duration、abandoned count、low_energy reason count、break skipped/completed count。
- 文案中提示是否存在恢复不足风险，但不做健康诊断。

完成定义：

- ReviewService 的 prompt 输入包含 focus event summary。

验证记录：

- `cd server && go test ./internal/service`

### E3. Energy-aware Daily Plan

目标：明日计划能考虑精力状态。

当前状态：已完成。

最小行为：

- DailyPlanService 读取最近 confirmed review 和 focus signals。
- 计划草稿分出 high-energy work 和 low-energy fallback。
- 低精力时减少大块深度工作建议。

完成定义：

- plan draft 中明确出现低精力备选任务或容量调整建议。

验证记录：

- `cd server && go test ./internal/service`

## 交接说明

E1 是数据入口；E2/E3 依赖 E1 但也可以先基于现有 `low_energy` focus reason 做软启动。任何新增字段都必须先定义迁移、SQL、Go response、前端类型。
