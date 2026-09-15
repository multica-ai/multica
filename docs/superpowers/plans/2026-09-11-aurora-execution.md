# Aurora 执行层（self-host fleet + agent + HyperFrames）— 实现计划（Plan 3）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> 修订：2026-09-13（评审回写，详见文末「修订记录」）

**Goal:** 让一次 Aurora generation 真正跑起来：入队 → 托管 agent 执行 → 产物回写 asset、结算积分。含 self-host runtime fleet、沙箱节点镜像（infra）与创建端点频率闸门。

**Architecture:** 复用 Multica 的 `AgentTaskQueue` + `server/pkg/agent`（coding CLI）+ `server/internal/daemon`（执行器），把 daemon 部署到沙箱化的服务端节点（managed 注册 + 未绑定 runtime claim）；16 个 Skill = 16 个 workspace 级系统 Agent（`system_key`）；媒体生成经 MCP、视频经 HyperFrames。Go 侧任务：种子 agent、入队+预留、managed runtime、完成回写、频率闸门；infra 侧两块：fleet controller、沙箱节点镜像。

**Tech Stack:** Go 1.26、sqlc、pgx、`server/pkg/agent`、`server/internal/daemon`、`server/internal/service`、Docker/gVisor/Firecracker、HyperFrames CLI。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§4 执行层）。

**依赖：** Plan 1（`aurora_generation` 表 + `CreateAuroraGeneration`）、Plan 2（`h.Credit`）。Task 2/4 需要的 `UpdateAuroraGeneration`/`GetAuroraGenerationByTaskID`/`CreateAuroraAsset`/列表查询由 **Plan 3.5 Task 1** 定义——实现顺序上 Plan 3.5 Task 1 先于本计划 Task 2/4 落地。

## Global Constraints

- 不加外键/级联。
- 服务端跑 coding CLI = 我们服务器执行用户 prompt 的任意代码：**容器/VM 级隔离 + 出网白名单 + 工具面收窄 + per-task 调用上限**是不可妥协的硬门槛（spec §10）。现状核实：CLI 启动硬编码 `--permission-mode bypassPermissions`（`pkg/agent/claude.go:720`）/`--dangerously-skip-permissions`（`antigravity.go:457`），`MaxTurns` 存在但 daemon 从不设置（`agent.go:39`），permission-presets 与 guard **不存在**——工具面收窄是**待建能力**（Task 6），不是配置。
- 供应商密钥只在服务端（fleet/MCP 侧）读取，绝不暴露前端。
- 任务终态从 durable 结果归约，不解析模型 prose（产物回传走结构化 out-of-band 上报，Task 4）。
- 默认测试不得解析或执行用户安装的真实 agent CLI（用 fake 可执行路径；新增默认 agent 命令加进 `scripts/agent-cli-command-names.txt`）。

---

### Task 1: 种子 16 个 workspace 级系统 Agent（+ 挂 Skill + managed runtime 行）

**Files:**
- Create: `server/internal/aurora/agents.go`
- Create: `server/internal/aurora/agents_test.go`
- Create: `server/pkg/db/queries/aurora_agents.sql`（种子查询，`make sqlc` 生成）

**Interfaces:**
- Consumes: `Agent` / `Skill` / `AgentSkill` / `AgentRuntime` 表（已存在）。
- Produces：
  - `aurora.SystemAgentDef{ SkillID, SystemKey, Name, Instructions string }` 与 `aurora.SystemAgents() []SystemAgentDef`（16 条，`SystemKey = "aurora:"+skillID`，与 `aurora.Catalog()` 一一对应）。
  - `aurora.EnsureSystemAgents(ctx, q *db.Queries, workspaceID, ownerID pgtype.UUID) error` —— 幂等：确保一个 managed runtime 行 + 16 个 `kind='system'` 的 `Agent` 行 + 16 个同名 `Skill` 行 + `agent_skill` 关联。

