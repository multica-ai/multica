# File Tree & File Preview for Agent Worktrees Implementation Plan

**Goal:** Give users visibility into an agent's working directory from the issue detail page — a live file tree with git status, a file preview with markdown/mermaid/syntax highlighting, and a delta view that shows the agent's changes as a git diff. Feature parity with agent-orchestrator's workspace panel.

**Architecture:** The daemon scans each active task's worktree and streams snapshots to the server, which broadcasts them over the existing WS channel. File content and diffs are fetched on demand via a server-side proxy to the daemon's health HTTP port. Completed worktrees are restored on daemon startup from a `.multica_task_id` marker so closed tasks remain browsable. On the frontend the workspace panel lives inside the issue detail `ResizablePanelGroup` and reuses the shared `Markdown` / `CodeBlock` components plus a new lazy-loaded `MermaidDiagram`.

**Tech Stack:** Go backend (Chi, gorilla/websocket), TanStack Query, Zustand (persisted per-agent selection), shiki (existing, reused for both syntax highlighting and the built-in `diff` language), mermaid (new dependency, lazy-loaded).

**Branch:** `feat/file-tree-and-preview`

---

## Overview

| # | Area | Type | Key files |
|---|------|------|-----------|
| 1 | Daemon file-tree scanner & watcher | New package | `server/internal/daemon/filetree/` |
| 2 | Daemon worktree restoration on restart | Backend change | `server/internal/daemon/daemon.go` |
| 3 | Daemon health endpoints: file content + diff | Backend change | `server/internal/daemon/health.go` |
| 4 | Server WS event + proxy endpoints | Backend change | `server/internal/handler/daemon.go`, `server/pkg/protocol/{events,messages}.go`, `server/cmd/server/router.go` |
| 5 | Frontend core types, queries, store | TS package | `packages/core/{types,api,issues}` |
| 6 | Workspace browser UI (tree + preview + diff) | New components | `packages/views/issues/components/workspace-*.tsx` |
| 7 | Issue detail layout integration | Component change | `packages/views/issues/components/issue-detail.tsx` |
| 8 | Sidebar trigger moved into workspace headers | UI polish | `packages/views/layout/dashboard-layout.tsx` + 12 page headers |
| 9 | Repo cache SSH URL normalization | Bug fix | `server/internal/daemon/repocache/cache.go` |

---

## Data Flow

```
Daemon task lifecycle
  ├─ task starts  → write .multica_task_id marker into workdir
  │                 register taskID → workdir in taskWorkDirs sync.Map
  │                 start filetree.Watcher polling every 3s with debounce
  ├─ watcher tick → filetree.ScanSnapshot(workdir)
  │                 POST /api/daemon/tasks/{taskID}/file-tree  (tree + git_status)
  │                 server stores last snapshot in fileTreeCache (in-memory, per task)
  │                 server broadcasts WS "task:file_tree" to the workspace
  └─ daemon start → restoreWorktrees() walks workspacesRoot for .multica_task_id
                    re-populates taskWorkDirs + pushes a fresh snapshot

Frontend on-demand
  GET /api/issues/{id}/tasks/{taskID}/file-tree   (REST fallback on mount)
  GET /api/issues/{id}/tasks/{taskID}/files/{path} (proxied to daemon health port)
  GET /api/issues/{id}/tasks/{taskID}/diff/{path}  (proxied; returns diff OR content for untracked)
```

---

## Task 1: Daemon — filetree package

**Files:**
- New: `server/internal/daemon/filetree/types.go`
- New: `server/internal/daemon/filetree/scanner.go`
- New: `server/internal/daemon/filetree/watcher.go`

### Types

`FileNode { Name, Path, IsDir, Children }` for the tree. `GitStatus` enum (`modified`, `added`, `deleted`, `renamed`, `untracked`). `FileTreeSnapshot { Tree []*FileNode; GitStatus map[string]GitStatus }` — git status keyed by repo-relative path with a subdir prefix when the workdir contains nested repos.

### Scanner

