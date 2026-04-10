# Monorepo Full Extraction Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让每个 app 只剩路由定义 + NavigationAdapter + 真正独有的功能（landing page、title bar、cookie）。所有业务逻辑、UI、状态管理、API、WS 全部在共享包里，零重复。

**核心洞察:** Electron renderer 就是浏览器。localStorage、fetch、WebSocket 和 Next.js 客户端页面完全一样。URL 是环境配置不是 app 差异。所以除了 NavigationAdapter（路由框架不同），没有任何东西需要在每个 app 里单独写。

**Architecture:** `@multica/core` 自带完整初始化（API、stores、WS），不需要每个 app 调用 factory。`@multica/views` 包含所有页面和 layout。每个 app 只提供路由壳子。

**Tech Stack:** React 19, TanStack Query, Zustand, Tailwind CSS v4, shadcn/ui, TypeScript strict mode.

**Branch:** `feat/monorepo-extraction` (from latest `feat/desktop-app`)

---

## Work Breakdown

| Phase | Tasks | What it achieves |
|---|---|---|
| Phase 1: Core 自包含初始化 | 1-2 | core 自己初始化 API/stores/WS，app 不需要写任何 platform 代码 |
| Phase 2: Sidebar & Layout | 3-5 | 共享 AppSidebar + DashboardLayout，删除两端重复 |
| Phase 3: Login | 6-7 | 共享 LoginPage + AuthInitializer |
| Phase 4: Agents | 8-10 | 1,279 行 → 共享模块 |
| Phase 5: Inbox | 11-13 | 468 行 → 共享模块 |
| Phase 6: Settings | 14-16 | 1,277 行 → 共享模块 |
| Phase 7: 清理 | 17-18 | 删除所有 platform 目录、placeholder、死代码 |

---

## Phase 1: Core 自包含初始化

### 设计思路

现在每个 app 都要手动调用 `new ApiClient()`、`createAuthStore()`、`createWorkspaceStore()`、包 `<WSProvider>`。但这些逻辑在两个 app 里完全一样。

方案：`@multica/core` 导出一个 `<CoreProvider>` 包裹整个应用。它内部自动完成所有初始化。配置通过环境变量（`VITE_API_URL` / `NEXT_PUBLIC_API_URL`）或 prop 注入。SSR-safe 的 localStorage wrapper 内置到 core 里作为默认 storage（`typeof window` 守卫对 Electron 无害）。

```tsx
// 任何 app 的根组件，只需要这样：
<CoreProvider
  apiBaseUrl={import.meta.env.VITE_API_URL ?? ""}
  wsUrl={import.meta.env.VITE_WS_URL ?? "ws://localhost:8080/ws"}
  onLogin={setLoggedInCookie}   // 可选，Web 独有
  onLogout={clearLoggedInCookie} // 可选，Web 独有
>
  {children}
</CoreProvider>
```

Desktop 更简单（没有可选回调）：
```tsx
<CoreProvider
  apiBaseUrl={import.meta.env.VITE_API_URL ?? "http://localhost:8080"}
  wsUrl={import.meta.env.VITE_WS_URL ?? "ws://localhost:8080/ws"}
>
  {children}
</CoreProvider>
```

### Task 1: 在 `@multica/core` 里创建 CoreProvider

**Files:**
- Create: `packages/core/platform/storage.ts` — 内置 SSR-safe localStorage
- Create: `packages/core/platform/core-provider.tsx` — CoreProvider 组件
- Create: `packages/core/platform/auth-initializer.tsx` — 共享 AuthInitializer
- Create: `packages/core/platform/types.ts` — CoreProviderProps
- Create: `packages/core/platform/index.ts` — barrel export
- Modify: `packages/core/package.json` — add `"./platform"` export

**Step 1: Create built-in SSR-safe storage**

```typescript
// packages/core/platform/storage.ts
import type { StorageAdapter } from "../types/storage";

/** SSR-safe localStorage. Works in both Next.js (SSR) and Electron (always client). */
export const defaultStorage: StorageAdapter = {
  getItem: (k) => (typeof window !== "undefined" ? localStorage.getItem(k) : null),
  setItem: (k, v) => { if (typeof window !== "undefined") localStorage.setItem(k, v); },
  removeItem: (k) => { if (typeof window !== "undefined") localStorage.removeItem(k); },
};
```