- [ ] **Step 1: 写测试**（断言 16 条、SystemKey 唯一、与 `aurora.Catalog()` 的 16 个 id 一一对应）

```go
func TestSystemAgentsMatchCatalog(t *testing.T) {
	defs := aurora.SystemAgents()
	cat := aurora.Catalog()
	if len(defs) != len(cat) {
		t.Fatalf("system agents %d != catalog %d", len(defs), len(cat))
	}
	keys := map[string]bool{}
	for _, d := range defs {
		if d.SystemKey != "aurora:"+d.SkillID {
			t.Fatalf("system key %q != aurora:%s", d.SystemKey, d.SkillID)
		}
		keys[d.SkillID] = true
	}
	for _, c := range cat {
		if !keys[c.ID] {
			t.Fatalf("catalog skill %q has no system agent", c.ID)
		}
	}
}
```

DB 测试（`package aurora_test`，raw pgxpool 模式同 Plan 2）：`EnsureSystemAgents` 后断言——1 个 managed runtime 行（`daemon_id NULL`、`runtime_mode='cloud'`）、16 个 `kind='system'` agent 行、16 个 skill 行、16 条 `agent_skill`；**再调一次幂等不增行**。

- [ ] **Step 2: 实现**

关键事实（2026-09-13 核实）：

- `agent.system_key` **无列级唯一约束**；现有唯一索引是 partial composite：`agent_system_identity_unique ON (workspace_id, owner_id, runtime_id, system_key) WHERE system_key IS NOT NULL`（`172_agent_system_identity_index.up.sql:3-5`）。**`ON CONFLICT (system_key)` 不合法**，upsert 必须写全四元组 + 匹配谓词：

```sql
INSERT INTO agent (workspace_id, owner_id, runtime_id, kind, system_key, name, instructions, runtime_mode, visibility, permission_mode, runtime_config)
VALUES ($1, $2, $3, 'system', $4, $5, $6, 'cloud', 'workspace', 'private', '{}'::jsonb)
ON CONFLICT (workspace_id, owner_id, runtime_id, system_key) WHERE system_key IS NOT NULL
DO UPDATE SET name = EXCLUDED.name, instructions = EXCLUDED.instructions;
```

- `owner_id` 有 FK 引用 `user` 表 → **用 workspace owner 的 user id**（个人空间下 = 调用者本人），不造「系统 user 行」。
- managed runtime 行（`agent_runtime`，`daemon_id NULL` + `runtime_mode='cloud'`）是 claim 的前提：`ListQueuedClaimCandidatesByRuntime` 要求 `atq.runtime_id = $1 AND a.runtime_id = atq.runtime_id`，且无 `runtime_id` 的 agent 在 admission 被拒（`service/agent_ready.go:110-118`）。**实现时核对 `agent_runtime` 的唯一约束以定 ON CONFLICT 目标**（诚实标注）。
- `skill` 有 `UNIQUE(workspace_id, name)`（`008_structured_skills.up.sql`）；`agent_skill` PK `(agent_id, skill_id)`。Skill 的 `content` MVP 先放最小 SKILL.md 工作流文本（后续 Plan 迭代充实）。
- 调用点：Task 2 的 `CreateAuroraGeneration` 内**懒执行**（幂等，一次 generation 创建只会 seed 一次；覆盖存量 workspace）。

- [ ] **Step 3: 跑测试 + commit**

```bash
cd server && go test ./internal/aurora/ -run 'TestSystemAgents|TestEnsureSystemAgents'
git add server/internal/aurora/agents.go server/internal/aurora/agents_test.go server/pkg/db/queries/aurora_agents.sql server/pkg/db/generated/
git commit -m "feat(aurora): seed 16 workspace-scoped system agents"
```

---

### Task 2: 入队 + 预留积分（含失败补偿）

