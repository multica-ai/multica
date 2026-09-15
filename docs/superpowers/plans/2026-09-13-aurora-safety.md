# Aurora 安全：内容审核 + 权益门禁 — 实现计划（Plan safety）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 spec §10 剩余两项硬性条件——**内容审核**（prompt + 生成物，中英双语）与**权益门禁**（`GateAurora*`）。频率闸门已由 Plan 3 Task 7 提前落地（创建端点 per-user 限流）；沙箱隔离与工具面收窄在 Plan 3 Task 6。

**Architecture:** 审核走**可替换 adapter**：`server/internal/aurora/moderation.go` 定义 `Moderator` 接口，MVP 默认实现 = 中英双语违禁词 + 图片 NSFW 本地检测；供应商审核 API 二阶段替换 adapter 实现。审核记录落 `moderation_log` 表（审计 + 复核）。权益门禁留 Deferred（self-host 下 `entitlement` fail-open 已核实，无云端时全放行，不影响 MVP）。

**Tech Stack:** Go 1.26、sqlc、pgx/v5、`server/internal/testutil`。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§10 安全与合规、§6.3 权益门禁）。

**依赖：** Plan 1（创建端点）、Plan 3 Task 4（回写路径）。审核接入点：`CreateAuroraGeneration`（prompt 前置审核）与 Plan 3 Task 4 的完成回写（生成物后置审核）。

## Global Constraints

- 不加外键/级联；`moderation_log` 与 generation 的关联为应用级 id。
- migration 序号以合并时 `server/migrations` 最新为准顺延（本计划假设 459 起；Plan 3.5 用 456-458，若合并顺序不同按实际重排）。索引用 CONCURRENTLY 单文件 + 注册 `cmd/migrate/main.go` cleanup 映射（同 Plan 2）。
- 审核失败**不可静默放行**：adapter 出错时按 fail-closed 处理（拒绝并记录），不得因审核器故障放行内容（对齐 spec §10 红线）。
- 供应商密钥仅服务端读取；MVP 默认实现无需任何外部密钥。

---

### Task 1: `Moderator` 接口 + 违禁词/NSFW 默认实现

**Files:**
- Create: `server/internal/aurora/moderation.go`
- Create: `server/internal/aurora/moderation_test.go`

**Interfaces:**
- Produces：
  - `type Decision struct { Allowed bool; Reason string }`
  - `type Moderator interface { ScreenPrompt(ctx context.Context, text string) (Decision, error); ScreenAsset(ctx context.Context, mediaURL, kind string) (Decision, error) }`
  - `func NewDefaultModerator() Moderator` —— 违禁词表（中英双语，仓库内常量，加载自 `server/internal/aurora/blocked_terms.json`）+ 图片 NSFW 本地检测（MVP 用本地模型推理，如 opennsfw2；实现时确认 Go 侧可用的本地方案，不引入外部服务）；`kind` 为 video 时 MVP 只做文件名/URL 与元数据校验（帧级审核二期）。

- [ ] **Step 1: 写测试**（违禁词中英命中/边界/大小写；`Decision.Allowed=false` 时 Reason 非空；未知异常 → fail-closed；NSFW 检测用固定样本图——测试夹具放 `server/internal/aurora/testdata/`）
- [ ] **Step 2: 实现 + 跑测试 + Commit**

```bash
cd server && go test ./internal/aurora/ -run TestModerator
git add server/internal/aurora/moderation.go server/internal/aurora/moderation_test.go server/internal/aurora/blocked_terms.json server/internal/aurora/testdata/
git commit -m "feat(aurora): moderation adapter with keyword and NSFW defaults"
```

---

### Task 2: 审核接入 + 记录表

