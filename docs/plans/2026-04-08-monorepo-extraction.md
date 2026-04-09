# Monorepo Extraction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract shared code into monorepo packages (`packages/core/`, `packages/ui/`, `packages/views/`), set up Turborepo, ensure `apps/web/` runs identically.

**Architecture:** Three packages, single-direction dependencies: `views/ → core/ + ui/`. Core is headless (zero react-dom). UI is atomic (zero business logic). Views is shared pages/components.

**Tech Stack:** pnpm workspaces + catalog, Turborepo, TypeScript internal packages (export TS source, no build), Tailwind CSS v4, shadcn/ui.

**Scope:** Monorepo extraction only. Desktop app is a separate future plan.

**Branch:** `feat/monorepo-extraction` (from latest `main` at f57cf44e)

---

## Work Breakdown

| Category | Files | Nature |
|---|---|---|
| Pure file moves | ~170 | Copy + fix relative imports |
| Code changes needed | ~17 | ApiClient callback, store factories, props refactor, nav adapter |
| Bulk import updates | ~140 consumer files | Mechanical find-and-replace |
| New files to create | ~15 | package.json, tsconfig, turbo.json, platform layer, nav adapter |

---

## Phase 1: Infrastructure (Tasks 1-3)

### Task 1: Turborepo + workspace

**Files:**
- Modify: `pnpm-workspace.yaml` — add `"packages/*"` to packages list, add `@tanstack/react-query` to catalog
- Create: `turbo.json`
- Modify: `package.json` (root) — add turbo devDep, update scripts to use turbo
- Modify: `.gitignore` — add `.turbo`

**turbo.json:**
```json
{
  "$schema": "https://turbo.build/schema.json",
  "tasks": {
    "build": {
      "dependsOn": ["^build"],
      "inputs": ["src/**", "app/**", "**/*.ts", "**/*.tsx", "**/*.css"],
      "outputs": [".next/**", "!.next/cache/**", "dist/**"]
    },
    "dev": { "cache": false, "persistent": true },
    "typecheck": { "dependsOn": ["^typecheck"] },
    "test": { "dependsOn": ["^typecheck"] },
    "lint": { "dependsOn": ["^typecheck"] }
  }
}
```

**Verify:** `pnpm typecheck` passes through turbo.

**Commit:** `chore: add Turborepo and configure workspace for packages/*`

---

### Task 2: Shared TypeScript config

**Files:**
- Create: `packages/tsconfig/package.json`
- Create: `packages/tsconfig/base.json`
- Create: `packages/tsconfig/react-library.json`

**base.json** — strict, ESNext, bundler resolution, declaration maps.
**react-library.json** — extends base, adds jsx: react-jsx and DOM lib.

All other packages will `"extends": "@multica/tsconfig/react-library.json"`.

**Commit:** `chore: add shared TypeScript config package`

---

### Task 3: Clean up empty package dirs

**Action:** `rm -rf packages/sdk packages/types packages/utils packages/ui`

These are leftover empty dirs (only contain node_modules).

---

## Phase 2: packages/core/ (Tasks 4-10)

### Task 4: Scaffold + move types/utils/logger

**Files:**
- Create: `packages/core/package.json` (name: @multica/core, deps: react, zustand, @tanstack/react-query, sonner)
- Create: `packages/core/tsconfig.json` (extends @multica/tsconfig/react-library.json)
- Move: `apps/web/shared/types/` → `packages/core/types/` (11 files, no changes needed)
- Move: `apps/web/shared/logger.ts` → `packages/core/logger.ts` (no changes)
- Move: `apps/web/shared/utils.ts` → `packages/core/utils.ts` (no changes)

**Verify:** `cd packages/core && npx tsc --noEmit`

---

### Task 5: Move API client (with onUnauthorized abstraction)

**Files:**
- Move: `apps/web/shared/api/ws-client.ts` → `packages/core/api/ws-client.ts` (no changes)
- Move: `apps/web/shared/api/client.ts` → `packages/core/api/client.ts` (**3 changes**)
- Create: `packages/core/api/index.ts`

**Code changes in client.ts:**
1. `import type { ... } from "@/shared/types"` → `from "../types"`
2. `import { ... } from "@/shared/logger"` → `from "../logger"`
3. Add `onUnauthorized?: () => void` to options, replace `handleUnauthorized()` body:
   ```typescript
   // Before: localStorage.removeItem + window.location.href = "/"
   // After:  this.token = null; this.workspaceId = null; this.options.onUnauthorized?.();
   ```