**Files:**
- Modify: `server/internal/handler/aurora.go`（`CreateAuroraGeneration` 扩展）
- Modify: `server/internal/handler/aurora_test.go`
- Consumes: Plan 3.5 的 `UpdateAuroraGeneration` 查询

**Interfaces:**
- Produces：`CreateAuroraGeneration` 改为：校验 skill（含 `available`，Plan 1）→ 懒 `EnsureSystemAgents` → 插 generation 行（`queued`，`credits_reserved=0`）→ `h.Credit.Reserve(entry.Credits × 1_000_000, reference=generation.ID)` → 余额不足：generation 置 `failed`（`error='insufficient credits'`）+ 返回 `402`；成功：`EnqueueQuickCreateTask` 入队 → 写回 `task_id`/`credits_reserved` → 入队失败：补偿 `Refund(reference=generation.ID)` + generation 置 `failed` → `201`。

- [ ] **Step 1: 写测试**

```go
func TestCreateAuroraGenerationReservesAndEnqueues(t *testing.T) {
	// 先给 testUserID 充 credits（h.Credit.Grant 或直接插 credit_balance）。
	// POST /api/aurora/generations，断言 201、creditsReserved > 0，
	// aurora_generation.task_id 非空（dbfx.QueryRow 查回）、余额已扣减。
}

func TestCreateAuroraGenerationRejectsInsufficientCredits(t *testing.T) {
	// 空余额 POST，断言 402、generation.status='failed'、无 task 入队（agent_task_queue 计数 0）。
}
```

- [ ] **Step 2: 实现 + 跑测试 + commit**

入队用 `EnqueueQuickCreateTask(ctx, workspaceID, requesterID, agentID, squadID pgtype.UUID, prompt, priority, dueDate string, projectID, parentIssueID pgtype.UUID, attachmentIDs []pgtype.UUID)`（`service/task.go:1605`，签名以实际为准）——会创建一条 `origin_type='quick_create'` 的 issue（Aurora 归属经 `aurora_generation.task_id` 反查，MVP 不加 origin 列；前端 issue 列表按 origin_type 过滤）。个人空间下系统 agent 的 `owner_id` = 调用者本人，quick-create 的权限/配额检查天然放行（实现时核对 `permission_mode='private'` 下的调用路径）。

```bash
cd server && go test ./internal/handler/ -run TestCreateAuroraGeneration
git add server/internal/handler/aurora.go server/internal/handler/aurora_test.go
git commit -m "feat(aurora): reserve credits and enqueue task on generation"
```

---

### Task 3: managed runtime 注册（沙箱 daemon 走服务端通道）

**Files:**
- Create: `server/internal/handler/aurora_runtime.go`（managed 注册端点）
- Create: `server/internal/handler/aurora_runtime_test.go`

**Interfaces:**
- Produces：`POST /api/daemon/managed/register`（服务端内部端点）——请求带 server-issued token（新配置 `AURORA_SANDBOX_TOKEN`，constant-time 比较，错误即 401），为沙箱 daemon 注册托管身份；响应返回其应认领的 managed runtime id（daemon 加入 claim 集）。

- [ ] **Step 1: 探明现状**（2026-09-13 已核实，实现时复核）

- `DaemonRegister` **硬编码 `runtime_mode='local'`**（`handler/daemon.go:501/558/692`），不存在 cloud 注册路径——沙箱 daemon 不伪装 cloud，走本任务新增的 managed 注册。
- claim 路径对未绑定机器的 runtime 有容忍：`rt.DaemonID` 为 NULL 的 runtime（cloud 行）可被任意 daemon claim（`handler/daemon.go:1745-1751`）——沙箱 daemon 把 managed runtime id 加入 claim 集即可认领系统 agent 的任务。
- **实现时核对**：daemon 端 claim 请求的 runtime id 集来源（`daemon/client.go:226 ClaimTask` / `:293 ClaimTasks`）——若 claim 集只含自注册 runtime，则在 managed 注册响应里下发 managed runtime id 供 daemon 显式加入；**备选方案 B**：系统 agent 的 `runtime_id` 直接指向沙箱 daemon 自注册的 local runtime 行（要求注册先于 seed；懒 re-bind）。

