# P0 Archive + Inbox Triage Design

## 目标

补齐当前个人与小团队阶段最高收益的两个闭环：

1. Issue Archive 生命周期闭环：旧 issue 可以安全退出默认工作视图，并可恢复。
2. Inbox Triage 手动分拣闭环：新通知和待处理输入可以被处理、忽略或延后。

## 非目标

- 不实现 Cycle/Iteration。
- 不实现 Roadmap。
- 不实现 Insights/Analytics。
- 不实现 Automation 规则引擎。
- 不实现 Enterprise 能力。
- 不把 Inbox Triage 和 Issue Archive 合并为同一概念。

## 当前架构基线

- Issue 后端逻辑集中在 `server/internal/handler/issue.go` 与 `server/pkg/db/queries/issue.sql`。
- Issue 前端类型在 `apps/workspace/src/shared/types/issue.ts`。
- Issue 路由由 `server/cmd/server/router.go` 暴露。
- Inbox 后端逻辑集中在 `server/internal/handler/inbox.go` 与 `server/pkg/db/queries/inbox.sql`。
- Inbox 前端逻辑集中在 `apps/workspace/src/features/inbox/`。
- Realtime event 类型由 `server/pkg/protocol` 和 `apps/workspace/src/shared/types/events.ts` 共同承载。

### 代码证据

- `server/pkg/db/queries/issue.sql` `DeleteIssue`：当前物理删除 issue。
- `server/internal/handler/issue.go` `DeleteIssue`：当前删除会清理 S3 附件并发布 deleted 事件。
- `server/pkg/db/queries/inbox.sql` `ListInboxItems`：当前默认只排除 `archived = true`。
- `server/internal/handler/inbox.go` `ArchiveInboxItem`：当前单条 archive 会清理同 issue sibling inbox items。
- `apps/workspace/src/features/inbox/components/inbox-page.tsx` `handleSelect`：打开 item 自动 mark read。

## 缺口定义

1. Issue 缺少可恢复 Archive 生命周期。
2. Issue 默认列表缺少 archived 过滤和 archived-only 视图。
3. Inbox 缺少 handled/dismissed/snooze 的 triage 语义。
4. Inbox read 与处理完成混淆。
5. Inbox archive 命名容易与 Issue archive 混淆。

## 方案与权衡

### 方案 A：继续使用 Delete 和 Inbox Archive

- 做法：把 issue delete 改成软删除，Inbox 继续使用 archive 清理。
- 优点：改动少。
- 风险：删除语义与归档语义混淆，Inbox archive 与 Issue archive 命名冲突。

### 方案 B：Issue Archive + Inbox Done/Dismiss/Snooze（推荐）

- 做法：Issue 新增 Archive/Restore API；Inbox 新增 triage_status 与 Done/Dismiss/Snooze 主动作。
- 优点：两个闭环边界清楚，可恢复生命周期和输入流处理语义分离。
- 风险：需要新增字段、接口、事件与前端动作。

### 方案 C：先做完整 Linear Cycle/Triage/Automation

- 做法：一次性补齐 Cycle、Triage、Automation 等能力。
- 优点：长期功能更完整。
- 风险：当前阶段过早引入团队计划模型和规则引擎，复杂度高。

## 推荐方案

选择方案 B。

Archive 管任务出口，Triage 管输入入口。二者都适合个人与小团队阶段，不需要提前引入 Cycle 或规则引擎。

## 数据模型或状态模型

### Issue

新增字段：

- `archived_at TIMESTAMPTZ NULL`
- `archived_by UUID NULL REFERENCES "user"(id)`

约束：

- 不新增 `status = archived`。
- Archive 是生命周期状态，与 issue workflow status 正交。
- Archive 时只更新 issue 本体，不级联软删除关联 entity。

### InboxItem

新增字段：

- `triage_status TEXT NOT NULL DEFAULT 'pending'`
- `snoozed_until TIMESTAMPTZ NULL`
- `handled_at TIMESTAMPTZ NULL`
- `dismissed_at TIMESTAMPTZ NULL`
- `triaged_by UUID NULL REFERENCES "user"(id)`

状态定义：

- `pending`：待处理。
- `handled`：已处理，从默认队列移出。
- `dismissed`：不需要处理，从默认队列移出。
- `snoozed`：延后处理，未到期不展示，到期后重新进入默认队列。

兼容约束：

- 保留现有 `read` 字段，继续表示已读。
- 保留现有 `archived` 字段，但新 UI 主路径不再用 Archive 表达 triage。

## 接口契约

