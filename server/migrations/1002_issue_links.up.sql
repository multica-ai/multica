-- 1002_issue_links: typed peer-level relationships between issues.
--
-- Supports four link types: blocks/blocked_by, depends_on/required_for,
-- duplicates/duplicated_by, relates_to. Cross-workspace allowed.
--
-- Storage strategy: one row per direction, mirrored on write. For an A->B
-- "blocks" link we insert two rows:
--   (id=X1, src=A, tgt=B, link_type='blocks',     direction='outgoing', pair=P)
--   (id=X2, src=B, tgt=A, link_type='blocks',     direction='incoming', pair=P)
-- This keeps reads simple ("show all links touching issue X" is one
-- index hit) and makes deletion symmetric (DELETE WHERE pair_id = P).

CREATE TABLE IF NOT EXISTS issue_link (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pair_id              UUID NOT NULL,

    -- The "from" side of this row. The mirror row swaps source and target.
    source_issue_id      UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    source_workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    target_issue_id      UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    target_workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,

    -- The relationship category. Both mirror rows share the same link_type.
    link_type            TEXT NOT NULL
        CHECK (link_type IN ('blocks', 'depends_on', 'duplicates', 'relates_to')),

    -- Which side this row represents. For symmetric types ('relates_to') we
    -- still write outgoing/incoming so the listing query is uniform; UIs can
    -- ignore the direction for symmetric types.
    direction            TEXT NOT NULL
        CHECK (direction IN ('outgoing', 'incoming')),

    -- Authorship. Mirror rows record the same actor.
    creator_type         TEXT NOT NULL
        CHECK (creator_type IN ('member', 'agent', 'system')),
    creator_id           UUID,

    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Source must differ from target.
    CONSTRAINT chk_issue_link_no_self CHECK (source_issue_id <> target_issue_id),

    -- A given (source, target, link_type) pair is unique. The mirror occupies
    -- the swapped slot so this constraint covers both rows naturally.
    CONSTRAINT uq_issue_link UNIQUE (source_issue_id, target_issue_id, link_type)
);

-- Fast lookup: "all links touching issue X" — used by every read path.
CREATE INDEX IF NOT EXISTS idx_issue_link_source
    ON issue_link (source_issue_id);

-- Pair lookup: deletion deletes both rows in one statement.
CREATE INDEX IF NOT EXISTS idx_issue_link_pair
    ON issue_link (pair_id);

-- Workspace-scoped listing (e.g. backfill / admin reports).
CREATE INDEX IF NOT EXISTS idx_issue_link_source_workspace
    ON issue_link (source_workspace_id);

-- "Blockers for issue X": frequent query for the soft-warning UI. We want
-- "all incoming blocks links into X" → "blockers". Composite covers it.
CREATE INDEX IF NOT EXISTS idx_issue_link_blockers
    ON issue_link (source_issue_id, link_type, direction)
    WHERE link_type = 'blocks';
