# Aurora 内容创作应用 — 架构设计

- 日期：2026-09-11
- 状态：已确认（待实现计划）
- 分支：`feat/integerat-aurora-ai`
- 关联来源：`aurora-ai-agents` 仓库（Apex AI 工具中心）的前端设计与 16 个 Skill 目录

## 1. 背景与目标

把 `aurora-ai-agents` 的前端设计（16 个 AI 内容创作功能 + 积分计费 + 消费级 UI）整合进 Multica 仓库，作为一个**独立前端应用**，面向**普通非专业设计人员、普通用户、尤其是社交媒体内容创作者**（全球、中英双语）。

目标架构与 Multica 对齐：**前端 + Go 后端 + agent 运行时**，作为同一个 monorepo 内的新领域，共享 auth/workspace/任务队列/用量计量/计费基础设施。

本文档覆盖两个交付物：(1) 系统架构设计；(2) 达到「上线可对外销售」所需的功能评估与分期路线。

## 2. 已确认的关键决策

| # | 决策点 | 结论 |
|---|--------|------|
| D1 | 目标市场 | 全球，中英双语（小红书 + Instagram/TikTok 等） |
| D2 | 后端共享方式 | 同一 monorepo + Go 服务新增「内容创作」领域 |
| D3 | 前端形态 | 仅 Web（Next.js App Router）为 MVP |
| D4 | 计费模式 | 订阅 + 积分额度（订阅给月额度，超额另购） |
| D5 | 账户模型 | 个人账户为主（注册即开通个人空间，团队协作后续） |
| D6 | agent 运行时 | 方案 B：托管 coding-CLI agent（复用 Multica agent 后端） |
| D7 | runtime fleet | **self-host 自建**（不依赖私有 multica-cloud） |
| D8 | 订阅产品线 | **新建** Aurora 个人订阅（不是 workspace 席位制） |
| D9 | 积分账本 | **复用 Cloud 钱包的数据契约**（micro-credit / 1 USD=1000 credit / 交易类型），在本地 Go 端自建账本 |

D7/D8/D9 的一致含义：**Aurora 是完全 self-host 的部署**，`multica-cloud` 的 fleet/订阅/钱包在本地都不可用（`cloudruntime/baseURL` 为空即 `ErrDisabled`）。「复用 Cloud 钱包」落地为**复用其 schema/API 契约**（见 `packages/core/types/billing.ts`：`micro-credit BIGINT`、`1 USD = 1000 credit`、交易类型 `topup/deduction/refund/expire/adjustment`、批次 `purchase/bonus/adjustment`），前端 `packages/core/billing` 的类型与查询几乎可直接沿用。

## 3. 系统架构

```
apps/aurora (Next.js) ──► packages/core (query/api/hooks) ──► Go server 新领域 /api/aurora/*
                                                                  │
                                  ┌───────────────┬───────────────┬──────────────┬──────────────┐
                                  ▼               ▼               ▼              ▼              ▼
                           auth/workspace     任务队列        用量计量        计费(本地账本)   billing/Stripe
                           (全复用)        AgentTaskQueue   TaskUsage→Hourly   credit_ledger   (新建)
                                  │           (复用)          (复用)          (新建)        (新建)
                                  │               │               ▲              ▲
                                  │               ▼               │              │
                                  │      fleet controller ──► 自建 runtime fleet（沙箱 daemon 节点）
                                  │      (新建，复用 cloudruntime 契约)     │
                                  │               │                    coding-CLI agent
                                  │               │                          │
                                  │               └────────── MCP: 图片/视频/转写/PPT/Excel 生成
                                  └────────────────────────────────────────────┘
```

### 3.1 组件清单

| 组件 | 位置 | 复用/新建 |
|------|------|-----------|
| 前端应用 | `apps/aurora`（Next.js） | 新建 |
| 共享业务页/组件 | `packages/views/aurora/` | 新建（复用 `packages/ui`/`packages/core`） |
| API client / query hooks | `packages/core/aurora/` | 新建（复用 `packages/core/api` schema 与 `parseWithFallback`） |
| Go 后端领域 | `server/internal/aurora/`（handler/service/models）+ `/api/aurora/*` 路由 | 新建 |
| auth / workspace / 多租户中间件 | `server/internal/auth`、`server/internal/middleware` | 复用 |
| 任务队列 | `AgentTaskQueue`、`server/internal/service/task.go` | 复用（新增任务类型） |
| agent 后端（CLI 执行） | `server/pkg/agent` | 复用 |
| daemon 执行器 | `server/internal/daemon` | 复用（部署到沙箱节点） |
| 用量计量 | `TaskUsage` / `TaskUsageHourly` / `metrics/pricing.go` | 复用 |
| runtime fleet | 新建 fleet controller + 沙箱节点编排 | **新建** |
| 计费账本 | `credit_balance` / `credit_ledger` | **新建**（复用 cloud 钱包契约） |
| 支付 | Stripe 集成 | **新建**（本地，不经 cloud） |

