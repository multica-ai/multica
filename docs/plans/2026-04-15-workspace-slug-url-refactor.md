# Workspace Slug URL Refactor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the URL the single source of truth for workspace context by moving from workspace-agnostic URLs (`/issues`) to slug-scoped URLs (`/{workspaceSlug}/issues`).

**Architecture:** URL carries the workspace slug as the first path segment. All derived state (API client workspace id, persist-store namespace, Zustand "current workspace" selector) flows from URL → React Query cache → derived hook. Switching workspace becomes navigation, never a state mutation. Web uses Next.js `[workspaceSlug]` dynamic segment; desktop mirrors with react-router `:workspaceSlug` param. `useWorkspaceStore.workspace` / `hydrateWorkspace` / `switchWorkspace` actions are removed in favor of URL-driven selectors.

**Tech Stack:** Next.js 15 App Router, react-router 6 (desktop memory router), TanStack Query, Zustand (persist + URL-driven), Go/Chi (backend, slug reserved-word validation only).

**Branch:** `NevilleQingNY/workspace-url-refactor` (from latest `main`).

**Scope:** One atomic PR. Intermediate states are not runnable because URL structure changes atomically. Develop in a worktree, self-test exhaustively, one review, merge.

---

## Why This Refactor

Current state (measured, not claimed):

- Workspace identity lives in 4 synchronized copies: `localStorage["multica_workspace_id"]`, `useWorkspaceStore.workspace`, `api._workspaceId`, `_currentWsId` module var in `packages/core/platform/workspace-storage.ts`. Any hand-synchronization drift = bug.
- URL `/issues/abc` has no workspace marker. A link shared across workspaces opens with the receiver's cached workspace → 404 or wrong data.
- `localStorage["multica_workspace_id"]` is a single global key. Two tabs on different workspaces overwrite each other on refresh.
- `useCreateWorkspace` / `useLeaveWorkspace` / `useDeleteWorkspace` call `switchWorkspace` / `hydrateWorkspace` inside `onSuccess`. This causes observable UX bugs (MUL-727 flash, MUL-728 no-navigate, MUL-820 sidebar Join).
- Mobile has no workspace switcher because switching lives only in sidebar UI, not in the URL (MUL-509).

Issues this PR closes: **MUL-43**, **MUL-509**, **MUL-723**, **MUL-727**, **MUL-728**, **MUL-820**. Out of scope: **#951** (WS half-open — unrelated realtime issue).

---

## Work Breakdown

| Category | Count | Notes |
|---|---|---|
| New files to create | 5 | Path builder, derived hooks, middleware, new layout, reserved slugs |
| Files moved (route restructure) | ~11 | `app/(dashboard)/*` → `app/[workspaceSlug]/(dashboard)/*` |
| Files heavily modified | ~8 | workspace store, mutations, sidebar, navigation store, link-handler, desktop routes, desktop nav adapter, auth initializer |
| Mechanical replace (path callsites) | ~20 | `push("/issues")` → `push(paths.issues())` etc. |
| Backend | 1 | Reserved-slug validation in workspace handler |
| E2E tests | ~20-30 assertions | URL assertions updated |
| Deleted references to `multica_workspace_id` | ~10 | After URL becomes source of truth |

---

## Architecture: Before vs After

### Before (current)

```
User action → switchWorkspace(ws) → mutates:
  1. api._workspaceId
  2. _currentWsId (module var)
  3. localStorage["multica_workspace_id"]
  4. useWorkspaceStore.workspace
  5. Rehydrates all workspace-namespaced persist stores
→ push("/issues")  [URL does not change workspace]
```

### After (target)

```
User action → push(paths.workspace(newSlug).issues())
→ Next.js routes to app/[workspaceSlug]/(dashboard)/issues/page
→ app/[workspaceSlug]/layout.tsx reads params.workspaceSlug
→ resolves from React Query workspace list → Workspace object
→ useEffect syncs:
   1. api.setWorkspaceId(ws.id)
   2. setCurrentWorkspaceId(ws.id)
   3. rehydrateAllWorkspaceStores()
→ WorkspaceIdProvider supplies id to subtree (unchanged interface)
```

**Invariants enforced:**
- No file reads `localStorage["multica_workspace_id"]` after this PR — it stops being written too.
- `useWorkspaceStore.workspace` is removed. Callers use `useCurrentWorkspace()` which reads URL + React Query.
- `switchWorkspace` / `hydrateWorkspace` removed. Workspace transitions = navigation only.
- Every path expression in shared code goes through `paths.*` builder. No hardcoded `/issues` strings outside the builder module.

---

## Phase 1: Foundation — Backend + Core Path Utilities (Tasks 1–5)

### Task 1: Backend reserved-slug validation

**Files:**
- Modify: `server/internal/handler/workspace.go` (around line 18, 146-154)
- Create: `server/internal/handler/workspace_reserved_slugs.go`
- Test: `server/internal/handler/workspace_test.go` (append test)

**Step 1: Write failing test**

Append to `server/internal/handler/workspace_test.go`:

```go
func TestCreateWorkspace_RejectsReservedSlug(t *testing.T) {
	ctx, testHandler := setupTest(t)
	user := createTestUser(ctx, t, testHandler.Queries, "user@example.com")

	for _, reserved := range []string{"login", "onboarding", "invite", "api", "settings", "admin", "auth", "signup", "logout", "_next", "favicon.ico", "robots.txt", "sitemap.xml"} {
		body := fmt.Sprintf(`{"name":"Test","slug":"%s"}`, reserved)
		req := httptest.NewRequest("POST", "/api/workspaces", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
		w := httptest.NewRecorder()

		testHandler.CreateWorkspace(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("slug %q: expected 400, got %d", reserved, w.Code)
		}
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd server && go test ./internal/handler/ -run TestCreateWorkspace_RejectsReservedSlug -v
```

Expected: FAIL (slugs currently pass)

**Step 3: Create reserved slugs module**

Create `server/internal/handler/workspace_reserved_slugs.go`:

```go
package handler

// reservedSlugs are workspace slugs that would collide with frontend top-level
// routes. The frontend URL shape is /{workspaceSlug}/... so any slug that
// matches a top-level route (e.g. /login, /api) would be unreachable.
//
// Keep this list in sync with packages/core/paths/reserved-slugs.ts.
var reservedSlugs = map[string]bool{
	// Auth + onboarding routes
	"login":      true,
	"logout":     true,
	"signup":     true,
	"onboarding": true,
	"invite":     true,
	"auth":       true,

	// Reserved for future platform routes
	"api":       true,
	"admin":     true,
	"settings":  true,
	"help":      true,
	"about":     true,
	"pricing":   true,
	"changelog": true,

	// Next.js / hosting internals
	"_next":           true,
	"favicon.ico":     true,
	"robots.txt":      true,
	"sitemap.xml":     true,
	"manifest.json":   true,
	".well-known":     true,
}

func isReservedSlug(slug string) bool {
	return reservedSlugs[slug]
}
```

**Step 4: Wire validation into handler**

In `server/internal/handler/workspace.go`, modify `CreateWorkspace` (around line 151-154, right after the regex check):

```go
if !workspaceSlugPattern.MatchString(req.Slug) {
    http.Error(w, "slug must be lowercase alphanumeric with hyphens", http.StatusBadRequest)
    return
}
if isReservedSlug(req.Slug) {
    http.Error(w, "slug is reserved", http.StatusBadRequest)
    return
}
```

