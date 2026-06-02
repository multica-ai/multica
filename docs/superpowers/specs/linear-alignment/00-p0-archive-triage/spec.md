# P0 Archive + Inbox Triage Spec

## 背景

当前 Multica 的使用阶段更偏个人与小团队协作。相比固定迭代计划，当前更需要先补齐两个高收益闭环：

1. Issue 如何安全退出默认工作视图，并且可以恢复。
2. Inbox 如何从通知列表升级为可处理队列，避免新输入流堆积。

因此当前 P0 从原先的 Archive + Cycle 调整为 Archive + Inbox Triage。Cycle/Iteration 暂缓，不进入本轮实现。

## 范围

- 本次覆盖：
  - Issue Archive 生命周期闭环。
  - Inbox Triage 手动分拣闭环。
  - 两个闭环的状态模型、接口方向、UI 口径、验收标准。
- 本次不覆盖：
  - Cycle/Iteration。
  - Roadmap。
  - Insights/Analytics。
  - Workflow Automation 规则引擎。
  - Enterprise 能力。

## 当前状态

### Issue Archive

已新增 issue archive/restore 生命周期。默认 issue 列表排除 archived issue，Archived Issues 视图可查看并恢复归档 issue。原 hard delete 路径保留为后端能力，但普通 UI 已切换为 archive。

### Inbox Triage

已新增 inbox triage 状态，read 与处理完成分离。Inbox 支持 Done、Dismiss、Snooze 和对应批量操作；默认列表展示 pending 与到期 snoozed items，隐藏 handled、dismissed、未到期 snoozed items。

## 证据

- `server/pkg/db/queries/issue.sql` `DeleteIssue`：当前为 `DELETE FROM issue WHERE id = $1`，属于物理删除。
- `server/internal/handler/issue.go` `DeleteIssue`：删除时会取消 task、收集附件 URL、删除 issue、删除 S3 对象并发布 `EventIssueDeleted`。
- `server/internal/handler/issue.go` `BatchDeleteIssues`：批量删除同样调用物理删除并发布 deleted 事件。
- `apps/workspace/src/shared/types/issue.ts` `Issue`：当前无 `archived_at` / `archived_by` 字段。
- `server/pkg/db/queries/issue.sql` `ListIssues`：当前无 archived 过滤条件。
- `server/pkg/db/queries/inbox.sql` `ListInboxItems`：当前默认只列出 `archived = false` 的 inbox item。
- `server/pkg/db/queries/inbox.sql` `MarkInboxRead`：当前只支持 read 状态。
- `server/pkg/db/queries/inbox.sql` `ArchiveInboxItem`：当前只支持 archived 清理状态。
- `server/internal/handler/inbox.go` `ArchiveInboxItem`：归档某个 issue 关联 inbox item 时，会一并归档同 issue sibling inbox items。
- `apps/workspace/src/shared/types/inbox.ts` `InboxItem`：当前无 `triage_status`、`snoozed_until`、`handled_at`、`dismissed_at` 字段。
- `apps/workspace/src/features/inbox/mutations.ts` `useInboxMutations`：当前无 handle/dismiss/snooze mutation。
- `apps/workspace/src/features/inbox/components/inbox-page.tsx` `handleSelect`：点击 item 会自动 mark read，但不代表处理完成。

## 缺口

1. Issue Archive 闭环已实现：archive/restore、批量 archive、默认隐藏与归档视图入口。
2. Inbox Triage 闭环已实现：handled/dismissed/snooze、批量 triage、到期 snooze 回流。
3. Cycle/Iteration 仍保持 Deferred，不进入本轮 P0。

## 交接说明

- 执行 Agent 必须先读取本目录下 `research.md`、`design.md`、`tasks.md`。
- Archive 和 Inbox Triage 是两个独立闭环，不允许把 dismiss inbox item 理解为 archive issue。
- 若实现阶段发现状态字段、事件名或 API 命名需要调整，必须先回写 `design.md` 再继续实现。