### Issue Archive API

- `POST /api/issues/{id}/archive`
  - 设置 `archived_at` / `archived_by`。
  - 取消未完成 agent task。
  - 不删除附件、评论、标签、依赖、订阅、工时。
  - 自动 dismiss 或 archive 该 issue 关联的 pending inbox items。
- `POST /api/issues/{id}/restore`
  - 清空 `archived_at` / `archived_by`。
  - 不恢复旧 inbox items。
- `POST /api/issues/batch-archive`
  - 对多个 issue 执行与单个 archive 一致的语义。

### Issue 查询 API

- 默认 issue list 排除 `archived_at IS NOT NULL`。
- 支持 archived-only 查询。
- 支持 `include_archived=true` 的搜索或列表场景。

### Inbox Triage API

- `POST /api/inbox/{id}/handle`
- `POST /api/inbox/{id}/dismiss`
- `POST /api/inbox/{id}/snooze`
- `POST /api/inbox/batch-handle`
- `POST /api/inbox/batch-dismiss`
- `POST /api/inbox/batch-snooze`

Snooze request 至少包含：

- `snoozed_until`：RFC3339 时间。

默认 Inbox 查询展示：

- `triage_status = 'pending'`
- 或 `triage_status = 'snoozed' AND snoozed_until <= now()`
- 同时要求 `archived = false`

## UI 或交互流程

### Issue Archive

1. 用户在 issue detail 或列表中选择 Archive。
2. 系统显示确认。
3. Archive 成功后 issue 从默认列表消失。
4. 用户进入 Archived Issues 视图。
5. 用户可以打开 archived issue detail。
6. Archived detail 显示归档状态和 Restore 主操作。
7. Restore 后 issue 回到普通列表。

### Inbox Triage

1. 用户进入 Inbox。
2. 默认只看到 pending 和到期 snoozed items。
3. 点击 item 打开详情并 mark read。
4. 用户显式选择 Done、Dismiss 或 Snooze。
5. Done/Dismiss 后 item 从默认队列移出。
6. Snooze 后 item 在到期前隐藏，到期后重新展示。

## 权限、边界条件、异常路径

- 所有操作必须校验 workspace membership。
- archive/restore issue 必须拒绝跨 workspace issue。
- triage inbox item 必须只允许 recipient 本人操作。
- `snoozed_until` 必须是有效未来时间。
- 对已 archived issue 再 archive 应保持幂等。
- 对未 archived issue restore 应保持幂等或返回明确错误。
- 对 handled/dismissed item 再 snooze 应返回 400，避免状态倒退。

## 实现约束

- 执行阶段不能引入 Cycle/Iteration。
- 执行阶段不能引入 Automation 规则引擎。
- 不允许把 inbox dismiss 理解为 issue archive。
- 不允许把 issue archive 实现为物理删除。
- 不允许删除 issue 关联附件或历史记录。
- 优先复用现有 handler/query/store/router 结构。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| Issue Archive 和 Inbox Archive 命名冲突 | 用户和代码语义混淆 | Issue 使用 Archive/Restore；Inbox UI 使用 Done/Dismiss/Snooze |
| 直接改 DELETE 语义 | 旧删除逻辑和测试误导实现 | 新增 archive/restore API，物理删除重命名为 hard delete |
| 归档 issue 后 inbox item 残留 | Inbox 继续提示已退出活跃区的 issue | Archive issue 时自动 dismiss 或 archive 关联 pending inbox items |
| read 与 handled 混淆 | 用户打开但未处理的 item 被误认为完成 | 打开只 mark read，Done 才 handled |
| Snooze 需要后台任务 | 增加实现复杂度 | 查询时根据 `snoozed_until <= now()` 自然出现 |

## 验收检查

1. 用户可以归档 issue。
2. 归档 issue 不出现在默认 issue 列表、Backlog、Today、Upcoming、Board、Project detail。
3. 用户可以在 Archived Issues 视图找到归档 issue。
4. 用户可以 restore 归档 issue。
5. 归档不会删除 comment、attachment、S3 object、label relation、dependency、subscriber、time entry。
6. 归档会取消未完成 agent task。
7. 用户点击 inbox item 只 mark read，不会自动 handled。
8. 用户可以 Done inbox item，item 从默认队列移出。
9. 用户可以 Dismiss inbox item，item 从默认队列移出。
10. 用户可以 Snooze inbox item，未到期不显示，到期后重新显示。
11. 批量 Done/Dismiss/Snooze 可用。
12. 相关文档与模块总览状态已同步回写。