**NOT moved:** `apps/web/shared/api/index.ts` (the singleton) — replaced by `apps/web/platform/api.ts` in Task 9.

---

### Task 6: Move stores

**Pure moves (fix imports only):**
- `features/issues/store.ts` → `packages/core/issues/store.ts`
- `features/issues/config/*.ts` → `packages/core/issues/config/` — fix `@/shared/types` → `../../types`
- `features/issues/stores/view-store.ts` → `packages/core/issues/stores/view-store.ts` — fix imports
- `features/issues/stores/view-store-context.tsx` → `packages/core/issues/stores/view-store-context.tsx`
- `features/issues/stores/draft-store.ts` → `packages/core/issues/stores/draft-store.ts`
- `features/issues/stores/issues-scope-store.ts` → `packages/core/issues/stores/issues-scope-store.ts`
- `features/issues/stores/selection-store.ts` → `packages/core/issues/stores/selection-store.ts`
- `features/navigation/store.ts` → `packages/core/navigation/store.ts` (no changes)
- `features/modals/store.ts` → `packages/core/modals/store.ts` (no changes)

**Factory refactor (code changes):**
- `features/auth/store.ts` → `packages/core/auth/store.ts` — change to `createAuthStore({ api, onLogin?, onLogout? })` factory
- `features/workspace/store.ts` → `packages/core/workspace/store.ts` — change to `createWorkspaceStore(api)` factory

**Also move:**
- `features/workspace/hooks.ts` → `packages/core/workspace/hooks.ts` — fix imports to relative

**view-store.ts special handling:** The dynamic `import("@/features/workspace")` for workspace sync — change to accept workspace store instance via `registerViewStoreForWorkspaceSync(viewStore, workspaceStore)`.

---

### Task 7: Move TanStack Query modules

**Pure moves (fix import paths only):**
- `apps/web/core/issues/{queries,mutations,ws-updaters}.ts` → `packages/core/issues/`
- `apps/web/core/inbox/{queries,mutations,ws-updaters}.ts` → `packages/core/inbox/`
- `apps/web/core/workspace/{queries,mutations}.ts` → `packages/core/workspace/`
- `apps/web/core/runtimes/queries.ts` → `packages/core/runtimes/`
- `apps/web/core/query-client.ts` → `packages/core/query-client.ts`
- `apps/web/core/provider.tsx` → `packages/core/provider.tsx`

All changes: `@/shared/api` → `../api`, `@/shared/types` → `../types`, `@core/xxx` → `./xxx` or `../xxx`

**Code change:**
- `apps/web/core/hooks.ts` → `packages/core/hooks.ts` — refactor `useWorkspaceId()` to use React Context instead of importing workspace store directly:
  ```typescript
  const WorkspaceIdContext = createContext<string | null>(null);
  export function WorkspaceIdProvider({ wsId, children }) { ... }
  export function useWorkspaceId() { return useContext(WorkspaceIdContext); }
  ```

---

### Task 8: Move realtime + shared hooks

**Pure moves (fix imports):**
- `features/realtime/hooks.ts` → `packages/core/realtime/hooks.ts`
- `features/realtime/use-realtime-sync.ts` → `packages/core/realtime/use-realtime-sync.ts`
- `shared/hooks/use-file-upload.ts` → `packages/core/hooks/use-file-upload.ts`

**Code change:**
- `features/realtime/provider.tsx` → `packages/core/realtime/provider.tsx` — accept `wsUrl` prop instead of reading `process.env.NEXT_PUBLIC_WS_URL`

**Note:** `use-realtime-sync.ts` needs auth/workspace store access. Since these are now factories, the realtime provider should receive the store instances. Simplest: WSProvider accepts `authStore` and `workspaceStore` props, passes them to `useRealtimeSync`.

---

### Task 9: Create platform bridge in apps/web/

**New files (all new code):**
- `apps/web/platform/api.ts` — creates api singleton with `NEXT_PUBLIC_API_URL`, `onUnauthorized` with `window.location.href`
- `apps/web/platform/auth.ts` — `export const useAuthStore = createAuthStore({ api, onLogin: setLoggedInCookie, onLogout: clearLoggedInCookie })`
- `apps/web/platform/workspace.ts` — `export const useWorkspaceStore = createWorkspaceStore(api)`
- `apps/web/platform/index.ts` — re-exports

---

### Task 10: Update imports in apps/web/ + delete old files

**Bulk find-and-replace across ~94 files:**

