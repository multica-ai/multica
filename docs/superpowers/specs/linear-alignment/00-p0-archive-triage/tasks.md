# P0 Archive + Inbox Triage Tasks

## 实现目标

按 Archive → Inbox Triage 顺序完成当前 P0。实现完成后，Issue 具备可恢复归档生命周期，Inbox 具备手动分拣闭环。

## 前置依赖

- 执行前必须读取本目录 `spec.md`、`research.md`、`design.md`。
- 执行前必须确认不实现 Cycle/Iteration。
- 执行阶段若改变 API、字段或事件语义，必须先回写 `design.md`。

## 任务切片

### Task 1：Issue Archive schema 与 query（已完成）

目标：

- 为 issue 新增 archive 生命周期字段。
- 调整 issue 查询默认排除 archived issue。

目标文件：

- `server/migrations/**`
- `server/pkg/db/queries/issue.sql`
- `server/pkg/db/generated/**`（通过 sqlc 生成）

改动：

- 新增 migration：`issue.archived_at`、`issue.archived_by`。
- 新增 `ArchiveIssue` query。
- 新增 `RestoreIssue` query。
- 新增 `BatchArchiveIssues` 或按单条 query 循环处理。
- `ListIssues`、`CountListedIssues`、`ListOpenIssues`、`CountIssues` 默认排除 archived issue。
- 增加 archived-only 或 include archived 参数支持。

完成定义：

- sqlc 生成成功。
- 默认 issue 查询不返回 archived issue。
- archived-only 查询能返回 archived issue。

验证方式：

- `make sqlc`
- Go query/handler 测试覆盖 archive/restore/list 口径。

### Task 2：Issue Archive handler、router 与 event（已完成）

目标：

- 新增 archive/restore API。
- 确保 archive 不删除关联历史。

目标文件：

- `server/internal/handler/issue.go`
- `server/cmd/server/router.go`
- `server/pkg/protocol/**`
- `server/internal/service/task.go`（如需要调整 task 取消行为）

改动：

- 新增 `ArchiveIssue` handler。
- 新增 `RestoreIssue` handler。
- 新增 `BatchArchiveIssues` handler。
- router 新增 archive/restore/batch-archive route。
- 新增 `issue:archived`、`issue:restored` 或 `issue:unarchived`、`issue:batch_archived` event。
- Archive 时取消未完成 agent task。
- Archive 时自动处理关联 pending inbox items。
- 保留 hard delete 时必须重命名，避免普通 UI 使用。

完成定义：

- 单个 archive/restore API 可用。
- 批量 archive API 可用。
- Archive 不调用 S3 删除。
- Archive 不发布 deleted event。

验证方式：

- Go handler 测试覆盖：
  - archive 后 issue 可恢复。
  - archive 不删除 comment/attachment/label/dependency/subscriber/time entry。
  - archive 取消未完成 task。
  - archive 触发正确 event。

### Task 3：Issue Archive 前端类型、API 与 UI（已完成）

目标：

- 前端可以展示、归档、恢复 issue。

目标文件：

- `apps/workspace/src/shared/types/issue.ts`
- `apps/workspace/src/shared/types/events.ts`
- `apps/workspace/src/shared/api/client.ts`
- `apps/workspace/src/features/issues/**`
- `apps/workspace/src/router.tsx`
- `apps/workspace/src/features/layout/navigation.ts`

改动：

- Issue 类型新增 `archived_at`、`archived_by`。
- API client 新增 archive/restore/batch archive 方法。
- issues mutation 新增 archive/restore。
- issue detail 增加 archived banner 与 Restore 主操作。
- issue 列表增加 Archive 动作。
- 新增 Archived Issues 入口或筛选。
- 默认列表排除 archived issue。
- realtime 同步 archive/restore event。

完成定义：

- 用户可以从 UI archive issue。
- 用户可以进入 Archived Issues 视图 restore issue。
- archived issue 不出现在默认列表。

验证方式：

- 前端单元测试覆盖 mutation 和过滤。
- E2E 覆盖 archive → hidden → archived view → restore。

### Task 4：Inbox Triage schema 与 query（已完成）

目标：

- 为 inbox item 新增 triage 状态和 snooze 支持。

目标文件：

- `server/migrations/**`
- `server/pkg/db/queries/inbox.sql`
- `server/pkg/db/generated/**`（通过 sqlc 生成）

改动：

- 新增 migration：`triage_status`、`snoozed_until`、`handled_at`、`dismissed_at`、`triaged_by`。
- `ListInboxItems` 默认展示 pending 和到期 snoozed items。
- 新增 `HandleInboxItem`、`DismissInboxItem`、`SnoozeInboxItem` query。
- 新增 batch handle/dismiss/snooze query。
- 保留现有 archive query 兼容旧路径。

