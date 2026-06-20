# Issue Energy Loop

## Summary

Issue Energy Loop 是把 issue 执行、专注记录和精力复盘串起来的个人工作闭环。当前版本不再提供结构化 Plan 页面或计划项模型；用户仍可以使用已有 daily plan Markdown 草稿作为轻量计划记录，但执行闭环的结构化对象回到 issue、focus session、time entry、daily review 和 issue type。

## Product Hard Rules

1. Issue 是当前版本唯一可执行对象；所有结构化执行入口必须优先关联 issue。
2. Daily plan 只能作为 Markdown 草稿、AI 摘要或轻量计划记录存在，不能拥有独立状态流转、排序、执行入口或完成语义。
3. Focus、Time Entry、Daily Review 只能记录 issue 执行过程和结果，不能把非 issue 对象升级为可执行工作项。
4. 如果后续重新引入 Plan 或计划项模型，必须先写新的 product spec 和 tech spec，明确为什么 issue 模型不足，并重新评审运行时对象矩阵。

## Scope

当前版本保留：

- issue 作为唯一可执行工作对象。
- issue type 作为 issue 的工作形态分类，用于表达 deep work、light work、recovery、neutral 等负载倾向。
- Focus / Flowtime / Pomodoro 作为执行入口。
- time entry 作为实际投入记录。
- daily review 作为精力、阻力、恢复需求和执行结果的复盘入口。
- 既有 `/api/daily-plans` Markdown 草稿能力，作为后续 AI 生成摘要和轻量计划记录。

当前版本移除或不做：

- 独立 `/plan` 页面。
- 结构化 Plan facade，例如 `/api/plans`。
- `plan_item` 表、计划项排序、计划项状态、计划项候选区。
- Focus 和 Time Entry 与计划项的结构化关联。
- 从计划项启动 Focus。

## User Model

1. 用户把所有待推进事项都创建为 issue。
2. 用户通过 issue type 表达这条 issue 对精力的要求，而不是把它放进结构化 Plan。
3. 用户进入 Focus 时可以直接关联 issue，也可以只记录一段无 issue 的专注。
4. Focus 完成后写入 time entry，并保留阻力原因、暂停原因、放弃原因和休息建议等信号。
5. 用户在 daily review 中回看当天投入、推进情况和精力状态。
6. 下一轮计划暂时仍以 daily plan Markdown 草稿表达，不提供可排序、可执行的计划项列表。

## Behavior

1. Issue 创建和编辑支持 `issue_type_id`。
2. Issue type 是 workspace 级配置，同一个 team 下 issue 和后续 knowledge 等对象应复用同一套标签/分类体系。
3. 内置 issue type 至少包含 task、feature、bug、chore、research、recovery。
4. Issue type 可以被归档；已经被 issue 使用的 type 不应硬删除。
5. Focus 启动请求只接受 issue、description、commitment、label 和阻力原因等上下文。
6. Focus 当前状态和事件响应不暴露 `plan_item_id`。
7. Focus 完成后只创建 time entry，不修改任何计划项状态。
8. Time entry 只关联 issue 和 label，不关联计划项。
9. Daily plan Markdown 能力继续存在，但它不是结构化 Plan，不出现在主导航中。
10. 如果后续重新设计 Plan，需要重新写 product spec 和 tech spec，再决定是否引入计划项模型。

## Validation

- 主导航不显示 Plan。
- `/plan` 不是已注册的前端页面。
- 后端不暴露 `/api/plans` 或 `/api/plan-items`。
- 数据库迁移删除 `plan_item` 和 `plan_item_id` 相关字段。
- Focus 和 Time Entry API 类型中没有 `plan_item_id`。
- Issue type 字段和既有 daily plan Markdown 能力继续工作。
