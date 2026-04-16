# Remove Onboarding & Fix Daemon Zero-Workspace Bootstrap

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Delete the entire onboarding flow, replace it with a minimal `/new-workspace` page, render an explicit "no access" page when the URL slug doesn't match a workspace the user can access, and fix the latent daemon bug that crashes when the user has zero workspaces.

**Architecture:**
- Extract `CreateWorkspaceForm` (form + mutation + slug validation) so both the existing `CreateWorkspaceModal` and the new global `/new-workspace` route reuse it.
- Replace silent `navigate("/")` redirects on slug-not-found with an explicit `NoAccessPage` so users get clear feedback instead of magic URL changes.
- Remove the `len(allRuntimeIDs) == 0 → fail` hard-stop in the Go daemon; let `workspaceSyncLoop` (already 30 s ticker) discover workspaces post-startup. This finishes what PR #1001 started.
- Three navigation states cleanly separated: (a) no workspace → `/new-workspace`, (b) wrong slug → `NoAccessPage`, (c) match → render.

**Tech Stack:** Next.js 15 (web), Electron + react-router-dom (desktop), Go (daemon/CLI), Vitest, Playwright, TanStack Query, Zustand.

---

## Prerequisites (MUST verify before starting)

1. **The current uncommitted workspace-slug refactor cleanup is merged to main.**
   Run `git status` in the project root — must be clean (outside the new worktree).
   Run `git log --oneline -5` — top commit should be the cleanup commit post-#1164.
2. **CI is green on main.** Check `gh run list --branch main --limit 3`.
3. **Working in a fresh worktree** (do not pollute the main checkout). Use `superpowers:using-git-worktrees`.
4. **Read this entire plan once before starting Task 1.** Each phase is sequential — Phase 6 cannot run before Phase 5 finishes.
5. **Verify line numbers before editing.** Plan line numbers were captured at HEAD 94c9d280; ±1-3 line drift is expected and safe. Always locate code by grep/pattern, not by raw line number.

If any prerequisite fails, **STOP** and report — do not start.

---

## Decisions Already Made (no need to re-ask)

| Decision | Choice | Reason |
|---|---|---|
| Demo issues (4 sample issues created in `step-complete.tsx`) | **Delete with onboarding** | Not part of new flow; new workspaces are empty by design |
| Runtime/Agent guidance step | **Delete entirely** | Desktop manages daemon itself; web users discover via empty state in `/runtimes` |
| `cmd_login.go` polling | **Keep polling, change URL** to `/new-workspace` | Minimal CLI change; UX equivalent |
| `NoAccessPage` copy | "This workspace doesn't exist or you don't have access." + two buttons: `Go to my workspaces` (→ `/`), `Sign in as a different user` (→ `/login`) | Doesn't leak existence; gives users a way out |
| Desktop daemon restart on first workspace creation | **Yes, do it** | 30s wait feels broken; one extra `useEffect` is cheap |
| Workspace 404 vs 403 distinction | **Don't distinguish on the client** | Avoid letting attackers enumerate workspace slugs |
| Keep `"onboarding"` in RESERVED_SLUGS (both frontend + backend) | **Keep — do NOT remove** | Frontend + backend must stay in sync; one extra string is zero cost; leaves door open if `/onboarding` route is ever revived. Only `paths.onboarding()` function and `/onboarding` from GLOBAL_PREFIXES are removed. |

If the user contradicts any of these mid-execution, pause and re-confirm.

### Known UX tradeoff (acknowledged, no action)