完成定义：

- 默认 inbox 查询不返回 handled、dismissed、未到期 snoozed items。
- 到期 snoozed items 会重新出现在默认查询。

验证方式：

- `make sqlc`
- Go query 测试覆盖 pending/handled/dismissed/snoozed 查询口径。

### Task 5：Inbox Triage handler、router 与 event（已完成）

目标：

- 新增 Done/Dismiss/Snooze API。

目标文件：

- `server/internal/handler/inbox.go`
- `server/cmd/server/router.go`
- `server/pkg/protocol/**`

改动：

- 新增 `HandleInboxItem` handler。
- 新增 `DismissInboxItem` handler。
- 新增 `SnoozeInboxItem` handler。
- 新增 batch handle/dismiss/snooze handler。
- 新增 `inbox:handled`、`inbox:dismissed`、`inbox:snoozed`、`inbox:batch_triaged` event。
- Handle 关联 issue item 时，同 issue pending sibling items 一并 handled。
- Dismiss/Snooze 默认只作用于目标 item，批量接口除外。

完成定义：

- Done/Dismiss/Snooze API 可用。
- read 和 handled 保持分离。
- sibling 处理符合设计。

验证方式：

- Go handler 测试覆盖：
  - handle。
  - dismiss。
  - snooze。
  - snooze 到期查询。
  - handle sibling items。
  - recipient 权限边界。

### Task 6：Inbox Triage 前端类型、API 与 UI（已完成）

目标：

- Inbox UI 使用 Done/Dismiss/Snooze 完成手动 triage。

目标文件：

- `apps/workspace/src/shared/types/inbox.ts`
- `apps/workspace/src/shared/types/events.ts`
- `apps/workspace/src/shared/api/client.ts`
- `apps/workspace/src/features/inbox/**`
- `apps/workspace/src/features/realtime/**`

改动：

- InboxItem 类型新增 triage 字段。
- API client 新增 handle/dismiss/snooze/batch 方法。
- `useInboxMutations` 新增 triage mutations。
- Inbox list item 增加 Done/Dismiss/Snooze 操作。
- 点击 item 仍只 mark read。
- 批量菜单改为 Done all / Dismiss all / Snooze selected 或等价语义。
- `archive completed` 迁移为 Handle completed issue items。
- realtime 同步 triage events。

完成定义：

- 用户可以 Done/Dismiss/Snooze item。
- Snooze 未到期 item 不显示。
- 到期 snoozed item 重新显示。
- read 不等于 handled。

验证方式：

- 前端单元测试覆盖 mutations 和列表过滤。
- E2E 覆盖 read、done、dismiss、snooze 主路径。

## 执行顺序

1. Task 1
2. Task 2
3. Task 3
4. Task 4
5. Task 5
6. Task 6

Archive 必须先于 Inbox Triage 完成，因为 Inbox Triage 需要明确 archive issue 后相关 inbox item 如何处理。

## 实现回写

- Issue Archive 已落地：schema、query、handler/router、event、前端类型/API/mutation、归档视图入口、详情恢复入口、列表/看板/批量归档动作。
- Inbox Triage 已落地：schema、query、handler/router、event、前端类型/API/mutation、Done/Dismiss/Snooze 单项与批量操作。
- Cycle/Iteration 未实现，仍按设计保持 Deferred。

## 验证结果

- 已通过：`make sqlc`
- 已通过：`set -a; source .env.worktree; set +a; cd server && go test ./internal/handler ./cmd/server`
- 已通过：`set -a; source .env.worktree; set +a; cd server && go test ./...`
- 已通过：`set -a; source .env.worktree; set +a; pnpm --filter @multica/workspace exec vitest run src/features/issues/mutations.test.tsx src/features/issues/utils/filter.test.ts src/features/issues/utils/template.test.ts src/features/issues/utils/workbench-view.test.ts`
- 未通过：`set -a; source .env.worktree; set +a; pnpm typecheck`，失败点在既有 `src/features/time-tracking/components/PomodoroTimer.test.tsx` 第 117、136 行，`"rain"` 不符合当前 `"none"` 类型约束，非本 P0 改动引入。
- 未执行：E2E archive/triage 主路径。当前已由 Go handler 测试和前端 mutation/unit 测试覆盖核心闭环。

## 回写要求

每个任务完成后更新：

- `docs/superpowers/specs/linear-alignment/module-overview.md`
- `docs/superpowers/specs/linear-alignment/00-p0-archive-triage/spec.md`
- `docs/superpowers/specs/linear-alignment/00-p0-archive-triage/design.md`
- `docs/superpowers/specs/linear-alignment/00-p0-archive-triage/tasks.md`

如果实现中发现设计边界变化，先更新 `design.md`，再继续编码。
