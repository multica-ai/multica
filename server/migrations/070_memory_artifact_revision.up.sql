-- Memory artifact revision history.
--
-- Adds an append-only log of prior states for each memory_artifact row.
-- The current state stays in memory_artifact; older states accumulate
-- here. On UpdateMemoryArtifact, the handler snapshots the OLD state
-- into this table before applying the new update.
--
-- Why a separate table (vs. JSONB array on the artifact row, vs.
-- temporal extensions):
--
--   - JSONB array of prior states would unbounded-grow the row size and
--     break the GIN content_tsv index by introducing variable bloat.
--   - pg_temporal / system_versioning are extensions we don't currently
--     install and would add operational surface for marginal value.
--   - A child table is queryable, indexable per-artifact, and
--     CASCADE-deletes cleanly when an artifact is hard-deleted.
--
-- Restore semantics: restoring to revision N does NOT rewrite history.
-- It produces a NEW edit (and thus a NEW revision row recording the
-- pre-restore state). Users see a linear history; "I rolled back" looks
-- like any other edit. This avoids the "lost change between snapshots"
-- footgun of destructive rollback.

CREATE TABLE memory_artifact_revision (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    memory_artifact_id  UUID         NOT NULL REFERENCES memory_artifact(id) ON DELETE CASCADE,
    -- Denormalized for query isolation: history endpoints take a workspace_id
    -- through the auth path and join on (artifact_id, workspace_id) to avoid
    -- cross-workspace leakage if an artifact id ever gets misused.
    workspace_id        UUID         NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    revision_number     INTEGER      NOT NULL,

    -- Snapshot of the artifact fields at the moment the revision was
    -- captured (i.e. the *prior* state, taken just before an Update).
    title               TEXT         NOT NULL,
    content             TEXT         NOT NULL,
    slug                TEXT         NULL,
    parent_id           UUID         NULL,
    anchor_type         TEXT         NULL,
    anchor_id           UUID         NULL,
    tags                TEXT[]       NOT NULL DEFAULT '{}',
    metadata            JSONB        NOT NULL DEFAULT '{}',
    always_inject_at_runtime BOOLEAN NOT NULL DEFAULT false,

    -- Editor identity at the time the new revision was committed (i.e.
    -- the actor who *caused* this older state to be captured by editing
    -- on top of it). NULL when an Update happens through a system path
    -- without an actor (rare; current handlers always have one).
    editor_type         TEXT         NULL,
    editor_id           UUID         NULL,

    -- created_at is "when this history row was captured", which is also
    -- the moment the new edit landed on the live artifact.
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),

    UNIQUE (memory_artifact_id, revision_number)
);

-- History queries always lead with the artifact id; (artifact, revision_number)
-- is also the natural order for newest-first listing via DESC.
CREATE INDEX idx_memory_revision_by_artifact
    ON memory_artifact_revision (memory_artifact_id, revision_number DESC);