Deleting demo issues removes the **only** automatic agent execution that happened in onboarding (issue #1 — "Say hello to the team!" — was `todo` + assigned to the created agent, and was the only way new users saw an end-to-end agent run without manual steps). Per user decision, we accept this: new workspaces are fully empty; users discover agent capabilities by assigning their own issues.

### Backlog logic is unaffected

Investigation confirmed `backlog` is the issue status "agent paused" state — orthogonal to onboarding. The 4 demo issues used `backlog` as a text-guide vehicle (issues #2-4), but no downstream UI depends on their existence. Issues/Projects/My-Issues/Inbox pages all have working empty-state UI already. Safe to delete without UX replacement.

---

## Phase 0: Setup

### Task 0.1: Create worktree and verify clean base

**Step 1: Create worktree**

Run from main checkout:
```bash
git worktree add ../multica-onboarding-removal -b chore/remove-onboarding origin/main
cd ../multica-onboarding-removal
```

**Step 2: Verify clean state**

```bash
git status              # → "nothing to commit, working tree clean"
git log -1 --oneline    # → must include the workspace-slug cleanup commit
```

**Step 3: Install + smoke build**

```bash
pnpm install
pnpm typecheck          # baseline must pass
make test               # baseline Go tests must pass
```

If any baseline fails, **STOP** — the issue is pre-existing, fix on main first.

**Step 4: No commit (worktree setup only).**

---

## Phase 1: Daemon Fix (smallest, ship-able alone)

This phase is independent — could merge to main alone if needed. Doing it first gives the safest foundation for everything else.

### Task 1.1: Write failing test for zero-workspace startup

**Files:**
- Modify: `server/internal/daemon/daemon_test.go`

**Step 1: Write the failing test**

Add to `daemon_test.go`:
```go
func TestDaemon_StartsWithZeroWorkspaces(t *testing.T) {
    // Server returns an empty workspace list
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/api/workspaces" {
            w.Header().Set("Content-Type", "application/json")
            _, _ = w.Write([]byte("[]"))
            return
        }
        w.WriteHeader(http.StatusNotFound)
    }))
    defer srv.Close()

    d := newTestDaemon(t, srv.URL)  // helper that exists in daemon_test.go; reuse pattern from other tests
    ctx, cancel := context.WithCancel(context.Background())

    // Run daemon in a goroutine; it MUST NOT return an error within 200ms.
    errCh := make(chan error, 1)
    go func() { errCh <- d.Run(ctx) }()

    select {
    case err := <-errCh:
        t.Fatalf("daemon exited unexpectedly with zero workspaces: %v", err)
    case <-time.After(200 * time.Millisecond):
        // Expected: daemon stays running
    }

    cancel()
    <-errCh  // drain
}
```

If `newTestDaemon` helper doesn't exist or has a different signature, **read the existing tests first** and adapt — don't invent a helper.

**Step 2: Run test to verify it fails**

```bash
cd server && go test ./internal/daemon/ -run TestDaemon_StartsWithZeroWorkspaces -v
```
Expected: FAIL with `daemon exited unexpectedly with zero workspaces: no runtimes registered`.

**Step 3: Commit the failing test (TDD discipline)**

```bash
git add server/internal/daemon/daemon_test.go
git commit -m "test(daemon): add failing test for zero-workspace startup"
```

### Task 1.2: Remove the hard-stop in daemon

**Files:**
- Modify: `server/internal/daemon/daemon.go:95-101`

**Step 1: Remove the runtime check**

Current code:
```go
// Fetch all user workspaces from the API and register runtimes.
if err := d.syncWorkspacesFromAPI(ctx); err != nil {
    return err
}
if len(d.allRuntimeIDs()) == 0 {
    return fmt.Errorf("no runtimes registered")
}
```

Change to:
```go
// Fetch all user workspaces from the API and register runtimes for any that
// exist. Zero workspaces is a valid state (new user before they create their
// first workspace) — workspaceSyncLoop below will discover and register
// runtimes when the user creates one. The 30s tick is acceptable; the
// desktop renderer additionally triggers a daemon restart on first workspace
// creation for instant pickup.
if err := d.syncWorkspacesFromAPI(ctx); err != nil {
    return err
}
```

**Step 2: Run failing test from Task 1.1**

```bash
cd server && go test ./internal/daemon/ -run TestDaemon_StartsWithZeroWorkspaces -v
```
Expected: PASS.

**Step 3: Run full daemon test suite — make sure nothing else broke**

```bash
cd server && go test ./internal/daemon/ -v
```
Expected: all green. If any test relied on "must fail with no runtimes" semantics, update that test to match the new contract (zero workspaces = valid).

**Step 4: Commit**

```bash
git add server/internal/daemon/daemon.go
git commit -m "fix(daemon): allow startup with zero workspaces

workspaceSyncLoop already polls every 30s to discover new workspaces,
but the runtime check at startup was failing fast before that loop
got a chance to run. This was masked by onboarding always creating a
workspace first; with onboarding being removed, the bug surfaces.

PR #1001 partially fixed this for the 'server has workspaces but
local CLI config is empty' case. This finishes the job for the
true zero-workspace case."
```

---

## Phase 2: Shared building blocks (no behavior change yet)

Pure additive — nothing breaks until Phase 5/6.

### Task 2.1: Extract CreateWorkspaceForm

**Files:**
- Create: `packages/views/workspace/create-workspace-form.tsx`
- Create: `packages/views/workspace/create-workspace-form.test.tsx`
- Modify: `packages/views/modals/create-workspace.tsx` (use the new form)

**Step 1: Write the failing test**

`packages/views/workspace/create-workspace-form.test.tsx`:
```tsx
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CreateWorkspaceForm } from "./create-workspace-form";

const mockMutate = vi.fn();
vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => ({ mutate: mockMutate, isPending: false }),
}));

function renderForm(onSuccess = vi.fn()) {
  const qc = new QueryClient();
  return render(
    <QueryClientProvider client={qc}>
      <CreateWorkspaceForm onSuccess={onSuccess} />
    </QueryClientProvider>,
  );
}

describe("CreateWorkspaceForm", () => {
  beforeEach(() => mockMutate.mockReset());

  it("auto-generates slug from name until user edits slug", () => {
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Acme Corp" },
    });
    expect(screen.getByDisplayValue("acme-corp")).toBeInTheDocument();
  });

  it("calls onSuccess with the created workspace", async () => {
    const onSuccess = vi.fn();
    mockMutate.mockImplementation((_args, opts) => {
      opts?.onSuccess?.({ id: "ws-1", slug: "acme", name: "Acme" });
    });
    renderForm(onSuccess);
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Acme" },
    });
    fireEvent.click(screen.getByRole("button", { name: /create workspace/i }));
    await waitFor(() =>
      expect(onSuccess).toHaveBeenCalledWith(
        expect.objectContaining({ slug: "acme" }),
      ),
    );
  });

  it("shows slug-conflict error inline without toast on 409", async () => {
    mockMutate.mockImplementation((_args, opts) => {
      opts?.onError?.({ status: 409 });
    });
    renderForm();
    fireEvent.change(screen.getByLabelText(/workspace name/i), {
      target: { value: "Taken" },
    });
    fireEvent.click(screen.getByRole("button", { name: /create workspace/i }));
    await waitFor(() =>
      expect(
        screen.getByText(/already taken/i),
      ).toBeInTheDocument(),
    );
  });
});
```

**Step 2: Run to verify failure**

```bash
pnpm --filter @multica/views exec vitest run workspace/create-workspace-form.test.tsx
```
Expected: FAIL — "Cannot find module './create-workspace-form'".

**Step 3: Implement CreateWorkspaceForm**

`packages/views/workspace/create-workspace-form.tsx`:
```tsx
"use client";

import { useRef, useState } from "react";
import { toast } from "sonner";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";
import type { Workspace } from "@multica/core/types";
import {
  WORKSPACE_SLUG_CONFLICT_ERROR,
  WORKSPACE_SLUG_FORMAT_ERROR,
  WORKSPACE_SLUG_REGEX,
  isWorkspaceSlugConflict,
  nameToWorkspaceSlug,
} from "./slug";

export interface CreateWorkspaceFormProps {
  onSuccess: (workspace: Workspace) => void;
}

export function CreateWorkspaceForm({ onSuccess }: CreateWorkspaceFormProps) {
  const createWorkspace = useCreateWorkspace();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugServerError, setSlugServerError] = useState<string | null>(null);
  const slugTouched = useRef(false);

  const slugValidationError =
    slug.length > 0 && !WORKSPACE_SLUG_REGEX.test(slug)
      ? WORKSPACE_SLUG_FORMAT_ERROR
      : null;
  const slugError = slugValidationError ?? slugServerError;
  const canSubmit =
    name.trim().length > 0 && slug.trim().length > 0 && !slugError;

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugTouched.current) {
      setSlug(nameToWorkspaceSlug(value));
      setSlugServerError(null);
    }
  };

  const handleSlugChange = (value: string) => {
    slugTouched.current = true;
    setSlug(value);
    setSlugServerError(null);
  };

  const handleCreate = () => {
    if (!canSubmit) return;
    createWorkspace.mutate(
      { name: name.trim(), slug: slug.trim() },
      {
        onSuccess,
        onError: (error) => {
          if (isWorkspaceSlugConflict(error)) {
            setSlugServerError(WORKSPACE_SLUG_CONFLICT_ERROR);
            toast.error("Choose a different workspace URL");
            return;
          }
          toast.error("Failed to create workspace");
        },
      },
    );
  };

  return (
    <Card className="w-full">
      <CardContent className="space-y-4 pt-6">
        <div className="space-y-1.5">
          <Label htmlFor="ws-name">Workspace Name</Label>
          <Input
            id="ws-name"
            autoFocus
            type="text"
            value={name}
            onChange={(e) => handleNameChange(e.target.value)}
            placeholder="My Workspace"
            onKeyDown={(e) => e.key === "Enter" && handleCreate()}
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="ws-slug">Workspace URL</Label>
          <div className="flex items-center gap-0 rounded-md border bg-background focus-within:ring-2 focus-within:ring-ring">
            <span className="pl-3 text-sm text-muted-foreground select-none">
              multica.ai/
            </span>
            <Input
              id="ws-slug"
              type="text"
              value={slug}
              onChange={(e) => handleSlugChange(e.target.value)}
              placeholder="my-workspace"
              className="border-0 shadow-none focus-visible:ring-0"
              onKeyDown={(e) => e.key === "Enter" && handleCreate()}
            />
          </div>
          {slugError && (
            <p className="text-xs text-destructive">{slugError}</p>
          )}
        </div>
        <Button
          className="w-full"
          size="lg"
          onClick={handleCreate}
          disabled={createWorkspace.isPending || !canSubmit}
        >
          {createWorkspace.isPending ? "Creating..." : "Create workspace"}
        </Button>
      </CardContent>
    </Card>
  );
}
```

**Step 4: Refactor `CreateWorkspaceModal` to use the form**

In `packages/views/modals/create-workspace.tsx`, replace the inline form (current lines 36-181) with:
```tsx
import { useNavigation } from "../navigation";
import { useImmersiveMode } from "../platform";
import { ArrowLeft } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { paths } from "@multica/core/paths";
import { CreateWorkspaceForm } from "../workspace/create-workspace-form";

export function CreateWorkspaceModal({ onClose }: { onClose: () => void }) {
  useImmersiveMode();
  const router = useNavigation();

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className="inset-0 flex h-full w-full max-w-none sm:max-w-none translate-0 flex-col items-center justify-center rounded-none bg-background ring-0 shadow-none"
      >
        <div
          aria-hidden
          className="absolute inset-x-0 top-0 h-10"
          style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
        />
        <Button
          variant="ghost"
          size="sm"
          className="absolute top-12 left-12 text-muted-foreground"
          style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
          onClick={onClose}
        >
          <ArrowLeft className="h-4 w-4" />
          Back
        </Button>

        <div className="flex w-full max-w-md flex-col items-center gap-6">
          <div className="text-center">
            <DialogTitle className="text-2xl font-semibold">
              Create a new workspace
            </DialogTitle>
            <DialogDescription className="mt-2">
              Workspaces are shared environments where teams can work on
              projects and issues.
            </DialogDescription>
          </div>
          <CreateWorkspaceForm
            onSuccess={(newWs) => {
              onClose();
              router.push(paths.workspace(newWs.slug).issues());
            }}
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}
```

**Step 5: Run tests**

```bash
pnpm --filter @multica/views exec vitest run workspace/create-workspace-form.test.tsx
pnpm --filter @multica/views exec vitest run modals/create-workspace.test.tsx
```
Both must pass. If `create-workspace.test.tsx` was relying on internal form structure, fix the test to assert behavior through the now-extracted form.

**Step 6: Commit**

```bash
git add packages/views/workspace/create-workspace-form.tsx \
        packages/views/workspace/create-workspace-form.test.tsx \
        packages/views/modals/create-workspace.tsx
git commit -m "refactor(views): extract CreateWorkspaceForm for reuse

Modal and the upcoming /new-workspace page share the same form +
mutation + slug validation. Extract to a shared component so they
can't drift."
```

### Task 2.2: NoAccessPage component

**Files:**
- Create: `packages/views/workspace/no-access-page.tsx`
- Create: `packages/views/workspace/no-access-page.test.tsx`

**Step 1: Write the failing test**

`packages/views/workspace/no-access-page.test.tsx`:
```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { NoAccessPage } from "./no-access-page";

const navigate = vi.fn();
vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: navigate, replace: navigate }),
}));

describe("NoAccessPage", () => {
  it("renders generic message that doesn't leak existence", () => {
    render(<NoAccessPage />);
    expect(
      screen.getByText(/doesn't exist or you don't have access/i),
    ).toBeInTheDocument();
  });

  it("navigates to root on 'Go to my workspaces'", () => {
    navigate.mockReset();
    render(<NoAccessPage />);
    fireEvent.click(screen.getByRole("button", { name: /go to my workspaces/i }));
    expect(navigate).toHaveBeenCalledWith("/");
  });

  it("navigates to login on 'Sign in as a different user'", () => {
    navigate.mockReset();
    render(<NoAccessPage />);
    fireEvent.click(screen.getByRole("button", { name: /sign in as a different user/i }));
    expect(navigate).toHaveBeenCalledWith("/login");
  });
});
```

**Step 2: Run to verify failure** — Expected: module not found.

**Step 3: Implement NoAccessPage**

`packages/views/workspace/no-access-page.tsx`:
```tsx
"use client";

import { Button } from "@multica/ui/components/ui/button";
import { paths } from "@multica/core/paths";
import { useNavigation } from "../navigation";

export function NoAccessPage() {
  const nav = useNavigation();
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 px-6 text-center">
      <div className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight">
          Workspace not available
        </h1>
        <p className="max-w-md text-muted-foreground">
          This workspace doesn't exist or you don't have access.
        </p>
      </div>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Button onClick={() => nav.push(paths.root())}>
          Go to my workspaces
        </Button>
        <Button variant="outline" onClick={() => nav.push(paths.login())}>
          Sign in as a different user
        </Button>
      </div>
    </div>
  );
}
```

**Step 4: Run test** — Expected: PASS.

**Step 5: Commit**

```bash
git add packages/views/workspace/no-access-page.tsx \
        packages/views/workspace/no-access-page.test.tsx
git commit -m "feat(views): add NoAccessPage for unknown/inaccessible workspace slugs"
```

### Task 2.3: Add `paths.newWorkspace()` and update reserved slugs (frontend)

**Files:**
- Modify: `packages/core/paths/paths.ts`
- Modify: `packages/core/paths/paths.test.ts`
- Modify: `packages/core/paths/consistency.test.ts` (verify `/new-workspace` is recognized global path — test file also contains `/onboarding` references that Phase 6 will clean up)
- Modify: `packages/core/paths/reserved-slugs.ts`
- Modify: `packages/core/paths/reserved-slugs.test.ts` (if it exists; check first)

**Step 1: Add failing test**

`paths.test.ts` — add:
```ts
it("newWorkspace returns /new-workspace", () => {
  expect(paths.newWorkspace()).toBe("/new-workspace");
});

it("isGlobalPath recognizes /new-workspace", () => {
  expect(isGlobalPath("/new-workspace")).toBe(true);
});
```

`reserved-slugs.test.ts` — add (or create):
```ts
it("reserves new-workspace", () => {
  expect(isReservedSlug("new-workspace")).toBe(true);
});
```

**Step 2: Run to verify failure**

```bash
pnpm --filter @multica/core exec vitest run paths/paths.test.ts paths/reserved-slugs.test.ts
```

**Step 3: Implement**

`paths.ts`:
```ts
// Add to paths object:
newWorkspace: () => "/new-workspace",
// Keep onboarding for now — Phase 6 deletes it. Don't break callers yet.

// Update GLOBAL_PREFIXES:
const GLOBAL_PREFIXES = [
  "/login", "/onboarding", "/new-workspace",
  "/invite/", "/auth/", "/logout", "/signup",
];
```

`reserved-slugs.ts`: add `"new-workspace",` to the auth/onboarding section. **Keep `"onboarding"` as reserved** — do not remove.

**Step 4: Run tests** — PASS.

**Step 5: Commit**

```bash
git add packages/core/paths/
git commit -m "feat(paths): add /new-workspace route + reserve slug"
```

### Task 2.4: Update reserved slugs in Go backend

**Files:**
- Modify: `server/internal/handler/workspace_reserved_slugs.go`
- Modify: `server/internal/handler/workspace_test.go`

**Step 1: Add failing test**

In `workspace_test.go` (find the existing reserved-slug test and extend):
```go
{name: "new-workspace is reserved", slug: "new-workspace", wantReserved: true},
```

**Step 2: Run to verify failure**

```bash
cd server && go test ./internal/handler/ -run TestReservedSlug -v
```

**Step 3: Implement**

`workspace_reserved_slugs.go`: add `"new-workspace": true,` to the Auth + onboarding routes section.

**Step 4: Run test** — PASS.

**Step 5: Commit**

```bash
git add server/internal/handler/
git commit -m "feat(workspace): reserve 'new-workspace' slug

Frontend will route /new-workspace to the workspace creation page.
Reserve here to prevent collision."
```

### Task 2.5: Database migration for new reserved slug audit

**Files:**
- Create: `server/migrations/046_audit_new_workspace_reserved.up.sql`
- Create: `server/migrations/046_audit_new_workspace_reserved.down.sql`

**Step 1: Read existing audit migration as template**

```bash
cat server/migrations/045_audit_dashboard_route_slugs.up.sql
```

**Step 2: Write up migration**

`046_audit_new_workspace_reserved.up.sql`:
```sql
-- Audit: ensure no existing workspace uses the new reserved slug
-- "new-workspace" before we wire it as a frontend route.
DO $$
DECLARE
    conflict_count INT;
BEGIN
    SELECT COUNT(*) INTO conflict_count
    FROM workspaces
    WHERE slug = 'new-workspace';

    IF conflict_count > 0 THEN
        RAISE EXCEPTION 'cannot reserve slug "new-workspace": % workspace(s) already use it', conflict_count;
    END IF;
END $$;
```

`046_audit_new_workspace_reserved.down.sql`:
```sql
-- No-op: the audit is informational; no schema change to revert.
SELECT 1;
```

**Step 3: Run the migration locally**

```bash
make migrate-up
```
Expected: clean apply. If it fails because a workspace already exists with that slug, **STOP** and ask the user how to handle the conflict (rename? warn the affected workspace owner?).

**Step 4: Commit**

```bash
git add server/migrations/046_audit_new_workspace_reserved.up.sql \
        server/migrations/046_audit_new_workspace_reserved.down.sql
git commit -m "chore(migrations): audit no existing workspace uses 'new-workspace' slug"
```

---

## Phase 3: New `/new-workspace` route on both apps

### Task 3.1: Web — `/new-workspace` page

**Files:**
- Create: `apps/web/app/(auth)/new-workspace/page.tsx`
- Create: `apps/web/app/(auth)/new-workspace/page.test.tsx` (if web app pattern uses page tests; check)

**Step 1: Read the existing `(auth)/onboarding/page.tsx` as template**

Note its auth check pattern + redirect after success.

**Step 2: Write failing test (if applicable)**

(If web app doesn't have unit tests for these pages, skip — covered by E2E in Phase 8.)

**Step 3: Implement**

`apps/web/app/(auth)/new-workspace/page.tsx`:
```tsx
"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { CreateWorkspaceForm } from "@multica/views/workspace/create-workspace-form";

export default function NewWorkspacePage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) router.replace(paths.login());
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  return (
    <div className="flex min-h-svh flex-col items-center justify-center bg-background px-6 py-12">
      <div className="flex w-full max-w-md flex-col items-center gap-6">
        <div className="text-center">
          <h1 className="text-3xl font-semibold tracking-tight">
            Welcome to Multica
          </h1>
          <p className="mt-2 text-muted-foreground">
            Create your workspace to get started.
          </p>
        </div>
        <CreateWorkspaceForm
          onSuccess={(ws) => router.push(paths.workspace(ws.slug).issues())}
        />
      </div>
    </div>
  );
}
```

**Step 4: Manual smoke**

```bash
pnpm dev:web
# Navigate to http://localhost:3000/new-workspace
# Verify form renders. (Don't actually create yet — blocked by daemon-management Phase 7.)
```

**Step 5: Commit**

```bash
git add apps/web/app/\(auth\)/new-workspace/
git commit -m "feat(web): add /new-workspace route"
```

### Task 3.2: Desktop — `/new-workspace` route

**Files:**
- Modify: `apps/desktop/src/renderer/src/routes.tsx`

**Step 1: Add new route**

In `routes.tsx`, add a sibling to `OnboardingRoute` (don't delete OnboardingRoute yet — Phase 6 does that):

```tsx
import { CreateWorkspaceForm } from "@multica/views/workspace/create-workspace-form";

function NewWorkspaceRoute() {
  const nav = useNavigation();
  return (
    <div className="flex min-h-svh flex-col items-center justify-center bg-background px-6 py-12">
      <div className="flex w-full max-w-md flex-col items-center gap-6">
        <div className="text-center">
          <h1 className="text-3xl font-semibold tracking-tight">
            Welcome to Multica
          </h1>
          <p className="mt-2 text-muted-foreground">
            Create your workspace to get started.
          </p>
        </div>
        <CreateWorkspaceForm
          onSuccess={(ws) => nav.push(paths.workspace(ws.slug).issues())}
        />
      </div>
    </div>
  );
}

// In appRoutes children, add (next to onboarding):
{
  path: "new-workspace",
  element: <NewWorkspaceRoute />,
  handle: { title: "Create Workspace" },
},
```

**Step 2: Manual smoke**

```bash
pnpm dev:desktop
# In a tab, navigate to /new-workspace
# Verify form renders
```

**Step 3: Commit**

```bash
git add apps/desktop/src/renderer/src/routes.tsx
git commit -m "feat(desktop): add /new-workspace route"
```

---

## Phase 4: NoAccessPage wired into workspace layouts

### Task 4.1: Web layout renders NoAccessPage on slug not found

**Files:**
- Modify: `apps/web/app/[workspaceSlug]/layout.tsx`

**Step 1: Read current layout**

Note lines 63-69 (the `useEffect` that does `router.replace(paths.root())`) and line 76 (`if (!workspace) return null;`).

**Step 2: Replace silent redirect with NoAccessPage render**

Remove the `useEffect` that calls `router.replace(paths.root())`. Replace the `if (!workspace) return null;` block:
```tsx
import { NoAccessPage } from "@multica/views/workspace/no-access-page";

// ... earlier code unchanged ...

if (isAuthLoading) return null;
if (!listFetched) return null;
if (!workspace) return <NoAccessPage />;

return (
  <WorkspaceSlugProvider slug={workspaceSlug}>
    {children}
  </WorkspaceSlugProvider>
);
```

**Step 3: Manual smoke**

```bash
pnpm dev:web
# Login. Navigate to /some-fake-slug/issues
# Verify NoAccessPage renders, URL stays at /some-fake-slug/issues (no auto-redirect)
# Click "Go to my workspaces" → URL → /
```

**Step 4: Commit**

```bash
git add apps/web/app/\[workspaceSlug\]/layout.tsx
git commit -m "feat(web): show NoAccessPage instead of silently redirecting unknown slugs

Users who hit /unknown-slug/issues now see an explicit message instead
of being magically bounced to their first workspace. This handles:
- Workspace doesn't exist
- Workspace exists but user isn't a member
- Stale bookmark / shared link from a former teammate"
```

### Task 4.2: Desktop layout renders NoAccessPage

**Files:**
- Modify: `apps/desktop/src/renderer/src/components/workspace-route-layout.tsx`

**Step 1: Same change pattern**

Remove the `useEffect` that calls `navigate(paths.root())` (lines 56-59). Replace the `if (!workspace) return null;` (line 68) with `if (!workspace) return <NoAccessPage />;`.

Add import:
```tsx
import { NoAccessPage } from "@multica/views/workspace/no-access-page";
```

**Step 2: Manual smoke**

```bash
pnpm dev:desktop
# In a tab, navigate to /unknown-slug/issues
# Same expected behavior as web
```

**Step 3: Commit**

```bash
git add apps/desktop/src/renderer/src/components/workspace-route-layout.tsx
git commit -m "feat(desktop): show NoAccessPage on unknown workspace slug"
```

---

## Phase 5: Switch all redirects from `/onboarding` → `/new-workspace`

This phase is mostly find-and-replace, but each touch point needs verification because some have nuance (e.g. dashboard guard previously distinguished "loading" from "no workspace").

### Task 5.1: List all call sites

**Step 1: Find every reference**

```bash
grep -rn "paths.onboarding" packages apps server
grep -rn '"/onboarding"' packages apps server  # raw strings, e.g. CLI
```

Expected hits (from earlier investigation):
- `apps/web/app/(auth)/login/page.tsx` (lines ~42, ~56)
- `apps/web/app/auth/callback/page.tsx` (line ~73)
- `apps/web/features/landing/components/redirect-if-authenticated.tsx` (line ~38)
- `apps/desktop/src/renderer/src/routes.tsx` (`IndexRedirect`, ~line 94)
- `packages/views/layout/use-dashboard-guard.ts` (~line 44)
- `packages/views/invite/invite-page.tsx` (~line 37)
- `packages/core/realtime/use-realtime-sync.ts` (~line 274)
- `packages/views/settings/components/workspace-tab.tsx` (~line 60)
- `packages/core/navigation/store.ts` (~line 13, 19 — exclusion list)

**Step 2: For each, change `paths.onboarding()` → `paths.newWorkspace()`**

Mechanical edit. Verify each touched test passes.

**Step 3: Update navigation store exclusion**

`packages/core/navigation/store.ts:13,19` currently filters out `/onboarding` from persisted history. Add `/new-workspace` to that list (and update `store.test.ts` to match).

**Step 4: Run all unit tests**

```bash
pnpm test
```
Expected: green. Fix any test that asserts the old path.

**Step 5: Commit (single commit for all redirect-point changes)**

```bash
git add packages/ apps/web/ apps/desktop/src/renderer/src/routes.tsx
git commit -m "refactor: redirect zero-workspace users to /new-workspace instead of /onboarding"
```

### Task 5.2: CLI — `cmd_login.go`

**Files:**
- Modify: `server/cmd/multica/cmd_login.go` (line ~128)

**Step 1: Update URL string**

Replace:
```go
onboardingURL := appURL + "/onboarding"
```
with:
```go
onboardingURL := appURL + "/new-workspace"
```

Rename the variable (`onboardingURL` → `createWorkspaceURL`) and update the user-facing log message:
```go
fmt.Fprintln(os.Stderr, "\nNo workspaces found. Opening workspace creation in your browser...")
```

**Step 2: Run CLI tests**

```bash
cd server && go test ./cmd/multica/ -v
```
Update any test that asserted the old URL/log message.

**Step 3: Commit**

```bash
git add server/cmd/multica/cmd_login.go
git commit -m "refactor(cli): point login polling at /new-workspace"
```

### Task 5.3: Desktop App.tsx — restart daemon on first workspace creation

**Files:**
- Modify: `apps/desktop/src/renderer/src/App.tsx`

**Step 1: Read current effect**

Lines 57-70: effect depends on `[user]` only.

**Step 2: Add a watcher effect**

Add a separate `useEffect` that subscribes to React Query workspace list and restarts the daemon when count goes from 0 → ≥1. Use the existing query options:

```tsx
import { useQuery } from "@tanstack/react-query";
import { workspaceListOptions } from "@multica/core/workspace/queries";

// inside the App component:
const { data: workspaces } = useQuery({
  ...workspaceListOptions(),
  enabled: !!user,
});
const wsCount = workspaces?.length ?? 0;
const prevCountRef = useRef(0);

useEffect(() => {
  if (!user) {
    prevCountRef.current = 0;
    return;
  }
  if (prevCountRef.current === 0 && wsCount >= 1) {
    // First workspace just appeared — bounce daemon so it picks up
    // the workspace immediately instead of waiting up to 30s for
    // workspaceSyncLoop's next tick.
    void window.daemonAPI.restart();
  }
  prevCountRef.current = wsCount;
}, [user, wsCount]);
```

**Step 3: Manual smoke**

```bash
pnpm dev:desktop
# Login as a brand-new user (no workspace).
# Open daemon panel — should show "stopped" then "starting" once you
# fill out /new-workspace.
# After "Create workspace", daemon should transition to "running" within seconds.
```

**Step 4: Commit**

```bash
git add apps/desktop/src/renderer/src/App.tsx
git commit -m "feat(desktop): restart daemon on first workspace creation

Without this, new users wait up to 30s for the daemon's
workspaceSyncLoop to discover their first workspace. Trigger an
immediate restart so daemon connects within seconds."
```

---

## Phase 6: Delete onboarding (point of no return)

After this phase the codebase no longer has any onboarding code. **Verify Phase 5 completely before starting.**

### Task 6.1: Verify no lingering references

```bash
grep -rn "paths.onboarding\|OnboardingWizard\|StepWorkspace\|StepRuntime\|StepAgent\|StepComplete\|OnboardingGate\|onboarding-wizard\|onboarding-gate" \
  packages apps server --include='*.ts' --include='*.tsx' --include='*.go' \
  | grep -v "docs/plans"
```

Expected: only the files about to be deleted should appear. If anything else hits, **fix in Phase 5** before continuing.

### Task 6.2: Delete onboarding files and remove package export

```bash
git rm -r packages/views/onboarding/
git rm -r apps/web/app/\(auth\)/onboarding/
git rm apps/desktop/src/renderer/src/components/onboarding-gate.tsx
git rm apps/desktop/src/renderer/src/components/onboarding-gate.test.tsx
```

Also edit **`packages/views/package.json`** — remove the onboarding export:

```json
// DELETE this line from the "exports" block:
"./onboarding": "./onboarding/index.ts",
```

Without this, `pnpm typecheck` may still report the export as valid and subsequent imports from `@multica/views/onboarding` won't be caught as broken.

### Task 6.3: Remove onboarding hooks from desktop-layout

**Files:**
- Modify: `apps/desktop/src/renderer/src/components/desktop-layout.tsx`

Remove imports of `StepWorkspace` and `OnboardingGate` (lines 16, 20). Remove the `<OnboardingGate>` wrapper around the shell content (lines 112-144). The `WorkspaceSlugProvider` and inner content stay; just remove the gate wrapper.

### Task 6.4: Remove onboarding route from desktop

**Files:**
- Modify: `apps/desktop/src/renderer/src/routes.tsx`

- Delete `OnboardingWizard` import (line 23).
- Delete `OnboardingRoute` function (lines 62-69).
- Delete the `path: "onboarding"` route entry (lines 122-126).
- In `IndexRedirect` (line 94), change the fallback from `paths.onboarding()` to `paths.newWorkspace()`.

### Task 6.5: Remove `paths.onboarding()` and `/onboarding` from GLOBAL_PREFIXES

**Files:**
- Modify: `packages/core/paths/paths.ts`
- Modify: `packages/core/paths/paths.test.ts`
- Modify: `packages/core/paths/consistency.test.ts` (remove `/onboarding` from any path arrays)
- Modify: `packages/core/navigation/store.ts` (remove `/onboarding` from EXCLUDED_PREFIXES)
- Modify: `packages/core/navigation/store.test.ts` (update test arrays)

Changes:

- In `paths.ts`:
  - Delete the `onboarding: () => "/onboarding",` line (line ~41)
  - Remove `"/onboarding"` from `GLOBAL_PREFIXES` (line ~51)
  - Update the module doc comment (line ~7) — the `paths.onboarding()` example should be removed or replaced with `paths.newWorkspace()`
- Update `paths.test.ts`: remove the `expect(paths.onboarding()).toBe(...)` and `expect(isGlobalPath("/onboarding")).toBe(true)` cases.
- Update `consistency.test.ts`: remove any `/onboarding` entry in the paths-to-test array.
- Update `navigation/store.ts` and `store.test.ts`: remove `/onboarding` from the persist-exclusion list and test assertions.

**DO NOT touch `RESERVED_SLUGS` for `"onboarding"`** — it stays reserved. The frontend `reserved-slugs.ts` and backend `workspace_reserved_slugs.go` both keep `"onboarding"` entries. Reasoning:
- Frontend + backend must stay in sync (comment in both files says "keep in sync")
- Removing carries no current benefit but blocks future re-introduction of an `/onboarding` route
- Cost of keeping: one string entry each side
- Historical migrations (043, 045) audit for this slug — removing it from the list diverges from the audit's historical truth

### Task 6.6: Verify everything compiles and tests pass

```bash
pnpm typecheck
pnpm test
cd server && go test ./...
```

Fix any breakage. Common issues:
- Missing imports (delete unused)
- Test expectations referring to deleted strings

### Task 6.7: Single commit for the deletion

```bash
git add -A
git commit -m "refactor: remove onboarding flow

The 4-step onboarding wizard (workspace → runtime → agent → demo issues)
is replaced by:
- /new-workspace: a single-page workspace creation form
- NoAccessPage: explicit feedback when a slug doesn't resolve
- daemon zero-workspace bootstrap (Phase 1) so the daemon doesn't
  crash before the user creates their first workspace
- desktop daemon restart on first workspace creation for instant pickup

Removed: StepWorkspace, StepRuntime, StepAgent, StepComplete (incl.
the 4 demo issues), OnboardingWizard, OnboardingGate, /onboarding
routes, paths.onboarding(), 'onboarding' reserved slug."
```

---

## Phase 7: Verification

### Task 7.1: Full test suite

```bash
make check
```

All four (typecheck, JS unit, Go, E2E) must pass. Don't proceed if any fail.

### Task 7.2: Manual end-to-end smoke (web + desktop)

Three personas to test for **both web and desktop**:

**Persona A — brand-new user**
1. Sign up (or use a dev account with zero workspaces).
2. Login → expect to land at `/new-workspace`.
3. Fill form → submit → expect redirect to `/{slug}/issues`.
4. **Desktop only:** verify daemon transitions stopped → running within ~5s of workspace creation.

**Persona B — existing user accessing wrong slug**
1. Login as a user with at least one workspace.
2. In address bar, type `/total-fake-slug/issues`.
3. Expect: `NoAccessPage` rendered. URL stays at `/total-fake-slug/issues` (no magic redirect).
4. Click "Go to my workspaces" → expect bounce to `/` then to your first workspace.

**Persona C — happy path workspace switching**
1. Login as user with multiple workspaces.
2. Switch between workspaces via sidebar.
3. Verify URL updates, data refreshes (no regressions from this change).

### Task 7.3: CLI smoke

```bash
# Use a dev account with zero workspaces:
multica login --server-url http://localhost:8080
# Expect browser to open at /new-workspace
# Create workspace in browser
# CLI should detect and continue
```

### Task 7.4: Update PR-ready commit log

```bash
git log main..HEAD --oneline
```
Expected ~10–12 atomic commits with clear scopes (`fix(daemon)`, `refactor(views)`, `feat(web)`, etc.).

---

## Phase 8: PR

### Task 8.1: Open PR

Use the existing PR template / `superpowers:requesting-code-review` skill.

PR title: `refactor: remove onboarding flow + fix daemon zero-workspace bootstrap`

PR body must include:
- **Summary** (3 bullets):
  - Daemon now starts with zero workspaces; `workspaceSyncLoop` discovers new ones (finishes #1001)
  - Onboarding wizard replaced by `/new-workspace` page (single workspace creation step) + explicit `NoAccessPage` for bad slugs
  - Desktop now restarts daemon immediately when user creates their first workspace
- **Test plan** mirroring the three personas in Task 7.2
- **Migration note**: backend deploy must include migration 046; if any workspace already uses slug `new-workspace`, deploy is blocked (audit migration will fail loudly)
- **Behavior changes** (visible to existing users):
  - URL `/{unknown-slug}/...` no longer auto-redirects; shows NoAccessPage
  - `/onboarding` URL is gone; bookmarks 404 (acceptable — onboarding was a one-time URL nobody bookmarks)

---

## Rollback strategy

If post-deploy a critical issue surfaces:

1. **Quick rollback** — revert the PR. The daemon-bootstrap change (Phase 1) is the only behavioral risk for existing users; reverting restores the old "fail with no runtimes" behavior, but since existing users always have workspaces this is fine.
2. **Migration 046** is read-only (audit) — no rollback needed.
3. Any user mid-create at `/new-workspace` when rollback happens will see a 404; rare and acceptable.

---

## Risk register

| Risk | Mitigation |
|---|---|
| Daemon `workspaceSyncLoop` 30s gap feels slow on web (no daemon restart there) | Accept — web users don't run a daemon; runtime connection is irrelevant until they configure one |
| Existing tests assert `paths.onboarding()` | Phase 5/6 updates them; `make check` catches anything missed |
| Workspace slug "new-workspace" collision in production | Migration 046 fails loudly during deploy; sysadmin renames the offending workspace before retrying |
| User bookmarks `/onboarding` | 404 — acceptable, onboarding was a single-use URL; nobody bookmarks it |
| Removing `useEffect` redirect in workspace layouts breaks downstream invariants | Phase 4 manual smoke + E2E catches |

---

## Completion criteria

- [ ] All 8 phases complete; commits atomic and conventional
- [ ] `make check` green
- [ ] All three persona smoke tests pass on both web and desktop
- [ ] CLI smoke passes
- [ ] PR opened with full body, migration note, and reviewers assigned
- [ ] No grep hit for `onboarding` anywhere except `docs/plans/` and historical PR descriptions
