# Desktop App (Electron) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create `apps/desktop/` Electron app that reuses `@multica/core`, `@multica/ui`, `@multica/views` — identical UI to web, with a custom frameless title bar.

**Architecture:** electron-vite (Vite for main/preload/renderer), react-router-dom `createHashRouter` for routing, platform layer mirrors `apps/web/platform/`. Title bar uses `titleBarStyle: 'hiddenInset'` with a draggable top bar reserved for future tabs.

**Tech Stack:** Electron 36+, electron-vite, react-router-dom v7, electron-store, next-themes, Tailwind CSS v4, shadcn/ui tokens.

**Branch:** `feat/desktop-app` (from latest `main`)

**Scope:** Skeleton app with full infrastructure + all extracted pages wired. Non-extracted pages (agents, inbox, settings) get placeholder routes — extraction is a follow-up task.

---

## Work Breakdown

| Category | Count | Nature |
|---|---|---|
| New files to create | ~20 | Scaffold, platform layer, router, shell components |
| Config files | ~5 | electron.vite.config, electron-builder.yml, package.json, tsconfig |
| Pages wired from packages | 6 | issues, issues/:id, my-issues, runtimes, skills, board |
| Placeholder pages | 3 | agents, inbox, settings (follow-up extraction) |
| Login page | 1 | Desktop-specific (no Google OAuth initially, email OTP only) |

---

## Phase 1: Scaffold + Config (Tasks 1-3)

### Task 1: Scaffold electron-vite project

**Action:** Run scaffolding, then clean up to fit monorepo.

```bash
cd apps && npm create @quick-start/electron@latest desktop -- --template react-ts
```

Then restructure to match monorepo conventions. Remove generated boilerplate files we don't need.

**Final directory structure:**
```
apps/desktop/
├── src/
│   ├── main/
│   │   └── index.ts
│   ├── preload/
│   │   ├── index.ts
│   │   └── index.d.ts
│   └── renderer/
│       ├── src/
│       │   ├── main.tsx
│       │   ├── App.tsx
│       │   ├── globals.css
│       │   ├── platform/
│       │   ├── components/
│       │   └── pages/
│       └── index.html
├── electron.vite.config.ts
├── electron-builder.yml
├── package.json
└── tsconfig.json (three: root, node, web)
```

**Modify:** `package.json` (root) — add `"dev:desktop": "turbo dev --filter=@multica/desktop"` to scripts.

**Verify:** `cd apps/desktop && pnpm install` succeeds.

**Commit:** `chore: scaffold electron-vite desktop app`

---

### Task 2: Configure electron.vite.config.ts for monorepo

**File:** `apps/desktop/electron.vite.config.ts`

Key requirements:
- Renderer uses `@vitejs/plugin-react` and `@tailwindcss/vite`
- Resolve monorepo packages (`@multica/core`, `@multica/ui`, `@multica/views`)
- Alias `@/` to `src/renderer/src/` for platform layer imports

```typescript
import { resolve } from "path";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
  },
  renderer: {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@": resolve("src/renderer/src"),
      },
    },
  },
});
```

**Verify:** `pnpm exec electron-vite build` completes without errors.

---

### Task 3: Configure package.json and dependencies

**File:** `apps/desktop/package.json`

```json
{
  "name": "@multica/desktop",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "main": "./out/main/index.js",
  "scripts": {
    "dev": "electron-vite dev",
    "build": "electron-vite build",
    "typecheck": "tsc --noEmit",
    "preview": "electron-vite preview",
    "package": "electron-builder"
  },
  "dependencies": {
    "@multica/core": "workspace:*",
    "@multica/ui": "workspace:*",
    "@multica/views": "workspace:*",
    "electron-store": "^10.0.0",
    "react-router-dom": "^7.6.0",
    "next-themes": "^0.4.6",
    "sonner": "^2.0.7"
  },
  "devDependencies": {
    "@multica/tsconfig": "workspace:*",
    "@tailwindcss/vite": "catalog:",
    "@types/react": "catalog:",
    "@types/react-dom": "catalog:",
    "@vitejs/plugin-react": "^4.5.2",
    "electron": "^36.3.1",
    "electron-builder": "^26.0.12",
    "electron-vite": "^3.1.0",
    "react": "catalog:",
    "react-dom": "catalog:",
    "tailwindcss": "catalog:",
    "typescript": "catalog:"
  }
}
```

