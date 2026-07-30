-- Version history for issue.description (MUL-5470).
--
-- The description is not a static ticket body here: humans write the original
-- request, agents rewrite it into plans and acceptance criteria, autopilots and
-- GitHub triage write it too. Until now every write overwrote the column in
-- place and the only trace was an activity_log row with empty details, so an
-- agent replacing a human's original intent was unrecoverable.
--
-- One row = one editing SESSION, not one write. The description editor
-- autosaves on a 1.5s debounce, so a naive row-per-write would bury the
-- timeline; the handler coalesces consecutive writes by the same actor (see
-- descriptionVersionCoalesceWindow) and rewrites the open row instead.
--
-- content is a full snapshot rather than a delta: descriptions are a few KB,
-- the session coalescing already bounds the row count, and full snapshots make
-- both diff and restore trivial.
--
-- parent_version_id points at the version this one is diffed against, so
-- added_lines/removed_lines can be recomputed from a single PK lookup when a
-- session row is rewritten. NULL marks the seeded V0 — the description as it
-- stood before the first tracked edit, which is the single most valuable
-- comparison point (original request vs. the agent's first rewrite).
--
-- No foreign keys per the repo migration rules: DeleteIssue cleans these rows
-- up explicitly in application code.
CREATE TABLE IF NOT EXISTS issue_description_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    parent_version_id UUID,
    actor_type TEXT CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id UUID,
    source_task_id UUID,
    content TEXT NOT NULL,
    added_lines INT NOT NULL DEFAULT 0,
    removed_lines INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
