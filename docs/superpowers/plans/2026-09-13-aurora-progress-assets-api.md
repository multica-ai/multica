# Aurora 进度与作品库 API — 实现计划（Plan 3.5）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐四份计划缺失的 API 面：generation 更新/asset 写入的 sqlc 查询与索引、`GET /generations`（列表+详情，详情 status 派生自任务行）、asset 列表/下载/删除——为 Plan 4 的作品库与进度轮询提供数据源。

**Architecture:** 查询写入 `server/pkg/db/queries/aurora.sql`（Plan 1 Task 1 同文件扩充）；端点加在 Plan 1/2 的 `/api/aurora/*` workspace 分组；generation 行的 `status` 列只在终态写回，**中间态由详情端点从 `agent_task_queue` 联查派生**（避免新增任务状态同步管道）。

**Tech Stack:** Go 1.26、sqlc、pgx/v5、`server/internal/testutil`。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§5 数据模型、§8 前端、§9.1 #4/#6）。

**依赖：** Plan 1（表 + handler 基建）、Plan 2（`h.Credit`）、Plan 3 Task 2/4 依赖本计划 Task 1 的查询（`UpdateAuroraGeneration*`/`GetAuroraGenerationByTaskID`/`CreateAuroraAsset`）。实现顺序：本计划 Task 1 → Plan 3 → 本计划 Task 2/3 → Plan 4。

## Global Constraints

- 不加外键/级联；asset 删除 = 应用代码删 storage 对象 + 删行（一个事务）。
- 新建索引必须 `CREATE INDEX CONCURRENTLY` 单文件 + **只注册 `concurrentIndexCleanups`（up 映射）**（`TestEveryConcurrentUpBuildHasCleanup` 强制）：`concurrentDownIndexCleanups` 只收 down 方向重建索引的 migration（`main.go:303-311`），本计划的 down 只有 `DROP INDEX CONCURRENTLY`，注册进 down 映射会被 `TestConcurrentIndexCleanupsMatchTheirMigrations` 判挂。up 注册后 pre-hook 自动派生。
- migration 序号以合并时 `server/migrations` 最新为准顺延（本计划假设 456 起；若与 Plan safety 的 459 冲突则按合并顺序重排）。
- 所有响应走 `parseWithFallback` 契约（前端侧 Plan 4 schema 已对齐本文档字段名）。
- 端点全部挂在既有 auth + workspace 成员分组内（无匿名路径）。

---

### Task 1: sqlc 查询 + 索引迁移

**Files:**
- Modify: `server/pkg/db/queries/aurora.sql`
- Create: `server/migrations/456_aurora_generation_workspace_idx.up.sql` / `.down.sql`
- Create: `server/migrations/457_aurora_asset_generation_idx.up.sql` / `.down.sql`
- Create: `server/migrations/458_aurora_asset_workspace_idx.up.sql` / `.down.sql`
- Modify: `server/cmd/migrate/main.go`（三个索引各注册 cleanup 映射）
- 自动生成：`make sqlc`

**Interfaces:**
- Produces（Plan 3 Task 2/4、本计划 Task 2/3 依赖）：
  - `UpdateAuroraGenerationTask`（`:one`：`SET task_id=$2, credits_reserved=$3, updated_at=now() WHERE id=$1 AND workspace_id=$4`）
  - `UpdateAuroraGenerationTerminal`（`:one`：`SET status=$2, error=$3, credits_charged=$4, updated_at=now() WHERE id=$1 AND workspace_id=$5`；结算幂等靠调用侧先查终态）
  - `GetAuroraGenerationByTaskID`（`:one`：`WHERE task_id=$1`，Plan 3 Task 4 回写用）
  - `ListAuroraGenerations`（`:many`：`WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`）
  - `CreateAuroraAsset`（`:one` INSERT：generation_id/workspace_id/kind/media_url/format）
  - `ListAuroraAssets`（`:many`：按 `generation_id` 或 `workspace_id` 过滤）
  - `GetAuroraAsset`（`:one`：`WHERE id=$1 AND workspace_id=$2`）
  - `DeleteAuroraAsset`（`:exec`：`WHERE id=$1 AND workspace_id=$2`，返回行数判断存在性）
- 索引：`aurora_generation(workspace_id, created_at DESC)`、`aurora_asset(generation_id)`、`aurora_asset(workspace_id, created_at DESC)`——各单文件 CONCURRENTLY。

- [ ] **Step 1: 写查询 + 迁移 + 注册 cleanup 映射**（模式同 Plan 2 Task 1 Step 1/2——up 映射 only）
- [ ] **Step 2: `make sqlc` + `make test` 验证**（`go test ./internal/migrations/` 不应用迁移——用 `make test`）
- [ ] **Step 3: Commit**

```bash
git add server/pkg/db/queries/aurora.sql server/pkg/db/generated/ server/migrations/456_* server/migrations/457_* server/migrations/458_* server/cmd/migrate/main.go
git commit -m "feat(aurora): generation update, asset queries and indexes"
```

---

### Task 2: `GET /api/aurora/generations` + `GET /api/aurora/generations/{id}`

