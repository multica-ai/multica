# Aurora 前端（apps/aurora + packages）— 实现计划（Plan 4）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 aurora 的前端设计（侧边栏 + 功能网格 + 抽屉任务窗 + 作品库 + 充值/账单）迁到 Multica 前端架构，作为 `apps/aurora` 对接 Plan 1–3 的后端 API。

**Architecture:** 三层复用 Multica 依赖方向：`packages/core/aurora`（类型 + zod schema + API client + React Query hooks）→ `packages/views/aurora`（共享业务页/组件）→ `apps/aurora`（Next.js App Router wiring）。state 规则照 CLAUDE.md：TanStack Query 管 server state（skills/generations/balance/transactions），Zustand 管 client state（当前视图/筛选/抽屉）。

**Tech Stack:** Next.js App Router、React 19、TanStack Query、Zustand、zod、`packages/ui`、`packages/core`（api/schema.ts 的 `parseWithFallback`）。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§8 前端）。

**API 契约（Plan 1–3.5 已定，前端据此写 schema）：**

| 端点 | 响应形状 |
|------|---------|
| `GET /api/aurora/skills` | `{skills:[{id,name,name_en,category,credits,input[],output[],featured,available}]}` |
| `POST /api/aurora/generations` `{skillId,prompt}` | `201 {generation:{id,skillId,prompt,status,creditsReserved}}`；余额不足 `402`（`status="failed"`）；月次数/并发超限 `429`（Plan 5 Task 6）；频率闸门 `429`（Plan 3 Task 7） |
| `GET /api/aurora/generations?limit=50` | `{generations:[...]}`（Plan 3.5） |
| `GET /api/aurora/generations/{id}` | `{generation:{...,status,assets:[...]}}`（Plan 3.5；status 服务端派生自 task，轮询进度用） |
| `GET /api/aurora/assets` / `GET /api/aurora/assets/{id}/download` / `DELETE /api/aurora/assets/{id}` | Plan 3.5 契约（作品库） |
| `GET /api/aurora/billing/balance` | `{availableMicro}` |
| `GET /api/aurora/billing/transactions?limit=50` | `{transactions:[{id,kind,amountMicro,balanceAfterMicro,reference,createdAt}]}` |

## Global Constraints

- 依赖方向 `views -> core + ui`；`core`/`ui` 独立。
- `packages/core/`：无 `react-dom`/`localStorage`/`process.env`/UI 库。
- `packages/views/`：无 `next/*`、无 `react-router-dom`、无 stores；用 `NavigationAdapter`/`useNavigation()`/`<AppLink>`。
- `apps/aurora`：Next.js 导航/platform API 只放这里。
- API 解析用 `packages/core/api/schema.ts` 的 `parseWithFallback` + zod，不 cast 网络 JSON。
- server 字段防御性 optional-chain + 默认值；枚举 switch 要 `default` 分支。
- 每个 workspace 包在自己的 `package.json` 声明直接 import 的外部依赖（用 `catalog:`）。
- 中文产品文案走 `packages/core/i18n`（中英双语，补 en 文案）；命名/i18n 规则见 `apps/docs/content/docs/developers/conventions.mdx`。

---

### Task 1: `packages/core/aurora`（类型 + schema + API + hooks）

**Files:**
- Create: `packages/core/aurora/types.ts`
- Create: `packages/core/aurora/schema.ts`
- Create: `packages/core/aurora/api.ts`
- Create: `packages/core/aurora/queries.ts`
- Create: `packages/core/aurora/mutations.ts`
- Create: `packages/core/aurora/types.test.ts`（或 schema 的 malformed 测试）
- Modify: `packages/core/package.json`（导出新路径）

