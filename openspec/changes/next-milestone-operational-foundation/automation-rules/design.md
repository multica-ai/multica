# Automation Rules Design

## 目标

把 automation 从内置模板启停升级为 workspace 级最小规则模型，让团队能配置有限、可审计、可验证的条件动作自动化。

## 非目标

- 不实现任意脚本、表达式语言或 webhook marketplace。
- 不实现多步骤工作流画布。
- 不允许规则跨 workspace 读写数据。
- 不把 agent task execution 替换成 automation；automation 只负责触发和协调。

## 当前架构基线

- `BuiltinTemplates` 是静态模板列表。
- `AutomationHandler` 负责 template list、enable、disable、manual run。
- `automation_rule` 当前只保存 template enablement。
- `AutomationTab` 只展示模板开关。

## 缺口定义

需要新增三个契约：

1. Rule definition：名称、状态、trigger、conditions、actions。
2. Rule execution：事件进入后计算条件，按动作白名单执行。
3. Rule observability：run log、last_run_at、last_error、dry-run preview。

## 方案与权衡

### 方案 A：继续添加 built-in templates

优点：改动小。缺点：无法让 workspace 自定义，后续每个需求都要改代码。

### 方案 B：最小 JSON rule model

优点：保留有限能力，同时支持用户自定义。缺点：需要新增 schema、校验和 UI。

### 方案 C：完整规则引擎

优点：表达力强。缺点：复杂度过高，不适合维护期后的下一里程碑。

## 推荐方案

采用方案 B。

第一版 rule model：

- `trigger`: `issue.created`、`issue.updated`、`issue.status_changed`、`schedule.daily`。
- `conditions`: 白名单字段条件，支持 `equals`、`not_equals`、`contains_any`、`before`、`after`。
- `actions`: `add_comment`、`update_issue`、`create_issue`、`notify_inbox`。
- `enabled`: bool。
- `source`: `custom` 或 `preset`。

## 数据模型或状态模型

建议新增：

- `automation_rule_v2`
  - `id`
  - `workspace_id`
  - `name`
  - `trigger_type`
  - `conditions_json`
  - `actions_json`
  - `enabled`
  - `source`
  - `created_by`
  - `created_at`
  - `updated_at`
- `automation_rule_run`
  - `id`
  - `workspace_id`
  - `rule_id`
  - `trigger_event`
  - `status`
  - `summary`
  - `error`
  - `created_at`

旧 `automation_rule` 可以在实现阶段迁移为 preset-backed rule。若产品还未上线，不要添加长期兼容层。

## 接口契约

建议接口：

- `GET /api/automation/rules`
- `POST /api/automation/rules`
- `GET /api/automation/rules/{id}`
- `PATCH /api/automation/rules/{id}`
- `DELETE /api/automation/rules/{id}`
- `POST /api/automation/rules/{id}/dry-run`
- `GET /api/automation/rules/{id}/runs`

所有接口必须要求 workspace membership。

## UI 或交互流程

Settings → Automation：

1. Rule list：显示 name、trigger、enabled、last run、last error。
2. Rule editor：选择 trigger，再配置 conditions 和 actions。
3. Presets：内置模板以 preset card 形式创建 rule，而不是只开关 template。
4. Run log drawer：展示最近执行记录。

## 权限、边界条件、异常路径

- 只有 workspace admin 或 owner 可创建、编辑、删除 rule。
- 普通成员可查看 rule list 和 run log。
- action 执行失败必须写 run log，不允许静默失败。
- 条件引用的 label/project/member 不存在时，rule dry-run 必须报错。

## 实现约束

- condition/action JSON 必须有 Go 结构体校验，不允许把任意 JSON 直接透传执行。
- action 执行必须复用现有 handler/service 或 query 语义，避免绕过事件和权限。
- 前端必须用现有 query/mutation 模式，不回到 long-lived Zustand remote state。

## 风险与对策

| 风险 | 对策 |
| --- | --- |
| 规则模型过宽 | 第一版只支持白名单条件和动作 |
| 运行失败不可见 | 每次执行写 `automation_rule_run` |
| preset 与 custom 双系统 | preset 只作为创建 rule 的模板，不长期并行 |
| 自动化误操作 | 提供 dry-run 和明确 action summary |

## 验收检查

- 能创建一条 issue status trigger rule。
- 规则触发后能执行一个白名单 action。
- dry-run 能展示将要执行的 action summary。
- run log 能记录 success 和 failed。
- 旧 template preset 不再作为独立长期入口。