**Step 2: Create types**

```typescript
// packages/core/platform/types.ts
export interface CoreProviderProps {
  children: React.ReactNode;
  /** API base URL. Default: "" (same-origin). */
  apiBaseUrl?: string;
  /** WebSocket URL. Default: "ws://localhost:8080/ws". */
  wsUrl?: string;
  /** Called after successful login (e.g. set cookie for Next.js middleware). */
  onLogin?: () => void;
  /** Called after logout (e.g. clear cookie). */
  onLogout?: () => void;
}
```

**Step 3: Create AuthInitializer**

Merge the identical logic from both apps. Uses `defaultStorage`, reads from existing singletons.

```typescript
// packages/core/platform/auth-initializer.tsx
import { useEffect, type ReactNode } from "react";
import { getApi } from "../api";
import { useAuthStore } from "../auth";
import { useWorkspaceStore } from "../workspace";
import { createLogger } from "../logger";
import { defaultStorage } from "./storage";

const logger = createLogger("auth");

export function AuthInitializer({
  children,
  onLogin,
  onLogout,
}: {
  children: ReactNode;
  onLogin?: () => void;
  onLogout?: () => void;
}) {
  useEffect(() => {
    const token = defaultStorage.getItem("multica_token");
    if (!token) {
      onLogout?.();
      useAuthStore.setState({ isLoading: false });
      return;
    }

    const api = getApi();
    api.setToken(token);
    const wsId = defaultStorage.getItem("multica_workspace_id");

    Promise.all([api.getMe(), api.listWorkspaces()])
      .then(([user, wsList]) => {
        onLogin?.();
        useAuthStore.setState({ user, isLoading: false });
        useWorkspaceStore.getState().hydrateWorkspace(wsList, wsId);
      })
      .catch((err) => {
        logger.error("auth init failed", err);
        api.setToken(null);
        api.setWorkspaceId(null);
        defaultStorage.removeItem("multica_token");
        defaultStorage.removeItem("multica_workspace_id");
        onLogout?.();
        useAuthStore.setState({ user: null, isLoading: false });
      });
  }, []);

  return <>{children}</>;
}
```

**Step 4: Create CoreProvider**

This is the one component that wires everything together. Each app wraps its root with this.

```typescript
// packages/core/platform/core-provider.tsx
"use client";

import { type ReactNode, useMemo } from "react";
import { ApiClient } from "../api/client";
import { setApiInstance } from "../api";
import { createAuthStore, registerAuthStore } from "../auth";
import { createWorkspaceStore, registerWorkspaceStore } from "../workspace";
import { WSProvider } from "../realtime";
import { QueryProvider } from "../provider";
import { createLogger } from "../logger";
import { defaultStorage } from "./storage";
import { AuthInitializer } from "./auth-initializer";
import type { CoreProviderProps } from "./types";

// Module-level singletons — created once, shared across renders.
let initialized = false;
let authStore: ReturnType<typeof createAuthStore>;
let workspaceStore: ReturnType<typeof createWorkspaceStore>;

function initCore(apiBaseUrl: string) {
  if (initialized) return;

  const api = new ApiClient(apiBaseUrl, {
    logger: createLogger("api"),
    onUnauthorized: () => {
      defaultStorage.removeItem("multica_token");
      defaultStorage.removeItem("multica_workspace_id");
    },
  });
  setApiInstance(api);

  // Hydrate token from storage
  const token = defaultStorage.getItem("multica_token");
  if (token) api.setToken(token);
  const wsId = defaultStorage.getItem("multica_workspace_id");
  if (wsId) api.setWorkspaceId(wsId);

  authStore = createAuthStore({ api, storage: defaultStorage });
  registerAuthStore(authStore);

  workspaceStore = createWorkspaceStore(api, {
    storage: defaultStorage,
  });
  registerWorkspaceStore(workspaceStore);

  initialized = true;
}

export function CoreProvider({
  children,
  apiBaseUrl = "",
  wsUrl = "ws://localhost:8080/ws",
  onLogin,
  onLogout,
}: CoreProviderProps) {
  // Initialize singletons on first render
  useMemo(() => initCore(apiBaseUrl), [apiBaseUrl]);

  return (
    <QueryProvider>
      <AuthInitializer onLogin={onLogin} onLogout={onLogout}>
        <WSProvider
          wsUrl={wsUrl}
          authStore={authStore}
          workspaceStore={workspaceStore}
          storage={defaultStorage}
        >
          {children}
        </WSProvider>
      </AuthInitializer>
    </QueryProvider>
  );
}
```