`ScanSnapshot(workDir)` walks the workdir with VSCode-parity ignore rules (`.git`, `node_modules`, `.next`, `dist`, `build`, `__pycache__`, `.DS_Store`, `*.pyc`, `.env*`, `vendor`, `coverage`, `.turbo`, `.cache`), builds the `FileNode` tree, then calls `collectGitStatus` for each nested repo it finds (supports both "workdir is a repo" and "workdir contains one subdir per cloned repo" layouts).

`FindBaseRef(repoPath)` — exported helper that returns the merge-base SHA between `HEAD` and the first existing ref in `origin/main`, `origin/master`, `main`, `master`. Used by both the scanner (for `git diff --name-status <base>`) and the diff endpoint, so status badges and diff output always agree on the same base.

`collectGitStatus` merges two sources so the UI sees both committed and uncommitted work:
1. `git diff --name-status <base>` — committed work on the agent's feature branch PLUS tracked uncommitted changes.
2. `git status --porcelain=v1 -uall` — untracked files + overrides for unstaged modifications/deletions.

### Watcher

Periodic (3s) poll with content-hash debounce. When the scan produces a different snapshot than the previous one, call a change callback. Start/stop is driven by the task lifecycle in `daemon.go`. Polling was chosen over fsnotify to match agent-orchestrator's approach: simple, no per-platform inotify/FSEvents limits, and a few seconds of latency is imperceptible in practice.

---

## Task 2: Daemon — worktree lifecycle & restoration

**Files:**
- Modify: `server/internal/daemon/daemon.go`
- Modify: `server/internal/daemon/client.go`

### Task lifecycle wiring

When a task's runner comes up, store `taskID → env.WorkDir` in a `sync.Map` (`taskWorkDirs`), write a `.multica_task_id` marker into the workdir, and start a `filetree.Watcher`. The watcher calls `client.ReportFileTree(ctx, taskID, snapshot)` which POSTs to `/api/daemon/tasks/{taskID}/file-tree`.

### Restoration on daemon start

`restoreWorktrees(ctx)` walks `{workspacesRoot}/{workspaceID}/{shortTaskID}/workdir/.multica_task_id`. For each marker it re-populates `taskWorkDirs`, runs `filetree.ScanSnapshot`, and pushes a one-shot snapshot to the server. This is what lets the UI browse previously-completed tasks after a daemon restart — without it, `d.taskWorkDirs.Load(taskID)` returns `false` for any task that wasn't currently running, and both the files and diff endpoints 404.

### Client

`ReportFileTree(ctx, taskID, snapshot) error` — plain POST with the snapshot payload. No retry logic; the next watcher tick will re-send.

---

## Task 3: Daemon — health endpoints

**Files:**
- Modify: `server/internal/daemon/health.go`

### `GET /tasks/{taskID}/files/{path...}`

Looks up `taskWorkDirs`, validates the resolved path doesn't escape the workdir, rejects binaries by extension (`.png`, `.jpg`, `.pdf`, `.zip`, …), caps size at 1MB, and returns `{content, path, size, mtime}`. ETag based on mtime+size so repeated reads on unchanged files can 304.

### `GET /tasks/{taskID}/diff/{path...}`

Same task lookup and traversal guards, then:

1. `findEnclosingRepo(workDir, relFilePath)` — returns the repo dir and repo-relative path, handling both "workdir is a repo" and "workdir has one nested-subdir-per-repo" layouts.
2. Check `git status --porcelain -- <relPath>`. If prefixed with `??`, treat as untracked: return `{status: "?", diff: null, content: <file body>}` so the frontend can still show the file.
3. Otherwise run `git -C <repo> diff <FindBaseRef> -- <relPath>`. Treat exit code 1 as "differences found", not an error. Reject binary diffs (`Binary files … differ`) and oversized diffs (>512KB).
4. Derive status from the diff header (`new file mode` → `A`, `deleted file mode` → `D`, else `M`) and return `{status, diff, content: null}`.

ETag computed from `sha256(diff + content)` so polling on a stable file avoids re-transfer.