**File:** `apps/desktop/electron-builder.yml`

```yaml
appId: ai.multica.desktop
productName: Multica
directories:
  buildResources: build
files:
  - "!**/.vscode/*"
  - "!src/*"
  - "!electron.vite.config.*"
  - "!{.eslintignore,.eslintrc.cjs,.prettierignore,.prettierrc.yaml,dev-app-update.yml,CHANGELOG.md,README.md}"
  - "!{tsconfig.json,tsconfig.node.json,tsconfig.web.json}"
mac:
  target:
    - dmg
    - zip
  artifactName: ${name}-${version}-${arch}.${ext}
  entitlementsInherit: build/entitlements.mac.plist
dmg:
  artifactName: ${name}-${version}.${ext}
linux:
  target:
    - AppImage
    - deb
  artifactName: ${name}-${version}-${arch}.${ext}
win:
  target:
    - nsis
  artifactName: ${name}-${version}-setup.${ext}
```

**Also add** `@tailwindcss/vite` to the pnpm catalog in `pnpm-workspace.yaml` if not already present.

**Verify:** `pnpm install` from root succeeds, no version conflicts.

**Commit:** `chore(desktop): configure dependencies and electron-builder`

---

## Phase 2: Electron Main + Preload (Tasks 4-5)

### Task 4: Main process — frameless window

**File:** `apps/desktop/src/main/index.ts`

```typescript
import { app, shell, BrowserWindow } from "electron";
import { join } from "path";
import { is } from "@electron-toolkit/utils";

let mainWindow: BrowserWindow | null = null;

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 900,
    minHeight: 600,
    titleBarStyle: "hiddenInset",       // macOS: keeps traffic lights, hides title
    trafficLightPosition: { x: 16, y: 14 }, // position traffic lights inside our custom bar
    show: false,
    webPreferences: {
      preload: join(__dirname, "../preload/index.js"),
      sandbox: false,
    },
  });

  mainWindow.on("ready-to-show", () => {
    mainWindow?.show();
  });

  // Open external links in browser
  mainWindow.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url);
    return { action: "deny" };
  });

  // Dev: load from Vite dev server. Prod: load built HTML.
  if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
    mainWindow.loadURL(process.env["ELECTRON_RENDERER_URL"]);
  } else {
    mainWindow.loadFile(join(__dirname, "../renderer/index.html"));
  }
}

app.whenReady().then(() => {
  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
```

**Note:** `@electron-toolkit/utils` provides `is.dev` helper. Add to devDependencies:
```
"@electron-toolkit/utils": "^4.0.0"
```

---

### Task 5: Preload script — electron-store bridge

**File:** `apps/desktop/src/preload/index.ts`

```typescript
import { contextBridge } from "electron";
import Store from "electron-store";

const store = new Store<Record<string, string>>({
  name: "multica-desktop",
});

contextBridge.exposeInMainWorld("electronStore", {
  get: (key: string): string | null => store.get(key) ?? null,
  set: (key: string, value: string): void => store.set(key, value),
  delete: (key: string): void => store.delete(key),
});
```

**File:** `apps/desktop/src/preload/index.d.ts`

```typescript
declare global {
  interface Window {
    electronStore: {
      get(key: string): string | null;
      set(key: string, value: string): void;
      delete(key: string): void;
    };
  }
}
export {};
```

**Commit:** `feat(desktop): add main process with frameless window and preload bridge`

---

## Phase 3: Renderer Platform Layer (Tasks 6-11)

These files mirror `apps/web/platform/` exactly.

### Task 6: Storage adapter

**File:** `apps/desktop/src/renderer/src/platform/storage.ts`

```typescript
import type { StorageAdapter } from "@multica/core/types/storage";

export const desktopStorage: StorageAdapter = {
  getItem: (key) => window.electronStore.get(key),
  setItem: (key, value) => window.electronStore.set(key, value),
  removeItem: (key) => window.electronStore.delete(key),
};
```

---

### Task 7: API client

**File:** `apps/desktop/src/renderer/src/platform/api.ts`

