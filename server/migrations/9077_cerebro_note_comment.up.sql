-- Wave 3 / G1 (TECH-3556) — comments + suggestions on a Note.
--
-- A note comment is a Google-Docs-style margin comment: it is attached to a
-- span of the note body and can be replied to, resolved, and (for suggestions)
-- accepted or rejected. The body lives on the underlying artifact as plain
-- markdown, so comments cannot live *inside* the text without breaking the
-- fork-clean "a note is just an artifact" model. Instead we anchor robustly:
--
--   ANCHORING STRATEGY — quote + context (Hypothes.is / Google-Docs model).
--   A naive char-offset breaks the moment text is inserted above the comment.
--   So every comment stores BOTH:
--     * anchor_quote    — the exact text the author selected,
--     * anchor_prefix   — up to ~32 chars immediately before the selection,
--     * anchor_suffix   — up to ~32 chars immediately after the selection,
--     * anchor_start/end — the char offsets at creation time (a fast hint).
--   On render the client first trusts the offsets; if the text there no longer
--   equals anchor_quote it re-locates the comment by searching for
--   prefix+quote+suffix. If that fails too the comment is shown as "orphaned"
--   in the sidebar (still readable, just detached from a span). This survives
--   ordinary editing — the single biggest robustness requirement in the issue.
--
-- Threads: a top-level comment has thread_root_id = NULL; replies point their
-- thread_root_id at the root comment. Resolving the root resolves the thread.
--
-- Suggestions: kind = 'suggestion' carries suggestion_text — the replacement
-- the author proposes for the anchored span. Accepting a suggestion is an
-- owner action that splices suggestion_text into the note body in place of the
-- anchored span and flips suggestion_state to 'accepted'; rejecting just flips
-- the state. A plain comment has kind = 'comment' and suggestion_text = NULL.
--
-- note_id FKs cerebro_note(artifact_id) ON DELETE CASCADE, so deleting a note
-- drops its whole comment tree with it (matching cerebro_note_reference).
CREATE TABLE IF NOT EXISTS cerebro_note_comment (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    note_id         UUID NOT NULL REFERENCES cerebro_note(artifact_id) ON DELETE CASCADE,
    thread_root_id  UUID REFERENCES cerebro_note_comment(id) ON DELETE CASCADE,

    kind            TEXT NOT NULL DEFAULT 'comment'
                    CHECK (kind IN ('comment', 'suggestion')),

    body            TEXT NOT NULL DEFAULT '',

    -- Anchor (NULL on a reply — replies inherit the root's anchor).
    anchor_quote    TEXT,
    anchor_prefix   TEXT,
    anchor_suffix   TEXT,
    anchor_start    INTEGER,
    anchor_end      INTEGER,

    -- Suggestion payload + lifecycle (only meaningful when kind='suggestion').
    suggestion_text  TEXT,
    suggestion_state TEXT NOT NULL DEFAULT 'pending'
                     CHECK (suggestion_state IN ('pending', 'accepted', 'rejected')),

    -- Resolution (a resolved thread collapses out of the active margin).
    resolved         BOOLEAN NOT NULL DEFAULT false,
    resolved_by      UUID,
    resolved_at      TIMESTAMPTZ,

    author_type     TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id       UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Margin render: every comment for a note, roots + replies, oldest first.
CREATE INDEX IF NOT EXISTS idx_cerebro_note_comment_note
    ON cerebro_note_comment (note_id, created_at);

-- Thread expansion: fetch a root's replies.
CREATE INDEX IF NOT EXISTS idx_cerebro_note_comment_thread
    ON cerebro_note_comment (thread_root_id);