**Step 5: Barrel export + package.json**

```typescript
// packages/core/platform/index.ts
export { CoreProvider } from "./core-provider";
export type { CoreProviderProps } from "./types";
export { AuthInitializer } from "./auth-initializer";
export { defaultStorage } from "./storage";
```

Add to `packages/core/package.json` exports:
```json
"./platform": "./platform/index.ts"
```

**Step 6: Run typecheck**

Run: `pnpm typecheck`
Expected: PASS

**Step 7: Commit**

```bash
git add packages/core/platform/ packages/core/package.json
git commit -m "feat(core): add CoreProvider — single component for full app initialization"
```

---

### Task 2: Migrate both apps to CoreProvider

**Files:**
- Modify: `apps/web/app/layout.tsx` — replace all providers with `<CoreProvider>`
- Modify: `apps/desktop/src/renderer/src/App.tsx` — replace all providers with `<CoreProvider>`
- Delete: `apps/web/platform/api.ts`
- Delete: `apps/web/platform/auth.ts`
- Delete: `apps/web/platform/workspace.ts`
- Delete: `apps/web/platform/storage.ts`
- Delete: `apps/web/platform/ws-provider.tsx`
- Delete: `apps/web/features/auth/initializer.tsx`
- Delete: `apps/desktop/src/renderer/src/platform/api.ts`
- Delete: `apps/desktop/src/renderer/src/platform/auth.ts`
- Delete: `apps/desktop/src/renderer/src/platform/workspace.ts`
- Delete: `apps/desktop/src/renderer/src/platform/storage.ts`
- Delete: `apps/desktop/src/renderer/src/platform/ws-provider.tsx`
- Delete: `apps/desktop/src/renderer/src/platform/auth-initializer.tsx`
- Keep: `apps/web/platform/navigation.tsx` — NavigationAdapter (唯一不可共享)
- Keep: `apps/desktop/src/renderer/src/platform/navigation.tsx` — NavigationAdapter
- Keep: `apps/web/features/auth/auth-cookie.ts` — Web 独有

**Step 1: Update web root layout**

```typescript
// apps/web/app/layout.tsx
import { CoreProvider } from "@multica/core/platform";
import { WebNavigationProvider } from "@/platform/navigation";
import { setLoggedInCookie, clearLoggedInCookie } from "@/features/auth/auth-cookie";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "sonner";

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body>
        <ThemeProvider>
          <CoreProvider
            apiBaseUrl={process.env.NEXT_PUBLIC_API_URL}
            wsUrl={process.env.NEXT_PUBLIC_WS_URL}
            onLogin={setLoggedInCookie}
            onLogout={clearLoggedInCookie}
          >
            <WebNavigationProvider>
              {children}
            </WebNavigationProvider>
          </CoreProvider>
          <Toaster />
        </ThemeProvider>
      </body>
    </html>
  );
}
```

**Step 2: Update desktop App.tsx**

```typescript
// apps/desktop/src/renderer/src/App.tsx
import { RouterProvider } from "react-router-dom";
import { CoreProvider } from "@multica/core/platform";
import { ThemeProvider } from "./components/theme-provider";
import { Toaster } from "sonner";
import { router } from "./router";

export function App() {
  return (
    <ThemeProvider>
      <CoreProvider
        apiBaseUrl={import.meta.env.VITE_API_URL}
        wsUrl={import.meta.env.VITE_WS_URL}
      >
        <RouterProvider router={router} />
      </CoreProvider>
      <Toaster />
    </ThemeProvider>
  );
}
```

**Step 3: Fix all `@/platform/*` imports across both apps**

Search all files for:
- `from "@/platform/api"` → `from "@multica/core/api"` (use singleton proxy `api`)
- `from "@/platform/auth"` → `from "@multica/core/auth"` (use singleton `useAuthStore`)
- `from "@/platform/workspace"` → `from "@multica/core/workspace"` (use singleton `useWorkspaceStore`)