```typescript
import { ApiClient } from "@multica/core/api/client";
import { setApiInstance } from "@multica/core/api";
import { createLogger } from "@multica/core/logger";
import { desktopStorage } from "./storage";

// TODO: make configurable via settings
const API_BASE_URL = "http://localhost:8080";

export const api = new ApiClient(API_BASE_URL, {
  logger: createLogger("api"),
  onUnauthorized: () => {
    desktopStorage.removeItem("multica_token");
    desktopStorage.removeItem("multica_workspace_id");
    // Navigate to login — handled by auth state change in React tree
  },
});

setApiInstance(api);

// Hydrate from persisted storage
const token = desktopStorage.getItem("multica_token");
if (token) api.setToken(token);
const wsId = desktopStorage.getItem("multica_workspace_id");
if (wsId) api.setWorkspaceId(wsId);
```

---

### Task 8: Auth + workspace store instances

**File:** `apps/desktop/src/renderer/src/platform/auth.ts`

```typescript
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { api } from "./api";
import { desktopStorage } from "./storage";

export const useAuthStore = createAuthStore({
  api,
  storage: desktopStorage,
  // No cookies in desktop — onLogin/onLogout are no-ops
});

registerAuthStore(useAuthStore);
```

**File:** `apps/desktop/src/renderer/src/platform/workspace.ts`

```typescript
import { createWorkspaceStore, registerWorkspaceStore } from "@multica/core/workspace";
import { toast } from "sonner";
import { api } from "./api";
import { desktopStorage } from "./storage";

export const useWorkspaceStore = createWorkspaceStore(api, {
  storage: desktopStorage,
  onError: (msg) => toast.error(msg),
});

registerWorkspaceStore(useWorkspaceStore);
```

---

### Task 9: Navigation provider (react-router → NavigationAdapter)

**File:** `apps/desktop/src/renderer/src/platform/navigation.tsx`

```typescript
import { useNavigate, useLocation } from "react-router-dom";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@multica/views/navigation";

export function DesktopNavigationProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const navigate = useNavigate();
  const location = useLocation();

  const adapter: NavigationAdapter = {
    push: (path) => navigate(path),
    replace: (path) => navigate(path, { replace: true }),
    back: () => navigate(-1),
    pathname: location.pathname,
    searchParams: new URLSearchParams(location.search),
  };

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}
```

---

### Task 10: WebSocket provider

**File:** `apps/desktop/src/renderer/src/platform/ws-provider.tsx`

```typescript
import { WSProvider } from "@multica/core/realtime";
import { useAuthStore } from "./auth";
import { useWorkspaceStore } from "./workspace";
import { desktopStorage } from "./storage";
import { toast } from "sonner";

// TODO: make configurable via settings
const WS_URL = "ws://localhost:8080/ws";

export function DesktopWSProvider({ children }: { children: React.ReactNode }) {
  return (
    <WSProvider
      wsUrl={WS_URL}
      authStore={useAuthStore}
      workspaceStore={useWorkspaceStore}
      storage={desktopStorage}
      onToast={(message, type) => {
        if (type === "error") toast.error(message);
        else toast.info(message);
      }}
    >
      {children}
    </WSProvider>
  );
}
```

---

### Task 11: Auth initializer (desktop version)

**File:** `apps/desktop/src/renderer/src/platform/auth-initializer.tsx`

Same as web version but uses `desktopStorage` instead of `localStorage`, and no cookies.

```typescript
import { useEffect, type ReactNode } from "react";
import { useAuthStore } from "./auth";
import { useWorkspaceStore } from "./workspace";
import { api } from "./api";
import { desktopStorage } from "./storage";
import { createLogger } from "@multica/core/logger";

const logger = createLogger("auth");

export function AuthInitializer({ children }: { children: ReactNode }) {
  useEffect(() => {
    const token = desktopStorage.getItem("multica_token");
    if (!token) {
      useAuthStore.setState({ isLoading: false });
      return;
    }

    api.setToken(token);
    const wsId = desktopStorage.getItem("multica_workspace_id");

    const mePromise = api.getMe();
    const wsPromise = api.listWorkspaces();

    Promise.all([mePromise, wsPromise])
      .then(([user, wsList]) => {
        useAuthStore.setState({ user, isLoading: false });
        useWorkspaceStore.getState().hydrateWorkspace(wsList, wsId);
      })
      .catch((err) => {
        logger.error("auth init failed", err);
        api.setToken(null);
        api.setWorkspaceId(null);
        desktopStorage.removeItem("multica_token");
        desktopStorage.removeItem("multica_workspace_id");
        useAuthStore.setState({ user: null, isLoading: false });
      });
  }, []);

  return <>{children}</>;
}
```

