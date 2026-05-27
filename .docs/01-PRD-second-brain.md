# PRD: Second Brain — Workspace Knowledge Base

**Status**: Design. No code written.
**Owner**: TBD (you, the implementing agent)
**Companion docs**: read `00-CONTEXT-PRIMER.md` first if you haven't.
**Estimated scope**: 6 PRs, ~7 sprints / ~1.75 months for one developer full-time.

---

## 1. One-paragraph summary

Add a workspace-scoped, hierarchical, versioned, markdown knowledge base to Multica. Humans and agents create/edit `.md` files identified by paths like `clients/acme/research/competitors.md`. A compact tree-style index of all docs is auto-injected into every agent's CLAUDE.md/AGENTS.md, so agents always know what knowledge exists and can fetch full content on demand via `multica doc get <path>`. Pinned docs are injected with full content. All mutations create append-only revisions; restore is one command. This solves the missing piece: today there is no way to share context across issues in a multica workspace.

---

## 2. Origin & motivation

A user described their existing workflow with raw Claude Code:

> "When I work on a project for a client, I create a folder and open it with Claude Code. I explain what I'll work on, ask for help setting up some skills and agents specific to that project. Then I start putting in context: who I am, what I do, what the client company does, what the project is, etc. I keep researching topics related to the project, and everything ends up as Markdown files in a logical, easy-to-read structure. With this, when I ask a question, Claude Code (through CLAUDE.md instructions and other project context files) starts reading what's already there so each new task has relevant context, but without needing to read everything in the repository."

Current state of Multica vs this workflow:

- ✅ Workspace concept exists (1 workspace = 1 project)
- ❌ No way to write a markdown KB shared across issues
- ❌ No way for the agent to "see what knowledge exists" without reading every issue
- ❌ Two dormant fields exist (`workspace.context` TEXT, `issue.context_refs` JSONB) that nobody reads — proves someone thought about it once
- ❌ pgvector loaded in DB but never used

**Comparison to the 4 reference repos studied:**

| Repo | Has workspace-level shared docs? | Notes |
|---|---|---|
| openclaw | No | Skills are user-scoped files in `~/.openclaw/workspaces/<id>/skills/` — closest analog, but for procedural knowledge, not free-form notes |
| hermes-agent | No (single user, has MEMORY.md/USER.md only) | Skills serve as procedural memory but not free-form |
| multica | **No** | Has dormant slots, never wired up |
| paperclip | **No** (despite having `documents` table — `documentUq` constraint forces 1:1 with issue) | Documents are per-issue; cannot reference one doc from many issues |

**This feature fills a real gap** that none of the studied platforms solve.

---

## 3. Goals & non-goals