**Step 5: Run test to verify it passes**

```bash
cd server && go test ./internal/handler/ -run TestCreateWorkspace_RejectsReservedSlug -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add server/internal/handler/workspace.go server/internal/handler/workspace_reserved_slugs.go server/internal/handler/workspace_test.go
git commit -m "feat(workspace): reject reserved slugs on creation"
```

---

### Task 2: Frontend reserved slugs + path builder module

**Files:**
- Create: `packages/core/paths/reserved-slugs.ts`
- Create: `packages/core/paths/paths.ts`
- Create: `packages/core/paths/index.ts`
- Test: `packages/core/paths/paths.test.ts`

**Step 1: Write failing test**

Create `packages/core/paths/paths.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { paths, isGlobalPath } from "./paths";

describe("paths.workspace(slug)", () => {
  const ws = paths.workspace("acme");

  it("builds dashboard paths with slug prefix", () => {
    expect(ws.issues()).toBe("/acme/issues");
    expect(ws.issueDetail("abc-123")).toBe("/acme/issues/abc-123");
    expect(ws.projects()).toBe("/acme/projects");
    expect(ws.projectDetail("p1")).toBe("/acme/projects/p1");
    expect(ws.autopilots()).toBe("/acme/autopilots");
    expect(ws.autopilotDetail("a1")).toBe("/acme/autopilots/a1");
    expect(ws.agents()).toBe("/acme/agents");
    expect(ws.inbox()).toBe("/acme/inbox");
    expect(ws.myIssues()).toBe("/acme/my-issues");
    expect(ws.runtimes()).toBe("/acme/runtimes");
    expect(ws.skills()).toBe("/acme/skills");
    expect(ws.settings()).toBe("/acme/settings");
  });

  it("URL-encodes special characters in ids", () => {
    expect(ws.issueDetail("id with space")).toBe("/acme/issues/id%20with%20space");
  });
});

describe("paths (global)", () => {
  it("builds global paths without slug", () => {
    expect(paths.login()).toBe("/login");
    expect(paths.onboarding()).toBe("/onboarding");
    expect(paths.invite("inv-1")).toBe("/invite/inv-1");
    expect(paths.authCallback()).toBe("/auth/callback");
  });
});

describe("isGlobalPath", () => {
  it("returns true for pre-workspace routes", () => {
    expect(isGlobalPath("/login")).toBe(true);
    expect(isGlobalPath("/onboarding")).toBe(true);
    expect(isGlobalPath("/invite/abc")).toBe(true);
    expect(isGlobalPath("/auth/callback")).toBe(true);
  });

  it("returns false for workspace-scoped paths", () => {
    expect(isGlobalPath("/acme/issues")).toBe(false);
    expect(isGlobalPath("/")).toBe(false);
  });
});
```

**Step 2: Run test to verify it fails**

```bash
pnpm --filter @multica/core exec vitest run paths/paths.test.ts
```

Expected: FAIL (module not found)

**Step 3: Implement reserved-slugs**

Create `packages/core/paths/reserved-slugs.ts`:

```ts
/**
 * Slugs reserved because they collide with frontend top-level routes.
 * Keep in sync with server/internal/handler/workspace_reserved_slugs.go.
 */
export const RESERVED_SLUGS = new Set([
  // Auth + onboarding
  "login",
  "logout",
  "signup",
  "onboarding",
  "invite",
  "auth",

  // Reserved for future platform routes
  "api",
  "admin",
  "settings",
  "help",
  "about",
  "pricing",
  "changelog",

  // Next.js / hosting internals
  "_next",
  "favicon.ico",
  "robots.txt",
  "sitemap.xml",
  "manifest.json",
  ".well-known",
]);

export function isReservedSlug(slug: string): boolean {
  return RESERVED_SLUGS.has(slug);
}
```

**Step 4: Implement paths builder**

Create `packages/core/paths/paths.ts`:

```ts
/**
 * Centralized URL path builder. All navigation in shared packages (packages/views)
 * MUST go through this module — no hardcoded string paths.
 *
 * Two kinds of paths:
 *  - workspace-scoped: paths.workspace(slug).xxx() — carry workspace in URL
 *  - global: paths.login(), paths.onboarding(), paths.invite(id) — pre-workspace routes
 */

const encode = (id: string) => encodeURIComponent(id);

function workspaceScoped(slug: string) {
  const ws = `/${encode(slug)}`;
  return {
    root: () => `${ws}/issues`,
    issues: () => `${ws}/issues`,
    issueDetail: (id: string) => `${ws}/issues/${encode(id)}`,
    projects: () => `${ws}/projects`,
    projectDetail: (id: string) => `${ws}/projects/${encode(id)}`,
    autopilots: () => `${ws}/autopilots`,
    autopilotDetail: (id: string) => `${ws}/autopilots/${encode(id)}`,
    agents: () => `${ws}/agents`,
    inbox: () => `${ws}/inbox`,
    myIssues: () => `${ws}/my-issues`,
    runtimes: () => `${ws}/runtimes`,
    skills: () => `${ws}/skills`,
    settings: () => `${ws}/settings`,
  };
}

export const paths = {
  workspace: workspaceScoped,

  // Global (pre-workspace) routes
  login: () => "/login",
  onboarding: () => "/onboarding",
  invite: (id: string) => `/invite/${encode(id)}`,
  authCallback: () => "/auth/callback",
  root: () => "/",
};

export type WorkspacePaths = ReturnType<typeof workspaceScoped>;

const GLOBAL_PREFIXES = ["/login", "/onboarding", "/invite/", "/auth/", "/logout", "/signup"];

export function isGlobalPath(path: string): boolean {
  return GLOBAL_PREFIXES.some((p) => path === p || path.startsWith(p));
}
```

**Step 5: Create index barrel**

Create `packages/core/paths/index.ts`:

```ts
export { paths, isGlobalPath } from "./paths";
export type { WorkspacePaths } from "./paths";
export { RESERVED_SLUGS, isReservedSlug } from "./reserved-slugs";
```

**Step 6: Export from core root**