**Interfaces:**
- Consumes: `packages/core/api/schema.ts` 的 `parseWithFallback`；现有 api client 模式（读 `packages/core/api/` 参考）。
- Produces（views/app 依赖）：
  - `type AuroraSkill = { id: string; name: string; nameEn: string; category: string; credits: number; input: string[]; output: string[]; featured: boolean; available: boolean }`
  - `type AuroraGeneration = { id: string; skillId: string; prompt: string; status: string; creditsReserved: number }`；`AuroraAsset = { id: string; generationId: string; kind: string; mediaUrl: string | null; format: string | null; createdAt: string }`
  - `parseAuroraSkills` / `parseAuroraGeneration` / `parseAuroraGenerations` / `parseAuroraGenerationDetail` / `parseAuroraAssets` / `parseAuroraBalance` / `parseAuroraTransactions`（zod schema + `parseWithFallback`）
  - `useAuroraSkills()`、`useAuroraBalance()`、`useAuroraTransactions()`、`useAuroraGenerations()`、`useAuroraGenerationDetail(id)`（非终态 status 时 `refetchInterval` 3s 轮询，Plan 3.5 契约）、`useAuroraAssets()`、`useCreateAuroraGeneration()`、`useDeleteAuroraAsset()`

- [ ] **Step 1: 写 schema + malformed 测试**

`packages/core/aurora/schema.ts`：

```ts
import { z } from "zod";
import { parseWithFallback } from "../api/schema";

// parseWithFallback(data, schema, fallback, { endpoint }) — opts.endpoint 必传
//（packages/core/api/schema.ts:38-55，先例 client.ts:1697-1702）。
export const auroraSkillSchema = z.object({
  id: z.string(),
  name: z.string(),
  nameEn: z.string().default(""),
  category: z.string(),
  credits: z.number(),
  input: z.array(z.string()).default([]),
  output: z.array(z.string()).default([]),
  featured: z.boolean().default(false),
  available: z.boolean().default(true), // false = 二阶段技能，仅展示
});
export type AuroraSkill = z.infer<typeof auroraSkillSchema>;

export const auroraSkillsSchema = z.object({ skills: z.array(auroraSkillSchema) });
export const auroraGenerationSchema = z.object({
  id: z.string(), skillId: z.string(), prompt: z.string(),
  status: z.string(), // "queued" | "running" | "completed" | "failed"；UI switch 必须带 default 分支
  creditsReserved: z.number().default(0),
});
export const auroraGenerationsSchema = z.object({ generations: z.array(auroraGenerationSchema) });
export const auroraAssetSchema = z.object({
  id: z.string(), generationId: z.string(), kind: z.string(),
  mediaUrl: z.string().nullable().default(null),
  format: z.string().nullable().default(null),
  createdAt: z.string().default(""),
});
export const auroraGenerationDetailSchema = z.object({
  generation: auroraGenerationSchema.extend({ assets: z.array(auroraAssetSchema).default([]) }),
});
export const auroraAssetsSchema = z.object({ assets: z.array(auroraAssetSchema) });
export const auroraBalanceSchema = z.object({ availableMicro: z.number().default(0) });
export const auroraTransactionSchema = z.object({
  id: z.string(), kind: z.string(), amountMicro: z.number(),
  balanceAfterMicro: z.number().default(0),
  reference: z.string().default(""), // generation id / 事件 id，UI 据此映射技能名
  createdAt: z.string().default(""),
});
export const auroraTransactionsSchema = z.object({ transactions: z.array(auroraTransactionSchema).default([]) });
```

`packages/core/aurora/types.test.ts`（`// @vitest-environment node`）：

```ts
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { auroraSkillsSchema } from "./schema";

describe("auroraSkillsSchema", () => {
  it("survives malformed skill entries with fallbacks", () => {
    const res = auroraSkillsSchema.parse({
      skills: [{ id: "poster", name: "海报制作", credits: 760, category: "image" }],
    });
    expect(res.skills[0].nameEn).toBe("");
    expect(res.skills[0].input).toEqual([]);
    expect(res.skills[0].available).toBe(true);
  });
  it("falls back to an empty list on non-array body", () => {
    const res = parseWithFallback({ skills: "not-an-array" }, auroraSkillsSchema, { skills: [] }, { endpoint: "GET /api/aurora/skills" });
    expect(res.skills).toEqual([]);
  });
});
```