## 4. 执行层设计（agent 运行时，方案 B + self-host fleet）

### 4.1 关键事实（来自代码库）

Multica 的 agent 运行时是**在用户机器上跑 coding CLI（claude/codex 等 25 种）** 的桌面 daemon 模型（`server/pkg/agent` + `server/internal/daemon`），不是托管式 SaaS 调用。对消费者不可接受，因此执行底座必须**服务端托管**。

`server/internal/cloudruntime/client.go`（255 行）只是指向私有 multica-cloud fleet 的 HTTP 代理（`provision/terminate/exec/gateway`）。self-host 下需要自建 fleet。

### 4.2 self-host runtime fleet 设计

复用 Multica 的 daemon 执行器，但**把 daemon 部署到沙箱化的服务端节点**，而不是用户桌面：

1. **fleet controller（新建）**：负责 provision/terminate 沙箱节点（容器或 microVM）、健康检查、节点生命周期。对外暴露与 `cloudruntime` 相同的契约（`/nodes`、`/start`、`/stop`、`/exec`、`/status`），这样本地 `cloudruntime/client.go` 的调用模式可复用，只是 `baseURL` 指向自建 controller。
2. **沙箱节点**：每个节点跑一个 `daemon` 进程（复用 `server/internal/daemon`），以「托管 runtime」身份注册到 `AgentRuntime`（`daemon_id` + `owner_id` + `visibility` 标记为托管）。节点内用**容器/VM 级隔离**（Docker/gVisor/Firecracker），出网白名单、CPU/内存/时长上限。
3. **任务流**：用户提交 → `AgentTaskQueue` 入队（新增 `origin=aurora` 任务类型）→ 沙箱 daemon claim → 本地 spawn coding CLI → 产物回写。
4. **成本/确定性护栏**：`max_turns` 上限、per-task 调用上限 guard、`TaskUsage` 计量反推积分；确定性产物（PPT/Excel/字幕裁剪）尽量走确定性 MCP 工具，不让 agent 自由发挥。

### 4.3 媒体生成 MCP

16 个 Skill 需要的能力（图片/视频生成、转写、PPT/Excel 渲染）通过 **MCP server** 暴露给 agent（复用 `server/pkg/agent/mcp_config.go`）：

- 每个 Skill = 一个 Multica `Agent`（系统行，`system_key` 如 `aurora:xhs-image`），`instructions` 为系统提示 + 挂一个 `Skill`（SKILL.md 工作流）+ `mcp_config` 指向对应媒体生成 MCP。
- MCP server 分两类：**模型供应商适配器**（OpenAI/Google/Anthropic/Ideogram/Runway/HeyGen 的 image/video/transcribe 封装）与**确定性渲染器**（ffmpeg 剪辑字幕、PPT 渲染、代码解释器算 Excel）。
- 供应商密钥只在服务端（fleet 或 MCP 侧）读取，任何值不得以 `NEXT_PUBLIC_` 或 API 响应暴露（沿用 aurora 红线）。

### 4.4 设计张力（记录为技术债，非阻断）

部分 Skill 是**纯确定性**的（证件照裁切、视频字幕、Excel 计算），本不需要 LLM agent。方案 B 下它们仍走 agent 编排确定性 MCP 工具，成本偏高。后续可加「确定性 Skill 快速通道」绕过 agent 直接跑流水线。MVP 先统一走 agent 以保证架构一致。

## 5. 数据模型（新领域，migrations）

> 遵循仓库硬约束：不加外键/级联删除，索引用 `CREATE INDEX CONCURRENTLY`（每个索引单独 migration 文件）。

