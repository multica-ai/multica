# Aurora 执行层（self-host fleet + agent + HyperFrames）— 实现计划（Plan 3）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让一次 Aurora generation 真正跑起来：入队 → 托管 agent 执行 → 产物回写 asset、结算积分。含 self-host runtime fleet 与沙箱节点镜像（infra）。

**Architecture:** 复用 Multica 的 `AgentTaskQueue` + `server/pkg/agent`（coding CLI）+ `server/internal/daemon`（执行器），把 daemon 部署到沙箱化的服务端节点（托管 runtime）；16 个 Skill = 16 个系统 Agent（`system_key`）；媒体生成经 MCP、视频经 HyperFrames。Go 侧的任务分四块：种子 agent、入队+预留、托管 runtime、完成回写；infra 侧两块：fleet controller、沙箱节点镜像。

**Tech Stack:** Go 1.26、sqlc、pgx、`server/pkg/agent`、`server/internal/daemon`、`server/internal/service`、Docker/gVisor/Firecracker、HyperFrames CLI。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§4 执行层）。

**依赖：** Plan 1（`aurora_generation` 表 + `CreateAuroraGeneration`）、Plan 2（`h.Credit`）。

## Global Constraints

- 不加外键/级联。
- 服务端跑 coding CLI = 我们服务器执行用户 prompt 的任意代码：**容器/VM 级隔离 + 出网白名单 + 工具面收窄（关 `tool-bash` 等）+ per-task 调用上限 guard** 是不可妥协的硬门槛（spec §10）。
- 供应商密钥只在服务端（fleet/MCP 侧）读取，绝不暴露前端。
- 任务终态从 durable 结果归约，不解析模型 prose（沿用 Multica 原则）。

---

### Task 1: 种子 16 个系统 Agent

**Files:**
- Create: `server/internal/aurora/agents.go`
- Create: `server/internal/aurora/agents_test.go`

**Interfaces:**
- Consumes: `Agent` / `Skill` / `AgentSkill` 表（已存在）。
- Produces：`aurora.SystemAgentDef{ SkillID, SystemKey, Name, Instructions string }` 与 `aurora.SystemAgents() []SystemAgentDef`（16 条，`SystemKey = "aurora:"+skillID`）；一个 `EnsureSystemAgents(ctx, queries) error`（幂等 upsert，缺则建 `Agent` 行，`runtime_mode='cloud'`、`visibility='workspace'`、`owner_id` 为系统保留值）。

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

- [ ] **Step 2/3/4: 实现 + 跑测试 + commit**（`EnsureSystemAgents` 用 `ON CONFLICT (system_key) DO UPDATE` 幂等 upsert；`system_key` 列已存在于 `agent` 表——确认其唯一约束，若无则加单文件 `CREATE UNIQUE INDEX CONCURRENTLY` migration）。

```bash
git add server/internal/aurora/agents.go server/internal/aurora/agents_test.go
git commit -m "feat(aurora): seed 16 system agents"
```

---

### Task 2: 入队 + 预留积分

**Files:**
- Modify: `server/internal/handler/aurora.go`（`CreateAuroraGeneration` 扩展）
- Modify: `server/internal/handler/aurora_test.go`

**Interfaces:**
- Consumes: `h.Credit.Reserve`（Plan 2）、`EnsureSystemAgents`（Task 1）、任务入队 API（读 `server/internal/service/task.go` 确认现有入队函数签名，如 `EnqueueTaskForIssue`/quick-create；若没有「纯 prompt 入队」，加一个最小 `EnqueueAuroraGeneration(ctx, agentID, prompt, workspaceID)`）。
- Produces：`CreateAuroraGeneration` 改为：校验 skill → 解析 skill 对应系统 agent → `h.Credit.Reserve(...)` 预留该 skill 的 `credits`（×1e6 得 micro）→ 写 generation 行（含 `credits_reserved`、`task_id`）→ 入队。

- [ ] **Step 1: 写测试**（断言创建后：`creditsReserved` 非 0；`aurora_generation.task_id` 被写回；余额被扣减）

```go
func TestCreateAuroraGenerationReservesAndEnqueues(t *testing.T) {
	// 先给 testUserID 充 credits：svc.Grant(...) 或直接插 credit_balance。
	// 再 POST /api/aurora/generations，断言 generation.creditsReserved > 0，
	// 且 aurora_generation.task_id 非空（dbfx.QueryRow 查回）。
}
```

- [ ] **Step 2/3/4: 实现 + 跑测试 + commit**（入队函数签名以 `task.go` 实际为准；预留金额 = `skill.credits * 1_000_000` micro，`reference = generation.ID`）。

```bash
git add server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/internal/service/task.go
git commit -m "feat(aurora): reserve credits and enqueue task on generation"
```

---

### Task 3: 托管 runtime 注册（服务端 daemon）

**Files:**
- Modify: `server/internal/handler/daemon.go`（若需区分「托管 runtime」的注册路径）或新建 `server/internal/handler/aurora_runtime.go`

