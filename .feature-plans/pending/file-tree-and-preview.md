# Feature Plan: File Tree & File Preview for Agent Worktrees

**Issue:** file-tree-and-preview
**Branch:** `feat/file-tree-and-preview`
**Status:** Pending

---

## Problem

- When an agent is working on an issue (task running in a worktree), users have no visibility into which files exist or changed in the agent's worktree
- Users can't preview file contents (plans, code, markdown) without switching to a terminal or editor
- Feature plans (`.feature-plans/`) and other markdown files need rich rendering (GFM + mermaid diagrams) for readability
- The agent-orchestrator project already has this feature — multica should have parity

## Research

### Daemon — Worktree Management

- **File:** `server/internal/daemon/execenv/git.go:71` — `setupGitWorktree()` creates per-task worktrees with `git worktree add -b <branch> <path> <baseRef>`
- **File:** `server/internal/daemon/execenv/execenv.go:54` — `Environment` struct holds `RootDir`, `WorkDir`, `CodexHome`
- **File:** `server/internal/daemon/daemon.go:905-930` — Task execution reuses prior worktrees via `PriorWorkDir` or creates new ones; `env.WorkDir` is the absolute path to the agent's working directory
- **File:** `server/internal/daemon/daemon.go:1153` — `WorkDir` is reported back on task completion
- **Risk:** MEDIUM — Daemon currently doesn't expose file-system data via API. Need new endpoints or WS messages.

### Backend — Task & WS Infrastructure

- **File:** `server/pkg/protocol/events.go` — Existing event types. Need new `task:file_tree` event.
- **File:** `server/pkg/protocol/messages.go:35-44` — `TaskMessagePayload` is how task messages flow. File tree needs a similar pattern.
- **File:** `server/internal/handler/daemon.go:349-375` — `ReportTaskProgress` shows the pattern: daemon POSTs data → server broadcasts via WS to workspace.
- **File:** `server/internal/handler/daemon.go:500-566` — `ReportTaskMessages` — daemon batches messages → server persists + broadcasts.
- **File:** `server/cmd/server/router.go` — Routes registered here.
- **Risk:** LOW — Well-established pattern: daemon POST → server broadcast → frontend invalidates.

### Frontend — WS & Issue Detail

- **File:** `packages/core/types/events.ts` — TS event type union. Add `task:file_tree` and `task:file_content`.
- **File:** `packages/core/realtime/use-realtime-sync.ts` — Global WS sync. File events would be handled per-issue (like timeline events).
- **File:** `packages/views/issues/components/issue-detail.tsx` — Issue detail page uses `ResizablePanelGroup` layout with right sidebar panel.
- **File:** `packages/views/issues/components/agent-live-card.tsx` — Existing live agent activity card; file tree/preview would be a sibling section or integrated tab.
- **Risk:** MEDIUM — Layout changes to issue detail are sensitive. Need to decide where file tree fits (tab? panel? collapsible section?).

### Reference Implementation (agent-orchestrator)