- [ ] **Step 2: 测试**：`Fixture.Runtime`（未绑定 cloud 行）+ `Fixture.Agent`（`runtime_mode='cloud'`、`runtime_id` 指向该行）+ `Fixture.Task` → 模拟 daemon claim 请求含该 runtime id → 任务被 claim（参照 `internal/service/runtime_claim_access_test.go` 模式）。managed 注册端点：错误 token → 401；正确 token → 注册成功且标记托管。

- [ ] **Step 3: 实现 + commit**

```bash
cd server && go test ./internal/handler/ -run TestManagedRuntime
git add server/internal/handler/aurora_runtime.go server/internal/handler/aurora_runtime_test.go
git commit -m "feat(aurora): support managed (server-hosted) runtime registration"
```

---

### Task 4: 完成回写 → asset + 结算/退款

**Files:**
- Create: `server/internal/service/aurora_completion.go`
- Modify: `server/internal/service/task.go`（`CompleteTask` :4311 / `FailTask` :4726 终态分支挂 Aurora 回写；sweeper 的 `FailStaleTasks`/`FailTasksForOfflineRuntimes` 全部归入 `HandleFailedTasks` → 失败路径单一收口）
- Modify: `server/internal/daemon/daemon.go`（终态后产物上报）
- Consumes: Plan 3.5 的 `GetAuroraGenerationByTaskID`/`UpdateAuroraGenerationStatus`/`CreateAuroraAsset`

**Interfaces:**
- Produces：任务终态时按 `task_id` 反查 generation：
  - 成功：沙箱 daemon 把 workdir 产物经 `/api/upload-file`（或对等上传端点）直传 storage，再经**新增 out-of-band RPC**（`ReportTaskUsage` 同款通道，`daemon/client.go:548` 先例）上报结构化 artifact 列表 `[{name, mediaURL, format}]` → server 写 `aurora_asset`（kind 按 skill output）→ generation `status='completed'`、`credits_charged = credits_reserved`（不二次扣费）。产物上传/上报失败 → 判 failed（产物是核心价值，不静默吞）。
  - 失败（含 sweeper 超时/离线归终态）：`status='failed'`、`error`=失败原因、`Refund(reference=generation.ID)` 全额退款。
  - **结算幂等**：仅当 generation 处于非终态（`queued`/`running`）时结算；重复终态事件 no-op。
- **关键事实**：`TaskResult` **没有 Attachments**（`daemon/types.go:287-310`：Comment/Usage 有，文件没有）——产物回传必须走本任务的 out-of-band 通道，不能依赖现有完成报文；也不解析模型 prose。

- [ ] **Step 1: 写测试**（构造一个 completed/failed 的 task + 其 generation，调回写 hook，断言 generation.status 变更 + asset 落行 + `credits_charged`/`Refund` 生效；对同一终态任务再调一次，断言 no-op）

- [ ] **Step 2: 实现 + 跑测试 + commit**

```bash
cd server && go test ./internal/service/ -run TestAuroraCompletion
git add server/internal/service/aurora_completion.go server/internal/service/task.go server/internal/daemon/daemon.go
git commit -m "feat(aurora): backfill assets and settle credits on completion"
```

---

### Task 5（infra）: self-host fleet controller

**Files:**
- Create: `server/internal/aurorafleet/`（controller 服务，独立部署单元）