**Interfaces:**
- Produces：服务端沙箱节点内的 daemon 以 `runtime_mode='cloud'`、`visibility='private'`、`owner_id`=系统保留值注册（现有 `DaemonRegister` 大概率已支持 `runtime_mode='cloud'`，见 `testutil.Fixture.Runtime`）。此任务主要确认并补「托管节点不被当作用户桌面端、允许在无 desktop 用户时 claim」的路径。

- [ ] **Step 1: 探明现状**（读 `server/internal/handler/daemon.go` 的 `DaemonRegister` 与 `server/internal/dispatch/reason.go` 的脱机排队逻辑，确认 cloud runtime 是否能 claim 任务；若 `runtime_mode='cloud'` + `daemon_id` 为空的 runtime 能 claim，则本任务退化为「加一条注册守卫/测试」，否则补齐托管 runtime 的 claim 分支）。

- [ ] **Step 2/3: 加测试 + 实现（或记录为无需改动）** + commit

```bash
git commit -m "feat(aurora): support managed (server-hosted) runtime claim"
```

---

### Task 4: 完成回写 → asset + 结算

**Files:**
- Create: `server/internal/service/aurora_completion.go`（或 `server/internal/handler/aurora.go`）
- Modify: `server/internal/service/task.go`（在 `CompleteTask`/`reportTerminalTask` 的终态分支挂 Aurora 回写）

**Interfaces:**
- Consumes: 任务终态（`TaskResult`：`Comment` + `Attachments` + `Usage`）、`h.Credit.Refund`/结算、`aurora_asset` 表。
- Produces：任务完成时，按 generation 的 skill：成功 → 写 `aurora_asset`（kind 按 skill 的 output），更新 generation `status='completed'`、`credits_charged`；失败 → `status='failed'`、`error`、`Refund` 预留。任务终态从 durable 结果归约（不解析 prose）。

- [ ] **Step 1: 写测试**（构造一个已完成的 task + 其 generation，调完成回写函数，断言 generation.status 变更 + asset 落行 + 积分结算/退款）

- [ ] **Step 2/3: 实现 + 跑测试 + commit**

```bash
git add server/internal/service/aurora_completion.go server/internal/service/task.go
git commit -m "feat(aurora): backfill assets and settle credits on completion"
```

---

### Task 5（infra）: self-host fleet controller

**Files:**
- Create: `server/internal/aurorafleet/`（或复用 `cloudruntime` 契约自建 controller 服务）

**交付与验收：**
- 提供 `provision/terminate/exec/status` 契约（对齐 `cloudruntime/client.go` 的 path 推断），`baseURL` 指向本 controller。
- 按需 provision/terminate 沙箱节点（容器或 microVM），健康检查，节点生命周期管理。
- 验收：一个空节点能被 provision，daemon 能注册并 claim 一个任务。
- 这是基础设施代码，用集成测试（非纯 TDD）；密钥/隔离在 Task 6 落实。

```bash
git commit -m "feat(aurora): self-host runtime fleet controller"
```

---

### Task 6（infra）: 沙箱节点镜像（daemon + agent CLI + MCP + HyperFrames）

**Files:**
- Create: `deploy/aurora-sandbox/`（Dockerfile、entrypoint、镜像构建脚本）

**交付与验收：**
- 镜像含：Multica daemon 二进制 + 至少一个 coding agent CLI（claude/codex）+ 媒体生成 MCP server（image/transcribe）+ HyperFrames CLI + Node 22 + FFmpeg + headless Chrome。
- 沙箱：容器/VM 级隔离、出网白名单、CPU/内存/时长上限、**工具面收窄**（关 `tool-bash`/`tool-pwsh`/`tool-fs-search` 等，`permission-presets`，per-task 调用上限 `guard`——对齐 aurora ADR-0001）。
- 验收：一个 HyperFrames「文生视频」任务在沙箱内跑通并产出 MP4 asset（`MULTICA_RUN_REAL_AGENT_SMOKE=1` 下的集成冒烟）。

```bash
git commit -m "feat(aurora): sandbox node image with agent, MCP and HyperFrames"
```

---

## Deferred / 风险

- **水平扩展**：MVP 先单节点沙箱；多节点调度与 `@hyperframes/aws-lambda` 渲染路径作后续扩展（spec §12）。
- **生成式视频（Veo/Runway）与数字人（HeyGen Avatar）**：第二阶段，本计划只覆盖 HyperFrames 确定性视频（spec §9.2）。
- **确定性 Skill 快速通道**：纯确定性 skill（证件照/字幕/Excel）仍走 agent，后续可绕过 agent 直跑流水线（spec §4.5 技术债）。

## Self-Review

- **Spec 覆盖**：§4.2 fleet 设计 → Task 5/6；§4.3 MCP + §4.4 HyperFrames → Task 6；任务入队/完成 → Task 2/4；16 Skill=Agent → Task 1。
- **诚实标注**：Task 2/4 的入队/终态函数签名需读 `task.go` 确认（与 Plan 1 同类诚实标注，非占位）。

## 执行交接

Plan 3 完成。剩余 Plan 4（前端）。