**File:** `apps/desktop/src/renderer/src/platform/index.ts`

```typescript
export { api } from "./api";
export { useAuthStore } from "./auth";
export { useWorkspaceStore } from "./workspace";
export { desktopStorage } from "./storage";
```

**Commit:** `feat(desktop): add renderer platform layer — storage, api, auth, ws, navigation`

---

## Phase 4: Title Bar + Shell (Tasks 12-14)

### Task 12: Title bar component

**File:** `apps/desktop/src/renderer/src/components/title-bar.tsx`

A draggable top bar that accounts for macOS traffic lights. Content is placeholder for future tabs.

```typescript
export function TitleBar() {
  return (
    <div
      className="h-11 shrink-0 flex items-center border-b bg-sidebar select-none"
      style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
    >
      {/* Left: traffic light inset area (macOS) */}
      <div className="w-[78px] shrink-0" />

      {/* Center: reserved for tabs (future) */}
      <div className="flex-1 flex items-center px-2">
        {/* Tab bar placeholder */}
      </div>

      {/* Right: reserved for window actions (future) */}
      <div className="w-[40px] shrink-0" />
    </div>
  );
}
```

**Note:** `WebkitAppRegion: "drag"` makes the whole bar draggable. Buttons/interactive elements inside need `WebkitAppRegion: "no-drag"`.

---

### Task 13: Dashboard shell (sidebar + title bar + content)

**File:** `apps/desktop/src/renderer/src/components/dashboard-shell.tsx`

Replicates `apps/web/app/(dashboard)/layout.tsx` structure but with title bar and react-router's `<Outlet>`.

```typescript
import { useEffect } from "react";
import { Outlet, useNavigate, useLocation } from "react-router-dom";
import { useNavigationStore } from "@multica/core/navigation";
import { SidebarProvider, SidebarInset } from "@multica/ui/components/ui/sidebar";
import { WorkspaceIdProvider } from "@multica/core/hooks";
import { ModalRegistry } from "@multica/views/modals/registry";
import { useAuthStore } from "@/platform/auth";
import { useWorkspaceStore } from "@/platform/workspace";
import { TitleBar } from "./title-bar";
import { AppSidebar } from "./app-sidebar";
import { MulticaIcon } from "./multica-icon";

export function DashboardShell() {
  const navigate = useNavigate();
  const location = useLocation();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useWorkspaceStore((s) => s.workspace);

  useEffect(() => {
    if (!isLoading && !user) {
      navigate("/login", { replace: true });
    }
  }, [user, isLoading, navigate]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(location.pathname);
  }, [location.pathname]);

  if (isLoading) {
    return (
      <div className="flex h-screen flex-col">
        <TitleBar />
        <div className="flex flex-1 items-center justify-center">
          <MulticaIcon className="size-6" />
        </div>
      </div>
    );
  }

  if (!user) return null;

  return (
    <div className="flex h-screen flex-col">
      <TitleBar />
      <div className="flex flex-1 min-h-0">
        <SidebarProvider className="flex-1">
          <AppSidebar />
          <SidebarInset className="overflow-hidden">
            {workspace ? (
              <WorkspaceIdProvider wsId={workspace.id}>
                <Outlet />
                <ModalRegistry />
              </WorkspaceIdProvider>
            ) : (
              <div className="flex flex-1 items-center justify-center">
                <MulticaIcon className="size-6 animate-pulse" />
              </div>
            )}
          </SidebarInset>
        </SidebarProvider>
      </div>
    </div>
  );
}
```

---

### Task 14: Copy shared app components

Copy these from `apps/web/` to `apps/desktop/src/renderer/src/components/`, adjusting imports:

- `multica-icon.tsx` — Copy as-is (pure CSS, no dependencies)
- `app-sidebar.tsx` — Copy from `apps/web/app/(dashboard)/_components/app-sidebar.tsx`, change:
  - `@/platform/auth` → `@/platform/auth` (same alias, different root)
  - `@/platform/workspace` → `@/platform/workspace`
  - Any `next/link` → `AppLink` from `@multica/views/navigation` (check if already using AppLink)