| Pattern | Replacement |
|---|---|
| `@/shared/types` | `@multica/core/types` |
| `@/shared/api"` (singleton usage) | `@/platform/api"` |
| `@/shared/logger` | `@multica/core/logger` |
| `@/shared/utils` | `@multica/core/utils` |
| `@/shared/hooks/` | `@multica/core/hooks/` |
| `@core/` | `@multica/core/` |
| `@/features/auth"` (useAuthStore) | `@/platform/auth"` |
| `@/features/workspace"` (useWorkspaceStore) | `@/platform/workspace"` |
| `@/features/workspace"` (useActorName) | `@multica/core/workspace/hooks"` |
| `@/features/realtime` | `@multica/core/realtime` |
| `@/features/navigation` | `@multica/core/navigation` |
| `@/features/modals"` (store) | `@multica/core/modals"` |
| `@/features/issues/store` | `@multica/core/issues` |
| `@/features/issues/stores/` | `@multica/core/issues/stores/` |
| `@/features/issues/config` | `@multica/core/issues/config` |

**Also:**
- Add `"@multica/core": "workspace:*"` to `apps/web/package.json`
- Add `transpilePackages: ["@multica/core"]` to `next.config.ts`
- Remove `"@core/*"` alias from `apps/web/tsconfig.json`

**Delete old files:**
```
apps/web/shared/types/, apps/web/shared/api/, apps/web/shared/logger.ts,
apps/web/shared/utils.ts, apps/web/shared/hooks/, apps/web/core/,
features/auth/store.ts, features/workspace/store.ts, features/workspace/hooks.ts,
features/realtime/, features/navigation/store.ts, features/modals/store.ts,
features/issues/store.ts, features/issues/stores/, features/issues/config/
```

**Keep:** `features/auth/auth-cookie.ts`, `features/auth/initializer.tsx`, `features/landing/`

**Verify:** `pnpm typecheck && pnpm test`

**Commit:** `feat(core): extract packages/core — headless business logic layer`

---

## Phase 3: packages/ui/ (Tasks 11-16)

### Task 11: Scaffold packages/ui/