These singletons already exist and are registered by CoreProvider on init. Every component can import them directly from core.

**Step 4: Delete all platform files except navigation**

Web — delete entire `apps/web/platform/` except `navigation.tsx`. Flatten:
```
apps/web/platform/navigation.tsx  →  keep (only file left)
```

Desktop — delete entire `apps/desktop/.../platform/` except `navigation.tsx`. Flatten:
```
apps/desktop/.../platform/navigation.tsx  →  keep (only file left)
```

**Step 5: Run typecheck + tests**

Run: `pnpm typecheck && pnpm test`
Expected: PASS

**Step 6: Commit**

```bash
git commit -m "refactor: migrate both apps to CoreProvider — delete all platform duplication"
```

---

## Phase 2: Sidebar & Layout

### Task 3: Extract `AppSidebar` to `@multica/views/layout`

**Why:** Web and Desktop sidebars are 99% identical (239 vs 236 lines). Only difference: `Link`/`usePathname`/`useRouter` (web) vs `AppLink`/`useNavigation` (desktop). Since `useNavigation` + `AppLink` is the abstraction in views, the desktop version is already the correct shared version.

**Files:**
- Create: `packages/views/layout/app-sidebar.tsx` — copy from desktop version
- Create: `packages/views/layout/index.ts`
- Modify: `packages/views/package.json` (add `"./layout"` export)
- Modify: `apps/web/app/(dashboard)/layout.tsx` — import from views
- Modify: `apps/desktop/src/renderer/src/components/dashboard-shell.tsx` — import from views
- Delete: `apps/web/app/(dashboard)/_components/app-sidebar.tsx`
- Delete: `apps/desktop/src/renderer/src/components/app-sidebar.tsx`

**Step 1: Create shared AppSidebar**

Copy desktop `app-sidebar.tsx` into `packages/views/layout/app-sidebar.tsx`. Key changes:
- `import { useAuthStore } from "@multica/core/auth"` (singleton)
- `import { useWorkspaceStore } from "@multica/core/workspace"` (singleton)
- `import { api } from "@multica/core/api"` (singleton proxy)
- `import { useNavigation, AppLink } from "../navigation"` (relative within views)
- `import { useModalStore } from "@multica/core/modals"`
- All `@multica/ui` imports unchanged

**Step 2: Barrel export + package.json**

```typescript
// packages/views/layout/index.ts
export { AppSidebar } from "./app-sidebar";
```

Add to `packages/views/package.json`:
```json
"./layout": "./layout/index.ts"
```

**Step 3: Update both apps, delete old files**

**Step 4: Run typecheck**

Run: `pnpm typecheck`

**Step 5: Commit**

```bash
git commit -m "refactor(views): extract shared AppSidebar to @multica/views/layout"
```

---

### Task 4: Extract `DashboardLayout` to `@multica/views/layout`

**Why:** Both apps have identical dashboard shell: auth guard → loading → sidebar + workspace provider + content. Only differences: web has `SearchCommand`, desktop has `TitleBar`. These are slots.

**Files:**
- Create: `packages/views/layout/dashboard-layout.tsx`
- Modify: `packages/views/layout/index.ts` (add export)
- Modify: `apps/web/app/(dashboard)/layout.tsx` (~10 lines after)
- Modify: `apps/desktop/src/renderer/src/components/dashboard-shell.tsx` (~10 lines after)

**Step 1: Create shared DashboardLayout**