---

## Task 4: Server — WS event, proxy, and in-memory cache

**Files:**
- Modify: `server/pkg/protocol/events.go`, `server/pkg/protocol/messages.go`
- Modify: `server/internal/handler/daemon.go`, `server/internal/handler/handler.go`, `server/internal/handler/agent.go`
- Modify: `server/cmd/server/router.go`

### Protocol

Add `EventTaskFileTree = "task:file_tree"` and `TaskFileTreePayload { IssueID, TaskID, Tree, GitStatus }`.

### Handlers

- `ReportTaskFileTree` — receives the snapshot from the daemon, looks up the issue for WS room routing, stores the raw payload in `fileTreeCache` (a `sync.Map` on the handler), and broadcasts the WS event to the workspace.
- `GetTaskFileTree` — serves the most recent cached snapshot so the frontend can render immediately on mount without waiting for a WS tick.
- `ProxyTaskFileContent` / `ProxyTaskFileDiff` — both wrap `proxyTaskFileRequest`, which resolves the task's runtime, reads `health_port` from the runtime metadata, forwards the GET to `http://127.0.0.1:<healthPort>/tasks/{taskID}/{kind}/{path}`, and streams the response back (with `If-None-Match` forwarding for ETag pass-through).

### Routes

Under `/api/issues/{id}`:
```go
r.Get("/tasks/{taskId}/file-tree", h.GetTaskFileTree)
r.Get("/tasks/{taskId}/files/*",   h.ProxyTaskFileContent)
r.Get("/tasks/{taskId}/diff/*",    h.ProxyTaskFileDiff)
```

---

## Task 5: Frontend — core layer

**Files:**
- Modify: `packages/core/types/{events,agent,index}.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/issues/queries.ts`
- New: `packages/core/issues/stores/worktree-view-store.ts`

### Types

Add `"task:file_tree"` to `WSEventType` and `TaskFileTreePayload`, `FileNode`, `TaskFileTreeData` to the agent types.

### API methods

```ts
getTaskFileTree(issueId, taskId)
getTaskFileContent(issueId, taskId, filePath)  // returns { content, path, size, mtime }
getTaskFileDiff(issueId, taskId, filePath)     // returns { path, status, diff, content }
```

### Query factories

```ts
issueKeys.taskFileTree(issueId, taskId)
issueKeys.taskFileContent(issueId, taskId, filePath)
issueKeys.taskFileDiff(issueId, taskId, filePath)
```

With `staleTime: 10_000` and `enabled: !!filePath` on the content/diff queries.

### Persisted selection store

`worktree-view-store.ts` keeps a per-`(issueId, agentId)` selected path in a Zustand store persisted via the platform StorageAdapter, so switching between agents inside an issue restores the last-viewed file. Lives in `packages/core/issues/stores/` per the hard rule that all shared stores live in core.

---

## Task 6: Frontend — workspace browser components

**Files:**
- New: `packages/views/issues/components/workspace-browser.tsx`
- New: `packages/views/issues/components/workspace-file-tree.tsx`
- New: `packages/views/issues/components/workspace-file-preview.tsx`
- New: `packages/views/issues/components/mermaid-diagram.tsx`
- New: `packages/views/issues/hooks/use-task-file-tree.ts`

### `useTaskFileTree(issueId, taskId, agentId?, diffMode)`

Pulls the tree from `taskFileTreeOptions` and, depending on `diffMode`, runs either `taskFileContentOptions` or `taskFileDiffOptions` for the selected file (never both). Subscribes to `task:file_tree` WS events via `useWSEvent` and writes the fresh tree directly into the Query cache with `qc.setQueryData` — the WS event acts as a push update, not a refetch trigger. On a `task:file_tree` event (and on WS reconnect) it invalidates both content and diff caches so stale files aren't displayed. Returns `{tree, gitStatus, selectedPath, selectFile, fileContent, contentLoading, fileDiff, diffLoading}`.

### `WorkspaceFileTree`

