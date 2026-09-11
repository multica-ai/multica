# Aurora 前端（apps/aurora + packages）— 实现计划（Plan 4）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 aurora 的前端设计（侧边栏 + 功能网格 + 抽屉任务窗 + 作品库 + 充值/账单）迁到 Multica 前端架构，作为 `apps/aurora` 对接 Plan 1–3 的后端 API。

**Architecture:** 三层复用 Multica 依赖方向：`packages/core/aurora`（类型 + zod schema + API client + React Query hooks）→ `packages/views/aurora`（共享业务页/组件）→ `apps/aurora`（Next.js App Router wiring）。state 规则照 CLAUDE.md：TanStack Query 管 server state（skills/generations/balance/transactions），Zustand 管 client state（当前视图/筛选/抽屉）。

**Tech Stack:** Next.js App Router、React 19、TanStack Query、Zustand、zod、`packages/ui`、`packages/core`（api/schema.ts 的 `parseWithFallback`）。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§8 前端）。

**API 契约（Plan 1–3 已定，前端据此写 schema）：**

| 端点 | 响应形状 |
|------|---------|
| `GET /api/aurora/skills` | `{skills:[{id,name,name_en,category,credits,input[],output[],featured}]}` |
| `POST /api/aurora/generations` `{skillId,prompt}` | `201 {generation:{id,skillId,prompt,status,creditsReserved}}` |
| `GET /api/aurora/billing/balance` | `{availableMicro}` |
| `GET /api/aurora/billing/transactions?limit=50` | `{transactions:[{id,kind,amountMicro,balanceAfterMicro,createdAt}]}` |

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
  - `type AuroraSkill = { id: string; name: string; nameEn: string; category: string; credits: number; input: string[]; output: string[]; featured: boolean }`
  - `type AuroraGeneration = { id: string; skillId: string; prompt: string; status: string; creditsReserved: number }`
  - `parseAuroraSkills` / `parseAuroraGeneration` / `parseAuroraBalance` / `parseAuroraTransactions`（zod schema + `parseWithFallback`）
  - `useAuroraSkills()`、`useAuroraBalance()`、`useAuroraTransactions()`、`useCreateAuroraGeneration()`

- [ ] **Step 1: 写 schema + malformed 测试**

`packages/core/aurora/schema.ts`：

```ts
import { z } from "zod";
import { parseWithFallback } from "../api/schema";

export const auroraSkillSchema = z.object({
  id: z.string(),
  name: z.string(),
  nameEn: z.string().default(""),
  category: z.string(),
  credits: z.number(),
  input: z.array(z.string()).default([]),
  output: z.array(z.string()).default([]),
  featured: z.boolean().default(false),
});
export type AuroraSkill = z.infer<typeof auroraSkillSchema>;

export const auroraSkillsSchema = z.object({ skills: z.array(auroraSkillSchema) });
export const auroraGenerationSchema = z.object({
  id: z.string(), skillId: z.string(), prompt: z.string(),
  status: z.string(), creditsReserved: z.number(),
});
// parseAuroraSkills = parseWithFallback(auroraSkillsSchema, { skills: [] }) ...
```

`packages/core/aurora/types.test.ts`（`// @vitest-environment node`）：

```ts
import { describe, expect, it } from "vitest";
import { auroraSkillsSchema } from "./schema";

describe("auroraSkillsSchema", () => {
  it("survives malformed skill entries with fallbacks", () => {
    const res = auroraSkillsSchema.parse({
      skills: [{ id: "poster", name: "海报制作", credits: 760, category: "image" }],
    });
    expect(res.skills[0].nameEn).toBe("");
    expect(res.skills[0].input).toEqual([]);
  });
});
```

- [ ] **Step 2/3: 跑测试（红→绿）+ 实现 api/queries/mutations + commit**

`api.ts` 用现有 api client 调四个端点；`queries.ts` 用 `useQuery`（workspace-scoped key 含 wsId 若需要）；`mutations.ts` 的 `useCreateAuroraGeneration` 用 `useMutation`，成功后 invalidate balance/transactions。

```bash
git add packages/core/aurora packages/core/package.json
git commit -m "feat(aurora): core types, schemas, api and query hooks"
```

---

### Task 2: `packages/views/aurora`（共享业务页）

**Files:**
- Create: `packages/views/aurora/skill-directory.tsx`（功能网格 + 分类 tab + 搜索，对应 aurora `page.tsx` 的 agents grid）
- Create: `packages/views/aurora/generation-composer.tsx`（抽屉任务窗，输入 prompt → `useCreateAuroraGeneration`）
- Create: `packages/views/aurora/works-list.tsx`（我的作品）
- Create: `packages/views/aurora/billing.tsx`（余额 + 流水 + 充值入口）
- Create: `packages/views/aurora/*.test.tsx`（组件 happy path）
- Create: i18n 文案（`packages/core/i18n` 的 zh/en，或 views 内 locale key）
- Modify: `packages/views/package.json`