- **File:** `agent-orchestrator/.../workspace/FileTree.tsx` — File tree component with git status badges, expand/collapse, changed-only filter
- **File:** `agent-orchestrator/.../workspace/FilePreview.tsx` — Markdown rendering with `react-markdown` + `remark-gfm` + `rehype-highlight` + mermaid
- **File:** `agent-orchestrator/.../workspace/MermaidDiagram.tsx` — Client-only mermaid rendering with `mermaid.initialize({ theme: "dark" })`
- **File:** `agent-orchestrator/.../workspace/useFileTree.ts` — Polls `/api/sessions/:id/files` every 5s (we'll use WS instead)
- **File:** `agent-orchestrator/.../workspace/useFileContent.ts` — Polls with ETag/304 support every 5s (we'll use WS for change notification + REST for fetch)
- **Key difference:** AO uses Next.js API routes that read the filesystem directly. In multica, the server doesn't have filesystem access to the worktree — the **daemon** does. So the daemon must serve or relay file data.

### Existing Markdown/Mermaid/File Tree in Multica

- **Markdown rendering already exists:**
  - `packages/ui/markdown/Markdown.tsx` — Main renderer with 3 modes (terminal, minimal, full), uses `react-markdown` + `remark-gfm` + `rehype-raw`
  - `packages/ui/markdown/CodeBlock.tsx` — Syntax highlighting via **shiki** (dual themes, LRU cache)
  - `packages/ui/markdown/StreamingMarkdown.tsx` — Optimized streaming with block memoization
  - `packages/views/common/markdown.tsx` — App-level wrapper with mention support
  - `packages/views/editor/readonly-content.tsx` — Lightweight read-only renderer using **lowlight**
- **File Tree component already exists:**
  - `packages/views/skills/components/file-tree.tsx` — Hierarchical tree from flat path array, expand/collapse, lucide icons, shadcn design tokens
  - `packages/views/skills/components/file-viewer.tsx` — File preview with markdown rendering (uses `<Markdown mode="full">`), YAML frontmatter parsing, edit/preview toggle
- **No mermaid** — zero mermaid.js usage/dependency anywhere. Only dep that needs adding.
- **Existing syntax highlighting:** shiki (in ui), lowlight (in views/editor). No highlight.js or prism.
- **Deps already in catalog:** `react-markdown` ^10.1.0, `remark-gfm` ^4.0.1, `rehype-raw` ^7.0.0, `shiki` ^3.21.0, `lowlight` ^3.3.0

## Approach

### Architecture Decision: Daemon as File Server

The daemon runs on the same machine as the worktrees, so it has direct filesystem access. Two approaches:

**Option A: Daemon → Server → Frontend (relay via WS)**
- Daemon periodically scans worktree, POSTs file tree + changed file list to server
- Server broadcasts via existing WS to frontend
- File content fetched via REST: frontend → server → daemon (proxy)
- Pro: Consistent with existing task message pattern
- Con: Server becomes a proxy for large file content; higher latency

**Option B: Daemon exposes local HTTP API (direct)**
- Daemon's health server already listens on a local port (`:19514`)
- Add `/files` and `/files/:path` endpoints to daemon's health server
- Frontend calls daemon directly for file content
- Pro: Low latency, no server proxy overhead
- Con: Only works on local network; doesn't work for cloud runtimes

**Chosen: Option A (relay via server)** — Consistent with existing architecture, works for both local and future cloud runtimes. File content payloads are small (text files < 1MB).

### Data Flow

```
Daemon (fsnotify/poll on worktree)
  → POST /api/daemon/tasks/:taskId/files (file tree + git status)
  → Server broadcasts WS event "task:file_tree" to workspace
  → Frontend invalidates file tree query

Frontend clicks file
  → GET /api/tasks/:taskId/files/:path (REST, through server)
  → Server proxies to daemon via pending-request pattern (like ping)
  → OR: daemon pre-reports file content alongside file tree for small files
```

**Simpler initial approach:** Daemon periodically reports the file tree (+ git status) as a WS-broadcast message. File content is fetched on-demand via a new REST endpoint where the server asks the daemon for the file content via the heartbeat/pending-request pattern.

**Even simpler MVP:** Since the daemon already has a health HTTP server on localhost, add file-serving endpoints there. The **server** can proxy requests to the daemon's health port (daemon reports its health port during registration). This avoids the complex pending-request dance.

### Revised Flow (MVP)

```
1. Daemon registration already reports daemon_id and health port
2. Daemon watches worktree filesystem (fsnotify or periodic scan)
3. On change: POST /api/daemon/tasks/:taskId/file-tree → server stores + broadcasts "task:file_tree"
4. File content: daemon serves GET /files/:taskId/:path on health port
5. Server proxies: GET /api/tasks/:taskId/files/*path → daemon health port
6. Frontend: WS event updates tree, REST fetches file content
```

### Component Reuse Strategy

Multica already has building blocks that can be adapted:

- **File tree:** `packages/views/skills/components/file-tree.tsx` — works with flat path arrays. Need to extend for: git status badges, changed-only filter, auto-expand to selected file. Can either extend in-place or create a new variant.
- **File preview/markdown:** `packages/views/skills/components/file-viewer.tsx` uses `<Markdown mode="full">` from `packages/ui/markdown/Markdown.tsx`. For read-only preview (no edit mode), we can use `<Markdown>` directly.
- **Code highlighting:** Already have shiki in `packages/ui/markdown/CodeBlock.tsx` — reuse for non-markdown files.
- **Mermaid only:** The only new dependency needed is `mermaid` for diagram rendering in markdown code blocks.

## Changes by Layer

---

### Section 1: Daemon Changes

#### 1A. File Tree Scanner

Add a file watcher/scanner to the daemon that monitors the active task's worktree directory.

| File | Change |
|------|--------|
| `server/internal/daemon/filetree/scanner.go` | **New** — Recursive directory scanner with git status, ignore patterns (.git, node_modules, etc.) |
| `server/internal/daemon/filetree/types.go` | **New** — `FileNode`, `GitStatus`, `FileTreeSnapshot` types |
| `server/internal/daemon/filetree/watcher.go` | **New** — fsnotify-based watcher that triggers scan on changes, with debounce |

#### 1B. File Tree Reporting

The daemon reports file tree changes to the server, which broadcasts them via WS.

| File | Change |
|------|--------|
| `server/internal/daemon/client.go` | Add `ReportFileTree(taskID, tree, gitStatus)` method |
| `server/internal/daemon/daemon.go` | Start file watcher when task begins, stop on completion. Report tree on change. |

#### 1C. File Content Serving (Daemon Health Server)

Add file content endpoints to the daemon's existing health HTTP server.

| File | Change |
|------|--------|
| `server/internal/daemon/health.go` | Add `GET /tasks/:taskId/files/*path` endpoint — serves file content with ETag, binary detection, size limits |

---

### Section 2: Server (Go Backend) Changes

#### 2A. New WS Event & REST Endpoints

| File | Change |
|------|--------|
| `server/pkg/protocol/events.go` | Add `EventTaskFileTree = "task:file_tree"` |
| `server/pkg/protocol/messages.go` | Add `TaskFileTreePayload` struct |
| `server/internal/handler/daemon.go` | Add `ReportTaskFileTree` handler — receives tree from daemon, broadcasts via WS |
| `server/internal/handler/daemon.go` | Add `GetTaskFileContent` handler — proxies file content request to daemon's health port |
| `server/cmd/server/router.go` | Wire new routes: `POST /daemon/tasks/:taskId/file-tree`, `GET /tasks/:taskId/files/*` |

#### 2B. Daemon Health Port Tracking

The server needs to know the daemon's health port to proxy file content requests.

| File | Change |
|------|--------|
| `server/internal/handler/daemon.go` | `DaemonRegister` — accept and store `health_port` in daemon metadata |
| `server/pkg/db/queries/*.sql` | Add `health_port` to agent_runtimes metadata or a new column |

---

### Section 3: Frontend (UI) Changes

#### 3A. New Dependencies

| File | Change |
|------|--------|
| `pnpm-workspace.yaml` | Add to catalog: `mermaid` (only new dep — markdown/shiki/lowlight already exist) |
| `packages/views/package.json` | Add `mermaid` from catalog |

#### 3B. Core — Types, Queries, WS Events

| File | Change |
|------|--------|
| `packages/core/types/events.ts` | Add `"task:file_tree"` to `WSEventType`, add `TaskFileTreePayload` |
| `packages/core/types/agent.ts` | Add `FileNode`, `FileTreeSnapshot` types (or new `file-tree.ts`) |
| `packages/core/api/client.ts` | Add `getTaskFileTree(taskId)` and `getTaskFileContent(taskId, path)` API methods |
| `packages/core/issues/queries.ts` | Add `taskFileTreeOptions(taskId)` query factory |

#### 3C. Views — Workspace File Tree Component

Extend existing `packages/views/skills/components/file-tree.tsx` pattern for worktree use case. The skills file tree builds from flat path arrays — the worktree version needs nested `FileNode` data (from daemon scanner), git status badges, and changed-only filtering.

| File | Change |
|------|--------|
| `packages/views/issues/components/workspace-file-tree.tsx` | **New** — Worktree-aware file tree with git status badges (M/A/D/U), changed-only filter toggle, auto-expand to selected file. Follows same design tokens/patterns as skills file tree. |
| `packages/views/issues/hooks/use-task-file-tree.ts` | **New** — Hook: initial REST fetch + WS `task:file_tree` invalidation |

#### 3D. Views — Workspace File Preview Component

Reuse existing `<Markdown mode="full">` from `packages/ui/markdown/Markdown.tsx` for markdown files. Add mermaid support via custom code block component. Use existing `CodeBlock` (shiki) for non-markdown files.

| File | Change |
|------|--------|
| `packages/views/issues/components/workspace-file-preview.tsx` | **New** — File content preview: delegates to `<Markdown>` for .md files (with mermaid code block override), wraps `CodeBlock` for other text files. Loading/error/unsupported states. |
| `packages/views/issues/components/mermaid-diagram.tsx` | **New** — Client-only lazy-loaded mermaid renderer (dark theme, strict security) |
| `packages/views/issues/hooks/use-task-file-content.ts` | **New** — Hook: fetch file content via REST, refetch on WS file_tree change |

#### 3E. Views — Integration into Issue Detail

| File | Change |
|------|--------|
| `packages/views/issues/components/issue-detail.tsx` | Add file tree + preview as a collapsible panel/tab when an active task exists |
| `packages/views/issues/components/agent-live-card.tsx` | Add "Files" tab or expandable section showing file tree alongside agent transcript |

---

## Risks / Open Questions

| # | Question | Notes |
|---|----------|-------|
| 1 | **Where in the issue detail UI should the file tree appear?** | Options: (a) New tab alongside activity/comments, (b) Expandable section inside agent-live-card, (c) Separate panel in the right sidebar. Recommend (b) — collocates with agent activity. |
| 2 | **Should the daemon proxy be via health port or pending-request?** | Health port is simpler and already exists. Pending-request (via heartbeat polling) is more resilient but adds latency. Start with health port, can add pending-request fallback later. |
| 3 | **How to handle large repos?** | Limit tree depth/file count. Skip node_modules/.git/dist. Only scan up to N levels deep initially. |
| 4 | **fsnotify vs polling for file changes?** | fsnotify is more responsive but platform-dependent and has watcher limits. Start with polling (every 3-5s) which matches agent-orchestrator's approach, consider fsnotify as optimization later. |
| 5 | **Security: path traversal on file content endpoint** | Must validate that requested path doesn't escape worktree root. Both daemon and server should check. |
| 6 | **File content for completed tasks?** | When task completes and daemon removes worktree, files are gone. Could snapshot file tree in DB on completion, but content would be unavailable. Accept this limitation initially — files only available while task is running or worktree exists. |
| 7 | **mermaid bundle size** | mermaid.js is ~2MB. Must be dynamically imported / lazy-loaded. Use `React.lazy` or dynamic import. |

## Validation

- **Unit tests:** File tree scanner (Go), file content handler (Go), type definitions (TS)
- **Integration tests:** Daemon reports file tree → server broadcasts → frontend receives
- **Component tests:** FileTree renders nodes, FilePreview renders markdown + mermaid + code
- **E2E test:** Create issue → assign agent → wait for task to start → verify file tree appears → click file → verify preview renders
- **Manual:** Verify real-time updates when agent writes files, verify mermaid diagrams render

## Checklist

### Phase 1 — Daemon: File Tree Scanner & Reporter

- [ ] **1.1** Create `server/internal/daemon/filetree/types.go` with `FileNode`, `GitStatus` types
- [ ] **1.2** Create `server/internal/daemon/filetree/scanner.go` — directory walker with git status, ignore patterns
- [ ] **1.3** Create `server/internal/daemon/filetree/watcher.go` — periodic scanner (3s interval), debounced change detection
- [ ] **1.4** Add `ReportFileTree` to daemon client (`server/internal/daemon/client.go`)
- [ ] **1.5** Integrate watcher into `daemon.go` task lifecycle — start on task begin, stop on complete
- [ ] **1.6** Unit tests for scanner (build tree from test directory, verify structure + git status)

### Phase 2 — Daemon: File Content Serving

- [ ] **2.1** Add `GET /tasks/:taskId/files/*path` to daemon health server
- [ ] **2.2** Implement path traversal protection, binary detection, size limits, ETag support
- [ ] **2.3** Unit tests for content handler (valid path, path traversal, binary, too large, ETag)

### Phase 3 — Server: WS Event & Proxy Endpoints

- [ ] **3.1** Add `EventTaskFileTree` to `protocol/events.go` and `TaskFileTreePayload` to `messages.go`
- [ ] **3.2** Add `ReportTaskFileTree` handler in `daemon.go` — receive + broadcast
- [ ] **3.3** Store daemon health port/address during registration
- [ ] **3.4** Add `GetTaskFileContent` handler — proxy to daemon
- [ ] **3.5** Wire routes in `router.go`
- [ ] **3.6** Integration tests for file tree broadcast and content proxy

### Phase 4 — Frontend: Core Layer (Types, API, Queries)

- [ ] **4.1** Add `task:file_tree` to `WSEventType` in `packages/core/types/events.ts`
- [ ] **4.2** Add `TaskFileTreePayload`, `FileNode` types
- [ ] **4.3** Add `getTaskFileTree()` and `getTaskFileContent()` to API client
- [ ] **4.4** Add `taskFileTreeOptions()` query factory to `packages/core/issues/queries.ts`
- [ ] **4.5** Add `useTaskFileTree` hook with WS-driven invalidation

### Phase 5 — Frontend: Components (reuse existing + mermaid)

- [ ] **5.1** Add `mermaid` to pnpm catalog + packages/views (only new dep needed)
- [ ] **5.2** Create `MermaidDiagram` component (lazy-loaded, dark theme, strict security)
- [ ] **5.3** Create `WorkspaceFileTree` component — extends skills file-tree pattern with git status badges, changed-only filter
- [ ] **5.4** Create `WorkspaceFilePreview` component — uses existing `<Markdown mode="full">` + `CodeBlock` (shiki), adds mermaid code block override
- [ ] **5.5** Component tests for WorkspaceFileTree + WorkspaceFilePreview

### Phase 6 — Frontend: Issue Detail Integration

- [ ] **6.1** Wire FileTree + FilePreview into issue detail (as section near agent-live-card)
- [ ] **6.2** Add file selection state (URL param or local state)
- [ ] **6.3** Only show when active task exists with a worktree
- [ ] **6.4** Verify responsive behavior (collapse file tree on small screens)
- [ ] **6.5** E2E test: agent working → file tree visible → click file → preview renders

### Phase 7 — Polish & Edge Cases

- [ ] **7.1** Handle task completion (gray out file tree, show "worktree removed" state)
- [ ] **7.2** Handle daemon disconnect (show "daemon offline" state)
- [ ] **7.3** Run `make check` — typecheck, lint, unit tests, Go tests
- [ ] **7.4** Manual QA with real agent task