- [ ] **Step 2/3: 跑测试（红→绿）+ 实现 api/queries/mutations + commit**

`api.ts` 用现有 api client 调契约表全部端点；`queries.ts` 用 `useQuery`（workspace-scoped key 含 wsId，CLAUDE.md 规则）；`useAuroraGenerationDetail(id)` 在 status 非终态时 `refetchInterval` 3s 轮询（MVP 进度机制）；`mutations.ts` 的 `useCreateAuroraGeneration` 用 `useMutation`，成功后 invalidate balance/transactions/generations（402 余额不足 → 提示充值并刷新 balance）。

```bash
git add packages/core/aurora packages/core/package.json
git commit -m "feat(aurora): core types, schemas, api and query hooks"
```

---

### Task 2: `packages/views/aurora`（共享业务页）

**Files:**
- Create: `packages/views/aurora/skill-directory.tsx`（功能网格 + 分类 tab + 搜索，对应 aurora `page.tsx` 的 agents grid；`available=false` 技能置灰 + 「即将上线」badge）
- Create: `packages/views/aurora/generation-composer.tsx`（抽屉任务窗，输入 prompt → `useCreateAuroraGeneration`；unavailable 技能禁用提交；402 余额不足展示充值引导；429 限额/限流展示「本月次数或并发已达上限」提示）
- Create: `packages/views/aurora/works-list.tsx`（我的作品：`useAuroraGenerations` + `useAuroraAssets` 真实端点 + 下载/删除）
- Create: `packages/views/aurora/billing.tsx`（余额 + 流水 + 充值入口占位；流水展示：`reference` 为 generation id 时经 catalog 映射技能名，Plan 5 的 `sub:`/`signup:`/`expire`/topup 引用按 **kind 兜底标签**显示——`billing.transaction.kind.*` 四语 key）
- Create: `packages/views/aurora/*.test.tsx`（组件 happy path）
- Create: i18n 文案（`packages/views/locales/{en,zh-Hans,ko,ja}/aurora.json` + `locales/index.ts` 注册——**四语全 key**，`parity.test.ts` 强制缺一 CI 挂；中文文案遵循 `apps/docs/content/docs/developers/conventions.mdx` 词汇表）
- Modify: `packages/views/package.json`

**Interfaces:**
- Consumes: Task 1 的 hooks/类型；`packages/ui` 组件；`useNavigation()`/`<AppLink>`。
- Produces：页面组件 `SkillDirectory`、`GenerationComposer`、`WorksList`、`AuroraBilling`，由 `apps/aurora` 组装。

- [ ] **Step 1: 写组件 happy-path 测试**（渲染目录：断言 16 个 skill 渲染、13 可用 3 个置灰「即将上线」；composer：mock `useCreateAuroraGeneration` 断言提交后状态切换 + unavailable 禁用 + 402 充值引导；works：mock `useAuroraGenerations`/`useAuroraAssets` 断言列表与删除；billing：mock balance/transactions hook 断言余额与 reference 映射展示）

> 用 `packages/views/*.test.tsx` 现有测试写法；mock `@multica/core/api` 与 stores 用 Zustand callable-store shape（CLAUDE.md Testing 规则）。测试不 mock `next/*`/`react-router-dom`。

- [ ] **Step 2/3: 实现 + 跑测试 + commit**（用 `--text-*` font token、语义 token；active/selected 态 hover 可辨识）

```bash
git add packages/views/aurora packages/views/package.json
git commit -m "feat(aurora): shared skill directory, composer, works and billing views"
```

---

### Task 3: `apps/aurora`（Next.js App Router wiring）