```typescript
// packages/views/layout/dashboard-layout.tsx
"use client";

import { useEffect, type ReactNode } from "react";
import { useNavigationStore } from "@multica/core/navigation";
import { SidebarProvider, SidebarInset } from "@multica/ui/components/ui/sidebar";
import { WorkspaceIdProvider } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { ModalRegistry } from "../modals/registry";
import { useNavigation } from "../navigation";
import { AppSidebar } from "./app-sidebar";

interface DashboardLayoutProps {
  children: ReactNode;
  /** Above sidebar (e.g. desktop TitleBar) */
  header?: ReactNode;
  /** Sibling of SidebarInset (e.g. web SearchCommand) */
  extra?: ReactNode;
  /** Loading indicator */
  loadingIndicator?: ReactNode;
  /** Redirect path when not authenticated. Default: "/" */
  loginPath?: string;
}

export function DashboardLayout({
  children, header, extra, loadingIndicator, loginPath = "/",
}: DashboardLayoutProps) {
  const { pathname, push } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useWorkspaceStore((s) => s.workspace);

  useEffect(() => {
    if (!isLoading && !user) push(loginPath);
  }, [user, isLoading, push, loginPath]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(pathname);
  }, [pathname]);

  if (isLoading) {
    return (
      <div className="flex h-screen flex-col">
        {header}
        <div className="flex flex-1 items-center justify-center">
          {loadingIndicator}
        </div>
      </div>
    );
  }

  if (!user) return null;

  return (
    <div className="flex h-screen flex-col">
      {header}
      <div className="flex flex-1 min-h-0">
        <SidebarProvider className="flex-1">
          <AppSidebar />
          <SidebarInset className="overflow-hidden">
            {workspace ? (
              <WorkspaceIdProvider wsId={workspace.id}>
                {children}
                <ModalRegistry />
              </WorkspaceIdProvider>
            ) : (
              <div className="flex flex-1 items-center justify-center">
                {loadingIndicator}
              </div>
            )}
          </SidebarInset>
          {extra}
        </SidebarProvider>
      </div>
    </div>
  );
}
```

**Step 2: Slim down web layout**

```typescript
// apps/web/app/(dashboard)/layout.tsx
"use client";
import { DashboardLayout } from "@multica/views/layout";
import { MulticaIcon } from "@/components/multica-icon";
import { SearchCommand } from "@/features/search";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardLayout
      loadingIndicator={<MulticaIcon className="size-6" />}
      extra={<SearchCommand />}
    >
      {children}
    </DashboardLayout>
  );
}
```

**Step 3: Slim down desktop shell**

```typescript
// apps/desktop/src/renderer/src/components/dashboard-shell.tsx
import { Outlet } from "react-router-dom";
import { DesktopNavigationProvider } from "@/platform/navigation";
import { DashboardLayout } from "@multica/views/layout";
import { TitleBar } from "./title-bar";
import { MulticaIcon } from "./multica-icon";

export function DashboardShell() {
  return (
    <DesktopNavigationProvider>
      <DashboardLayout
        header={<TitleBar />}
        loginPath="/login"
        loadingIndicator={<MulticaIcon className="size-6" />}
      >
        <Outlet />
      </DashboardLayout>
    </DesktopNavigationProvider>
  );
}
```

**Step 4: Run typecheck**

Run: `pnpm typecheck`

**Step 5: Commit**

```bash
git commit -m "refactor(views): extract shared DashboardLayout to @multica/views/layout"
```

---

### Task 5: Build + smoke test

Run: `pnpm build && make check`

Fix any issues, commit:
```bash
git commit -m "fix: fixups from layout extraction"
```

---

## Phase 3: Shared Login Page

### Task 6: Extract `LoginPage` to `@multica/views/auth`

**Why:** Desktop login (139 lines) is a simple email/code form. Web login (393 lines) has extra: CLI callback, Google OAuth, OTP component. Strategy: extract the core email/code form to views. Desktop uses it directly. Web keeps its own richer version (too different to merge).

**Files:**
- Create: `packages/views/auth/login-page.tsx`
- Create: `packages/views/auth/index.ts`
- Modify: `packages/views/package.json` (add `"./auth"` export)
- Modify: `apps/desktop/src/renderer/src/pages/login.tsx` (~10 lines after)

**Step 1: Create shared LoginPage**

Props: `logo?: ReactNode`, `onSuccess: () => void`. Internally uses `useAuthStore`/`useWorkspaceStore`/`api` from core singletons.

**Step 2: Update desktop login**

```typescript
import { useNavigate } from "react-router-dom";
import { LoginPage } from "@multica/views/auth";
import { MulticaIcon } from "../components/multica-icon";
import { TitleBar } from "../components/title-bar";

export function DesktopLoginPage() {
  const navigate = useNavigate();
  return (
    <div className="flex h-screen flex-col">
      <TitleBar />
      <LoginPage
        logo={<MulticaIcon bordered size="lg" />}
        onSuccess={() => navigate("/issues", { replace: true })}
      />
    </div>
  );
}
```