**Interfaces:**
- Consumes: Task 1 的 hooks/类型；`packages/ui` 组件；`useNavigation()`/`<AppLink>`。
- Produces：页面组件 `SkillDirectory`、`GenerationComposer`、`WorksList`、`AuroraBilling`，由 `apps/aurora` 组装。

- [ ] **Step 1: 写组件 happy-path 测试**（渲染目录：断言 16 个 skill 渲染；composer：mock `useCreateAuroraGeneration` 断言提交后状态切换；billing：mock balance hook 断言余额展示）

> 用 `packages/views/*.test.tsx` 现有测试写法；mock `@multica/core/api` 与 stores 用 Zustand callable-store shape（CLAUDE.md Testing 规则）。测试不 mock `next/*`/`react-router-dom`。

- [ ] **Step 2/3: 实现 + 跑测试 + commit**（用 `--text-*` font token、语义 token；active/selected 态 hover 可辨识）

```bash
git add packages/views/aurora packages/views/package.json
git commit -m "feat(aurora): shared skill directory, composer, works and billing views"
```

---

### Task 3: `apps/aurora`（Next.js App Router wiring）

**Files:**
- Create: `apps/aurora/package.json`、`next.config.*`、`tsconfig.json`、`eslint.config.*`
- Create: `apps/aurora/app/layout.tsx`、`app/page.tsx`、`app/globals.css`（或复用 `packages/ui/styles`）
- Create: `apps/aurora/app/skills/page.tsx`（或单页内 view 切换）、`app/works/page.tsx`、`app/billing/page.tsx`
- Create: 平台 wiring：`NavigationProvider`、`WorkspaceIdProvider`、auth guard（复用 `packages/views/layout` 的 guard，如 `DashboardGuard`）
- Modify: `pnpm-workspace.yaml`（若需加 app）、根 `turbo.json`（若需加 task）、根 `package.json` 脚本

**Interfaces:**
- Consumes: Task 2 的 views；`packages/core`（auth/navigation/platform）。
- Produces：可运行的前端 app `apps/aurora`（`pnpm dev` 起）。

- [ ] **Step 1: 脚手架**（参照 `apps/web/` 的 package.json/next.config/tsconfig/layout 结构，新建 aurora 版；在 `pnpm-workspace.yaml` 加入 `apps/aurora`）

- [ ] **Step 2: 组装路由 + guard + provider**（个人空间路由：`/skills`、`/works`、`/billing`；登录/注册复用现有 auth 流，注册后自动进个人空间——Plan 1 Task 4 后端已保证）

- [ ] **Step 3: 验证 + commit**

Run: `pnpm typecheck`、`pnpm lint`、`pnpm test`（`packages/core`/`views` 的 aurora 测试）、`pnpm dev`（手动起 `apps/aurora` 点通 skills → 提交 generation → 看 balance）

```bash
git add apps/aurora pnpm-workspace.yaml package.json turbo.json
git commit -m "feat(aurora): Next.js app wiring for skill directory, works and billing"
```

---

## Deferred / 边界

- **桌面/移动端**：MVP 仅 Web（spec §13）。
- **作品库的 asset 下载/删除**：后端 `aurora_asset` 列表/删除端点（Plan 3 完成回写后补）未在本计划——前端 `WorksList` 先用 generation 列表 + asset 占位，接 Plan 3 完成后补齐。
- **充值 UI**：`billing.tsx` 只做余额 + 流水 + 充值入口占位；真实 Stripe checkout 属 Plan 2 Deferred。

## Self-Review

- **Spec 覆盖**：§8 的「单一事实来源 GET /api/aurora/skills」→ Task 1 schema + Task 2 目录；「POST /api/aurora/generations」→ Task 1 mutation + Task 2 composer；「余额/充值/用量」→ Task 2 billing；「apps/aurora」→ Task 3。
- **契约一致性**：Task 1 的 zod 字段名与 Plan 1/2 的 JSON 契约逐字段对齐（`name_en`、`creditsReserved`、`availableMicro`、`amountMicro`）。
- **诚实标注**：`parseWithFallback` 精确签名、现有 api client/query hook 形状需读 `packages/core/api/` 参考实现（与 Plan 1/3 同类诚实标注）。

## 执行交接

四个计划（Plan 1–4）全部完成。建议实现顺序：Plan 1 → Plan 2 → Plan 3 → Plan 4，每个计划结束跑对应测试全绿再进下一个。