**Files:**
- Create: `server/migrations/459_aurora_moderation_log.up.sql` / `.down.sql`（表：`id uuid PK`、`generation_id uuid`、`workspace_id uuid`、`scope text`（`prompt|asset`）、`verdict text`（`allowed|blocked`）、`reason text`、`created_at timestamptz`；索引 `(workspace_id, created_at DESC)` CONCURRENTLY 单文件 + 注册 cleanup）
- Create: `server/pkg/db/queries/aurora_moderation.sql`
- Modify: `server/internal/handler/handler.go`（`Handler` 加 `Moderation aurora.Moderator`）
- Modify: `server/internal/handler/aurora.go`（`CreateAuroraGeneration` 前置审核）
- Modify: `server/internal/service/aurora_completion.go`（Plan 3 Task 4 回写路径后置审核）

**接入语义：**
- **prompt 前置**：`CreateAuroraGeneration` 在插行前 `ScreenPrompt` → `Allowed=false` → `422` + 记录 `moderation_log`（generation 不落行，`generation_id` 记 NULL 或记请求 hash——实现时择一并在注释说明）；adapter 报错 → `500`（fail-closed）。
- **asset 后置**：Plan 3 Task 4 回写前对每个产物 `ScreenAsset` → 未过 → 任务判 `failed`（`error='moderation blocked'`）+ `Refund` + asset 不落库 + 记 `moderation_log`。

- [ ] **Step 1: 写失败测试**（违禁 prompt → 422 + log 落行；正常 prompt → 201；后置：blocked 产物 → generation failed + 退款 + 无 asset 行）
- [ ] **Step 2: 实现 + 跑测试 + Commit**

```bash
cd server && go test ./internal/handler/ -run TestAuroraModeration
git add server/migrations/459_* server/pkg/db/queries/aurora_moderation.sql server/pkg/db/generated/ server/cmd/migrate/main.go server/internal/handler/handler.go server/internal/handler/aurora.go server/internal/service/aurora_completion.go
git commit -m "feat(aurora): wire moderation into generation create and completion"
```

---

## Deferred

- **供应商审核 API**：adapter 接口已就位，二阶段替换默认实现（火山/阿里内容安全等，中英双语 + 社媒合规规则），密钥仅服务端。
- **视频帧级审核**：MVP 只做元数据校验；帧级抽帧审核二期。
- **权益门禁 `GateAurora*`（cloud）**：self-host 下 `entitlement` 未配 BaseURL 即 fail-open（`client.go:48-50/95-100`，Provider 为 nil → `ActionOff`），MVP 无需云端即可运行。**本地 enforcement 已由 Plan 5 Task 6 落地**（`aurora.LimitsForUser`：月次数/并发，值来自 `TierCatalog`）；接云端时需同一 PR 改 `entitlement/types.go` 的 `GateName` 枚举 **+** `normalizePolicy`（`client.go:278-299` 硬性要求两个 gate 都在，加第三个 gate 会让旧 policy 整体被拒）**+** 消费点（`GateAuroraGenerations` 创建端点、`GateAuroraConcurrency` 并发上限）——见 Plan 5 Deferred。
- **审核复核 UI**：`moderation_log` 记录已落库，人工复核界面与申诉流程二期。

## Self-Review

- **Spec 覆盖**：§10.4 内容审核 → Task 1/2（adapter + 前置/后置接入 + 日志）；§6.3 权益门禁 → Deferred（fail-open 已核实，附 normalizePolicy 双 gate 硬校验警示）；§10.3 频率闸门 → 已在 Plan 3 Task 7。
- **fail-closed**：adapter 报错拒绝而非放行，与 spec §10 红线一致。
- **可替换性**：供应商审核只换 adapter 实现，接入点（handler/回写）不变。

## 执行交接

完成顺序：Plan 3.5 → Plan 4 → 本计划（可在 Plan 4 之后、上线前完成；Task 1 可随时先行）→ Plan 5（订阅 + Stripe + 权益门禁 enforcement，`2026-09-13-aurora-subscriptions-payments.md`，已编写）。