- **复用 `Agent` / `Skill` / `AgentSkill`**：16 个技能表示为系统 Agent 行（`system_key` 如 `aurora:*`），任务/队列/运行时机制零改造。
- 新建 `aurora_skill_catalog`：消费者面向元数据（`skill_id`、`credits` 定价、`category`、`input/output` 类型、`featured`、i18n 显示名）。单一事实来源，替代 aurora 当前 `lib/skill-runtime.ts` 与 `app/page.tsx` 双份不同步的问题。
- 新建 `aurora_generation`：一次用户提交 = 一行，关联 `AgentTaskQueue.task_id`，字段 `status`、`skill_id`、`prompt`、输入资产、输出资产、`credits_reserved`/`credits_charged`、`error`。
- 新建 `aurora_asset`：内容资产（图/视频/文案/文档），媒体 URL/存储、版本、格式、平台目标。
- 新建 `credit_balance` + `credit_ledger`（见 §6）。
- 复用 `Attachment` 存原始文件；`TaskUsage`/`TaskUsageHourly` 计量。

## 6. 计费设计（订阅 + 积分额度）

### 6.1 订阅（新建产品线）

现订阅是 workspace 席位制（`seatcapacity` + cloud-subscriptions 代理）。Aurora 是个人订阅，需**新建产品线**：个人订阅档位（如 Free/Creator/Pro），月付/年付，Stripe 本地集成。订阅权益 = 每月发放积分额度 + 若干上限（并发任务数、月生成次数等，通过扩展 `entitlement` 的 `GateName`）。

### 6.2 积分账本（本地自建，复用 Cloud 钱包契约）

- 契约沿用 `packages/core/types/billing.ts`：`micro-credit BIGINT`、`1 USD = 1000 credit`、交易类型 `topup/deduction/refund/expire/adjustment`、批次 `purchase/bonus/adjustment`。
- 新建 `credit_balance`（余额）与 `credit_ledger`（流水），事务 + 幂等 + 对账。
- 订阅月额度 = 按月发放 credit（`adjustment` 交易）；超额另购 = `topup`（Stripe checkout → webhook → 记账）。
- **扣费**：任务完成 → `TaskUsage` 成本 × 加价率（`APEX_CREDIT_MARGIN` 思路）→ 换算 credit → 冻结/结算两步扣减；任务开始前预留 `credits_reserved`（沿用 aurora 概念）。

### 6.3 权益门禁

扩展 `server/internal/entitlement` 的 `GateName`（当前仅 `GateIssueCount`/`GateAutopilotRuns`），新增 Aurora 门禁（如 `GateAuroraGenerations`、`GateAuroraConcurrency`）。注意 `entitlement/client.go` 对 gate 集合有硬校验，需同步改 `normalizePolicy`。

## 7. 认证 / 账户

- 复用邮箱 6 位验证码 + Google OAuth + JWT cookie + 自助注册（默认开放）。
- **新增**：注册即静默开通个人 workspace（单成员 owner）——复用全部 workspace 中间件，改动最小。Aurora 注册后自动建「个人空间」，`DISABLE_WORKSPACE_CREATION` 不影响这条内部开通路径。
- 双语/时区已内建（`packages/core/i18n`、`user.language`/`timezone`）。

## 8. 前端设计（apps/aurora）

把 aurora 的 `app/page.tsx` 设计（侧边栏 + 功能网格 + 抽屉任务窗 + 充值/账户弹窗 + 登录页）迁到 Multica 架构：

| aurora 现状 | Multica 目标 |
|-------------|--------------|
| 单页 `page.tsx` 全 state | `packages/views/aurora/`（共享业务页）+ `packages/core`（query/hooks/store，Zustand 存 client state，TanStack Query 存 server state） |
| `lib/skill-runtime.ts` 与 `page.tsx` 双份目录 | 单一事实来源 `GET /api/aurora/skills`，前端消费 schema 化响应 |
| `POST /api/tasks`（占位） | `POST /api/aurora/generations`（真实入队） |
| 无实时 | WebSocket（复用 `server/internal/realtime`）推送任务进度/产物就绪 |
| 积分 mock | 真实 `credit_balance`/`credit_ledger` + 充值/用量 API |
| 简体中文硬编码 | 中英双语（复用 `packages/core/i18n`，补 en 文案） |
| Cloudflare vinext + dsh | Next.js + Go（方案 B 已决定弃用 dsh skill-worker） |

