-- A squad's structured request for a decision by the boss, backed by an
-- ordinary issue (origin_type='proposal', origin_id=proposal.id) so comments and
-- history work unchanged. Rows are written by POST /api/proposals together with
-- the backing issue in one transaction.
--
-- state: pending -> approved | rejected. "Ask for changes" is a rejection with
-- a decision_note.
--
-- No foreign keys; squad, issue and workspace relationships are validated in
-- application code. The primary key is built concurrently in 611/612, and the
-- lookup indexes in 613-615, following issue_unread (597-599).
CREATE TABLE IF NOT EXISTS proposal (
    id               UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    squad_id         UUID NOT NULL,
    issue_id         UUID NOT NULL,
    handoff_squad_id UUID,
    urgent           BOOLEAN NOT NULL DEFAULT false,
    urgent_reason    TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'approved', 'rejected')),
    created_by_type  TEXT NOT NULL CHECK (created_by_type IN ('agent', 'member')),
    created_by_id    UUID NOT NULL,
    decided_by       UUID,
    decided_at       TIMESTAMPTZ,
    decision_note    TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