Web login stays as-is (CLI callback + Google OAuth = web-only features).

**Step 3: Run typecheck**

**Step 4: Commit**

```bash
git commit -m "feat(views): extract shared LoginPage to @multica/views/auth"
```

---

### Task 7: Verify login flow in both apps

Run: `pnpm typecheck && pnpm test`

---

## Phase 4: Extract Agents Page (1,279 lines → shared module)

### Task 8: Create `@multica/views/agents`

**Files:**
- Create: `packages/views/agents/config.ts` — statusConfig, taskStatusConfig
- Create: `packages/views/agents/components/agents-page.tsx` — main page
- Create: `packages/views/agents/components/create-agent-dialog.tsx`
- Create: `packages/views/agents/components/agent-list-item.tsx`
- Create: `packages/views/agents/components/agent-detail.tsx`
- Create: `packages/views/agents/components/tabs/instructions-tab.tsx`
- Create: `packages/views/agents/components/tabs/skills-tab.tsx`
- Create: `packages/views/agents/components/tabs/tasks-tab.tsx`
- Create: `packages/views/agents/components/tabs/settings-tab.tsx`
- Create: `packages/views/agents/components/index.ts`
- Create: `packages/views/agents/index.ts`
- Modify: `packages/views/package.json` (add `"./agents"` export)

**Key migration:** All `@/platform/*` imports → `@multica/core/*` singletons. All `@multica/ui` and `@multica/core` imports stay as-is. `@multica/views` imports become relative.

**Step 1:** Extract config → components → barrel
**Step 2:** Run `pnpm typecheck`
**Step 3:** Commit

```bash
git commit -m "feat(views): extract agents page to @multica/views/agents"
```

---

### Task 9: Wire web agents route

```typescript
// apps/web/app/(dashboard)/agents/page.tsx — 1 line replaces 1,279
export { AgentsPage as default } from "@multica/views/agents";
```

Commit: `refactor(web): replace agents page with @multica/views/agents import`

---

### Task 10: Wire desktop agents route

```typescript
// router.tsx
import { AgentsPage } from "@multica/views/agents";
{ path: "agents", element: <AgentsPage /> },
```

Commit: `feat(desktop): wire agents page from @multica/views`

---

## Phase 5: Extract Inbox Page (468 lines → shared module)

### Task 11: Create `@multica/views/inbox`

**Files:**
- Create: `packages/views/inbox/components/inbox-page.tsx`
- Create: `packages/views/inbox/components/inbox-list-item.tsx`
- Create: `packages/views/inbox/components/inbox-detail-label.tsx`
- Create: `packages/views/inbox/components/index.ts`
- Create: `packages/views/inbox/index.ts`
- Modify: `packages/views/package.json` (add `"./inbox"` export)

**Key migration:**
- `import { useSearchParams } from "next/navigation"` → `import { useNavigation } from "../navigation"` — use `searchParams` from adapter
- `window.history.replaceState(null, "", url)` → `replace(url)` from `useNavigation()`
- `@/platform/*` → `@multica/core/*` singletons

Commit: `feat(views): extract inbox page to @multica/views/inbox`

---

### Task 12: Wire web inbox route

```typescript
// apps/web/app/(dashboard)/inbox/page.tsx — 1 line replaces 468
export { InboxPage as default } from "@multica/views/inbox";
```

Commit: `refactor(web): replace inbox page with @multica/views/inbox import`

---

### Task 13: Wire desktop inbox route

```typescript
import { InboxPage } from "@multica/views/inbox";
{ path: "inbox", element: <InboxPage /> },
```

Commit: `feat(desktop): wire inbox page from @multica/views`

---

## Phase 6: Extract Settings Page (1,277 lines → shared module)

### Task 14: Create `@multica/views/settings`

**Files:**
- Create: `packages/views/settings/components/settings-page.tsx`
- Create: `packages/views/settings/components/account-tab.tsx`
- Create: `packages/views/settings/components/appearance-tab.tsx`
- Create: `packages/views/settings/components/tokens-tab.tsx`
- Create: `packages/views/settings/components/workspace-tab.tsx`
- Create: `packages/views/settings/components/members-tab.tsx`
- Create: `packages/views/settings/components/repositories-tab.tsx`
- Create: `packages/views/settings/components/index.ts`
- Create: `packages/views/settings/index.ts`
- Modify: `packages/views/package.json` (add `"./settings"` export)