**Files:**
- Create: `apps/aurora/package.json`、`next.config.ts`、`tsconfig.json`、`eslint.config.*`
- Create: `apps/aurora/app/layout.tsx`、`app/page.tsx`、`app/globals.css`（import `packages/ui/styles/tokens.css` + `base.css`，apps/web 先例 `globals.css:1-9`）
- Create: `apps/aurora/app/[workspaceSlug]/layout.tsx`（`setCurrentWorkspace` + auth 重定向 + `last_workspace_slug` cookie，apps/web 先例 `app/[workspaceSlug]/layout.tsx:80`）
- Create: `apps/aurora/app/[workspaceSlug]/skills/page.tsx`、`app/[workspaceSlug]/works/page.tsx`、`app/[workspaceSlug]/billing/page.tsx`（或单页内 view 切换）
- Create: `apps/aurora/app/login/page.tsx`（复用 `packages/views/auth` 的 `LoginPage`，apps/web 先例 `app/(auth)/login/page.tsx:30`）、`apps/aurora/app/auth/callback/page.tsx`（OAuth 回调壳，apps/web 先例 `auth/callback/page.tsx:36-77`）
- Create: `apps/aurora/features/auth/auth-cookie.ts`（`multica_logged_in` marker cookie，apps/web 先例）
- Create: `apps/aurora/proxy.ts`（生产运行时重写 `REMOTE_API_URL`，apps/web 先例 `proxy.ts:46-59`）
- Create: 平台 wiring：`WebNavigationProvider`、`CoreProvider`（cookieAuth + CSRF + api/ws URL）、auth guard（复用 `packages/views/layout` 的 `DashboardGuard`）
- Modify: `.github/workflows/ci.yml`（paths filter :47-51 加 `apps/aurora/**`）、根 `package.json`（`dev:aurora` 脚本）

**Interfaces:**
- Consumes: Task 2 的 views；`packages/core`（auth/navigation/platform）。
- Produces：可运行的前端 app `apps/aurora`（`pnpm dev:aurora` 起）。

**已核实的 wiring 事实（2026-09-13）：**
- `WorkspaceIdProvider` **不存在**（slug-first 重构已移除，`packages/views/layout/dashboard-guard.tsx:15-17`）——workspace 上下文由 `[workspaceSlug]` 路由 layout 的 `setCurrentWorkspace` 建立，`useWorkspaceId()` 从 `useCurrentWorkspace()` 派生。不要按旧名写 wiring。
- web 客户端不发 `X-Workspace-ID`，发 `X-Workspace-Slug` + `X-CSRF-Token`（读 `multica_csrf` cookie），`credentials: include`（`packages/core/api/client.ts:684-698,763`）——CoreProvider 需配 cookieAuth（`apps/web/components/web-providers.tsx:71-96` 先例）。
- `pnpm-workspace.yaml` 用 glob（`apps/*`）自动包含 `apps/aurora`；turbo 按任务名自动拾取——**这两处不需要改**。需要改的是 CI paths filter（否则 Aurora-only PR 静默跳过前端检查）与根 `dev:aurora` 脚本。
- `make up C=aurora`（`scripts/dev-env.sh:34` 的 `ALL_COMPONENTS`）与容器化 Dockerfile 留二阶段（本计划用 `pnpm dev:aurora` + next dev 代理）。

- [ ] **Step 1: 脚手架**（参照 `apps/web/` 的 package.json/next.config（`transpilePackages` + dev rewrites 代理 `/api/:path*`、`/ws` 等，`next.config.ts:43-97` 先例）/tsconfig/layout 结构，新建 aurora 版；`output: "standalone"` 同 web）

- [ ] **Step 2: 组装路由 + guard + provider**（个人空间路由：`/[workspaceSlug]/skills`、`/works`、`/billing` + `[workspaceSlug]/layout.tsx`；登录复用 `LoginPage` + 自有 login 路由壳与 OAuth callback 壳 + `auth-cookie.ts` + `proxy.ts`；注册后自动进个人空间——Plan 1 Task 4 后端已保证）

- [ ] **Step 3: CI + 脚本 + 验证 + commit**

