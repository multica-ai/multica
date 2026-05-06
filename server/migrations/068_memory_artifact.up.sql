-- Memory artifact: a single workspace-scoped knowledge primitive that
-- humans curate and agents append to. The shared substrate for wiki
-- pages, agent notes, runbooks, decision logs, and anything else that
-- looks like "markdown content with an author and a workspace."
--
-- Why one table, not several:
--
--   The codebase already has multiple half-overlapping markdown surfaces
--   (skill, workspace.context, project_resource for ref pointers, issue
--   descriptions, comment bodies). Adding wiki_page, agent_note, runbook
--   as separate tables compounds the soup. A single polymorphic table
--   with a `kind` discriminator lets every kind share search, archive,
--   anchor lookup, and runtime injection — features that are otherwise
--   re-implemented per surface.
--
-- The design borrows from project_resource's `resource_type` pattern
-- (open-string discriminator validated in the service layer) and from
-- channel_message's tsvector + JSONB metadata for search and extension.
--
-- See the RFC body on the PR for the full positioning vs wiki_page.

CREATE TABLE memory_artifact (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,

    -- Open-string kind discriminator. Initial supported values:
    --   'wiki_page'    — workspace-curated knowledge page
    --   'agent_note'   — agent-authored finding / decision / dead-end
    --   'runbook'      — operational procedure
    --   'decision'     — architectural decision record
    -- Validated in service.MemoryService — adding a new kind needs zero
    -- migrations.
    kind          TEXT NOT NULL,

    -- Hierarchy: parent doc / folder. NULL = top-level.
    -- ON DELETE CASCADE so removing a parent removes its descendants.
    parent_id     UUID NULL REFERENCES memory_artifact(id) ON DELETE CASCADE,

    -- Content surface
    title         TEXT NOT NULL,
    content       TEXT NOT NULL DEFAULT '',
    -- Optional URL-safe identifier. NULL means "no slug." Uniqueness
    -- is per (workspace_id, kind, slug) so 'getting-started' can exist
    -- as both a wiki_page and a runbook in the same workspace.
    slug          TEXT NULL,

    -- Anchoring: what this artifact is *about*. Polymorphic, NULL = free-floating.
    --
    -- anchor_type ∈ ('issue', 'project', 'agent', 'channel', ...);
    -- anchor_id   is the corresponding entity UUID.
    --
    -- This is what powers "fetch all notes about issue X" used by the
    -- daemon's runtime context injection (a future PR will hydrate
    -- anchored notes into CLAUDE.md when an agent claims a task).
    anchor_type   TEXT NULL,
    anchor_id     UUID NULL,

    -- Authorship: humans curate, agents append.
    author_type   TEXT NOT NULL,
    author_id     UUID NOT NULL,

    -- Categorization
    tags          TEXT[] NOT NULL DEFAULT '{}',
    -- Free-form per-kind extension point. e.g. wiki_page might store
    -- { "icon": "📘" }; agent_note might store { "task_id": "...",
    -- "session_id": "..." } so the note links back to the run that
    -- produced it.
    metadata      JSONB NOT NULL DEFAULT '{}',

    -- Search: tsvector regenerated automatically on every UPDATE.
    -- Phase 2 (separate PR) will add a pgvector embedding column for
    -- semantic search; pgvector is already in the workspace's stack.
    content_tsv   tsvector GENERATED ALWAYS AS (
        to_tsvector('english', coalesce(title, '') || ' ' || coalesce(content, ''))
    ) STORED,

    -- Lifecycle: archive (soft delete) mirrors agent + project archive.
    archived_at   TIMESTAMPTZ NULL,
    archived_by   UUID NULL REFERENCES "user"(id),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Slug uniqueness scoped to (workspace, kind). NULL slugs are
    -- allowed multiply per Postgres NULL-distinctness; only non-null
    -- slugs collide.
    UNIQUE (workspace_id, kind, slug)
);

-- Default-list path: workspace, optional kind/parent filter, exclude
-- archived. The most common page-load query.
CREATE INDEX idx_memory_workspace_kind ON memory_artifact(workspace_id, kind, parent_id, created_at DESC)
    WHERE archived_at IS NULL;

-- Anchor lookup: "fetch every artifact about issue X" — used by the
-- daemon's runtime injection to surface relevant notes at task start.
CREATE INDEX idx_memory_anchor ON memory_artifact(anchor_type, anchor_id)
    WHERE archived_at IS NULL;

-- Full-text search backbone.
CREATE INDEX idx_memory_tsv ON memory_artifact USING GIN(content_tsv);

-- Tag filtering.
CREATE INDEX idx_memory_tags ON memory_artifact USING GIN(tags)
    WHERE archived_at IS NULL;
