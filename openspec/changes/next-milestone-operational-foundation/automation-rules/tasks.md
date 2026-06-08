# Automation Rules Tasks

## 实现目标

交付最小 automation rule model，替代 template-only enablement，使 workspace 可以创建、编辑、启停、dry-run 和查看执行记录。

## 前置依赖

- 本 design 包范围已确认。
- `structured-execution-foundation` 中 issue graph 方向保持不变。
- 执行前需决定旧 `automation_rule` 是迁移还是替换。

## 任务切片

### Task 1：规则 schema 与类型

- 目标：新增 rule definition 与 run log 数据模型。
- 目标文件：
  - `server/migrations/`
  - `server/pkg/db/queries/automation.sql`
  - `server/internal/automation/`
- 完成定义：
  - DB 能保存 rule name、trigger、conditions、actions、enabled、source。
  - DB 能保存 rule run log。
  - Go 类型能校验白名单条件和动作。
- 验证方式：
  - `make sqlc`
  - `cd server && go test ./internal/automation/...`

### Task 2：后端 CRUD 与 dry-run

- 目标：提供 rule CRUD 和 dry-run API。
- 目标文件：
  - `server/internal/handler/automation.go`
  - `server/cmd/server/router.go`
- 完成定义：
  - 支持 list/create/detail/update/delete。
  - 支持 dry-run 返回 action summary。
  - workspace membership 和 admin 权限校验明确。
- 验证方式：
  - handler tests 覆盖权限、校验、dry-run。

### Task 3：事件触发与执行日志

- 目标：把 issue event 接入 rule evaluator。
- 目标文件：
  - `server/internal/events/`
  - `server/internal/service/`
  - `server/internal/automation/`
- 完成定义：
  - issue status change 可触发规则。
  - action 成功和失败都写 run log。
  - 失败不阻断原始 issue mutation。
- 验证方式：
  - Go tests 覆盖 matched、skipped、failed 三种 run log。

### Task 4：前端 Automation UI

- 目标：把 template card 替换为 rule list、rule editor 和 run log。
- 目标文件：
  - `apps/workspace/src/features/automation/`
  - `apps/workspace/src/shared/api/`
  - `apps/workspace/src/shared/types/`
- 完成定义：
  - 用户能创建、编辑、启停 rule。
  - 用户能 dry-run 并查看 run log。
  - preset 作为创建入口，不是独立 enable switch。
- 验证方式：
  - Vitest 覆盖 rule editor 和 dry-run 状态。

### Task 5：回归与回写

- 目标：验证 automation 不破坏现有 settings 和 issue flow。
- 目标文件：
  - `e2e/settings.spec.ts`
  - 本 OpenSpec 目录文档
- 完成定义：
  - 自动化核心路径有 E2E 覆盖。
  - `spec.md` 当前状态回写为已完成或部分完成。
- 验证方式：
  - `pnpm typecheck`
  - `pnpm test`
  - `make test`
  - 相关 Playwright test

## 回写要求

实现中如果扩大 trigger/action 白名单，必须先更新 `design.md` 的接口契约和风险边界。