Modify `packages/core/package.json` exports (if there's an explicit exports map; otherwise this is consumed via `@multica/core/paths`).

Also add to any root barrel if present. Check `packages/core/index.ts`:

```bash
cat packages/core/index.ts | grep -n export
```

If path exports are listed there, add `export * from "./paths";`.

**Step 7: Run tests**

```bash
pnpm --filter @multica/core exec vitest run paths/paths.test.ts
```

Expected: PASS

**Step 8: Commit**

```bash
git add packages/core/paths/
git commit -m "feat(paths): add workspace path builder and reserved slugs"
```

---

### Task 3: Derived workspace hooks (URL-driven)

**Files:**
- Create: `packages/core/paths/hooks.ts`
- Modify: `packages/core/paths/index.ts` (add hook exports)
- Test: `packages/core/paths/hooks.test.tsx`

These hooks replace `useWorkspaceStore((s) => s.workspace)` callsites. They read workspace slug from URL (via a platform-provided source) + workspace list from React Query, and return a derived result.

**Step 1: Define `WorkspaceSlugProvider` interface**

Because `packages/core/` cannot import `next/navigation` or `react-router-dom`, the slug must come from a platform-provided Context. This mirrors how `WorkspaceIdProvider` works today.

**Step 2: Write failing test**

Create `packages/core/paths/hooks.test.tsx`:

```tsx
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkspaceSlugProvider, useWorkspaceSlug, useCurrentWorkspace } from "./hooks";
import { workspaceKeys } from "../workspace/queries";

function setup(slug: string | null, wsList: any[] = []) {
  const qc = new QueryClient();
  qc.setQueryData(workspaceKeys.list(), wsList);
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <WorkspaceSlugProvider slug={slug}>{children}</WorkspaceSlugProvider>
      </QueryClientProvider>
    );
  };
}

describe("useWorkspaceSlug", () => {
  it("returns the provided slug", () => {
    function Probe() {
      return <div>{useWorkspaceSlug() ?? "null"}</div>;
    }
    render(<Probe />, { wrapper: setup("acme") });
    expect(screen.getByText("acme")).toBeInTheDocument();
  });

  it("returns null when no slug is provided", () => {
    function Probe() {
      return <div>{useWorkspaceSlug() ?? "null"}</div>;
    }
    render(<Probe />, { wrapper: setup(null) });
    expect(screen.getByText("null")).toBeInTheDocument();
  });
});

describe("useCurrentWorkspace", () => {
  const acme = { id: "id-1", slug: "acme", name: "Acme", description: null, context: null, settings: {}, repos: [], issue_prefix: "ACM", created_at: "", updated_at: "" };

  it("resolves workspace from slug and list", () => {
    function Probe() {
      const ws = useCurrentWorkspace();
      return <div>{ws?.name ?? "none"}</div>;
    }
    render(<Probe />, { wrapper: setup("acme", [acme]) });
    expect(screen.getByText("Acme")).toBeInTheDocument();
  });

  it("returns null when slug does not match any workspace", () => {
    function Probe() {
      const ws = useCurrentWorkspace();
      return <div>{ws?.name ?? "none"}</div>;
    }
    render(<Probe />, { wrapper: setup("bogus", [acme]) });
    expect(screen.getByText("none")).toBeInTheDocument();
  });
});
```

**Step 3: Run test to verify failure**

```bash
pnpm --filter @multica/core exec vitest run paths/hooks.test.tsx
```

Expected: FAIL (module not found)

**Step 4: Implement hooks**

Create `packages/core/paths/hooks.ts`:

```tsx
"use client";

import { createContext, useContext, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Workspace } from "../types";
import { workspaceListOptions } from "../workspace/queries";
import { paths, type WorkspacePaths } from "./paths";

/**
 * Context for the current workspace slug (from URL).
 * Platform layer (apps/web or apps/desktop) must provide this.
 */
const WorkspaceSlugContext = createContext<string | null>(null);

export function WorkspaceSlugProvider({
  slug,
  children,
}: {
  slug: string | null;
  children: ReactNode;
}) {
  return (
    <WorkspaceSlugContext.Provider value={slug}>
      {children}
    </WorkspaceSlugContext.Provider>
  );
}

/** Current workspace slug from URL, or null outside workspace-scoped routes. */
export function useWorkspaceSlug(): string | null {
  return useContext(WorkspaceSlugContext);
}

/** Same as useWorkspaceSlug, but throws if called outside a workspace route. */
export function useRequiredWorkspaceSlug(): string {
  const slug = useWorkspaceSlug();
  if (!slug) {
    throw new Error("useRequiredWorkspaceSlug called outside workspace-scoped route");
  }
  return slug;
}

/**
 * The currently-selected workspace, derived from URL slug + React Query list.
 * Returns null if slug is missing or doesn't match any workspace in the list.
 */
export function useCurrentWorkspace(): Workspace | null {
  const slug = useWorkspaceSlug();
  const { data: list = [] } = useQuery(workspaceListOptions());
  if (!slug) return null;
  return list.find((w) => w.slug === slug) ?? null;
}

/**
 * Path builder bound to the current workspace. Throws if called outside a
 * workspace route (use paths.workspace(slug) manually for cross-workspace links).
 */
export function useWorkspacePaths(): WorkspacePaths {
  const slug = useRequiredWorkspaceSlug();
  return paths.workspace(slug);
}
```

**Step 5: Update index barrel**

Modify `packages/core/paths/index.ts`:

```ts
export { paths, isGlobalPath } from "./paths";
export type { WorkspacePaths } from "./paths";
export { RESERVED_SLUGS, isReservedSlug } from "./reserved-slugs";
export {
  WorkspaceSlugProvider,
  useWorkspaceSlug,
  useRequiredWorkspaceSlug,
  useCurrentWorkspace,
  useWorkspacePaths,
} from "./hooks";
```

**Step 6: Run tests**

```bash
pnpm --filter @multica/core exec vitest run paths/
```

Expected: PASS

**Step 7: Commit**

```bash
git add packages/core/paths/
git commit -m "feat(paths): add URL-driven workspace hooks"
```

---

### Task 4: Collapse `useWorkspaceStore` to read-only shell

Remove `hydrateWorkspace` / `switchWorkspace` / `clearWorkspace`. Keep `updateWorkspace` only (used on rename). The `workspace` field is kept temporarily but will be driven by the new layout — not written by user actions.

**Note:** This is a breaking change for many callsites. Phase 3 replaces all callers. This task only updates the store's public API; the callers still compile because `workspace` property remains.

**Files:**
- Modify: `packages/core/workspace/store.ts`
- Modify: `packages/core/workspace/mutations.ts` (remove `switchWorkspace` / `hydrateWorkspace` calls)
- Modify: `packages/core/platform/auth-initializer.tsx` (remove `hydrateWorkspace` call and localStorage read)
- Modify: `apps/desktop/src/renderer/src/App.tsx` (remove same)
- Modify: `apps/web/app/auth/callback/page.tsx` (remove same)
- Modify: `apps/web/app/(auth)/login/page.tsx` (remove same)

**Step 1: Rewrite store**

Replace `packages/core/workspace/store.ts`:

```ts
import { create } from "zustand";
import type { Workspace } from "../types";

interface WorkspaceState {
  /** Current workspace. Written only by the workspace layout (URL-driven). */
  workspace: Workspace | null;
}

interface WorkspaceActions {
  /** Set the current workspace. Called by the URL-bound layout. */
  setWorkspace: (ws: Workspace | null) => void;
  /** Update the current workspace in place (e.g. after rename). */
  updateWorkspace: (ws: Workspace) => void;
}

export type WorkspaceStore = WorkspaceState & WorkspaceActions;

export function createWorkspaceStore() {
  return create<WorkspaceStore>((set) => ({
    workspace: null,
    setWorkspace: (ws) => set({ workspace: ws }),
    updateWorkspace: (ws) =>
      set((state) => ({
        workspace: state.workspace?.id === ws.id ? ws : state.workspace,
      })),
  }));
}
```

Note: `api` and `storage` parameters are gone. The store no longer touches `api._workspaceId` or localStorage. The layout does.

**Step 2: Update store instantiation**

Find where `createWorkspaceStore` is called:

```bash
grep -rn "createWorkspaceStore" packages/ apps/
```

Expected callsites: one in `packages/core/platform/core-provider.tsx` (or wherever the singleton is created). Update the call to drop arguments:

Replace `createWorkspaceStore(api, { storage })` with `createWorkspaceStore()`.

**Step 3: Clean up mutations**

Modify `packages/core/workspace/mutations.ts`:

```ts
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Workspace } from "../types";
import { api } from "../api";
import { workspaceKeys, workspaceListOptions } from "./queries";

export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { name: string; slug: string; description?: string }) =>
      api.createWorkspace(data),
    onSuccess: (newWs) => {
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] = []) => [...old, newWs]);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useLeaveWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.leaveWorkspace(workspaceId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.deleteWorkspace(workspaceId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}
```

The UI callers (create-workspace modal, settings page) handle navigation in their own `onSuccess` callbacks — that pattern continues but without the mutation calling `switchWorkspace`.

**Step 4: Simplify auth-initializer**

Modify `packages/core/platform/auth-initializer.tsx`:

- Delete: `const wsId = storage.getItem("multica_workspace_id");`
- Delete: `useWorkspaceStore.getState().hydrateWorkspace(wsList, wsId);` (both branches)
- Delete: `storage.removeItem("multica_workspace_id");` (in catch block)

The initializer just loads user + workspace list into React Query. Workspace selection now happens in the URL layout.

Full replacement:

```tsx
"use client";

import { useEffect, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { getApi } from "../api";
import { useAuthStore } from "../auth";
import { configStore } from "../config";
import { workspaceKeys } from "../workspace/queries";
import { createLogger } from "../logger";
import { defaultStorage } from "./storage";
import type { StorageAdapter } from "../types/storage";

const logger = createLogger("auth");

export function AuthInitializer({
  children,
  onLogin,
  onLogout,
  storage = defaultStorage,
  cookieAuth,
}: {
  children: ReactNode;
  onLogin?: () => void;
  onLogout?: () => void;
  storage?: StorageAdapter;
  cookieAuth?: boolean;
}) {
  const qc = useQueryClient();

  useEffect(() => {
    const api = getApi();

    api.getConfig().then((cfg) => {
      if (cfg.cdn_domain) configStore.getState().setCdnDomain(cfg.cdn_domain);
    }).catch(() => { /* config is optional */ });

    if (cookieAuth) {
      Promise.all([api.getMe(), api.listWorkspaces()])
        .then(([user, wsList]) => {
          onLogin?.();
          useAuthStore.setState({ user, isLoading: false });
          qc.setQueryData(workspaceKeys.list(), wsList);
        })
        .catch((err) => {
          logger.error("cookie auth init failed", err);
          onLogout?.();
          useAuthStore.setState({ user: null, isLoading: false });
        });
      return;
    }

    const token = storage.getItem("multica_token");
    if (!token) {
      onLogout?.();
      useAuthStore.setState({ isLoading: false });
      return;
    }

    api.setToken(token);

    Promise.all([api.getMe(), api.listWorkspaces()])
      .then(([user, wsList]) => {
        onLogin?.();
        useAuthStore.setState({ user, isLoading: false });
        qc.setQueryData(workspaceKeys.list(), wsList);
      })
      .catch((err) => {
        logger.error("auth init failed", err);
        api.setToken(null);
        storage.removeItem("multica_token");
        onLogout?.();
        useAuthStore.setState({ user: null, isLoading: false });
      });
  }, []);

  return <>{children}</>;
}
```

**Step 5: Commit**

```bash
git add packages/core/workspace/store.ts packages/core/workspace/mutations.ts packages/core/platform/auth-initializer.tsx packages/core/platform/core-provider.tsx
git commit -m "refactor(core): remove imperative workspace switching APIs"
```

*Note: this commit does not yet build, because 20+ callers still reference `switchWorkspace` etc. We fix them in Phase 3. Use `git commit --no-verify` if a pre-commit build check blocks; otherwise stage this alongside Phase 3 Task 11.*

**If pre-commit hook fails:** Do NOT bypass. Continue to Task 11 first, then commit all together in one larger commit. Re-sequence tasks if needed — the intent is atomic per-logical-unit, not strict commit ordering.

---

### Task 5: Add `api.getWorkspaceBySlug` client method (frontend only)

We don't need a new backend endpoint because the workspace list is already fetched. But the frontend needs a helper for the layout to resolve slug → workspace cleanly without duplicating `list.find` everywhere.

**Files:**
- Modify: `packages/core/workspace/queries.ts`

**Step 1: Add selector helper**

Append to `packages/core/workspace/queries.ts`:

```ts
import { queryOptions } from "@tanstack/react-query";
// ... existing imports

/** Query options that resolves the workspace matching the given slug from the list. */
export function workspaceBySlugOptions(slug: string) {
  return queryOptions({
    ...workspaceListOptions(),
    select: (list: Workspace[]) => list.find((w) => w.slug === slug) ?? null,
  });
}
```

**Step 2: Commit**

```bash
git add packages/core/workspace/queries.ts
git commit -m "feat(workspace): add workspaceBySlugOptions selector"
```

---

## Phase 2: Web Routing Restructure (Tasks 6–10)

### Task 6: Move `(dashboard)` routes under `[workspaceSlug]`

**Files moved** (use `git mv` to preserve history):

```bash
mkdir -p apps/web/app/\[workspaceSlug\]
git mv apps/web/app/\(dashboard\) apps/web/app/\[workspaceSlug\]/\(dashboard\)
```

After the move, structure is:

```
apps/web/app/
├── (auth)/                 # unchanged
├── (landing)/              # unchanged
├── auth/callback/          # unchanged
├── [workspaceSlug]/
│   ├── layout.tsx          # NEW — created in next task
│   └── (dashboard)/
│       ├── layout.tsx      # moved from old (dashboard)/layout.tsx
│       ├── issues/
│       ├── projects/
│       ├── autopilots/
│       ├── agents/
│       ├── inbox/
│       ├── my-issues/
│       ├── runtimes/
│       ├── skills/
│       └── settings/
├── layout.tsx              # unchanged
└── page.tsx                # modified later for redirect
```

**Commit:**

```bash
git add -A
git commit -m "refactor(web): move dashboard routes under [workspaceSlug] segment"
```

---

### Task 7: Create `[workspaceSlug]/layout.tsx` with slug-driven side effects

**Files:**
- Create: `apps/web/app/[workspaceSlug]/layout.tsx`

**Step 1: Implementation**

```tsx
"use client";

import { use, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { notFound } from "next/navigation";
import { api } from "@multica/core/api";
import {
  WorkspaceSlugProvider,
  workspaceBySlugOptions,
} from "@multica/core/paths";
import { useWorkspaceStore } from "@multica/core/workspace";
import { setCurrentWorkspaceId, rehydrateAllWorkspaceStores } from "@multica/core/platform/workspace-storage";
import { useAuthStore } from "@multica/core/auth";

export default function WorkspaceLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = use(params);
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  // Don't resolve workspace until auth is settled — list query is enabled only
  // when user is present (see queries.ts).
  const { data: workspace, isFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug),
    enabled: !!user,
  });

  // Sync URL-derived workspace into API client + persist namespace + Zustand mirror.
  useEffect(() => {
    if (!workspace) return;
    api.setWorkspaceId(workspace.id);
    setCurrentWorkspaceId(workspace.id);
    rehydrateAllWorkspaceStores();
    useWorkspaceStore.getState().setWorkspace(workspace);

    // Write non-HttpOnly cookie for middleware to read on root-path redirect.
    if (typeof document !== "undefined") {
      document.cookie = `last_workspace_slug=${encodeURIComponent(workspaceSlug)}; path=/; max-age=${60 * 60 * 24 * 365}; SameSite=Lax`;
    }
  }, [workspace, workspaceSlug]);

  if (isLoading) return null; // DashboardGuard below shows its own loader
  if (isFetched && !workspace) notFound();

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      {children}
    </WorkspaceSlugProvider>
  );
}
```

**Step 2: Verify route compiles**

```bash
pnpm --filter @multica/web build 2>&1 | head -40
```

Expected: may fail because of Phase 3 missing pieces, but the route file itself should be valid. Check for syntax errors.

**Step 3: Commit**

```bash
git add apps/web/app/\[workspaceSlug\]/layout.tsx
git commit -m "feat(web): add [workspaceSlug] layout for URL-driven workspace"
```

---

### Task 8: Update root `app/page.tsx` — redirect logged-in users

**Files:**
- Modify: `apps/web/app/page.tsx` (or `apps/web/app/(landing)/page.tsx` — check which is the real root)
- Verify: the landing page still renders for logged-out users

**Check current root:**

```bash
ls apps/web/app/page.tsx 2>/dev/null || ls apps/web/app/\(landing\)/page.tsx
```

**Implementation:**

If root is the landing page group, `middleware.ts` (next task) handles the redirect before this page renders. No change needed here.

If there's a separate `apps/web/app/page.tsx` that redirects, update it to use middleware too. Confirm which case applies:

```bash
cat apps/web/app/page.tsx 2>/dev/null
```

**Commit:** Only if changes are made. Otherwise skip.

---

### Task 9: Add Next.js middleware for root redirect

**Files:**
- Create: `apps/web/middleware.ts`

**Step 1: Implementation**

```ts
import { NextResponse, type NextRequest } from "next/server";

// Paths that should not be touched by middleware (assets, API, auth callbacks).
const PUBLIC_PREFIXES = [
  "/_next",
  "/api",
  "/auth/",
  "/login",
  "/signup",
  "/onboarding",
  "/invite/",
  "/favicon",
  "/robots.txt",
  "/sitemap.xml",
  "/manifest.json",
];

function isPublic(pathname: string): boolean {
  return PUBLIC_PREFIXES.some((p) => pathname === p || pathname.startsWith(p));
}

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;

  // Only handle the root "/"
  if (pathname !== "/") return NextResponse.next();
  if (isPublic(pathname)) return NextResponse.next();

  // Check auth cookie (HttpOnly, set by /login). We can't read its value,
  // but we can check presence — if there's no session cookie, show landing.
  const hasSession = req.cookies.has("multica_session");
  if (!hasSession) return NextResponse.next();

  // Logged-in user at /: redirect to last workspace's issues page.
  const lastSlug = req.cookies.get("last_workspace_slug")?.value;
  if (lastSlug) {
    const url = req.nextUrl.clone();
    url.pathname = `/${lastSlug}/issues`;
    return NextResponse.redirect(url);
  }

  // No last-workspace cookie yet (first login) — let the landing page render.
  // Client code will redirect to /{firstWorkspace.slug}/issues after auth loads.
  return NextResponse.next();
}

export const config = {
  matcher: ["/"],
};
```

**Step 2: Confirm auth cookie name**

```bash
grep -rn "multica_session\|setLoggedInCookie\|cookie" apps/web/features/auth/
```

Find the actual cookie name. Update `middleware.ts` if it's different (likely `multica_logged_in` or similar — the cookie only needs to indicate presence, not carry auth).

**Step 3: Commit**

```bash
git add apps/web/middleware.ts
git commit -m "feat(web): middleware redirect to last workspace on root"
```

---

### Task 10: Client-side redirect for logged-in users without `last_workspace_slug`

For users who log in for the first time (no cookie yet), the root page's landing component should redirect when workspace list loads.

**Files:**
- Modify: `apps/web/app/(landing)/page.tsx` or wherever the landing component lives
- Or create: a small `<RedirectIfAuthenticated>` component and wrap landing

**Implementation option:** add a client effect in the landing page:

```tsx
"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { useQuery } from "@tanstack/react-query";
import { workspaceListOptions } from "@multica/core/workspace";
import { paths } from "@multica/core/paths";

function useRedirectLoggedIn() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: list } = useQuery({ ...workspaceListOptions(), enabled: !!user });

  useEffect(() => {
    if (isLoading || !user || !list) return;
    if (list.length === 0) {
      router.replace(paths.onboarding());
    } else {
      router.replace(paths.workspace(list[0].slug).issues());
    }
  }, [isLoading, user, list, router]);
}
```

Invoke `useRedirectLoggedIn()` at the top of the landing page component.

**Commit:**

```bash
git add apps/web/app/\(landing\)/page.tsx
git commit -m "feat(web): client redirect from landing to first workspace when authenticated"
```

---

## Phase 3: Replace All Path Callsites (Tasks 11–18)

Mechanical replacements. Each task handles one cluster.

### Task 11: Update `app-sidebar.tsx` — workspace switcher and menu items

**File:** `packages/views/layout/app-sidebar.tsx`

**Changes:**

1. Replace `useWorkspaceStore((s) => s.workspace)` with `useCurrentWorkspace()`
2. Replace `useWorkspaceStore((s) => s.switchWorkspace)` import — DELETE the line (no longer exists)
3. Replace workspace switcher click handler:

**Before (around line 284-287):**

```tsx
if (ws.id !== workspace?.id) {
  push("/issues");
  switchWorkspace(ws);
}
```

**After:**

```tsx
if (ws.id !== workspace?.id) {
  push(paths.workspace(ws.slug).issues());
}
```

4. Replace hardcoded menu paths. Search for all `"/issues"`, `"/projects"`, etc. in the file. Menu items (around lines 73-87):

**Before:**

```tsx
{ title: "Inbox", url: "/inbox", icon: Inbox },
{ title: "My Issues", url: "/my-issues", icon: User },
{ title: "Issues", url: "/issues", icon: ListTodo },
// ...
```

**After:**

```tsx
const p = useWorkspacePaths();
const menuItems = [
  { title: "Inbox", url: p.inbox(), icon: Inbox },
  { title: "My Issues", url: p.myIssues(), icon: User },
  { title: "Issues", url: p.issues(), icon: ListTodo },
  // ...
];
```

5. Replace pin navigation (around line 105):

**Before:**

```tsx
push(`/${pinType}s/${pin.item_id}`);
```

**After:**

```tsx
push(pinType === "issue" ? p.issueDetail(pin.item_id) : p.projectDetail(pin.item_id));
```

6. Replace logout's `clearWorkspace` call (line 223):

**Before:**

```tsx
const logout = () => {
  queryClient.clear();
  authLogout();
  useWorkspaceStore.getState().clearWorkspace();
};
```

**After:**

```tsx
const logout = () => {
  queryClient.clear();
  authLogout();
  useWorkspaceStore.getState().setWorkspace(null);
};
```

7. The `push("/issues")` inside the switch handler at line 289 becomes `push(paths.workspace(ws.slug).issues())` (already covered in change #3).

**Commit:**

```bash
git add packages/views/layout/app-sidebar.tsx
git commit -m "refactor(sidebar): workspace switcher uses URL navigation"
```

---

### Task 12: Update `use-dashboard-guard.ts`

**File:** `packages/views/layout/use-dashboard-guard.ts`

**Changes:**

1. Replace `useWorkspaceStore((s) => s.workspace)` with `useCurrentWorkspace()`
2. Replace `/login`, `/onboarding` string paths with `paths.login()`, `paths.onboarding()`

**Commit:**

```bash
git add packages/views/layout/use-dashboard-guard.ts
git commit -m "refactor(dashboard-guard): use URL-derived workspace"
```

---

### Task 13: Update issues components

**Files:**
- `packages/views/issues/components/issue-detail.tsx` (lines 327, 469, 532, 552, 589, 608)
- `packages/views/issues/components/issues-page.tsx` (lines 29, 41)
- `packages/views/my-issues/components/my-issues-page.tsx` (line 30)

**Changes per file:**

`issue-detail.tsx`:
- Line 327: `useWorkspaceStore((s) => s.workspace)` → `useCurrentWorkspace()`
- Lines 469, 532, 552: `"/issues"` → `paths.issues()` (use `useWorkspacePaths`)
- Lines 589, 608: `` `/issues/${id}` `` → `paths.issueDetail(id)`

`issues-page.tsx`:
- Line 29: `useWorkspaceStore((s) => s.workspace)` → `useCurrentWorkspace()`
- Line 41: `useWorkspaceStore.subscribe` — THIS IS A PROBLEM. `filter-workspace-sync` subscribes to store changes. Refactor: replace with a URL-based effect that invalidates filters when `wsId` changes.

For line 41, read the full context:

```bash
grep -n -A 10 "initFilterWorkspaceSync" packages/views/issues/components/issues-page.tsx
grep -rn "initFilterWorkspaceSync" packages/
```

If this is used only to clear filter state on workspace change, convert to a `useEffect(() => { clearFilters(); }, [wsId])` pattern where wsId comes from `useWorkspaceId()`.

`my-issues-page.tsx`:
- Line 30: same replacement

**Commit:**

```bash
git add packages/views/issues/ packages/views/my-issues/
git commit -m "refactor(views/issues): use path builder and URL-derived workspace"
```

---

### Task 14: Update projects, autopilots, agents components

**Files:**
- `packages/views/projects/components/projects-page.tsx` (line 252, 303)
- `packages/views/projects/components/project-detail.tsx` (lines 185, 237, 266)
- `packages/views/autopilots/components/autopilot-detail-page.tsx` (lines 405, 421)
- `packages/views/agents/components/*` (check for path references)

**Changes per file** (same pattern):
- `useWorkspaceStore((s) => s.workspace)` → `useCurrentWorkspace()`
- Hardcoded paths → `paths.xxx()` via `useWorkspacePaths()`

**Commit:**

```bash
git add packages/views/projects/ packages/views/autopilots/ packages/views/agents/
git commit -m "refactor(views): projects/autopilots/agents use path builder"
```

---

### Task 15: Update search, modals, editor extensions

**Files:**
- `packages/views/search/search-command.tsx` (lines 84-91, 232-234)
- `packages/views/modals/create-issue.tsx` (line 134)
- `packages/views/modals/create-workspace.tsx` (line 70)
- `packages/views/editor/extensions/mention-suggestion.tsx` (line 222)

**Changes:**

`search-command.tsx`:
- Lines 84-91 menu routes → `paths.xxx()`
- Lines 232-234 result navigation → `paths.projectDetail(...)` / `paths.issueDetail(...)`

`create-issue.tsx`:
- Line 134: `` `/issues/${issue.id}` `` → `paths.issueDetail(issue.id)` via `useWorkspacePaths()`

`create-workspace.tsx`:
- Line 70: `"/onboarding"` — this is a global path. Stays as `paths.onboarding()`.
- If the modal creates a workspace and then navigates INTO it: the `onSuccess` should push to `paths.workspace(newWs.slug).issues()` instead of `/onboarding`. Verify product intent.

`mention-suggestion.tsx`:
- Line 222: `useWorkspaceStore.getState().workspace?.id` — this runs outside React render (inside a TipTap extension factory). Need a different approach.

Option: the factory receives `wsId` as a parameter. Change `createMentionSuggestion(qc)` signature to `createMentionSuggestion(qc, getWorkspaceId)` where `getWorkspaceId` is a callback that reads current workspace id. Update callers to pass `() => api._workspaceId` or use a subscribable source.

Simpler option for this PR: keep using `useWorkspaceStore.getState().workspace?.id` for now — the store's `workspace` field is still written by the layout's `setWorkspace` call, so it works. Revisit if we want to fully remove the store later.

**Decision for this PR:** keep the `useWorkspaceStore.getState().workspace?.id` read since it works. Only change component-level reads (those use `useCurrentWorkspace()`).

**Commit:**

```bash
git add packages/views/search/ packages/views/modals/ packages/views/editor/
git commit -m "refactor(views): search/modals/editor use path builder"
```

---

### Task 16: Update invite / onboarding pages

**Files:**
- `packages/views/invite/invite-page.tsx` (lines 20, 42, 46, 88, 120)
- `packages/views/onboarding/onboarding-wizard.tsx` (lines 24, 28)
- `apps/web/app/(auth)/login/page.tsx` (line 32)
- `apps/web/app/(auth)/invite/[id]/page.tsx`
- `apps/web/app/auth/callback/page.tsx` (line 67, 71)

**Changes:**

`invite-page.tsx`:
- Line 20: `switchWorkspace` import — DELETE (no longer exists)
- Line 42: `switchWorkspace(ws)` → DELETE. Then line 46 pushes to `paths.workspace(ws.slug).issues()` (use the newly-joined workspace's slug).
- Lines 88, 120: `push("/issues")` — target depends on context. If user already has a workspace, push to current-workspace issues. If not, push to `/onboarding`. Read the component logic carefully.

`onboarding-wizard.tsx`:
- Line 24, 28: same `useCurrentWorkspace()` replacement

`auth/callback/page.tsx`:
- Line 67: `localStorage.getItem("multica_workspace_id")` → DELETE
- Line 71: after auth success, navigate to `paths.workspace(wsList[0].slug).issues()` (or `paths.onboarding()` if list is empty) — pick first workspace as default landing. The middleware cookie will update on next visit.

`(auth)/login/page.tsx`:
- Line 32: `localStorage.getItem("multica_workspace_id")` → DELETE
- Login success redirect: same logic as auth callback.

**Commit:**

```bash
git add packages/views/invite/ packages/views/onboarding/ apps/web/app/
git commit -m "refactor(auth-flow): remove localStorage workspace id, use URL navigation"
```

---

### Task 17: Update link-handler for internal markdown links

**File:** `packages/views/editor/utils/link-handler.ts`

When a markdown link like `[foo](/issues/abc)` is clicked inside an issue's content, it dispatches `multica:navigate` with path `/issues/abc`. After the refactor, this is not a valid route — it needs workspace slug.

**Change:**

`openLink` should detect workspace-scoped paths without slug and dispatch an event that the layout layer can prepend the current slug to. Or, add a slug-prepend helper that callers use.

Simplest: change `openLink` to accept a current slug and prepend:

```ts
import { paths, isGlobalPath } from "@multica/core/paths";

/** Open a link — internal paths dispatch multica:navigate, external open new tab. */
export function openLink(href: string, currentSlug?: string | null): void {
  if (href.startsWith("/")) {
    // Workspace-scoped path without slug → prepend current slug
    let path = href;
    if (!isGlobalPath(path) && currentSlug && !path.startsWith(`/${currentSlug}/`)) {
      // Detect paths like /issues/abc (missing slug) vs /acme/issues/abc (already has slug)
      // Heuristic: path's first segment matches a known top-level dashboard route
      const firstSegment = path.split("/")[1];
      if (["issues", "projects", "autopilots", "agents", "inbox", "my-issues", "runtimes", "skills", "settings"].includes(firstSegment)) {
        path = `/${currentSlug}${path}`;
      }
    }
    window.dispatchEvent(
      new CustomEvent("multica:navigate", { detail: { path } }),
    );
  } else {
    window.open(href, "_blank", "noopener,noreferrer");
  }
}

export function isMentionHref(href: string | null | undefined): href is string {
  return !!href && href.startsWith("mention://");
}
```

Update callers of `openLink` (content-editor, readonly-content, link-hover-card) to pass the current slug from `useWorkspaceSlug()`.

**Commit:**

```bash
git add packages/views/editor/utils/link-handler.ts packages/views/editor/
git commit -m "refactor(editor): link-handler prepends current workspace slug"
```

---

### Task 18: Update `NavigationStore.lastPath`

**File:** `packages/core/navigation/store.ts`

After the refactor, `lastPath` storing `/acme/issues/123` is still useful for "remember last page on refresh within same workspace", but it needs to either:
- Store workspace-relative path (`/issues/123`) and prepend slug on restore, OR
- Store absolute path and rely on workspace-aware persist namespace (already true for drafts).

**Decision:** storing absolute path `/acme/issues/123` in a workspace-namespaced store is simplest. It already works across workspace switches because the persist key is suffixed by wsId. The audit's Task 3 (`useNavigationStore` not workspace-aware) is now easy to fix.

**Changes:**

```ts
"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

const EXCLUDED_PREFIXES = ["/login", "/pair/", "/invite/", "/onboarding", "/auth/"];

interface NavigationState {
  lastPath: string | null;
  onPathChange: (path: string) => void;
}

export const useNavigationStore = create<NavigationState>()(
  persist(
    (set) => ({
      lastPath: null,
      onPathChange: (path: string) => {
        if (!EXCLUDED_PREFIXES.some((prefix) => path.startsWith(prefix))) {
          set({ lastPath: path });
        }
      },
    }),
    {
      name: "multica_navigation",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ lastPath: state.lastPath }),
    }
  )
);

registerForWorkspaceRehydration(() => useNavigationStore.persist.rehydrate());
```

Also update `packages/core/platform/storage-cleanup.ts`:

```ts
// Add "multica_navigation" to WORKSPACE_SCOPED_KEYS array
```

**Commit:**

```bash
git add packages/core/navigation/store.ts packages/core/platform/storage-cleanup.ts
git commit -m "fix(navigation-store): isolate lastPath per workspace"
```

---

## Phase 4: Desktop (Tasks 19–22)

### Task 19: Add `:workspaceSlug` to desktop routes

**File:** `apps/desktop/src/renderer/src/routes.tsx`

**Changes:**

Update route definitions (lines 71-143) to wrap dashboard paths under `:workspaceSlug`:

```tsx
const router = createMemoryRouter([
  { path: "/login", element: <LoginPage /> },
  { path: "/onboarding", element: <OnboardingPage /> },
  { path: "/invite/:id", element: <InvitePage /> },
  {
    path: "/:workspaceSlug",
    element: <WorkspaceRouteLayout />, // NEW — mirrors web's [workspaceSlug]/layout.tsx
    children: [
      { path: "issues", element: <IssuesPage /> },
      { path: "issues/:id", element: <IssueDetailPage /> },
      { path: "projects", element: <ProjectsPage /> },
      { path: "projects/:id", element: <ProjectDetailPage /> },
      { path: "autopilots", element: <AutopilotsPage /> },
      { path: "autopilots/:id", element: <AutopilotDetailPage /> },
      { path: "agents", element: <AgentsPage /> },
      { path: "inbox", element: <InboxPage /> },
      { path: "my-issues", element: <MyIssuesPage /> },
      { path: "runtimes", element: <RuntimesPage /> },
      { path: "skills", element: <SkillsPage /> },
      { path: "settings", element: <SettingsPage /> },
    ],
  },
]);
```

Create `WorkspaceRouteLayout` component that mirrors the web `[workspaceSlug]/layout.tsx` — reads `useParams()`, resolves workspace, provides `WorkspaceSlugProvider`.

**Commit:**

```bash
git add apps/desktop/src/renderer/src/routes.tsx
git commit -m "refactor(desktop): add :workspaceSlug to all dashboard routes"
```

---

### Task 20: Desktop tab store — workspace-aware paths

**File:** `apps/desktop/src/renderer/src/stores/tab-store.ts`

The tab system's paths now include slug naturally (because routes require slug). Main changes:

1. `ROUTE_ICONS` map keys should match first post-slug segment, not first segment.

Change the `resolveRouteIcon` function (lines 47-64) to split the path, skip the slug segment, and match the next segment:

```ts
function resolveRouteIcon(path: string): LucideIcon {
  // Path shape: /{slug}/issues/... → strip slug, take next segment
  const segments = path.split("/").filter(Boolean);
  const routeSegment = segments[1] ?? "";
  return ROUTE_ICONS[routeSegment] ?? Square;
}
```

2. Default tab on app startup: when the user logs in, open `/{defaultSlug}/issues` instead of `/issues`.

Find where the initial tab is created (in `App.tsx` or `DesktopShell`), update to read the first workspace's slug.

**Commit:**

```bash
git add apps/desktop/src/renderer/src/stores/tab-store.ts apps/desktop/src/renderer/src/
git commit -m "refactor(desktop/tabs): workspace-aware tab paths"
```

---

### Task 21: Desktop navigation adapter — `getShareableUrl` includes slug

**File:** `apps/desktop/src/renderer/src/platform/navigation.tsx`

Current (line 67, 110): `getShareableUrl: (path: string) => \`https://www.multica.ai${path}\``

After the refactor, `path` already contains slug (because the tab's memory router uses slug), so the existing implementation is correct. **No change needed.**

However, double-check by testing: open an issue, click "copy link", paste it. Verify it's `https://www.multica.ai/{slug}/issues/{id}`.

If the web URL domain is configurable (e.g. different staging domains), extract the domain to an env var:

```tsx
const WEB_URL = import.meta.env.VITE_WEB_URL || "https://www.multica.ai";
// ...
getShareableUrl: (path: string) => `${WEB_URL}${path}`,
```

**Commit (if changed):**

```bash
git add apps/desktop/src/renderer/src/platform/navigation.tsx
git commit -m "refactor(desktop/nav): make shareable URL domain configurable"
```

---

### Task 22: Desktop App.tsx — remove localStorage workspace id

**File:** `apps/desktop/src/renderer/src/App.tsx`

**Changes:**

Lines 29-31 currently do:

```tsx
const wsList = await api.listWorkspaces();
const lastWsId = localStorage.getItem("multica_workspace_id");
useWorkspaceStore.getState().hydrateWorkspace(wsList, lastWsId);
```

Replace with: no explicit workspace selection. The URL (memory router) carries the workspace, and the initial route selects the first workspace's slug.

```tsx
const wsList = await api.listWorkspaces();
if (wsList.length > 0) {
  // Navigate to first workspace's dashboard
  router.navigate(paths.workspace(wsList[0].slug).issues());
} else {
  router.navigate(paths.onboarding());
}
```

Make sure `router` is accessible here — or dispatch the navigation event from a provider.

**Commit:**

```bash
git add apps/desktop/src/renderer/src/App.tsx
git commit -m "refactor(desktop): URL-driven workspace, remove localStorage read"
```

---

## Phase 5: Verification & Cleanup (Tasks 23–27)

### Task 23: Remove all `multica_workspace_id` references

Run:

```bash
grep -rn "multica_workspace_id" packages/ apps/ | grep -v ".test." | grep -v "docs/plans/"
```

Each result should be gone or replaced. Verify:
- `packages/core/workspace/store.ts` — gone (step 4)
- `packages/core/platform/auth-initializer.tsx` — gone (step 4)
- `packages/core/platform/core-provider.tsx` — check any remaining reads
- `apps/desktop/src/renderer/src/App.tsx` — gone (step 22)
- `apps/desktop/src/renderer/src/pages/login.tsx` — check
- `apps/web/app/auth/callback/page.tsx` — gone (step 16)
- `apps/web/app/(auth)/login/page.tsx` — gone (step 16)
- `e2e/auth.spec.ts` — test cleanup, should use the new cookie

Commit cleanups:

```bash
git add -A
git commit -m "chore: remove all multica_workspace_id references"
```

---

### Task 24: TypeScript check

```bash
pnpm typecheck
```

Expected: PASS. Fix any type errors surfaced by the refactor.

Common issues to expect:
- `useWorkspaceStore((s) => s.switchWorkspace)` — the store no longer has this method. Replace with URL navigation.
- `useWorkspaceStore.getState().hydrateWorkspace(...)` — same.
- Any function signature that took `workspaceId` from the store but should now take it via `useWorkspaceId()`.

---

### Task 25: Unit tests

```bash
pnpm test
```

Expected: PASS. Tests that mocked `useWorkspaceStore.workspace` need updating to mock `useCurrentWorkspace()` directly, or provide a mock workspace list via React Query setup.

Check each of these test files:

```bash
grep -rln "useWorkspaceStore" packages/views/ apps/
```

Update each to the new mocking pattern.

---

### Task 26: E2E tests

**File:** `e2e/**/*.spec.ts`

Find all URL assertions:

```bash
grep -rn "/issues\|/projects\|/inbox\|/settings" e2e/
```

Update each to include the workspace slug. Use the test workspace's slug from the fixture.

Example transform:

```ts
// Before
await page.goto("/issues");
await expect(page).toHaveURL(/\/issues\/[a-f0-9-]+/);

// After
const ws = await api.getTestWorkspace();
await page.goto(`/${ws.slug}/issues`);
await expect(page).toHaveURL(new RegExp(`/${ws.slug}/issues/[a-f0-9-]+`));
```

Run E2E:

```bash
make dev   # background, wait for ready
pnpm exec playwright test
```

Expected: all tests pass.

---

### Task 27: Full verification pipeline

```bash
make check
```

Runs: typecheck + unit tests + Go tests + E2E. Must pass end-to-end before declaring done.

---

## Phase 6: Manual QA Checklist (Pre-Deploy)

Before pushing to test environment, verify each scenario manually in local dev:

- [ ] Log in fresh → lands on `/{firstWorkspace.slug}/issues`
- [ ] Click around (issues, projects, settings) → URL updates with slug
- [ ] Open 2 tabs at different workspaces → each tab shows correct data on refresh (no cross-pollution)
- [ ] Copy URL from tab A, paste in tab B → correct workspace's data loads
- [ ] Workspace switcher in sidebar → navigates, URL updates, no flash
- [ ] Create new workspace → slug validation rejects reserved words (`login`, `api`, etc.)
- [ ] Create new workspace → successful creation navigates to new workspace's issues
- [ ] Delete current workspace → navigates to another workspace (no stuck on `/settings`)
- [ ] Leave workspace → same
- [ ] Accept invite via email link → navigates into invited workspace's issues
- [ ] Accept invite via sidebar "Join" dropdown → same path (no stale behavior)
- [ ] Share link from Copy Link → opens correctly in another user's browser
- [ ] Log out → returns to landing
- [ ] Mobile view → workspace switching works (even without sidebar open)
- [ ] Markdown link `[foo](/issues/abc)` in comment → navigates within current workspace
- [ ] Desktop: open multiple tabs, each with workspace path → tabs persist across app restart
- [ ] Desktop: copy-link generates `https://www.multica.ai/{slug}/issues/{id}`
- [ ] Back/forward buttons → history is preserved correctly

---

## Phase 7: Deploy to Test Environment

### Task 28: Push to test env

```bash
git push origin NevilleQingNY/workspace-url-refactor
```

Open PR against `main`. CI runs full test suite.

Deploy to test environment (per project deployment procedure — typically via PR preview or staging deploy).

**Staging verification (1-2 days):**
- Team members use test environment
- Watch for regressions in bug tracker
- Confirm MUL-43, MUL-509, MUL-723, MUL-727, MUL-728, MUL-820 are fixed
- Verify no new issues emerge in navigation, workspace switching, or data loading

### Task 29: Merge to main

After staging verification passes, merge PR to `main`. Production deploy follows normal CI/CD flow.

---

## Rollback Plan

If a critical regression is discovered in production:

1. Revert the PR via `git revert <merge-commit>`.
2. Deploy the revert.
3. Users with stored `last_workspace_slug` cookie are unaffected (cookie is ignored by old code).
4. URLs in external bookmarks (e.g. `/acme/issues/abc`) become 404. Since product has no real users, impact is minimal. If it matters: add a one-time middleware that strips the first segment and redirects.

---

## Out of Scope (Explicit)

The following are NOT part of this PR:

- **#951 (WS half-open cache stale)** — unrelated realtime problem. Separate plan.
- **Slug rename support** — backend currently forbids slug updates. If product later wants renameable slugs, needs separate migration (URL redirects for old slugs).
- **Backend slug-based API routes** — backend stays on UUID. Frontend resolves slug → id via workspace list.
- **Cross-workspace linking in mentions** — `mention://` protocol already carries UUID only. If/when product wants rich cross-workspace references, that's a separate design.
- **Multi-workspace-tab UX on desktop** — desktop can now technically support multiple workspace tabs open, but UI promoting this pattern is future work.

---

## Progress Tracking

Use TaskCreate during execution to track per-task progress. Commit after each task unless explicitly batching. Run `make check` before the final push.

**Estimated timeline:**
- Phase 1 (Foundation): ~0.5 day
- Phase 2 (Web routes): ~0.5 day
- Phase 3 (Path callsites): ~1 day
- Phase 4 (Desktop): ~0.5 day
- Phase 5 (Verification): ~0.5-1 day (depending on test fixture updates)
- Phase 6-7 (QA + deploy): ~1-2 days in staging

**Total: ~4-5 engineer-days** (with AI-assisted code generation + careful review).