### Goals
1. Workspace-scoped, hierarchical (path-based) markdown documents.
2. Append-only revision history with restore.
3. Compact index injected into every agent prompt; full content fetched on demand by the agent.
4. Pinned documents always injected with full content.
5. CLI parity with UI — agents can `list/read/write/patch/rename/tag/pin/archive/history/revert/link`.
6. Frontend editor (port from paperclip's `IssueDocumentsSection.tsx`) with autosave + diff viewer + restore.
7. Re-use the dormant `workspace.context` (workspace-level overlay) and `issue.context_refs` (issue-level pointer list) instead of inventing new fields.
8. Issue ↔ document linking (N:N), with three link types: `referenced` / `consumed` / `produced`.

### Non-goals (this PRD)
- Vector / semantic search (pgvector is there; defer to a later phase or PRD).
- Multi-workspace cross-references.
- Rich-text or non-markdown formats.
- A file-attachment uploader (use the existing `attachment` table for binary files; this PRD is markdown-text only).
- Real-time collaborative editing (autosave is enough).
- Permission model beyond workspace-membership (no per-document ACL).

---

## 4. Current state of Multica (load-bearing facts)

Read these files in `repos/multica/`:

| File | What's there today |
|---|---|
| `server/migrations/006_workspace_context.up.sql` | Adds `workspace.context TEXT NULL` — **never read by any handler**. Free for us to use. |
| `server/migrations/001_init.up.sql` (line 67) | `issue.context_refs JSONB NOT NULL DEFAULT '[]'` — **never populated, never consumed**. Free for us to use. |
| `server/internal/daemon/execenv/runtime_config.go` (`InjectRuntimeConfig`, `buildMetaSkillContent`) | Where CLAUDE.md/AGENTS.md is written for each task. **This is the one critical file you'll modify** to inject the KB. |
| `server/internal/daemon/execenv/types.go` (`TaskContextForEnv`) | The struct passed from server to daemon. Add fields here for KB content. |
| `server/internal/handler/daemon.go` (`ClaimTaskByRuntime`, lines ~870–960) | Builds `TaskContextForEnv` from DB. You'll add queries here. |
| `server/cmd/server/router.go` | Where new routes register. |
| `server/internal/middleware/daemon_auth.go` | Auth middleware. Daemon tokens (`mdt_*`) need read+write on documents. |
| `server/cmd/multica/cmd_workspace.go` | Existing workspace CLI; you'll add `set-context` here. |
| `apps/web/` and `packages/views/` | Frontend; you'll add document UI. |
| `e2e/` | Playwright tests. |

---

## 5. Proposed design

### 5.1 Schema

#### Migration: `XXX_workspace_documents.up.sql`

```sql
-- Main document table. Path is the human key.
CREATE TABLE workspace_document (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    path            TEXT NOT NULL,                     -- 'clients/acme/research/competitors.md'
    title           TEXT,                              -- nullable; defaults to basename(path)
    description     TEXT,                              -- 1-line preview shown in the index
    content         TEXT NOT NULL DEFAULT '',
    format          TEXT NOT NULL DEFAULT 'markdown',
    tags            TEXT[] NOT NULL DEFAULT '{}',
    pinned          BOOLEAN NOT NULL DEFAULT false,    -- always injected with full content
    archived_at     TIMESTAMPTZ,                       -- soft-delete
    current_revision_id UUID,                          -- FK below; nullable for bootstrap
    created_by      UUID REFERENCES "user"(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, path)
);
CREATE INDEX idx_workspace_doc_workspace_active
    ON workspace_document(workspace_id) WHERE archived_at IS NULL;
CREATE INDEX idx_workspace_doc_pinned
    ON workspace_document(workspace_id) WHERE pinned = true AND archived_at IS NULL;
CREATE INDEX idx_workspace_doc_tags ON workspace_document USING gin(tags);
CREATE INDEX idx_workspace_doc_path_trgm
    ON workspace_document USING gin(path gin_trgm_ops);
CREATE INDEX idx_workspace_doc_content_fts
    ON workspace_document USING gin(to_tsvector('simple', content));
-- Use 'simple' (language-agnostic) instead of 'portuguese'/'english' so we don't
-- discriminate by workspace language. Stem-aware search comes in a later phase.

-- Append-only revision history.
CREATE TABLE workspace_document_revision (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id     UUID NOT NULL REFERENCES workspace_document(id) ON DELETE CASCADE,
    revision_number INT  NOT NULL,
    parent_revision UUID REFERENCES workspace_document_revision(id),
    title           TEXT,
    description     TEXT,
    content         TEXT NOT NULL DEFAULT '',
    tags            TEXT[] NOT NULL DEFAULT '{}',
    -- Provenance (mirrors Skills 2.0 — keep formats identical)
    author_type     TEXT NOT NULL CHECK (author_type IN
        ('human','agent_foreground','agent_background','import')),
    author_id       UUID,
    task_id         UUID REFERENCES agent_task_queue(id),
    operation       TEXT NOT NULL CHECK (operation IN
        ('create','edit','rename','restore','tag','pin','archive')),
    change_summary  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, revision_number)
);
CREATE INDEX idx_workspace_doc_rev_doc
    ON workspace_document_revision(document_id, revision_number DESC);

-- Now that the table exists, add the FK on workspace_document
ALTER TABLE workspace_document
    ADD CONSTRAINT fk_workspace_document_current_revision
    FOREIGN KEY (current_revision_id) REFERENCES workspace_document_revision(id);

-- Issue ↔ document N:N linking (intentionally N:N; paperclip's 1:1 is a deliberate departure)
CREATE TABLE issue_document_link (
    issue_id     UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    document_id  UUID NOT NULL REFERENCES workspace_document(id) ON DELETE CASCADE,
    link_type    TEXT NOT NULL CHECK (link_type IN ('referenced','produced','consumed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, document_id)
);
CREATE INDEX idx_issue_doc_link_issue ON issue_document_link(issue_id);
CREATE INDEX idx_issue_doc_link_document ON issue_document_link(document_id);

-- Workspace-level setting: do agents have write permission?
ALTER TABLE workspace
    ADD COLUMN documents_agent_write_mode TEXT NOT NULL DEFAULT 'allow'
    CHECK (documents_agent_write_mode IN ('allow','read_only_for_agents','disabled'));
```

Down migration mirrors all of the above with DROPs in reverse order.

#### Reusing dormant slots
- `workspace.context` (TEXT) — already exists. Becomes "the README of the KB", injected verbatim at the top of every CLAUDE.md.
- `issue.context_refs` (JSONB) — already exists. Stores an array of strings (paths or document UUIDs) to be auto-injected as full content for THIS issue.

### 5.2 SQL queries (sqlc)

Create `server/pkg/db/queries/workspace_document.sql` and `server/pkg/db/queries/issue_document_link.sql`. Required queries:

**workspace_document.sql**
```sql
-- name: CreateWorkspaceDocument :one
INSERT INTO workspace_document (workspace_id, path, title, description, content, tags, pinned, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetWorkspaceDocumentByPath :one
SELECT * FROM workspace_document
WHERE workspace_id = $1 AND path = $2 AND archived_at IS NULL;

-- name: GetWorkspaceDocumentByID :one
SELECT * FROM workspace_document WHERE id = $1;

-- name: ListWorkspaceDocuments :many
SELECT * FROM workspace_document
WHERE workspace_id = $1
  AND archived_at IS NULL
  AND ($2::text IS NULL OR path LIKE $2 || '%')
  AND ($3::text[] IS NULL OR tags && $3::text[])
ORDER BY path;

-- name: ListPinnedWorkspaceDocuments :many
SELECT * FROM workspace_document
WHERE workspace_id = $1 AND pinned = true AND archived_at IS NULL
ORDER BY path;

-- name: ListWorkspaceDocumentIndex :many
-- Returns just (path, description) — the compact index.
SELECT id, path, description, pinned FROM workspace_document
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY path;

-- name: UpdateWorkspaceDocumentContent :exec
UPDATE workspace_document
SET content = $2, title = $3, description = $4, tags = $5,
    current_revision_id = $6, updated_at = now()
WHERE id = $1;

-- name: SearchWorkspaceDocumentsByContent :many
SELECT id, workspace_id, path, title, description,
       ts_rank(to_tsvector('simple', content), plainto_tsquery('simple', $2)) AS rank
FROM workspace_document
WHERE workspace_id = $1
  AND archived_at IS NULL
  AND to_tsvector('simple', content) @@ plainto_tsquery('simple', $2)
ORDER BY rank DESC
LIMIT $3;

-- name: ArchiveWorkspaceDocument :exec
UPDATE workspace_document SET archived_at = now() WHERE id = $1;

-- name: RenameWorkspaceDocument :exec
UPDATE workspace_document SET path = $2, updated_at = now() WHERE id = $1;

-- name: SetWorkspaceDocumentPinned :exec
UPDATE workspace_document SET pinned = $2, updated_at = now() WHERE id = $1;

-- name: InsertWorkspaceDocumentRevision :one
INSERT INTO workspace_document_revision
    (document_id, revision_number, parent_revision, title, description, content, tags,
     author_type, author_id, task_id, operation, change_summary)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: ListWorkspaceDocumentRevisions :many
SELECT id, revision_number, author_type, author_id, task_id, operation,
       change_summary, created_at
FROM workspace_document_revision
WHERE document_id = $1
ORDER BY revision_number DESC;

-- name: GetWorkspaceDocumentRevision :one
SELECT * FROM workspace_document_revision
WHERE document_id = $1 AND revision_number = $2;

-- name: GetMaxRevisionNumber :one
SELECT COALESCE(MAX(revision_number), 0)::int FROM workspace_document_revision
WHERE document_id = $1;
```

**issue_document_link.sql**
```sql
-- name: LinkIssueDocument :exec
INSERT INTO issue_document_link (issue_id, document_id, link_type)
VALUES ($1, $2, $3)
ON CONFLICT (issue_id, document_id) DO UPDATE SET link_type = $3;

-- name: UnlinkIssueDocument :exec
DELETE FROM issue_document_link WHERE issue_id = $1 AND document_id = $2;

-- name: ListLinkedDocumentsForIssue :many
SELECT d.* FROM workspace_document d
JOIN issue_document_link l ON l.document_id = d.id
WHERE l.issue_id = $1 AND d.archived_at IS NULL;
```

After writing the SQL, run `make sqlc` and commit `server/pkg/db/generated/`.

### 5.3 Service layer

Create `server/internal/service/document.go`. Key methods (signatures illustrative):

```go
type DocumentService struct {
    Queries *db.Queries
    DB      *pgxpool.Pool
    Events  *eventbus.Bus
}

// Put upserts a document. If baseRevisionID is non-nil and doesn't match the
// current revision, returns ErrConflict (caller can retry with the new base).
// Provenance is built from ctx (see middleware/provenance.go).
func (s *DocumentService) Put(
    ctx context.Context,
    workspaceID uuid.UUID,
    path string,
    payload DocumentPayload, // {title, description, content, tags}
    baseRevisionID *uuid.UUID,
    changeSummary string,
) (*Document, error)

// Patch applies fuzzy find/replace to the current content and persists as a
// new revision. Reuses the same _apply_patch logic from Hermes (port to Go).
func (s *DocumentService) Patch(
    ctx context.Context,
    documentID uuid.UUID,
    findText, replaceText string,
    changeSummary string,
) (*Document, error)

// Restore creates a new revision whose content equals revision N's content.
// Doesn't destroy intermediate revisions.
func (s *DocumentService) Restore(
    ctx context.Context,
    documentID uuid.UUID,
    revisionNumber int,
) (*Document, error)

// BuildIndexTree returns the compact (path, description, pinned) list for
// injection into the agent prompt. Optionally scoped by path prefix.
func (s *DocumentService) BuildIndexTree(
    ctx context.Context,
    workspaceID uuid.UUID,
    pathPrefix *string,
) ([]IndexEntry, error)
```

**Critical**: every mutation method must transactionally:
1. INSERT into `workspace_document_revision` (next `revision_number`)
2. UPDATE `workspace_document.current_revision_id`, `content`, `title`, etc.

Wrap in `db.Tx` (use `pgxpool.BeginTx`).

### 5.4 Provenance helper (shared with Skills 2.0 — implement once)

Create `server/internal/middleware/provenance.go`:

```go
type Provenance struct {
    AuthorType string // 'human', 'agent_foreground', 'agent_background', 'import'
    AuthorID   *uuid.UUID
    TaskID     *uuid.UUID
}

// ProvenanceFromContext inspects the request context (set by Auth/DaemonAuth
// middleware) and returns the provenance to record on revisions.
//
// - mdt_* daemon token: agent_foreground (or agent_background if X-Curator-Run header is set)
// - mul_*/JWT user token: human
//
// task_id comes from X-Multica-Task-ID header if present (set by daemon
// when subprocess CLI calls run as part of a known task).
func ProvenanceFromContext(ctx context.Context) Provenance {
    // ... read context values set upstream in Auth/DaemonAuth ...
}
```

Used by both this PRD and Skills 2.0.

### 5.5 Fuzzy patch helper (shared with Skills 2.0 — implement once)

Create `server/internal/textpatch/fuzzy.go`. Port from `repos/hermes-agent/tools/skill_manager_tool.py:465-560` (`_apply_patch`):

- Whitespace-tolerant find (collapse runs of whitespace before comparison).
- Returns `(newContent, found bool, error)`.
- If `found == false`, the caller returns 422 to the agent so it can retry.

### 5.6 HTTP handlers

Create `server/internal/handler/document.go`. Endpoints (all under workspace context; auth allows daemon tokens unless noted):

| Method | Path | Description |
|---|---|---|
| GET | `/api/workspaces/{ws}/documents` | List with `?path-prefix=…&tag=…&pinned=true` filters |
| GET | `/api/workspaces/{ws}/documents/index` | Compact index `(id, path, description, pinned)` |
| GET | `/api/workspaces/{ws}/documents/tree` | ASCII or JSON tree view |
| GET | `/api/workspaces/{ws}/documents/search?q=…` | FTS via `SearchWorkspaceDocumentsByContent` |
| GET | `/api/workspaces/{ws}/documents/by-path/*path` | Get by path (use chi wildcard) |
| GET | `/api/workspaces/{ws}/documents/{id}` | Get by id |
| PUT | `/api/workspaces/{ws}/documents/by-path/*path` | Upsert (create or update). Body: `{title, description, content, tags, baseRevisionId?, changeSummary}` |
| POST | `/api/workspaces/{ws}/documents/{id}/patch` | Body: `{find, replace, changeSummary}` |
| POST | `/api/workspaces/{ws}/documents/{id}/rename` | Body: `{newPath}` |
| POST | `/api/workspaces/{ws}/documents/{id}/tags` | Body: `{add: [...], remove: [...]}` |
| POST | `/api/workspaces/{ws}/documents/{id}/pin` / `/unpin` | No body |
| POST | `/api/workspaces/{ws}/documents/{id}/archive` | Body: `{reason}` |
| GET | `/api/workspaces/{ws}/documents/{id}/revisions` | History list |
| GET | `/api/workspaces/{ws}/documents/{id}/revisions/{n}` | Get revision N |
| POST | `/api/workspaces/{ws}/documents/{id}/restore` | Body: `{revisionNumber}` |
| POST | `/api/issues/{issueId}/documents/links` | Body: `{documentId or path, linkType}` |
| DELETE | `/api/issues/{issueId}/documents/links/{documentId}` | Unlink |
| GET | `/api/issues/{issueId}/documents/links` | List linked docs |

**Auth nuance**: when `workspace.documents_agent_write_mode = 'read_only_for_agents'`, all mutating endpoints reject `mdt_*` tokens with 403.

Register all routes in `server/cmd/server/router.go`.

### 5.7 Agent prompt injection — the critical change

Edit `server/internal/daemon/execenv/runtime_config.go`. Modify `buildMetaSkillContent()` to prepend FOUR new sections (in this order, before existing content):

```markdown
## Workspace overview
{workspace.context, verbatim, with leading newline if non-empty}

## Pinned documents
{For each pinned doc, in path order, render:}
### {path}
{full content}

---

## Knowledge base index
You have access to a workspace knowledge base. The tree below is everything
that exists. Read individual files with `multica doc get <path>`.

{rendered tree, e.g.:}
clients/
├── acme/
│   ├── brief.md          — Briefing inicial; objetivos do projeto Q2
│   ├── contract.md       — SLA e escopo
│   └── research/
│       └── competitors.md   — Análise de 5 concorrentes
internal/
└── playbooks/
    └── lgpd.md           — Checklist de compliance

## Linked to this issue
{For each doc in issue.context_refs + issue_document_link, render full content:}
### {path}
{full content}

---

## Knowledge base usage
1. Look at the index above; identify docs likely relevant to this task.
2. Read with `multica doc get <path>`.
3. Cite docs you used in your final comment with `[path](mention://doc/<path>)`.
4. Write back insights worth keeping with `multica doc put <path>`.
   Use a clear path like `clients/<name>/research/<topic>.md`.
5. Update existing docs surgically with `multica doc patch <path> --find ... --replace ...`.
6. Link the issue to docs you used or produced:
   `multica doc link <issue-id> <path> --type referenced|consumed|produced`.
Pinned docs above are already loaded — do not re-read them.
```

**Token budget caps** (prevent prompt bloat):
- Pinned docs: cap 5 docs OR 4000 tokens total, whichever first; truncate with note.
- Index: cap 200 entries; if exceeded, render only path-prefixes that match the issue's project (heuristic) or first 200 alphabetically.
- Linked-to-this-issue: cap 10 docs OR 6000 tokens.

Implement caps in helper `truncateForBudget(items, maxItems, maxTokens)` (estimate tokens as `len(text)/4`).

### 5.8 TaskContextForEnv updates

Edit `server/internal/daemon/execenv/types.go` to add:

```go
type TaskContextForEnv struct {
    // ...existing fields...
    WorkspaceContext      string                  // workspace.context content
    PinnedDocuments       []DocumentForEnv         // pinned, with content
    DocumentIndex         []DocumentIndexEntry     // (path, description) only
    IssueLinkedDocuments  []DocumentForEnv         // from issue.context_refs + links
}

type DocumentForEnv struct {
    Path        string
    Title       string
    Description string
    Content     string
}

type DocumentIndexEntry struct {
    Path        string
    Description string
    Pinned      bool
}
```

Edit `server/internal/handler/daemon.go:ClaimTaskByRuntime` to populate these by querying the new tables.

### 5.9 CLI commands

Create `server/cmd/multica/cmd_doc.go`. Subcommands (Cobra):

```
multica doc list [--path-prefix <prefix>] [--tag <tag>] [--pinned] [--output json]
multica doc tree [--path-prefix <prefix>]
multica doc index [--path-prefix <prefix>]                # (path + description) only
multica doc search "<query>" [--limit N]
multica doc grep "<regex>"                                # client-side after pulling list+content
multica doc get <path>                                    # prints content
multica doc show <path> --rev N                           # prints revision N's content
multica doc put <path> --content-stdin
                       [--title T] [--description D] [--tags t1,t2]
                       [--base-revision <id>] [--summary "..."]
multica doc patch <path> --find "..." --replace "..." [--summary "..."]
multica doc rename <old-path> <new-path>
multica doc tag <path> --add t1,t2 --remove t3
multica doc pin <path>
multica doc unpin <path>
multica doc archive <path> [--reason "..."]
multica doc history <path>                                # revision list
multica doc diff <path> --from N --to M                   # client-side diff of two revisions
multica doc revert <path> --to-rev N
multica doc link <issue-id> <path> [--type referenced|consumed|produced]
multica doc unlink <issue-id> <path>
```

Also add to existing `cmd_workspace.go`:
```
multica workspace get-context
multica workspace set-context --content-stdin
```

All CLI commands should use the same HTTP client pattern as existing commands; auth header from `~/.multica/config.json` is automatic via `cli.Client`.

### 5.10 Frontend

Port `repos/paperclip/ui/src/components/IssueDocumentsSection.tsx` to `apps/web/src/views/documents/` (or `packages/views/documents/` if the workspace uses shared views). Adapt for workspace scope:

- Tree-view sidebar (paths render as folders)
- Editor pane: markdown textarea with autosave (debounce 900ms — same as paperclip)
- Tag input
- Pin toggle
- "History" tab showing revisions with author + summary; click to view + restore button
- Diff view (use `react-diff-viewer` or similar)
- "Linked issues" section showing where this doc is referenced
- Filter by tag in the sidebar
- "New document" dialog (path-aware: prefilled with current selected folder + ".md")

Also add a `workspace.context` editor in workspace settings page (`apps/web/src/views/settings/workspace/`).

---

## 6. Phased rollout (PRs)

| PR | Title | Files touched | Approx LOC |
|---|---|---|---|
| **A1** | `feat(doc): schema + queries for workspace_document` | new migration, 2 new SQL files, sqlc regen, 1 service file (Get/Put only), no handlers yet | ~600 |
| **A2** | `feat(doc): handlers + CLI for basic doc CRUD` | handler/document.go, cmd_doc.go (list/get/put/history/revert), router.go, middleware updates | ~800 |
| **A3** | `feat(daemon): inject workspace.context + KB index into agent prompt` | runtime_config.go, types.go, handler/daemon.go (ClaimTaskByRuntime), workspace settings setter | ~400 |
| **A4** *(parallel with A2)* | `feat(web): document editor UI with revisions` | new `views/documents/`, settings page for workspace.context | ~1200 |
| **A5** | `feat(doc): tags + pin + archive + issue linking` | extend handler+CLI+UI; new issue_document_link table queries | ~700 |
| **A6** | `feat(doc): full-text search + grep CLI + scoped index` | FTS query + handler + CLI + UI search bar | ~500 |

**Estimate**: ~7 sprints if 1 dev full-time. ~3 sprints with 2 devs in parallel (A2/A4 split).

Each PR independently deployable. After A3 lands, the agent immediately starts seeing the KB index in its prompt — that's the moment the feature "comes alive".

---

## 7. Test plan

### Unit (Go)
- `service/document_test.go`:
  - Put creates revision 1 on first call
  - Put with stale `baseRevisionID` returns ErrConflict
  - Patch returns 422 if find text not found
  - Restore creates a new revision with old content; doesn't delete intermediates
  - Rename updates path but preserves history (joinable on document_id)
  - Archive sets `archived_at`; subsequent List excludes the doc
- `textpatch/fuzzy_test.go`:
  - Exact match
  - Whitespace-tolerant match (`"foo  bar"` matches `"foo bar"`)
  - No match returns `found=false`
  - Multiple matches: should fail or replace first only? **Decision: fail with error to force agent to be specific.**
- `middleware/provenance_test.go`:
  - daemon token + `X-Multica-Task-ID` → `agent_foreground` + task_id set
  - daemon token alone → `agent_foreground`, task_id nil
  - PAT → `human`, author_id set
  - JWT → `human`, author_id set

### Integration (Go, with test Postgres)
- Full Put → List → Get cycle
- Workspace isolation: doc in WS-A invisible from WS-B token
- ClaimTaskByRuntime returns the right `WorkspaceContext`, `PinnedDocuments`, `DocumentIndex`, `IssueLinkedDocuments`

### CLI (shell)
- `multica doc put`/`get`/`patch`/`history`/`revert` all round-trip
- `multica doc tree` shows valid hierarchy
- `multica doc link <issue> <path>` creates the link; `unlink` removes it

### E2E (Playwright)
- Human creates a workspace doc via UI
- Human assigns issue to an agent
- Agent's CLAUDE.md (read from working directory in test) contains the doc index
- Agent runs `multica doc get` and uses content
- Agent posts comment that references doc with `mention://doc/<path>` — link resolves in UI
- Agent creates a new doc via `multica doc put`; humanly visible in UI within ~1s (WS push)

### Performance
- Index render for 1000 docs in a workspace: < 50ms server-side
- FTS search across 10k docs: < 200ms p95

---

## 8. Acceptance criteria

For the full feature (after A6 lands), the system MUST:

1. Allow humans (UI + CLI) and agents (CLI under `mdt_*` token) to create, read, update (full or fuzzy patch), rename, tag, pin, archive, and restore workspace documents.
2. Inject `workspace.context`, pinned docs, the index tree, and issue-linked docs into every agent's CLAUDE.md/AGENTS.md/GEMINI.md before subprocess spawn.
3. Preserve every prior version of every document in `workspace_document_revision`. `multica doc history <path>` lists them; `multica doc revert <path> --to-rev N` restores.
4. Honor `workspace.documents_agent_write_mode = 'read_only_for_agents'` by rejecting daemon-token mutations with 403.
5. Surface workspace.context editor in workspace settings UI.
6. Pass all unit, integration, CLI round-trip, and E2E tests above.
7. Index injection adds ≤ 10% to ClaimTaskByRuntime p95 latency.
8. Documentation in `CONTRIBUTING.md` (or new `docs/second-brain.md`) explains the user workflow with at least one example.

---

## 9. Reference implementations to study

- **Editor UI**: `repos/paperclip/ui/src/components/IssueDocumentsSection.tsx` — autosave loop, inline editor, revision drawer, diff viewer.
- **Upsert with conflict detection**: `repos/paperclip/server/src/services/documents.ts` (lines 217–227 for `baseRevisionId` check).
- **Restore endpoint**: `repos/paperclip/server/src/services/documents.ts` (line ~414, `changeSummary: "Restored from revision N"`).
- **Lazy index injection**: `repos/hermes-agent/agent/prompt_builder.py:712-860` (`build_skills_system_prompt`) — read for the disk-snapshot caching approach if our index renders get expensive.
- **Fuzzy patch**: `repos/hermes-agent/tools/skill_manager_tool.py:465-560` (`_apply_patch`) — whitespace-tolerant find/replace.
- **What NOT to do**: `repos/paperclip/packages/db/src/schema/issue_documents.ts:23` — `documentUq` constraint forces 1:1; we deliberately go N:N.

---

## 10. Open questions (decide before A1 ships)

1. **Path constraints**: do we forbid `..`, leading `/`, control chars? Recommendation: regex `^[a-z0-9][a-z0-9/_\-]*\.md$`. Document in OpenAPI.
2. **Concurrent writes**: paperclip uses `baseRevisionId`; if conflict, what does the agent do? Recommendation: return 409 with current revision ID; agent re-fetches, may re-apply patch, retries.
3. **Pinning cap**: how many pinned docs may a workspace have? Recommendation: hard cap 10 per workspace (UI prevents adding more); soft warning at 5 (token budget concern).
4. **Index scope when workspace has > 200 docs**: render scoped subset based on issue's project? Render all but truncate? Recommendation: alphabetic-first 200 + always include any path under the issue's project name; deferred to A6.
5. **Markdown sanitization**: strip script tags or not? Recommendation: render as-is in agent prompt (agent is sandboxed); UI uses a sandboxed markdown renderer.
6. **`workspace.context` versioning**: does the workspace overlay also live in a revisions table? Recommendation: NO in v1 — it's a single field, not big enough to merit. If a user complains, promote it to a pinned document.
7. **Mention link resolution**: `mention://doc/<path>` — how does the web UI resolve it? Recommendation: extend the existing mention parser in `server/internal/mention/expand.go` to recognize the `doc/` scheme.
8. **GC / archived doc retention**: keep archived docs forever? Recommendation: forever in v1; revisit if storage explodes.

---

## 11. Risks & mitigations

| Risk | Mitigation |
|---|---|
| KB index in prompt grows too large | Token caps + scoped subset rendering (A6) |
| Agent writes garbage docs that pollute the KB | `pending_review` lifecycle is in Skills 2.0; for docs, a workspace-level `documents_agent_write_mode='read_only_for_agents'` toggle. Could add per-doc approval in v2. |
| Concurrent edits cause lost updates | `baseRevisionId` conflict detection (paperclip pattern). 409 response, agent retries. |
| `workspace.context` missing or empty for pre-existing workspaces | Fine — empty section in prompt; UI shows "set workspace context" CTA. |
| Large content in revisions blows up DB | Phase 2: content-addressable blobs (hash → content table) with revisions storing only hashes. Defer until measured pain. |
| Path renames break agent muscle memory | `rename` updates path; old path returns 404. Agent must re-query index. Acceptable. |
| Agents over-read full content of every doc | Behavioral instruction in CLAUDE.md ("read only docs you need"); telemetry of `GET /documents/by-path` per task to detect; cap on per-task GETs if abuse seen. |

---

## 12. Out of scope (future PRDs)

- Vector / semantic search via pgvector (the table indexes are FTS-only here).
- Cross-workspace document references (consultant with multiple clients).
- Image/binary attachments to documents (use `attachment` table separately).
- Per-document ACL (today: workspace membership = full access).
- Real-time collaborative editing (CRDT or OT).
- AI-generated descriptions ("write a one-line description for this doc").
- Automatic tag suggestions.
- Doc templates (could be solved later as bundled docs in `provenance='bundled'`).

---

## 13. Definition of done

- All 6 PRs merged to `main`
- All tests in §7 passing in CI
- Manual smoke test: a fresh workspace, a human creates 5 docs (one pinned), assigns an issue to an agent, agent's run reads from the KB and writes one new doc, history visible in UI, restore works
- Telemetry: at least basic logging of doc CRUD per task; nice-to-have a `/admin/documents/stats` endpoint
- `CONTRIBUTING.md` and the project's main README mention the Second Brain capability
- A migration guide note for self-hosters (the migration is non-breaking; pre-existing workspaces work without docs)

You're done. Now go read `repos/multica/CLAUDE.md` for the project's house style, then `repos/multica/CONTRIBUTING.md` for dev setup, then start with PR A1.