- `theme-provider.tsx` — Copy, uses `next-themes` which works in Electron too

**Commit:** `feat(desktop): add title bar, dashboard shell, and shared components`

---

## Phase 5: Router + Pages (Tasks 15-17)

### Task 15: CSS entry point

**File:** `apps/desktop/src/renderer/src/globals.css`

```css
@import "tailwindcss";
@import "tw-animate-css";
@import "shadcn/tailwind.css";
@import "@multica/ui/styles/tokens.css";

@custom-variant dark (&:is(.dark *));

@source "../../../../packages/ui/**/*.tsx";
@source "../../../../packages/core/**/*.tsx";
@source "../../../../packages/views/**/*.tsx";
@source "./**/*.tsx";

@layer base {
  * {
    @apply border-border outline-ring/50;
    scrollbar-width: thin;
    scrollbar-color: var(--scrollbar-thumb) var(--scrollbar-track);
  }
  *::-webkit-scrollbar { width: 6px; height: 6px; }
  *::-webkit-scrollbar-track { background: var(--scrollbar-track); }
  *::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 3px; }
  *::-webkit-scrollbar-thumb:hover { background: var(--scrollbar-thumb-hover); }
  body {
    @apply bg-background text-foreground;
  }
  html {
    @apply font-sans;
  }
}
```

**Note:** `@source` paths need to be relative from `apps/desktop/src/renderer/src/` to `packages/`.

---

### Task 16: Router + page wiring

**File:** `apps/desktop/src/renderer/src/router.tsx`

```typescript
import { createHashRouter, Navigate } from "react-router-dom";
import { DashboardShell } from "./components/dashboard-shell";
import { LoginPage } from "./pages/login";

// Extracted pages from @multica/views
import { IssuesPage } from "@multica/views/issues/components";
import { IssueDetail } from "@multica/views/issues/components";
import MyIssuesPage from "@multica/views/my-issues";
import RuntimesPage from "@multica/views/runtimes";
import SkillsPage from "@multica/views/skills";

// Placeholder pages (not yet extracted to @multica/views)
import { PlaceholderPage } from "./pages/placeholder";

export const router = createHashRouter([
  {
    path: "/",
    element: <DashboardShell />,
    children: [
      { index: true, element: <Navigate to="/issues" replace /> },
      { path: "issues", element: <IssuesPage /> },
      { path: "issues/:id", element: <IssueDetail /> },
      { path: "my-issues", element: <MyIssuesPage /> },
      { path: "runtimes", element: <RuntimesPage /> },
      { path: "skills", element: <SkillsPage /> },
      { path: "agents", element: <PlaceholderPage title="Agents" /> },
      { path: "inbox", element: <PlaceholderPage title="Inbox" /> },
      { path: "settings", element: <PlaceholderPage title="Settings" /> },
      { path: "board", element: <PlaceholderPage title="Board" /> },
    ],
  },
  { path: "/login", element: <LoginPage /> },
]);
```

**File:** `apps/desktop/src/renderer/src/pages/placeholder.tsx`

```typescript
export function PlaceholderPage({ title }: { title: string }) {
  return (
    <div className="flex flex-1 items-center justify-center">
      <div className="text-center">
        <h1 className="text-2xl font-bold">{title}</h1>
        <p className="mt-2 text-muted-foreground">
          Coming soon — requires page extraction to @multica/views.
        </p>
      </div>
    </div>
  );
}
```

**Note on IssueDetail route:** The web version uses `issues/[id]/page.tsx` which passes the `id` param. The desktop version needs a thin wrapper:

**File:** `apps/desktop/src/renderer/src/pages/issue-detail-page.tsx`

```typescript
import { useParams } from "react-router-dom";
import { IssueDetail } from "@multica/views/issues/components";

export function IssueDetailPage() {
  const { id } = useParams<{ id: string }>();
  if (!id) return null;
  return <IssueDetail issueId={id} />;
}
```

Check how `IssueDetail` receives the issue ID (via prop or via navigation adapter). Adjust accordingly.

---

### Task 17: App entry + provider nesting

**File:** `apps/desktop/src/renderer/src/App.tsx`