**Key migration:** Same pattern — `@/platform/*` → `@multica/core/*` singletons.

Commit: `feat(views): extract settings page to @multica/views/settings`

---

### Task 15: Wire web settings route

```typescript
// apps/web/app/(dashboard)/settings/page.tsx — 1 line replaces 1,277 (page + 6 tabs)
export { SettingsPage as default } from "@multica/views/settings";
```

Delete `apps/web/app/(dashboard)/settings/_components/` (all 6 files).

Commit: `refactor(web): replace settings page with @multica/views/settings import`

---

### Task 16: Wire desktop settings route

```typescript
import { SettingsPage } from "@multica/views/settings";
{ path: "settings", element: <SettingsPage /> },
```

Commit: `feat(desktop): wire settings page from @multica/views`

---

## Phase 7: Cleanup

### Task 17: Delete dead code

- Delete `apps/desktop/src/renderer/src/pages/placeholder.tsx`
- Delete `apps/web/platform/` directory entirely (only `navigation.tsx` remains — move to `apps/web/app/` or `apps/web/lib/`)
- Delete `apps/desktop/src/renderer/src/platform/` directory (only `navigation.tsx` remains — move)
- Remove unused imports across both apps
- Clean up `apps/web/features/auth/` — only `auth-cookie.ts` should remain

Commit: `chore: delete dead platform code after monorepo extraction`

---

### Task 18: Full verification

Run: `make check`
Expected: ALL PASS

---

## Final Architecture

### Each app after extraction

```
apps/web/
├── app/
│   ├── layout.tsx              # CoreProvider + WebNavigationProvider + ThemeProvider
│   ├── (auth)/login/page.tsx   # Web 独有：CLI callback, Google OAuth
│   ├── (dashboard)/
│   │   ├── layout.tsx          # DashboardLayout + SearchCommand (10 行)
│   │   ├── issues/page.tsx     # 1 行 re-export
│   │   ├── agents/page.tsx     # 1 行 re-export
│   │   ├── inbox/page.tsx      # 1 行 re-export
│   │   ├── settings/page.tsx   # 1 行 re-export
│   │   └── ... (all 1-line)
│   └── (landing)/              # Web 独有
├── lib/
│   └── navigation.tsx          # WebNavigationProvider（唯一平台代码）
├── features/
│   ├── auth/auth-cookie.ts     # Web 独有
│   ├── landing/                # Web 独有
│   └── search/                 # Web 独有
└── components/                 # theme, icon, loading (少量)

apps/desktop/
├── src/main/                   # Electron 主进程
├── src/preload/                # preload bridge
├── src/renderer/src/
│   ├── App.tsx                 # CoreProvider + RouterProvider + ThemeProvider
│   ├── router.tsx              # 路由表（全部 @multica/views/*）
│   ├── lib/
│   │   └── navigation.tsx      # DesktopNavigationProvider（唯一平台代码）
│   ├── components/
│   │   ├── dashboard-shell.tsx # DashboardLayout + TitleBar (10 行)
│   │   ├── title-bar.tsx       # Desktop 独有
│   │   └── multica-icon.tsx    # Desktop 独有
│   └── pages/
│       └── login.tsx           # LoginPage + TitleBar (10 行)
```

### 数字对比

| 指标 | 之前 | 之后 |
|------|------|------|
| Web platform 文件 | 6 个 | 1 个 (navigation.tsx) |
| Desktop platform 文件 | 7 个 | 1 个 (navigation.tsx) |
| Web agents/page.tsx | 1,279 行 | 1 行 |
| Web inbox/page.tsx | 468 行 | 1 行 |
| Web settings/ 总计 | 1,277 行 | 1 行 |
| Web sidebar | 239 行 | 0 (共享) |
| Desktop sidebar | 236 行 (重复) | 0 (共享) |
| Desktop placeholders | 3 个 | 0 |
| 共享 views 模块 | 7 个 | 12 个 |
| 两端重复代码 | ~1,500 行 | 0 行 |
