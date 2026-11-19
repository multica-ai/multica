-- MUL-9420: when what a member can see changes without the content changing
-- (a member becomes a guest, a guest gains or loses a project, a guest
-- becomes a member), local search sync has no change row to deliver. The
-- transaction that changes the access records its xid here; a search index
-- cursor whose snapshot does not include that xid was built under the old
-- access and must be rebuilt.
--
-- One row per (workspace, user), overwritten by each change. The primary key
-- attaches after a concurrent build (701, 702). No foreign keys (repo rule):
-- a row outliving its member is inert, because a later snapshot always
-- includes its xid.
CREATE TABLE IF NOT EXISTS member_search_access (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    changed_xid xid8 NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
