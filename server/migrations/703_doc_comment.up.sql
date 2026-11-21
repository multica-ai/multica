-- Discussions on a doc's text (MUL-8370). A root comment carries the anchor
-- (the quoted text and its surrounding context, re-located by text after each
-- save); replies point at their root through parent_id. No foreign keys: the
-- doc and workspace delete paths remove comments in application code.
CREATE TABLE IF NOT EXISTS doc_comment (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    doc_id UUID NOT NULL,
    parent_id UUID,
    author_type TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id UUID NOT NULL,
    content TEXT NOT NULL CHECK (char_length(content) BETWEEN 1 AND 20000),
    anchor_quote TEXT NOT NULL DEFAULT '' CHECK (char_length(anchor_quote) <= 4000),
    anchor_prefix TEXT NOT NULL DEFAULT '' CHECK (char_length(anchor_prefix) <= 64),
    anchor_suffix TEXT NOT NULL DEFAULT '' CHECK (char_length(anchor_suffix) <= 64),
    resolved_at TIMESTAMPTZ,
    resolved_by_type TEXT CHECK (resolved_by_type IN ('member', 'agent')),
    resolved_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