Hierarchical tree built from the flat `FileNode[]` the daemon returns. Git status badges (M/A/D/R/?) pulled from the `gitStatus` map, "delta only" filter that collapses the tree to just paths that appear in `gitStatus`, auto-expand to the selected file. Styling follows the existing skills file-tree pattern (design tokens, lucide icons, `bg-muted` hover, `bg-accent` selected).

### `WorkspaceFilePreview`

Takes either a file content result or a diff result plus a `diffMode` flag. Renders states in order:

- No path → empty state
- Loading → skeleton with path header
- `diffMode`:
  - No diff data → "Unable to load diff" fallback
  - Untracked (`status === "?"` or `"A"`) with `content` present → render via `CodeBlock` with the file's own language (so new files still get syntax highlighting)
  - Otherwise → render the unified diff via `CodeBlock` with `language="diff"` (shiki ships a built-in `diff` grammar, no extra dep)
  - Empty diff body → "No changes in this file"
- Non-diff mode:
  - Markdown files → `MarkdownWithMermaid` wrapper (intercepts ` ```mermaid ` blocks, replaces with placeholders, renders `MermaidDiagram` between `Markdown` chunks)
  - Everything else → `CodeBlock` with language detection from file extension

### `MermaidDiagram`

Client-only component that lazy-imports `mermaid`, initializes with the dark theme and strict security level, and renders the diagram into a div ref. Lazy-loading keeps mermaid out of the initial bundle.

### `WorkspaceBrowser`

Top-level panel. Owns the delta-only toggle, hosts the file tree on the left and preview on the right inside a nested `ResizablePanelGroup`, wires `deltaOnly → diffMode` through to `useTaskFileTree`. Titled "Agent Worktree" to match the product language.

---

## Task 7: Frontend — issue detail integration

**Files:**
- Modify: `packages/views/issues/components/issue-detail.tsx`

The issue detail's outer `ResizablePanelGroup` gets a new panel between content and the right properties sidebar that mounts `WorkspaceBrowser` when there's an active-or-recent task with a worktree. Panel layout is persisted via `useDefaultLayout`, so users who collapse the worktree panel keep it collapsed across reloads.

---

## Task 8: Sidebar toggle always visible

**Files:**
- Modify: `packages/views/layout/dashboard-layout.tsx`
- Modify: `packages/views/issues/components/issue-detail.tsx`
- Modify: `packages/views/issues/components/issues-page.tsx`
- Modify: `packages/views/my-issues/components/my-issues-page.tsx`
- Modify: `packages/views/inbox/components/inbox-page.tsx`
- Modify: `packages/views/projects/components/projects-page.tsx`
- Modify: `packages/views/projects/components/project-detail.tsx`
- Modify: `packages/views/agents/components/agents-page.tsx`
- Modify: `packages/views/runtimes/components/runtimes-page.tsx`
- Modify: `packages/views/runtimes/components/runtime-list.tsx`
- Modify: `packages/views/skills/components/skills-page.tsx`
- Modify: `packages/views/settings/components/settings-page.tsx`

Remove the `md:hidden` dedicated top bar from `dashboard-layout.tsx` and wrap children in `SidebarInset`. Add a shared `<SidebarTrigger />` to the left of every page's workspace/title header so the sidebar can be collapsed on desktop without a separate top bar eating vertical space. Padding shifts from `px-4` to `pl-2 pr-4` in each header to accommodate the trigger.

Sidebar uses `collapsible="offcanvas"` which already supports desktop collapse — this task is purely about making the trigger reachable at all breakpoints.

---

## Task 9: Repo cache — GitHub SSH normalization + non-interactive git

**Files:**
- Modify: `server/internal/daemon/repocache/cache.go`
- Modify: `server/internal/daemon/repocache/cache_test.go`

Discovered while testing end-to-end: the daemon was prompting for GitHub credentials on every clone because workspace repo URLs are stored as HTTPS and `git clone <https-url>` hits the credential prompt for private repos. The intended behavior is "use the SSH agent key the user already has."

### Changes

- `normalizeRepoURL(url)` — rewrites `http(s)://github.com/owner/repo(.git)` → `git@github.com:owner/repo.git`. Non-GitHub URLs and already-SSH URLs pass through unchanged (can't assume GitLab/Bitbucket users have SSH set up). Applied in both `Sync` (clone path) and `Lookup` (worktree creation path).
- `ensureRemoteURL(barePath, wantURL)` — on every `Sync` of a cached repo, reads `remote.origin.url` and `git remote set-url origin` if it differs. Self-heals caches that were cloned before this fix.
- `gitCmd(args...)` — all network-touching git calls (`clone`, `fetch`, `remote set-head --auto`, `remote set-url`) run with `GIT_TERMINAL_PROMPT=0` and `GIT_ASKPASS=/bin/true` so any future auth failure errors out immediately instead of hanging the daemon sync loop on a stdin prompt the user can't reach.
- `TestNormalizeRepoURL` pins the rewrite rules (GitHub HTTP→SSH, idempotent on SSH, pass-through for GitLab/Bitbucket, degenerate inputs).

---

## Risks / Open Questions (Resolved)

| # | Question | Resolution |
|---|----------|------------|
| 1 | Where in the issue detail UI should the workspace panel appear? | Inside the existing `ResizablePanelGroup`, between content and properties sidebar. Only mounted when a task with a worktree exists. |
| 2 | Daemon proxy via health port or pending-request? | Health port proxy — low latency, simpler, works for local runtimes (the only runtime kind today). Cloud runtimes will need a different path when they land. |
| 3 | Large repos / ignore patterns? | VSCode-parity deny-list in the scanner (see Task 1). |
| 4 | fsnotify vs polling for file changes? | 3s poll with content-hash debounce. Matches agent-orchestrator, avoids per-platform watcher limits. |
| 5 | Path traversal safety | Daemon and server both validate the resolved path is inside the workdir before reading. |
| 6 | Completed tasks — can the user still browse? | Yes, via `restoreWorktrees` at daemon startup (Task 2). `.multica_task_id` is the marker that survives restarts. |
| 7 | Mermaid bundle size | Lazy-loaded via dynamic import inside `MermaidDiagram`. |
| 8 | Should delta mode fetch content AND diff? | No — `useTaskFileTree` gates the two queries on `diffMode` so only one is active at a time. Prevents both firing and cluttering the cache. |
| 9 | Shared sidebar trigger — own component or reuse? | Reuse the existing `SidebarTrigger` from `@multica/ui/components/ui/sidebar`. No new abstraction needed. |

---

## Validation

- **Unit tests:**
  - `TestBareDirName`, `TestNormalizeRepoURL`, `TestIsBareRepo` in `server/internal/daemon/repocache/cache_test.go`.
  - Filetree scanner tests in `server/internal/daemon/filetree/`.
- **Manual end-to-end verified:**
  - Daemon's health endpoint returns a correct unified diff for `multica/docs/design.md` in the running task workdir.
  - `normalizeRepoURL` correctness pinned by unit tests; end-to-end clone verified against the user's private GitHub repo.
  - Typecheck clean on `@multica/views` and `@multica/core`.
- **Known-stale-binary gotcha:** the diff route is registered in `router.go` and `daemon.go`, but a long-running `go run ./cmd/server/` will not have it until restarted. Symptom: frontend shows "Unable to load diff" for every file. Fix: restart the server (`make stop && make start`).

---

## Checklist

### Phase 1 — Daemon: scanner + watcher + reporter

- [x] **1.1** `filetree/types.go` — `FileNode`, `GitStatus`, `FileTreeSnapshot`
- [x] **1.2** `filetree/scanner.go` — walker, ignore rules, `FindBaseRef`, `collectGitStatus` (diff against base + porcelain merge)
- [x] **1.3** `filetree/watcher.go` — 3s polling with content-hash debounce
- [x] **1.4** `daemon.Client.ReportFileTree`
- [x] **1.5** Start/stop watcher in task lifecycle; populate `taskWorkDirs`; write `.multica_task_id` marker
- [x] **1.6** `restoreWorktrees` on daemon startup — re-populate `taskWorkDirs` and push a snapshot

### Phase 2 — Daemon: health endpoints

- [x] **2.1** `GET /tasks/{id}/files/{path...}` — path traversal guard, binary detection, 1MB cap, ETag
- [x] **2.2** `GET /tasks/{id}/diff/{path...}` — enclosing-repo resolution, untracked-as-content shortcut, `git diff <base>` with binary/size caps, derived M/A/D status

### Phase 3 — Server: protocol, handlers, routes

- [x] **3.1** `EventTaskFileTree`, `TaskFileTreePayload` in `server/pkg/protocol/`
- [x] **3.2** `ReportTaskFileTree` handler with in-memory `fileTreeCache`
- [x] **3.3** `GetTaskFileTree` serves cached snapshot for mount-time fallback
- [x] **3.4** `ProxyTaskFileContent` + `ProxyTaskFileDiff` via shared `proxyTaskFileRequest` reading `health_port` from runtime metadata
- [x] **3.5** Wire routes: `file-tree`, `files/*`, `diff/*` under `/api/issues/{id}/tasks/{taskId}/`

### Phase 4 — Frontend: core layer

- [x] **4.1** `"task:file_tree"` added to `WSEventType` + payload type
- [x] **4.2** `FileNode`, `TaskFileTreeData` types
- [x] **4.3** `api.getTaskFileTree` / `getTaskFileContent` / `getTaskFileDiff`
- [x] **4.4** `taskFileTreeOptions` / `taskFileContentOptions` / `taskFileDiffOptions`
- [x] **4.5** `useWorktreeViewStore` for per-(issue,agent) persisted selection

### Phase 5 — Frontend: components

- [x] **5.1** Add `mermaid` to the pnpm catalog + `packages/views`
- [x] **5.2** `MermaidDiagram` — lazy `import("mermaid")`, dark theme, strict security
- [x] **5.3** `WorkspaceFileTree` — git status badges, delta-only filter, auto-expand
- [x] **5.4** `WorkspaceFilePreview` — markdown-with-mermaid, shiki `CodeBlock` for code, shiki `diff` language for unified diffs, untracked-content fallback
- [x] **5.5** `useTaskFileTree` hook — WS push into Query cache, content/diff gated on `diffMode`
- [x] **5.6** `WorkspaceBrowser` — resizable tree/preview split, delta toggle

### Phase 6 — Frontend: integration + layout polish

- [x] **6.1** Mount `WorkspaceBrowser` inside the issue detail `ResizablePanelGroup`
- [x] **6.2** Persist panel layout via `useDefaultLayout`
- [x] **6.3** Restore selection per (issue, agent) via the worktree-view store
- [x] **6.4** Remove `md:hidden` top bar from `dashboard-layout.tsx`; wrap children in `SidebarInset`
- [x] **6.5** Add `<SidebarTrigger />` to 12 page headers so collapse is reachable on desktop

### Phase 7 — Bug fixes surfaced during integration

- [x] **7.1** `normalizeRepoURL` — GitHub HTTP(S) → SSH rewrite so clones use the existing SSH agent key
- [x] **7.2** `ensureRemoteURL` — self-heal cached repos whose `remote.origin.url` was saved as HTTPS
- [x] **7.3** `gitCmd` — set `GIT_TERMINAL_PROMPT=0` / `GIT_ASKPASS=/bin/true` on all network git calls to fail fast instead of hanging on stdin
- [x] **7.4** `TestNormalizeRepoURL` pins the rewrite rules

### Phase 8 — Verification

- [x] **8.1** `go test ./internal/daemon/repocache/` green
- [x] **8.2** `pnpm typecheck` green for `@multica/views` and `@multica/core`
- [x] **8.3** End-to-end smoke: daemon endpoint returns correct unified diff for a real task workdir
- [ ] **8.4** Final `make check` run before PR