Run: `pnpm typecheck`、`pnpm lint`、`pnpm test`（`packages/core`/`views` 的 aurora 测试）、`pnpm dev:aurora`（手动点通 skills → 提交 generation → 轮询进度 → 看 balance）

```bash
git add apps/aurora .github/workflows/ci.yml package.json
git commit -m "feat(aurora): Next.js app wiring for skill directory, works and billing"
```

---

## Deferred / 边界

- **桌面/移动端**：MVP 仅 Web（spec §13）。
- **充值/订阅 UI**：本计划的充值入口占位已由 Plan 5 Task 7 接线（订阅卡片 + checkout 跳转 + topup，`2026-09-13-aurora-subscriptions-payments.md`）。
- **WebSocket 实时进度**：MVP 轮询（Task 1 的 `useAuroraGenerationDetail` refetchInterval）；`task:progress` 事件帧已存在（`packages/core/types/events.ts:30`），实时化二期——届时服务端加协议常量 + 生产者 + listener，前端扩展事件 union + `use-realtime-sync.ts`。
- **make up / 容器化**：`make up C=aurora`（`scripts/dev-env.sh:34` 的 `ALL_COMPONENTS`）与 Dockerfile 留二阶段；MVP 用 `pnpm dev:aurora` + next dev 代理。

## Self-Review

- **Spec 覆盖**：§8 的「单一事实来源 GET /api/aurora/skills」→ Task 1 schema + Task 2 目录；「POST /api/aurora/generations」→ Task 1 mutation + Task 2 composer；「余额/流水/用量」→ Task 2 billing；「作品库/进度」→ Plan 3.5 端点 + Task 1/2；「apps/aurora」→ Task 3。
- **契约一致性**：Task 1 的 zod 字段名与 Plan 1/2/3.5 的 JSON 契约逐字段对齐（`name_en`、`available`、`creditsReserved`、`availableMicro`、`amountMicro`、`reference`）。
- **wiring 修正（2026-09-13）**：`WorkspaceIdProvider` 不存在（slug-first 重构已移除）→ `[workspaceSlug]` layout + `setCurrentWorkspace`；CI paths filter 必须补 `apps/aurora/**`；pnpm-workspace/turbo 自动覆盖无需改。
- **诚实标注**：`parseWithFallback` 精确签名（`opts.endpoint` 必传，`packages/core/api/schema.ts:38-55`）、现有 api client/query hook 形状需读 `packages/core/api/` 参考实现（与 Plan 1/3 同类诚实标注）。

## 执行交接

实现顺序：Plan 1 → Plan 2 → Plan 3.5 Task 1（查询）→ Plan 3 → Plan 3.5 其余 → Plan 4 → Plan safety（内容审核 + 权益门禁）→ Plan 5（订阅 + Stripe，`2026-09-13-aurora-subscriptions-payments.md`，已编写）。

## 修订记录（2026-09-13 评审回写）

| 处 | 修正 |
|----|------|
| 契约表 | skills 加 `available`；transactions 加 `reference`；新增 Plan 3.5 的 generations/assets 端点；POST 补 402 语义 |
| Task 1 | schema 补 `available`/`reference`/generations/assets；`parseWithFallback` 签名注明 `opts.endpoint` 必传；malformed 测试补 fallback 用例；hooks 补轮询与 invalidation 面 |
| Task 2 | 目录置灰 `available=false`；composer 禁用 + 402 引导；works 用真实端点；i18n 改四语（en/zh-Hans/ko/ja + parity 测试） |
| Task 3 | 删除不存在的 `WorkspaceIdProvider`；补 `[workspaceSlug]` layout、OAuth callback 壳、`auth-cookie.ts`、`proxy.ts`、CI paths filter、`dev:aurora` 脚本；注明 pnpm-workspace/turbo 自动覆盖、`make up` 留二阶段 |
| Deferred | asset 占位移除（Plan 3.5 承担）；补实时化与 make up 边界 |
