-- MUL-9420: when a member loses access to content (becomes a guest, or a guest
-- loses a project) nothing it can no longer see is rewritten, so local search
-- sync has no change to turn into a deletion. The transaction that narrows the
-- access records its xid here; a search index cursor whose snapshot does not
-- include that xid was built under the old access and must be rebuilt.
--
-- One row per (workspace, user), overwritten by each narrowing. No foreign
-- keys (repo rule): a row outliving its member is inert, because a later
-- snapshot always includes its xid.
CREATE TABLE IF NOT EXISTS member_search_access (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    narrowed_xid xid8 NOT NULL,
    narrowed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);