**交付与验收：**
- 契约对齐 `server/internal/handler/cloud_runtime.go:41-97` 的实际路径面：`GET/POST/DELETE /api/v1/nodes`、`/nodes/{id}/start|stop|reboot|status|exec`（本地 `cloudruntime` client 的 `baseURL` 指向本 controller 即可复用调用模式；不存在 provision/terminate/gateway 命名）。
- 按需 provision/terminate 沙箱节点（Docker 容器，gVisor/Firecracker 二期）、健康检查、节点生命周期管理。
- 验收：一个空节点能被 provision，沙箱 daemon 以 managed 身份注册（Task 3）并 claim 一个任务（Task 3 测试路径打通）。
- 这是基础设施代码，用集成测试（非纯 TDD）；密钥/隔离在 Task 6 落实。

```bash
git commit -m "feat(aurora): self-host runtime fleet controller"
```

---

### Task 6（infra）: 沙箱节点镜像 + 工具面收窄（待建能力）

**Files:**
- Create: `deploy/aurora-sandbox/`（Dockerfile、entrypoint、镜像构建脚本）
- Modify: `server/pkg/agent/`（**additive** per-agent 启动配置，见下）
- Modify: `server/internal/daemon/`（aurora 任务设置 `MaxTurns`）

**交付与验收：**
- 镜像含：Multica daemon 二进制 + 至少一个 coding agent CLI（claude/codex）+ 媒体生成 MCP server（image/transcribe）+ HyperFrames CLI + Node 22 + FFmpeg + headless Chrome。
- 沙箱：容器/VM 级隔离、出网白名单、CPU/内存/时长上限；daemon 设 `MULTICA_AGENT_TIMEOUT`（daemon 级第一道超时闸，如 30min；server sweeper 2.5h 兜底）。
- **工具面收窄（新建能力，2026-09-13 核实现状）**：
  - `pkg/agent` additively 支持 per-agent 启动参数覆盖（默认**不改变**现有用户 agent 的 `bypassPermissions` 行为）；Aurora 系统 agent 显式窄配置（关闭 bypass、按 provider 支持面关 bash/fs-search 等危险工具）。
  - 为 aurora 任务设置 `MaxTurns`（`agent.Options.MaxTurns` 已存在但 daemon 从不设置；本次接上，MVP 建议 30）。per-task 调用上限 guard（按 task 分键）留二期（spec §10）。
  - 默认测试用 fake CLI 断言参数（不执行真实 agent；新增默认 agent 命令加进 `scripts/agent-cli-command-names.txt`）。
- 验收：一个 HyperFrames「文生视频」任务在沙箱内跑通并产出 MP4 asset（`MULTICA_RUN_REAL_AGENT_SMOKE=1` 下的集成冒烟，`-tags=agentintegration`）；fake-CLI 参数断言测试证明窄配置生效。

```bash
git commit -m "feat(aurora): sandbox node image with agent, MCP and HyperFrames"
```

---

### Task 7: 创建端点频率闸门

**Files:**
- Modify: `server/cmd/server/router.go`（`POST /api/aurora/generations` 挂限流）
- Create: `server/internal/handler/aurora_rate_limit_test.go`

**交付与验收：**
- per-user 限流中间件（复用 `authRL` 先例模式，`router.go:1388`），MVP 统一阈值（如 20 次/分钟，可配置）；超限 429。端点已在 auth + workspace 组内，**无匿名路径**（spec §10 的「匿名」表述修正为「新用户」，首日更严阈值二期）。
- TDD：连打阈值次数断言 429 + 恢复窗口。

```bash
cd server && go test ./internal/handler/ -run TestAuroraRateLimit
git add server/cmd/server/router.go server/internal/handler/aurora_rate_limit_test.go
git commit -m "feat(aurora): rate-limit generation creation"
```

---

## Deferred / 风险

