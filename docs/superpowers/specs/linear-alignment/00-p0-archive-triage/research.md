# P0 Archive + Inbox Triage Research

## 调研目标

1. 确认 Issue 删除生命周期的当前实现。
2. 确认 Inbox 当前 read/archive 能力与缺口。
3. 明确 Archive 与 Inbox Triage 的边界，避免后续实现混淆。

## 现状链路

### Issue 删除链路

1. 前端触发删除或批量删除 issue。
2. API 调用后端 issue delete 或 batch delete route。
3. `server/internal/handler/issue.go` `DeleteIssue` 或 `BatchDeleteIssues` 加载 issue。
4. handler 调用 `TaskService.CancelTasksForIssue`。
5. 单个删除会收集附件 URL 并在删除后清理 S3 对象。
6. handler 调用 `server/pkg/db/queries/issue.sql` `DeleteIssue` 执行物理删除。
7. handler 发布 `EventIssueDeleted`。

### Inbox 处理链路

1. 后端通过 `CreateInboxItem` 写入 inbox item。
2. `ListInboxItems` 默认读取 `archived = false` 的 inbox item。
3. 前端 `useInboxItemsQuery` 调用 `api.listInbox()`。
4. `useInboxStore` 对同 issue item 做去重展示。
5. 用户点击 item 时，`InboxPage` `handleSelect` 调用 mark read。
6. 用户可以 archive 单个 item，或执行 archive all/archive all read/archive completed。

## 关键代码证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `server/pkg/db/queries/issue.sql` | `DeleteIssue` | 当前 issue 删除是物理删除 |
| `server/internal/handler/issue.go` | `DeleteIssue` | 删除会清 S3 附件并发布 deleted 事件 |
| `server/internal/handler/issue.go` | `BatchDeleteIssues` | 批量删除同样是物理删除语义 |
| `apps/workspace/src/shared/types/issue.ts` | `Issue` | 前端类型无法表达 archived 状态 |
| `server/pkg/db/queries/issue.sql` | `ListIssues` | 默认列表没有 archived 过滤 |
| `server/pkg/db/queries/inbox.sql` | `ListInboxItems` | 默认 inbox 仅排除 `archived = true` |
| `server/pkg/db/queries/inbox.sql` | `MarkInboxRead` | read 只表示已读 |
| `server/pkg/db/queries/inbox.sql` | `ArchiveInboxItem` | archive 只表示清出 inbox |
| `server/internal/handler/inbox.go` | `ArchiveInboxItem` | 当前单条 archive 会清理同 issue sibling items |
| `apps/workspace/src/shared/types/inbox.ts` | `InboxItem` | 无 triage 状态与 snooze 字段 |
| `apps/workspace/src/features/inbox/mutations.ts` | `useInboxMutations` | 无 handle/dismiss/snooze action |
| `apps/workspace/src/features/inbox/components/inbox-page.tsx` | `handleSelect` | 打开 item 只 mark read，不代表处理完成 |

## 数据模型或状态流

### Issue

- 当前字段：status、priority、project_id、parent_issue_id、due_date、start_date、end_date 等。
- 缺失字段：`archived_at`、`archived_by`。
- 当前删除后关联数据依赖数据库级联与 handler 附件清理，无法恢复。

### InboxItem

- 当前字段：read、archived、type、severity、issue_id、actor、details。
- 缺失字段：`triage_status`、`snoozed_until`、`handled_at`、`dismissed_at`、`triaged_by`。
- 当前状态：
  - read 表示用户看过。
  - archived 表示从默认 inbox 清出。
- 目标状态：
  - read 与处理完成分离。
  - triage_status 表示 pending/handled/dismissed/snoozed。

## 边界条件

- 所有 Issue Archive 与 Inbox Triage 操作必须保持 workspace 边界。
- Issue Archive 不删除 comment、attachment、label、dependency、subscriber、time entry。
- Inbox Triage 不改变 issue 生命周期。
- 点击 inbox item 只 mark read，不自动 handled。
- Snooze 到期不引入后台任务，查询时根据 `snoozed_until <= now()` 重新展示。

## 未决问题

本设计包将以下问题作为已决策内容写入 `design.md`，执行阶段不再重新决策：

1. Issue Archive 使用独立 API，不把现有 DELETE 静默改为 archive。
2. Inbox 主交互使用 Done/Dismiss/Snooze，不继续用 Archive 作为新 UI 主语义。
3. Cycle/Iteration 暂缓，不进入当前 P0。
