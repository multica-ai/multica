# Unify Workspace Identity Resolver Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix broken file uploads caused by the workspace slug refactor (v2, PR #1138/#1141), and eliminate the structural bug source that allowed it. File uploads from within a workspace on the desktop and web apps currently land in S3 without a corresponding DB attachment record — the file is orphaned and the UI never sees it.

**Architecture:** The server currently has **two independent implementations** of the same logic — extract the workspace UUID from an HTTP request. One lives in the workspace middleware (post-v2, accepts slug header → DB lookup → UUID). The other lives inside the handler package (pre-v2, only accepts UUID header/query). The v2 refactor updated the middleware one and forgot the handler one; routes that sit *outside* the workspace middleware group (notably `/api/upload-file`) still run through the stale resolver and can't translate the frontend's new `X-Workspace-Slug` header.

The root cause is duplication. The fix is to collapse both resolvers into a single shared function that middleware and handlers both delegate to, so any future change to "how do we read workspace identity" is impossible to forget. The existing middleware's resolver already has the full logic; we extract it into a package-level function and have the handler helper call it.

**Tech Stack:** Go (Chi router, sqlc, pgx).

**Non-goals:**
- No frontend changes. The frontend has been sending `X-Workspace-Slug` since v2; this plan makes the server finish accepting it everywhere.
- No route reshuffling. `/api/upload-file` stays outside `RequireWorkspaceMember` because it serves two distinct use cases (avatar upload + workspace attachment); the avatar path needs to work without a workspace context.
- No change to CLI / daemon clients. They still send `X-Workspace-ID` (UUID); the resolver keeps UUID as a fallback.

---

## Overview

