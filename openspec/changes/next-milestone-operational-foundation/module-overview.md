# Next Milestone Operational Foundation

## 目标与范围

本 change 承接 `docs/superpowers` 中未完成但仍有下一里程碑价值的能力，并把它们收敛为正式 OpenSpec 设计包。范围只覆盖已有产品基线可以延展的运营基础能力：

- automation rules：从内置模板开关演进到最小条件 + 动作规则模型。
- planning insights：把估算、项目健康和任务流指标收敛为规划统计基线。
- data management：把现有 workspace 数据导入导出扩展成稳定 canonical manifest。

本 change 不实现代码，不新增产品功能，只把下一里程碑候选范围从 `docs/superpowers` 迁入 OpenSpec，作为后续实现的唯一设计入口。

## 能力列表

| 能力 | 当前状态 | 优先级 | 来源 | 处理方式 |
| --- | --- | --- | --- | --- |
| Automation rules | 部分完成 | P1 | `docs/superpowers/specs/linear-alignment` | 新建单能力设计包 |
| Planning insights | 部分完成 | P1 | `docs/superpowers/specs/linear-alignment`、`docs/superpowers/specs/feature-audit/04-analytics` | 新建单能力设计包 |
| Data management | 部分完成 | P1 | `docs/superpowers/specs/feature-audit/05-data-sync` | 新建单能力设计包 |

## 当前状态证据

- `server/internal/handler/automation.go` `ListTemplates` / `EnableRule` / `DisableRule` / `RunRule`：自动化当前围绕内置 template_id 开关和手动执行，缺少用户可配置条件与动作。
- `server/internal/automation/templates.go` `BuiltinTemplates`：自动化模板是静态注册表，不是 workspace 自定义规则模型。
- `apps/workspace/src/shared/types/issue.ts` `Issue`：issue 类型包含 priority、dates、project、archive 等执行字段，但没有估算、cycle、milestone 或 roadmap 规划字段。
- `server/internal/handler/time_entry.go` `GetTeamTimeStats`：团队统计当前只返回 `by_user` 与 `by_project` 工时聚合。
- `server/pkg/db/queries/pomodoro.sql` `GetPomodoroStats`：番茄统计只有 today/week/total_seconds 轻量摘要。
- `server/internal/service/data_sync.go` `ManifestData`：canonical manifest 当前只包含 `Issues []ManifestIssue`。
- `apps/workspace/src/features/settings/components/data-tab.tsx` `DataTab`：设置页已有导出、导入、dry-run、apply 的入口，但 UI 和契约仍围绕 issue manifest。

## 非目标

- 不迁移 blocklist、bookmarks、global notes、多端离线同步等低优先级个人工具能力。
- 不把 `docs/superpowers` 的审计台账原样搬进 OpenSpec。
- 不把 broad roadmap 文档直接作为实现计划；每个可执行能力必须有独立 `spec/research/design/tasks`。
- 不在本 change 中实现任何后端、前端或数据库变更。

## 优先级与顺序

1. 先做 data management，因为已有 DataTab 和 DataSyncService，且统一 manifest 能降低后续维护成本。
2. 再做 planning insights，因为已有 project/time/pomodoro 基线，但统计口径需要先固定。
3. 最后做 automation rules，因为规则引擎需要依赖更稳定的 issue、planning、data 事件语义。

## 共享约束

- 所有能力必须继续以 workspace 为租户边界。
- 所有规划与统计能力必须优先复用 `issue`、`project`、`time_entry`、`pomodoro` 现有实体，不新增平行工作项模型。
- 所有自动化能力必须保留 agents as teammates 的产品差异化，不做隐藏式后台自动化黑箱。
- 后续执行时必须先读取对应能力的 `design.md` 和 `tasks.md`，不得从已删除的 `docs/superpowers` 源文档继续实现。

## 风险与依赖

| 风险或依赖 | 影响 | 处理方式 |
| --- | --- | --- |
| 把 superpowers 的大量 backlog 全部迁入 OpenSpec | 维护期范围继续膨胀 | 只迁移 P1 且有代码基线的三组能力 |
| 新 design 包与 `structured-execution-foundation` 重叠 | 后续执行入口不清晰 | 本 change 只做下一里程碑可执行细化，`structured-execution-foundation` 保留为路线图蓝图 |
| 统计和自动化直接上全量能力 | 架构复杂度快速上升 | 每个单能力设计包都定义最小可用范围和非目标 |
| 删除 `docs/superpowers` 后丢失历史上下文 | 需要追溯时成本上升 | 有价值内容已迁移到本 change，旧文档可通过 git 历史追溯 |

## 回写规则

- 任一能力实现后，必须回写对应 `spec.md` 的当前状态、缺口和交接说明。
- 如果实现阶段发现范围变化，先更新对应 `design.md` 和 `tasks.md`，再继续编码。
- 本 change 完成迁移后，`docs/superpowers` 不再作为权威设计入口。