**Files:**
- Modify: `server/internal/handler/aurora.go`
- Modify: `server/internal/handler/aurora_test.go`
- Modify: `server/cmd/server/router.go`

**Interfaces:**
- Produces：
  - `GET /api/aurora/generations?limit=50&offset=0` → `200 {"generations":[{id,skillId,prompt,status,creditsReserved,creditsCharged,error,createdAt}]}`
  - `GET /api/aurora/generations/{id}` → `200 {"generation":{...同上, assets:[{id,generationId,kind,mediaUrl,format,createdAt}]}}`
- **status 派生规则（中间态）**：generation 行终态才写回 `status`；详情端点若行状态为 `queued`，联查 `agent_task_queue.status` 派生：`queued`→`queued`；`dispatched`/`running`/`waiting_local_directory`/`deferred`→`running`；`completed`→`completed`；`failed`/`cancelled`→`failed`。派生只在读侧，不新增状态同步管道（终态写回由 Plan 3 Task 4 完成）。

- [ ] **Step 1: 写失败测试**（`dbfx.Generation`-级 fixture 或 `dbfx.Insert` 造行：列表按 created_at 倒序 + 分页边界；详情含 assets；详情对 running 任务派生 `running`）
- [ ] **Step 2: 实现 handler + 路由**（`/api/aurora/generations` 与 `/api/aurora/generations/{id}`，后者 path 参数经 `parseUUIDOrBadRequest`）
- [ ] **Step 3: 跑测试 + Commit**

```bash
cd server && go test ./internal/handler/ -run TestListAuroraGenerations
git add server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/cmd/server/router.go
git commit -m "feat(aurora): list and detail generation endpoints"
```

---

### Task 3: asset 列表 / 下载 / 删除

**Files:**
- Modify: `server/internal/handler/aurora.go`
- Modify: `server/internal/handler/aurora_test.go`
- Modify: `server/cmd/server/router.go`

**Interfaces:**
- Produces：
  - `GET /api/aurora/assets?generationId=...&limit=50` → `200 {"assets":[{id,generationId,kind,mediaUrl,format,createdAt}]}`
  - `GET /api/aurora/assets/{id}/download` → `302` 到签名 URL（**复用 Attachment 的下载/签名模式**，实现时读现有 attachment 下载 handler 对齐；storage 用 `Handler.Storage` 接口）
  - `DELETE /api/aurora/assets/{id}` → `204`；应用代码先删 storage 对象再删行（无级联，一个事务或先删对象后删行的明确顺序，失败补偿）
- 权限：全部按 workspace 过滤（`workspace_id` 参数化）；删除后对应 generation 的 assets 查询不再返回该行。

- [ ] **Step 1: 写失败测试**（列表过滤 generationId；下载对不存在/跨 workspace 的行 404；删除幂等或 404、行消失）
- [ ] **Step 2: 实现 + 跑测试 + Commit**

```bash
cd server && go test ./internal/handler/ -run TestAuroraAsset
git add server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/cmd/server/router.go
git commit -m "feat(aurora): asset list, download and delete endpoints"
```

---

### Task 4: 实时化设计注记（不实现）

MVP 用轮询（Plan 4 `useAuroraGenerationDetail` refetchInterval 3s）。二期接 WebSocket 的扩展点（2026-09-13 已核实）：

- 客户端事件帧已存在：`task:progress`（`packages/core/types/events.ts:30`），通用帧 `{type, payload}`；订阅经 `useWSEvent`（`packages/core/realtime/hooks.ts`），缓存失效集中在 `packages/core/realtime/use-realtime-sync.ts`。
- 服务端扩展点：(a) `server/pkg/protocol/events.go` 加事件常量；(b) `events.Bus` 生产者（入队/状态变更处发布）；(c) `server/cmd/server/listeners.go` `registerListeners` 注册 workspace 广播（`realtime.Broadcaster.BroadcastToWorkspace`，注意 `internalOnlyPayloadKeys` 不得含新字段）；(d) 前端 union + payload map + handler。
- 本计划的轮询契约不堵死实时化：详情端点按快照返回，事件化后只是把「3s 拉」换成「推送触发同一次 fetch」。

---

## Deferred

- **分页游标**：MVP 用 limit/offset；数据量大后换 created_at 游标。
- **generation 取消端点**：MVP 不提供（`agent_task_queue` 有 cancelled 终态，取消端点二期）。
- **实时进度**：见 Task 4 注记（二期）。

## Self-Review

- **Spec 覆盖**：§9.1 #4 进度（列表+详情+派生 status）→ Task 2；#6 作品库（asset 列表/下载/删除）→ Task 3；§5 数据模型写入路径 → Task 1；Plan 3 Task 2/4 依赖的查询 → Task 1。
- **与 Plan 3 的分工**：查询/索引/读端点在本计划；入队/预留/回写/结算在 Plan 3；本计划不含任何执行逻辑。
- **幂等与终态**：终态写回由 Plan 3 Task 4 负责且只写一次（先查终态）；本计划只提供读侧派生。

## 执行交接

完成顺序：本计划 Task 1 → Plan 3 → 本计划 Task 2/3 → Plan 4 → Plan safety。实现后 `make test` + `pnpm typecheck` 全绿再进 Plan 4。
