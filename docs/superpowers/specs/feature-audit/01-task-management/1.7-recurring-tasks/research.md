# 1.7 重复任务调研

## 1. 调研目标

- 确认仓库内是否已有重复任务实现。
- 明确重复任务必须与现有 issue 模型如何对接。

## 2. 现状链路

- 搜索关键词：`recurr`、`repeat`、`recurring`、`recurrence`、`cron`
- 搜索结果：未找到与 issue 重复任务相关的实现匹配。

## 3. 关键代码证据

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/shared/types/issue.ts` · `Issue` | issue 类型没有重复规则、系列、实例、例外字段。 |
| `server/migrations/001_init.up.sql` · `issues` | 数据库 schema 没有重复任务相关表或字段。 |
| `server/pkg/db/queries/issue.sql` · `UpdateIssue` / `ListIssues` | 服务端只围绕单条 issue 工作，没有系列生成或例外处理。 |
| 搜索关键词 `recurr` / `repeat` / `recurring` / `recurrence` / `cron` | 未找到匹配，说明当前仓库没有现成重复任务实现可复用。 |

## 4. 数据模型或状态流

- 当前唯一任务实体是 issue。
- `IssueStatus` 仅表达工作流状态，不能承载重复系列状态。

## 5. 边界条件

- 重复任务必须仍然产出真实 issue，才能兼容项目、标签、附件、时间追踪。
- 若直接在一条 issue 上循环复用，会破坏历史完成记录与 2.1 时间记录。

## 6. 未决问题

1. 下一次实例是在“完成当前任务时”生成，还是按计划窗口提前生成。
2. 单次修改与“修改全部未来实例”的例外模型如何落表。