| # | Change | Type | Files |
|---|--------|------|-------|
| 1 | Extract shared resolver into middleware package | Refactor | `server/internal/middleware/workspace.go` |
| 2 | Promote handler `resolveWorkspaceID` to `(h *Handler).resolveWorkspaceID` + delegate to shared | Refactor | `server/internal/handler/handler.go` |
| 3 | Rename 47 call sites from `resolveWorkspaceID(r)` → `h.resolveWorkspaceID(r)` | Mechanical | handler/*.go (see exhaustive list in task 3) |
| 4 | Add test for upload-file with slug header | Test | `server/internal/handler/file_test.go` |
| 5 | Add test for shared resolver | Test | `server/internal/middleware/workspace_test.go` |
| 6 | `make check` and commit | Verify | — |

---

## Background: what's broken and why

**Frontend (current, post-v2):** `ApiClient.authHeaders()` in `packages/core/api/client.ts:121` sends:
```
X-Workspace-Slug: <slug>
```

**Server middleware resolver** (`server/internal/middleware/workspace.go:53-86`, `resolveWorkspaceUUID`): accepts the slug header, looks up the slug via `queries.GetWorkspaceBySlug`, and writes the resolved UUID into the request context. Every handler behind `RequireWorkspaceMember` / `RequireWorkspaceRole` / `RequireWorkspaceMemberFromURL` sees the UUID in context and works correctly.

**Handler resolver** (`server/internal/handler/handler.go:155-165`, `resolveWorkspaceID`): a parallel implementation used by handlers that are NOT behind the workspace middleware. It only checks:
1. `middleware.WorkspaceIDFromContext(r.Context())`
2. `?workspace_id` query param
3. `X-Workspace-ID` header

Never touches slug, because it has no `*db.Queries` access (it's a package-level function, not a method).

**Impact:** `/api/upload-file` (registered at `server/cmd/server/router.go:166`, in the user-scoped group, outside workspace middleware) calls `resolveWorkspaceID(r)`, gets `""` because the frontend only sends slug, thinks "no workspace context", and silently skips the DB attachment record creation (`server/internal/handler/file.go:235-245`). The file reaches S3; the UI never sees it.

**Why `/api/upload-file` is outside workspace middleware:** it serves both "avatar upload (no workspace)" and "attachment upload (with workspace)", branching on the resolved workspace ID inside the handler. Moving it under `RequireWorkspaceMember` would break avatar uploads.

**Structural root cause:** two resolvers, same job, divergent capabilities. The duplication is what let v2 ship "mostly working" — most handlers live behind middleware, so the broken handler resolver had a low blast radius that wasn't caught in review.

---

### Task 1: Extract shared resolver into middleware package

**Problem:** The middleware's `resolveWorkspaceUUID` closure captures `*db.Queries` and can look up slugs. The handler's `resolveWorkspaceID` is a bare package-level function without queries access. We need a single implementation both sides can reuse. Putting it in the `middleware` package is fine — the `handler` package already imports `middleware`.

**Files:**
- Modify: `server/internal/middleware/workspace.go`

**Step 1: Add `ResolveWorkspaceIDFromRequest` export**

After `errWorkspaceNotFound` (around line 45), add a package-level exported function that takes `(r *http.Request, queries *db.Queries)` and returns the workspace UUID as a string (empty if none found or slug doesn't resolve).

Priority order (mirrors `resolveWorkspaceUUID`, plus a context lookup first so handlers behind middleware still get the fast path):

```go
// ResolveWorkspaceIDFromRequest returns the workspace UUID for an HTTP
// request, using the same priority order as the workspace middleware.
// Handlers behind workspace middleware get it from context (cheap); handlers
// outside middleware (e.g. /api/upload-file) still resolve slug → UUID via
// a DB lookup instead of silently falling through to "no workspace".
//
// Priority:
//  1. middleware-injected context (if the route is behind workspace middleware)
//  2. X-Workspace-Slug header → GetWorkspaceBySlug → UUID (post-refactor frontend)
//  3. ?workspace_slug query → GetWorkspaceBySlug → UUID
//  4. X-Workspace-ID header (CLI/daemon compat)
//  5. ?workspace_id query (CLI/daemon compat)
//
// Returns "" when no identifier was provided OR a slug was provided but doesn't
// resolve to any workspace. Callers that need the "slug provided but invalid"
// distinction should use the resolver inside the middleware directly.
func ResolveWorkspaceIDFromRequest(r *http.Request, queries *db.Queries) string {
    if id := WorkspaceIDFromContext(r.Context()); id != "" {
        return id
    }
    if slug := r.Header.Get("X-Workspace-Slug"); slug != "" {
        if ws, err := queries.GetWorkspaceBySlug(r.Context(), slug); err == nil {
            return util.UUIDToString(ws.ID)
        }
    }
    if slug := r.URL.Query().Get("workspace_slug"); slug != "" {
        if ws, err := queries.GetWorkspaceBySlug(r.Context(), slug); err == nil {
            return util.UUIDToString(ws.ID)
        }
    }
    if id := r.Header.Get("X-Workspace-ID"); id != "" {
        return id
    }
    return r.URL.Query().Get("workspace_id")
}
```

**Step 2: Refactor `resolveWorkspaceUUID` to delegate**

The existing middleware closure has slightly different semantics (returns `errWorkspaceNotFound` when a slug was provided but doesn't resolve, so middleware can 404 instead of 400). Keep that, but share the resolution logic:

Leave `resolveWorkspaceUUID` as-is for now — it distinguishes "no identifier" (400) from "invalid slug" (404). `ResolveWorkspaceIDFromRequest` returns "" in both cases because handler-level callers don't need that distinction (they just check for empty).

Document in a comment near `resolveWorkspaceUUID` that it's an internal variant that preserves the error distinction for middleware gating, and point to `ResolveWorkspaceIDFromRequest` as the handler-facing API.

**Step 3: Build and verify**

```bash
cd server && go build ./...
```
Expected: clean build.

**Step 4: Commit**

```
refactor(server): extract ResolveWorkspaceIDFromRequest from middleware

Introduces a shared helper that consolidates the workspace-identity
resolution logic used by both the workspace middleware and the handler
package. No behavior change yet — callers still use the old functions.
Sets up the next commit to fix the /api/upload-file slug bug by routing
the handler-side resolver through this shared function.
```

---

### Task 2: Promote handler resolver to a method + delegate

**Problem:** The package-level `resolveWorkspaceID(r *http.Request)` in `handler.go` can't call `GetWorkspaceBySlug` because it has no queries access. Promoting it to a method on `*Handler` gives it access to `h.Queries` at no syntactic cost elsewhere.

**Files:**
- Modify: `server/internal/handler/handler.go:155-165`

**Step 1: Replace `resolveWorkspaceID` with a Handler method**

```go
// resolveWorkspaceID resolves the workspace UUID for this request.
// Delegates to middleware.ResolveWorkspaceIDFromRequest so routes inside
// and outside workspace middleware see identical resolution behavior.
//
// Returns "" when no workspace identifier was provided or a slug was
// provided but doesn't match any workspace.
func (h *Handler) resolveWorkspaceID(r *http.Request) string {
    return middleware.ResolveWorkspaceIDFromRequest(r, h.Queries)
}
```

Delete the old package-level `resolveWorkspaceID` function.

**Step 2: Build — expect errors at 47 call sites**

```bash
cd server && go build ./... 2>&1 | head -60
```

Expected: `resolveWorkspaceID is not a value` or `undefined: resolveWorkspaceID` errors at each existing call site. That's the signal to run Task 3.

**Do not commit yet.** Task 2 and 3 are a single logical change; they commit together after Task 3 fixes the compile.

---

### Task 3: Rename 47 call sites to `h.resolveWorkspaceID(r)`

**Problem:** Every `resolveWorkspaceID(r)` call in the handler package now fails to compile because the function became a method. All 47 call sites are inside methods on `*Handler` (or similar receiver types that have access to `h`), so the rename is mechanical.

**Files affected** (verified via `grep -rn "resolveWorkspaceID" server/internal/handler/`):

- `server/internal/handler/handler.go:275, 365, 388` (3 sites)
- `server/internal/handler/issue.go:447, 559, 731, 783, 1294, 1476` (6 sites)
- `server/internal/handler/activity.go:133` (1 site)
- `server/internal/handler/autopilot.go:178, 203, 255, 306, 386, 414, 490, 578, 615, 662` (10 sites)
- `server/internal/handler/project.go:80, 127, 150, 192, 273, 430` (6 sites)
- `server/internal/handler/comment.go:443, 510` (2 sites)
- `server/internal/handler/runtime.go:207, 247, 296` (3 sites)
- `server/internal/handler/pin.go:59, 105, 175, 202` (4 sites)
- `server/internal/handler/reaction.go:43, 110` (2 sites)
- `server/internal/handler/skill.go:126, 146, 187, 384, 815` (5 sites)
- `server/internal/handler/agent.go:158, 254` (2 sites)
- `server/internal/handler/file.go:83, 115, 282, 306` (4 sites)

Total: 48 (the resolver declaration itself + 47 callers).

**Step 1: Mechanical rename**

For each file above, change every `resolveWorkspaceID(r)` to `h.resolveWorkspaceID(r)`. In the one case in `file.go:83` inside `groupAttachments`, the receiver is already `*Handler`, so the method is accessible.

**Semantic check:** all 47 call sites are on methods with an `h *Handler` receiver (verifiable by scrolling up a few lines from each grep match). If any call site is inside a non-method function, that site needs to either take `*Handler` as a parameter or be skipped from this rename. Spot-check three sites before doing the rename.

**Step 2: Build**

```bash
cd server && go build ./...
```
Expected: clean build.

**Step 3: Run Go tests**

```bash
cd server && go test ./...
```
Expected: all pass. The 46 call sites behind workspace middleware hit the context branch (identical behavior to before). Only `UploadFile` gains new capability (slug resolution); it wasn't tested before, will be covered in Task 4.

**Step 4: Commit**

```
fix(server): resolve X-Workspace-Slug in /api/upload-file and other middleware-less handlers

The v2 workspace URL refactor updated the workspace middleware to accept
X-Workspace-Slug but left the handler-package resolveWorkspaceID helper
(used by handlers outside the middleware group) stuck on X-Workspace-ID.
The frontend switched to the slug header, so /api/upload-file was
receiving a slug it couldn't translate to a UUID, silently falling
through to the avatar-upload branch and skipping DB attachment record
creation — files were landing in S3 with no database reference.

Promote resolveWorkspaceID to a Handler method and delegate to the new
middleware.ResolveWorkspaceIDFromRequest so middleware-behind and
middleware-outside handlers share the same resolution logic. The 46
call sites that live inside the workspace middleware group are
unaffected (context lookup still wins). /api/upload-file now correctly
recognizes slug requests and creates the attachment record.

Fixes: missing DB attachment rows for files uploaded since v2 (#1141)
```

---

### Task 4: Add handler test for upload-file with slug header

**Problem:** The bug manifested exactly because there was no test covering the "upload-file with only a slug header" code path. Prevent regression.

**Files:**
- Modify: `server/internal/handler/file_test.go` (or create if absent)

**Step 1: Locate existing upload-file test infrastructure**

```bash
grep -rn "UploadFile\|upload-file" server/internal/handler/*_test.go
```

If there's an existing upload-file test, add a new test case alongside it. If not, scaffold one using the same `handler_test.go` fixture pattern (`testWorkspaceID`, `testUserID`, seeded workspace).

**Step 2: Write the test**

Test name: `TestUploadFile_ResolvesWorkspaceViaSlugHeader`.

Flow:
1. Seed a workspace with a known slug and the default test user as a member.
2. POST a multipart form to `/api/upload-file` with an `issue_id` field referencing a seeded issue, with only `X-Workspace-Slug: <slug>` in headers (no `X-Workspace-ID`).
3. Assert response is 200.
4. Assert a DB row exists in `attachments` with the expected `workspace_id`, `uploader_id`, `issue_id`, and `filename`.

Anti-regression: also add `TestUploadFile_ResolvesWorkspaceViaIDHeaderStill` to confirm legacy `X-Workspace-ID` header still works (CLI / daemon compat).

**Step 3: Run the new test**

```bash
cd server && go test ./internal/handler/ -run UploadFile
```
Expected: both pass.

**Step 4: Commit**

```
test(server): cover upload-file slug and UUID header resolution

Regression test for the v2 refactor bug: uploads from the frontend
(which sends X-Workspace-Slug) now reach the workspace-aware branch
and create attachment records.
```

---

### Task 5: Add unit test for the shared resolver

**Problem:** The shared function will be the single point through which all workspace identity resolution flows. It deserves table-driven test coverage for each priority level.

**Files:**
- Create or modify: `server/internal/middleware/workspace_test.go`

**Step 1: Table test**

Cases to cover:
- Context UUID present → returns context UUID, ignores headers/query
- Only `X-Workspace-Slug` → DB lookup succeeds → returns UUID
- Only `X-Workspace-Slug` → DB lookup fails → returns ""
- Only `?workspace_slug` → DB lookup succeeds → returns UUID
- Only `X-Workspace-ID` → returns UUID
- Only `?workspace_id` → returns UUID
- Slug header + UUID header both present → slug wins (frontend priority)
- Nothing → returns ""

**Step 2: Run**

```bash
cd server && go test ./internal/middleware/ -run ResolveWorkspaceIDFromRequest
```
Expected: all cases pass.

**Step 3: Commit**

```
test(server): table-driven coverage for ResolveWorkspaceIDFromRequest

Pins down the priority order (context > slug header > slug query >
UUID header > UUID query) so future changes can't silently diverge.
```

---

### Task 6: Full verification

**Step 1: `make check`**

```bash
make check
```
Expected: typecheck, TS tests, Go tests, E2E (if backend+frontend up) all green.

**Step 2: Manual smoke test**

1. Start desktop dev environment.
2. Open an issue, attach a file via drag-and-drop or the file picker.
3. Refresh the issue. The attachment should appear in the attachments list.

Before this fix: attachment silently disappears on refresh (file is in S3, DB has no row).

**Step 3: Open PR**

Branch name: `fix/unify-workspace-identity-resolver`.

Title: `fix(server): resolve X-Workspace-Slug in middleware-less handlers`

Body should:
- Link to the symptom PR (v2 refactor #1141) and reference that it's a latent follow-up.
- Describe the structural change (two resolvers → one).
- Note that 46 of 47 call sites see zero behavior change (context branch wins); only `/api/upload-file` gains capability.

---

## Risk / blast radius

**Low risk.** The 46 middleware-protected callers hit the context branch in `ResolveWorkspaceIDFromRequest` identically to how they hit `WorkspaceIDFromContext` before — zero semantic change. The only new code path exercised in production is the slug-header branch for `/api/upload-file`, which is already exercised by every other slug-header-carrying request (just via the middleware's version of the same logic). Task 4 and 5 lock the behavior down with tests.

## Rollback plan

If a regression surfaces after deploy, revert the single commit from Task 3. `ResolveWorkspaceIDFromRequest` and the Handler method remain but are unused — harmless dead code until the next attempt.