- 设计 token 复用 `packages/ui/styles` 语义 token；遵循仓库 UI 规则（font 用 `--text-*` 尺度、避免硬编码色）。
- 状态规则：TanStack Query 管 server state（skills/generations/assets/balance），Zustand 管 client state（当前视图/筛选/抽屉开关）。

## 9. 上线功能评估与分期

### 9.1 MVP（可对外销售）

| # | 功能 | 说明 |
|---|------|------|
| 1 | 认证 + 注册自动开个人空间 | 邮箱验证码 + Google OAuth |
| 2 | Skill 目录（中英双语） | 16 功能目录，服务端单一事实来源 |
| 3 | 确定性优先的两类 Skill | 文字类（小红书文案/简历/文件总结/录音转写）+ 图片类（海报/小红书图片/商品图/文字生图/图片修改/证件照） |
| 4 | 异步任务 + 进度 + 产物下载 | 入队 → 沙箱执行 → 实时进度 → `aurora_asset` 下载 |
| 5 | 计费闭环 | 订阅档位 + 积分额度 + 充值（Stripe）+ 扣费 + 账单/用量明细 |
| 6 | 作品库 | asset 列表/下载/删除 |
| 7 | 安全硬性条件（见 §10） | 沙箱隔离 + 频率闸门 + 调用上限 + 内容审核 |
| 8 | 支付 | Stripe（全球） |

### 9.2 第二阶段（稳定后）

视频类（图生视频/文生视频/剪辑字幕/数字人口播）、PPT/Excel、社交平台一键发布、多语言扩展。

### 9.3 第三阶段（增长）

团队协作、模板市场、品牌资产库、公开 API、移动端。

## 10. 安全与合规（不可妥协）

对齐 aurora ADR-0001 的三条上线硬性条件，并因 self-host fleet 强化：

1. **沙箱隔离**：容器/VM 级隔离 + 出网白名单 + 资源上限。服务端跑任意 coding CLI = 我们服务器执行用户 prompt 的任意代码，隔离不是可选项。
2. **工具面收窄**：关 `tool-bash` 等危险工具行，`permission-presets`，per-task 调用上限 `guard`（按 task 分键）。
3. **频率闸门**：匿名/新用户任务创建频率限制。
4. **内容审核**：prompt 审核 + 生成物审核（社媒内容合规，含中英双语）。
5. **密钥安全**：供应商密钥仅服务端读取，绝不暴露前端。

## 11. 错误处理 / 测试 / 可观测性

- **API 兼容**：所有 Aurora API 响应走 `parseWithFallback` + zod schema（沿用仓库 API 兼容规则），配 malformed-response 测试。
- **幂等扣费**：账本写入事务 + 幂等键；任务终态从 durable 结果归约（沿用 Multica 的「不许解析模型 prose」原则）。
- **测试分层**：纯逻辑/账本状态机 → `packages/core/*.test.ts`；组件/页面 → `packages/views/*.test.tsx`；Go 后端用 `internal/testutil`（`dbfx`/`Call`）。DB-backed 测试经 `testutil`，不 open-code `INSERT...RETURNING`。
- **可观测**：复用 `TaskUsage`/`TaskUsageHourly` + Prometheus（`metrics/pricing.go`）+ PostHog（`analytics`）。

## 12. 风险与开放问题

| 风险/问题 | 影响 | 缓解 |
|-----------|------|------|
| self-host fleet 基建量（provision/沙箱/编排） | 方案 B 最大新增成本 | fleet controller 复用 `cloudruntime` 契约；MVP 先单节点沙箱，再水平扩展 |
| 服务端 coding agent 的安全面 | 生产安全 | §10 沙箱隔离为硬门槛 |
| coding agent 每任务成本高、非确定 | 利润率/体验 | `max_turns` + 确定性 MCP 工具 + 计量护栏 |
| 积分扣费从零建（本地无账本） | 后端工作量 | 复用 cloud 钱包契约，前端类型沿用 |
| Stripe 本地集成 + 订阅产品线新建 | 计费复杂度 | MVP 先单一订阅档位 + 积分 topup，再扩展 |

## 13. 非目标（MVP 明确不做）

- 桌面端（Electron）与移动端（Expo）—— 仅 Web。
- 团队协作 / workspace 多人成员 —— 个人账户为主。
- dsh skill-worker 复用 —— 方案 B 已弃用。
- 社交平台 API 自动发布 —— 第二阶段。
- 模板市场 / 公开 API —— 第三阶段。
