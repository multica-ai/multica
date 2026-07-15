-- Custom issue properties (MUL-4463).
--
-- Two-part model:
--   1. issue_property — workspace-level property definitions (the catalog),
--      structurally a sibling of issue_label.
--   2. issue.properties — JSONB value bag on each issue keyed by property
--      definition UUID, structurally a sibling of issue.metadata. Values are
--      typed per the definition and validated in the handler; keys are UUIDs
--      so definition renames never require value migrations.
--
-- Definitions are archived (archived_at), never deleted — historical values
-- stay resolvable. The active-definition cap (20/workspace) and per-type value
-- validation live in the handler.

CREATE TABLE issue_property (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url')),
    description TEXT NOT NULL DEFAULT '',
    -- select / multi_select: {"options": [{"id", "name", "color"}]}; {} for other types.
    config JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(config) = 'object'),
    position FLOAT NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE issue ADD COLUMN properties JSONB NOT NULL DEFAULT '{}'::jsonb;
-- NOT VALID + VALIDATE keeps the ACCESS EXCLUSIVE lock instantaneous; the
-- validation pass then runs under SHARE UPDATE EXCLUSIVE without blocking
-- writes (zero-downtime deploys, review round 3).
ALTER TABLE issue ADD CONSTRAINT issue_properties_is_object
    CHECK (jsonb_typeof(properties) = 'object') NOT VALID;
ALTER TABLE issue VALIDATE CONSTRAINT issue_properties_is_object;
-- Larger than metadata's 8KB: multi_select arrays and text values are bigger
-- than metadata primitives, but the bag is still bounded by the definition cap.
ALTER TABLE issue ADD CONSTRAINT issue_properties_size_limit
    CHECK (pg_column_size(properties) <= 16384) NOT VALID;
ALTER TABLE issue VALIDATE CONSTRAINT issue_properties_size_limit;
-- All indexes live in follow-up, single-statement migrations because CREATE
-- INDEX CONCURRENTLY cannot share a migration with other statements. The GIN
-- index is in 192; the issue_property indexes are in 194 and 195.
