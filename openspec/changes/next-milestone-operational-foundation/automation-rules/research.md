# Automation Rules Research

## 调研目标

确认现有 automation 的数据模型、后端接口、前端入口和执行边界，找出从 template toggle 到最小 rule model 的可复用部分。

## 现状链路

当前链路是：

1. `server/internal/automation/templates.go` 定义 built-in templates。
2. `server/internal/handler/automation.go` 读取 templates 并结合 workspace 的 `automation_rule` enablement 状态返回前端。
3. `apps/workspace/src/features/automation/components/automation-tab.tsx` 展示模板卡片、开关和手动运行按钮。
4. `server/pkg/db/queries/automation.sql` 通过 `template_id` 保存启停状态。
5. `RunRule` 只为 `standup_summary` 注册了手动 runner。

## 关键代码证据

| 文件 | 符号 | 结论 |
| --- | --- | --- |
| `server/internal/automation/templates.go` | `BuiltinTemplates` | 模板是静态注册表，不支持 workspace 自定义条件和动作。 |
| `server/internal/automation/templates.go` | `FindTemplate` | handler 只能校验 template_id 是否属于内置模板。 |
| `server/internal/handler/automation.go` | `TemplateResponse` | 返回字段只有 template metadata 和 enabled，没有 rule condition/action。 |
| `server/internal/handler/automation.go` | `EnableRule` | 请求只接受 `template_id`，说明当前启用语义不是规则创建。 |
| `server/internal/handler/automation.go` | `RunRule` | 手动执行通过 switch templateID 分派，扩展性有限。 |
| `server/pkg/db/queries/automation.sql` | `UpsertAutomationRule` | SQL 只保存 template_id 与 enabled 状态。 |
| `apps/workspace/src/features/automation/components/automation-tab.tsx` | `AutomationTab` | UI 只呈现模板列表与开关。 |

## 数据模型或状态流

当前 DB 状态是 `automation_rule(template_id, enabled, workspace_id, created_by)`。它可以作为旧 preset enablement 的过渡基线，但不能承载：

- condition group。
- action list。
- rule name。
- rule status。
- last_run_at / last_error。
- run log。

## 边界条件

- 第一版 rule action 必须限制在 issue/comment/inbox 相关动作。
- rule evaluation 必须以 workspace_id 过滤。
- rule execution 不得绕过现有 issue 权限和 membership 校验。
- manual template runner 需要被迁移为 preset action 或保留为 legacy endpoint 直到替换完成，但不能长期并行。

## 未决问题

1. 第一版是否允许多个 condition group，还是只允许单组 AND 条件？
2. `standup_summary` 应作为 automation preset 继续存在，还是迁到独立 reporting action？
3. run log 的保留周期是否需要限制？