```typescript
import { RouterProvider } from "react-router-dom";
import { ThemeProvider } from "./components/theme-provider";
import { Toaster } from "@multica/ui/components/ui/sonner";
import { QueryProvider } from "@multica/core/provider";
import { AuthInitializer } from "./platform/auth-initializer";
import { DesktopWSProvider } from "./platform/ws-provider";
import { DesktopNavigationProvider } from "./platform/navigation";
import { router } from "./router";

export default function App() {
  return (
    <ThemeProvider>
      <QueryProvider>
        <RouterProvider router={router} />
        <Toaster />
      </QueryProvider>
    </ThemeProvider>
  );
}
```

**Important:** `DesktopNavigationProvider` and `AuthInitializer` need to be INSIDE the router context (they use `useNavigate`/`useLocation`). So they go inside `DashboardShell`, not at the App root. The `RouterProvider` provides the router context.

Adjust `DashboardShell` to wrap children with providers:

```typescript
// Inside DashboardShell, wrap the return:
return (
  <DesktopNavigationProvider>
    <AuthInitializer>
      <DesktopWSProvider>
        <div className="flex h-screen flex-col">
          <TitleBar />
          {/* ... rest of shell */}
        </div>
      </DesktopWSProvider>
    </AuthInitializer>
  </DesktopNavigationProvider>
);
```

**File:** `apps/desktop/src/renderer/src/main.tsx`

```typescript
import React from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./globals.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
```

**File:** `apps/desktop/src/renderer/index.html`

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Multica</title>
  </head>
  <body class="h-full overflow-hidden antialiased">
    <div id="root" class="h-full"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

**Commit:** `feat(desktop): add router, pages, and app entry with provider nesting`

---

## Phase 6: Login Page + Final Verification (Tasks 18-20)

### Task 18: Desktop login page

**File:** `apps/desktop/src/renderer/src/pages/login.tsx`

Simplified login page — email OTP only (no Google OAuth for desktop MVP). Uses `useAuthStore` and `useWorkspaceStore` from platform layer.

Uses `useNavigate` from react-router-dom instead of `useRouter` from next/navigation.

Structure: same Card-based layout as web login, with email input → send code → verify code flow.

After successful login, navigate to `/issues`.

```typescript
import { useState, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useAuthStore } from "@/platform/auth";
import { useWorkspaceStore } from "@/platform/workspace";
import { api } from "@/platform/api";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { MulticaIcon } from "../components/multica-icon";
import { TitleBar } from "../components/title-bar";

export function LoginPage() {
  const navigate = useNavigate();
  const [step, setStep] = useState<"email" | "code">("email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSendCode = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      await useAuthStore.getState().sendCode(email);
      setStep("code");
    } catch {
      setError("Failed to send code");
    } finally {
      setLoading(false);
    }
  }, [email]);

  const handleVerify = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      await useAuthStore.getState().verifyCode(email, code);
      const wsList = await api.listWorkspaces();
      useWorkspaceStore.getState().hydrateWorkspace(wsList);
      navigate("/issues", { replace: true });
    } catch {
      setError("Invalid code");
    } finally {
      setLoading(false);
    }
  }, [email, code, navigate]);

  return (
    <div className="flex h-screen flex-col">
      <TitleBar />
      <div className="flex flex-1 items-center justify-center">
        <Card className="w-[380px]">
          <CardHeader className="text-center">
            <div className="mx-auto mb-4">
              <MulticaIcon bordered size="lg" />
            </div>
            <CardTitle>Sign in to Multica</CardTitle>
            <CardDescription>
              {step === "email"
                ? "Enter your email to get a login code"
                : `We sent a code to ${email}`}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {step === "email" ? (
              <form onSubmit={(e) => { e.preventDefault(); handleSendCode(); }}>
                <div className="space-y-4">
                  <div className="space-y-2">
                    <Label htmlFor="email">Email</Label>
                    <Input
                      id="email"
                      type="email"
                      placeholder="you@example.com"
                      value={email}
                      onChange={(e) => setEmail(e.target.value)}
                      autoFocus
                    />
                  </div>
                  {error && <p className="text-sm text-destructive">{error}</p>}
                  <Button className="w-full" disabled={!email || loading}>
                    {loading ? "Sending..." : "Send Code"}
                  </Button>
                </div>
              </form>
            ) : (
              <form onSubmit={(e) => { e.preventDefault(); handleVerify(); }}>
                <div className="space-y-4">
                  <div className="space-y-2">
                    <Label htmlFor="code">Verification Code</Label>
                    <Input
                      id="code"
                      placeholder="Enter 6-digit code"
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                      autoFocus
                    />
                  </div>
                  {error && <p className="text-sm text-destructive">{error}</p>}
                  <Button className="w-full" disabled={!code || loading}>
                    {loading ? "Verifying..." : "Verify"}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    className="w-full"
                    onClick={() => { setStep("email"); setCode(""); setError(""); }}
                  >
                    Back
                  </Button>
                </div>
              </form>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
```