**Files:**
- Create: `packages/ui/package.json` (name: @multica/ui, deps: all @radix-ui/*, clsx, tailwind-merge, lucide-react, emoji-mart, react-markdown, shiki, etc.)
- Create: `packages/ui/tsconfig.json` (extends shared config, with `@/lib/utils`, `@/hooks/*`, `@/components/ui/*` path aliases for internal shadcn imports)
- Create: `packages/ui/components.json` (shadcn config for this package)

---

### Task 12: Move shadcn + lib + hooks

**Pure moves (no code changes):**
- `apps/web/components/ui/*.tsx` (56 files) → `packages/ui/components/ui/`
- `apps/web/lib/utils.ts` → `packages/ui/lib/utils.ts`
- `apps/web/hooks/{use-auto-scroll,use-mobile,use-scroll-fade}.ts` → `packages/ui/hooks/`

---

### Task 13: Extract CSS tokens

- Copy `@theme inline { ... }` + `:root` + `.dark` blocks from `globals.css` → `packages/ui/styles/tokens.css`
- Update `globals.css`: replace inline tokens with `@import "@multica/ui/styles/tokens.css"` + add `@source` directives for packages

---

### Task 14: Refactor + move common components

**Code changes (3 files):**
- `actor-avatar.tsx` — remove `useActorName()`, accept `name/initials/avatarUrl/isAgent` props
- `mention-hover-card.tsx` — remove `useQuery`, accept resolved data props
- `reaction-bar.tsx` — remove `useActorName()`, add `getActorName` prop

**Pure moves (3 files):**
- `file-upload-button.tsx`, `emoji-picker.tsx`, `quick-emoji-picker.tsx` → direct copy

All go to `packages/ui/components/common/`.

---

### Task 15: Move markdown components

**Code change (1 file):**
- `Markdown.tsx` — add `renderMention?: (props: { type: string; id: string }) => ReactNode` prop, remove hardcoded `IssueMentionCard` import

**Pure moves (5 files):**
- `CodeBlock.tsx`, `StreamingMarkdown.tsx`, `linkify.ts`, `mentions.ts`, `index.ts`

All go to `packages/ui/markdown/`.

---

### Task 16: Update imports + delete old files

**Bulk find-and-replace across ~118 files:**

| Pattern | Replacement |
|---|---|
| `@/components/ui/` | `@multica/ui/components/ui/` |
| `@/components/common/` | `@multica/ui/components/common/` |
| `@/components/markdown` | `@multica/ui/markdown` |
| `@/lib/utils` | `@multica/ui/lib/utils` |
| `@/hooks/use-mobile` | `@multica/ui/hooks/use-mobile` |
| `@/hooks/use-auto-scroll` | `@multica/ui/hooks/use-auto-scroll` |
| `@/hooks/use-scroll-fade` | `@multica/ui/hooks/use-scroll-fade` |

**Also:**
- Add `"@multica/ui": "workspace:*"` to `apps/web/package.json`
- Add `"@multica/ui"` to `transpilePackages` in `next.config.ts`
- Update `apps/web/components.json` aliases to point to `@multica/ui`

**Delete:** `components/ui/`, `components/common/`, `components/markdown/`, `hooks/`, `lib/utils.ts`

**Keep:** `components/{theme-provider,theme-toggle,multica-icon,loading-indicator,spinner,locale-sync}.tsx`

**Verify:** `pnpm typecheck && pnpm test`

**Commit:** `feat(ui): extract packages/ui — shared atomic UI layer`

---

## Phase 4: packages/views/ + navigation (Tasks 17-22)

### Task 17: Create navigation adapter

**New files (all new code, ~60 lines total):**
- `packages/views/package.json` (deps: @multica/core, @multica/ui, @dnd-kit/*, @tiptap/*, sonner, recharts)
- `packages/views/tsconfig.json`
- `packages/views/navigation/types.ts` — `NavigationAdapter` interface (push, replace, back, pathname, searchParams)
- `packages/views/navigation/context.tsx` — `NavigationProvider` + `useNavigation()` hook
- `packages/views/navigation/app-link.tsx` — `<AppLink>` component (replaces `next/link`)
- `packages/views/navigation/index.ts`

---

### Task 18: Create WebNavigationProvider

**New file:**
- `apps/web/platform/navigation.tsx` — wraps `useRouter`/`usePathname`/`useSearchParams` into `NavigationAdapter`

Wire into dashboard layout.

---

### Task 19: Move feature UI components

**Next.js decouple (7 files, ~2 lines each):**

| File | Import change | JSX change |
|---|---|---|
| `issue-mention-card.tsx` | `next/link` → `../navigation` | `<Link` → `<AppLink` |
| `board-card.tsx` | same | same |
| `list-row.tsx` | same | same |
| `issue-detail.tsx` | `next/link` + `next/navigation` → `../navigation` | `<Link` → `<AppLink`, `router.push` → `nav.push` |
| `create-issue.tsx` | `next/navigation` → `../navigation` | `router.push` → `nav.push` |
| `create-workspace.tsx` | same | same |

**Pure moves (~85 files, fix import paths only):**
- `features/issues/components/` (24 files) → `packages/views/issues/components/`
- `features/issues/hooks/` (3 files) → `packages/views/issues/hooks/`
- `features/issues/utils/` (5 files) → `packages/views/issues/utils/`
- `features/editor/` (16 files incl CSS) → `packages/views/editor/`
- `features/modals/{create-issue,create-workspace,registry}.tsx` → `packages/views/modals/`
- `features/my-issues/` (4 files) → `packages/views/my-issues/`
- `features/skills/` (5 files) → `packages/views/skills/`
- `features/runtimes/` (16 files) → `packages/views/runtimes/`
- `features/workspace/components/workspace-avatar.tsx` → `packages/views/workspace/`

---

### Task 20: Extract fat pages

Move logic from page.tsx files into packages/views/:

| Page | Lines | Target |
|---|---|---|
| `(dashboard)/agents/page.tsx` | 1,280 | `packages/views/agents/agents-page.tsx` |
| `(dashboard)/inbox/page.tsx` | 468 | `packages/views/inbox/inbox-page.tsx` |
| `(auth)/login/page.tsx` | 389 | `packages/views/auth/login-page.tsx` |

Each original page.tsx becomes a 3-line thin shell:
```typescript
"use client";
import { AgentsPage } from "@multica/views/agents";
export default function Page() { return <AgentsPage />; }
```

Login page: pass `googleClientId` as prop instead of reading env var.

---

### Task 21: Update imports + delete old files

**Bulk find-and-replace across ~18 files:**

| Pattern | Replacement |
|---|---|
| `@/features/issues/components` | `@multica/views/issues/components` |
| `@/features/issues/hooks/` | `@multica/views/issues/hooks/` |
| `@/features/editor` | `@multica/views/editor` |
| `@/features/modals/` (components) | `@multica/views/modals/` |
| `@/features/my-issues` | `@multica/views/my-issues` |
| `@/features/skills` | `@multica/views/skills` |
| `@/features/runtimes` | `@multica/views/runtimes` |

**Also:**
- Add `"@multica/views": "workspace:*"` to `apps/web/package.json`
- Add `"@multica/views"` to `transpilePackages`
- Add `@source "../../packages/views/**/*.tsx"` to `globals.css`

**Delete old feature files.**

**Verify:** `pnpm typecheck && pnpm test`

**Commit:** `feat(views): extract packages/views — shared business UI + navigation adapter`

---

### Task 22: Final verification

```bash
make check                    # typecheck + unit tests + Go tests + E2E
cd apps/web && npx shadcn@latest add --dry-run badge   # shadcn CLI works

# Package constraints
grep -r "@multica/core" packages/ui/ || echo "PASS: ui/ has zero core imports"
grep -r "react-dom" packages/core/ || echo "PASS: core/ has zero react-dom"
grep -r "from \"next/" packages/views/ || echo "PASS: views/ has zero next/* imports"
```

**Commit:** `chore: monorepo extraction complete — all checks pass`

---

## Final Directory Structure

```
multica/
├── packages/
│   ├── tsconfig/          # Shared TS config
│   ├── core/              # @multica/core — 三端共用 (零 react-dom)
│   │   ├── api/           #   ApiClient class + WSClient
│   │   ├── types/         #   所有领域类型
│   │   ├── auth/          #   createAuthStore factory
│   │   ├── workspace/     #   createWorkspaceStore factory + useActorName
│   │   ├── issues/        #   stores, config, queries, mutations, ws-updaters
│   │   ├── inbox/         #   queries, mutations, ws-updaters
│   │   ├── runtimes/      #   queries
│   │   ├── realtime/      #   WSProvider, hooks, sync
│   │   ├── navigation/    #   useNavigationStore
│   │   ├── modals/        #   useModalStore
│   │   └── hooks.ts       #   useWorkspaceId (Context-based)
│   ├── ui/                # @multica/ui — Web+Desktop 共用 (零业务逻辑)
│   │   ├── components/ui/ #   56 shadcn 组件
│   │   ├── components/common/ # actor-avatar, emoji-picker... (纯 props)
│   │   ├── markdown/      #   Markdown, StreamingMarkdown (renderMention slot)
│   │   ├── hooks/         #   use-auto-scroll, use-mobile, use-scroll-fade
│   │   ├── lib/utils.ts   #   cn()
│   │   └── styles/tokens.css
│   └── views/             # @multica/views — Web+Desktop 共用页面
│       ├── navigation/    #   NavigationAdapter + AppLink
│       ├── issues/        #   IssuesPage, IssueDetail, BoardView...
│       ├── editor/        #   ContentEditor, TitleEditor
│       ├── modals/        #   CreateIssue, CreateWorkspace
│       ├── agents/        #   AgentsPage (从 1280 行 page.tsx 提取)
│       ├── inbox/         #   InboxPage (从 468 行 page.tsx 提取)
│       ├── auth/          #   LoginPage (从 389 行 page.tsx 提取)
│       ├── my-issues/     #   MyIssuesPage
│       ├── skills/        #   SkillsPage
│       └── runtimes/      #   RuntimesPage
├── apps/
│   └── web/
│       ├── app/           # Next.js 路由薄壳 (每个 page < 15 行)
│       ├── platform/      # Web 平台适配 (api 单例, auth store, nav provider)
│       ├── features/
│       │   ├── auth/      #   auth-cookie.ts (Web 独有) + initializer.tsx
│       │   └── landing/   #   Landing 页面 (Web 独有, 用 next/image)
│       └── components/    #   theme-provider, multica-icon 等 app 级组件
├── turbo.json
└── pnpm-workspace.yaml
```

---

## Execution Order & Commits

| # | Commit | 影响范围 | 风险 |
|---|---|---|---|
| 1 | `chore: Turborepo + workspace` | 配置文件 | 低 |
| 2 | `chore: shared TypeScript config` | 新文件 | 低 |
| 3 | `feat(core): extract packages/core` | 94 文件 import 变更 | 中 — 最大批量替换 |
| 4 | `feat(ui): extract packages/ui` | 118 文件 import 变更 | 中 — 最多文件 |
| 5 | `feat(views): extract packages/views` | 18 文件 + 3 胖壳 | 中 |
| 6 | `chore: final verification` | 0 | 低 |
