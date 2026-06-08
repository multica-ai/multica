# Automation Rules Spec

## 背景

Multica 已有 automation template 开关和手动运行能力，但它仍是固定模板列表，不能表达 workspace 自定义条件、动作或规则执行记录。下一里程碑如果要让 agents 成为稳定的协作队友，需要先把 automation 从“模板启停”升级为“最小规则模型”。

## 范围

本能力只定义最小 automation rule：

- workspace-scoped rule 列表。
- rule condition：基于 issue status、assignee、label、project、priority、due date 的有限条件。
- rule action：创建 comment、更新 issue 字段、创建 follow-up issue、通知 inbox。
- rule run log：记录触发、跳过、失败、执行结果。
- 保留现有 built-in template 作为规则 preset，不保留双系统。

不覆盖跨 workspace automation、任意脚本、webhook marketplace、复杂流程编排器。

## 当前状态

- 状态：部分完成。
- 已完成：内置 template 注册表、workspace enable/disable、手动运行单个 template。
- 缺失：条件模型、动作模型、规则 CRUD、执行日志、规则预览、安全边界。

## 证据

- `server/internal/automation/templates.go` `BuiltinTemplates`：模板通过静态 Go slice 注册，说明当前能力不是用户定义规则模型。
- `server/internal/handler/automation.go` `ListTemplates`：返回 built-in templates 并附加 workspace enabled 状态。
- `server/internal/handler/automation.go` `EnableRule`：请求体只包含 `template_id`，无法表达条件或动作。
- `server/internal/handler/automation.go` `RunRule`：当前只允许 manual template，且只有 `standup_summary` 有 runner。
- `server/pkg/db/queries/automation.sql` `UpsertAutomationRule`：automation_rule 以 `template_id` 为核心，不包含 condition/action JSON。
- `apps/workspace/src/features/automation/components/automation-tab.tsx` `AutomationTab`：前端展示 template toggle 和 run now，不提供 rule builder。

## 缺口

1. 规则缺口：现有 `template_id` 开关无法表达 “当 issue 进入 blocked 时通知负责人” 等条件动作。
2. 执行缺口：没有 rule run log，失败原因不可追溯。
3. UI 缺口：没有规则列表、创建、编辑、禁用、预览。
4. 安全缺口：动作边界未定义，执行 Agent 可能在后续实现中引入过宽权限。
5. 迁移缺口：built-in templates 如何变成 presets 需要明确。

## 交接说明

执行 Agent 必须以本目录 `design.md` 和 `tasks.md` 为入口。禁止直接实现任意条件表达式语言或脚本执行器；第一版只能实现白名单条件和白名单动作。