---

### Task 19: TypeScript configuration

**File:** `apps/desktop/tsconfig.json`

```json
{
  "files": [],
  "references": [
    { "path": "tsconfig.node.json" },
    { "path": "tsconfig.web.json" }
  ]
}
```

**File:** `apps/desktop/tsconfig.node.json`

```json
{
  "extends": "@multica/tsconfig/base.json",
  "compilerOptions": {
    "outDir": "out",
    "types": ["electron-vite/node"]
  },
  "include": ["src/main/**/*", "src/preload/**/*", "electron.vite.config.ts"]
}
```

**File:** `apps/desktop/tsconfig.web.json`

```json
{
  "extends": "@multica/tsconfig/react-library.json",
  "compilerOptions": {
    "outDir": "out",
    "baseUrl": ".",
    "paths": {
      "@/*": ["src/renderer/src/*"]
    },
    "types": ["electron-vite/client"]
  },
  "include": ["src/renderer/src/**/*", "src/preload/index.d.ts"]
}
```

---

### Task 20: Final verification

**Step 1:** Run `pnpm install` from root.

**Step 2:** Run `pnpm dev:desktop` (or `cd apps/desktop && pnpm dev`).

**Step 3:** Verify:
- [ ] Electron window opens with frameless title bar
- [ ] Traffic lights (🔴🟡🟢) appear at correct position
- [ ] Title bar area is draggable
- [ ] Redirects to login page (no stored token)
- [ ] Login flow works (email → code → dashboard)
- [ ] Sidebar renders with correct navigation links
- [ ] Issues page loads and shows data
- [ ] Issue detail works when clicking an issue
- [ ] My Issues, Runtimes, Skills pages load
- [ ] Agents, Inbox, Settings show placeholder
- [ ] Dark/light theme switching works
- [ ] WebSocket connection establishes (check console)
- [ ] Real-time updates work (create issue in web → appears in desktop)

**Step 4:** Run `pnpm typecheck` from root — verify desktop passes.

**Commit:** `feat(desktop): desktop app MVP — full dashboard with shared packages`

---

## Follow-Up Tasks (Not in scope)

These are for future PRs:

1. **Extract fat pages to @multica/views:**
   - `agents/page.tsx` (1279 lines) → `packages/views/agents/`
   - `inbox/page.tsx` (468 lines) → `packages/views/inbox/`
   - `settings/page.tsx` + `_components/` → `packages/views/settings/`
   - `login/page.tsx` → `packages/views/auth/` (with `useNavigation()` instead of `useRouter()`)

2. **App sidebar extraction:** Copy from web or extract shared sidebar to `@multica/views/layout/`

3. **Desktop-specific features:** Tray icon, auto-updater, global shortcuts, daemon management, deep links

4. **Google OAuth for desktop:** Electron OAuth flow (redirect to system browser → callback)

5. **Configurable API URL:** Settings page or env file for connecting to different backends

---

## Execution Order & Commits

| # | Commit | Files | Risk |
|---|---|---|---|
| 1 | `chore: scaffold electron-vite desktop app` | config | Low |
| 2 | `chore(desktop): configure dependencies and electron-builder` | config | Low |
| 3 | `feat(desktop): add main process with frameless window and preload bridge` | 3 files | Low |
| 4 | `feat(desktop): add renderer platform layer` | 7 files | Low |
| 5 | `feat(desktop): add title bar, dashboard shell, and shared components` | 4 files | Med — sidebar copy |
| 6 | `feat(desktop): add router, pages, and app entry` | 6 files | Med — integration |
| 7 | `feat(desktop): desktop app MVP — verification pass` | fixes | Low |