- **水平扩展**：MVP 先单节点沙箱；多节点调度与 `@hyperframes/aws-lambda` 渲染路径作后续扩展（spec §12）。
- **生成式视频（Veo/Runway）与数字人（HeyGen Avatar）**：第二阶段，本计划只覆盖 HyperFrames 确定性视频（spec §9.2）。
- **确定性 Skill 快速通道**：纯确定性 skill（证件照/字幕/Excel）仍走 agent，后续可绕过 agent 直跑流水线（spec §4.5 技术债）。
- **权益门禁（`GateAurora*`）**：本地 enforcement 在 Plan 5 Task 6（`aurora.LimitsForUser`）；cloud `GateName`/`normalizePolicy` 扩展待接云时同 PR 处理（Plan safety Deferred）。
- **内容审核**：Plan safety（prompt 关键词 + 图片 NSFW adapter）。
- **per-skill TTL**：MVP 用 `MULTICA_AGENT_TIMEOUT`（daemon 级）+ sweeper 常数兜底；按 skill 配置二期。
- **origin 列**：MVP 不加（`aurora_generation.task_id` 反查归属）。

## Self-Review

- **Spec 覆盖**：§4.2 fleet 设计 → Task 5/6；managed 注册与未绑定 runtime claim → Task 3；§4.3 MCP + §4.4 HyperFrames → Task 6；任务入队/预留/补偿 → Task 2；完成回写 + 结算/退款 → Task 4；16 Skill=Agent → Task 1；频率闸门（§10）→ Task 7。
- **前提修正（2026-09-13）**：`system_key` 无单列唯一约束（partial composite 索引，`ON CONFLICT` 目标按四元组+谓词）；`DaemonRegister` 硬编码 local（managed 走新端点）；`TaskResult` 无 Attachments（产物走 out-of-band 上报）；`EnqueueQuickCreateTask` 复用（quick_create issue 行）。
- **诚实标注**：Task 2/3/4 的入队/claim/上报函数签名需读 `task.go`/`daemon.go` 核对（与 Plan 1 同类诚实标注，非占位）；`agent_runtime` 唯一约束待实现时确认。

## 执行交接

Plan 3 完成。后续顺序：Plan 3.5（进度与作品库 API）→ Plan 4（前端）→ Plan safety（频率闸门已在本计划 Task 7 提前落地，剩内容审核与权益门禁）→ Plan 5（订阅 + Stripe，`2026-09-13-aurora-subscriptions-payments.md`，已编写）。

## 修订记录（2026-09-13 评审回写）

| 处 | 修正 |
|----|------|
| Task 1 | `ON CONFLICT (system_key)` 不合法（唯一索引是 `(workspace_id, owner_id, runtime_id, system_key)` partial composite，`172_agent_system_identity_index.up.sql`）→ 改全四元组 + `WHERE system_key IS NOT NULL`；`owner_id` 用 workspace owner（agent.owner_id 有 FK 到 user）；种子范围扩为「managed runtime 行 + 16 agent + 16 skill + agent_skill」；调用点=懒执行于创建端点 |
| Task 2 | 入队改复用 `EnqueueQuickCreateTask`（`task.go:1605`，quick_create issue 行）；新增失败补偿（余额不足 → 402+failed；入队失败 → Refund+failed） |
| Task 3 | 前提修正：`DaemonRegister` 硬编码 `runtime_mode='local'`（`daemon.go:501/558/692`）→ 新增 managed 注册端点（server-issued token）；claim 经未绑定 runtime 容忍分支（`daemon.go:1745-1751`） |
| Task 4 | 前提修正：`TaskResult` 无 Attachments（`daemon/types.go:287-310`）→ 产物走 daemon 直传 storage + out-of-band RPC 上报；结算幂等（终态 no-op）；sweeper 失败路径单一收口退款 |
| Task 5 | 契约对齐实际路径（`cloud_runtime.go:41-97` 的 `/api/v1/nodes` 面），删去 provision/terminate/gateway 表述 |
| Task 6 | 工具面收窄改为待建能力：additive per-agent 启动配置（默认不动用户 agent）+ aurora 任务 `MaxTurns` + `MULTICA_AGENT_TIMEOUT`；guard 二期 |
| Task 7（新增） | 创建端点 per-user 频率闸门（spec §10 落点；「匿名」表述修正为「新用户」） |
