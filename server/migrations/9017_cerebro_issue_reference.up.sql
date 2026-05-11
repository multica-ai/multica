-- JEH-837 — Backend fundament for cerebro_issue_reference. Stores arbitrary
-- typed references attached to an issue (e.g. linked Stripe customer, GitHub
-- PR, Linear issue, internal entity). Generic shape on purpose so the
-- frontend `cerebro-references` extension can grow new object kinds without
-- a migration per kind: `(object, type)` describes what the row points at,
-- `ref_id` is the external/internal identifier, `metadata` carries
-- payload-specific fields.
--
-- Uniqueness is `(issue_id, object, type, ref_id)` so the same external
-- entity is attached at most once per issue under a given type — the POST
-- handler relies on this to UPSERT idempotently.

CREATE TABLE IF NOT EXISTS cerebro_issue_reference (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id        UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    object          TEXT NOT NULL,
    type            TEXT,
    ref_id          TEXT NOT NULL,
    label           TEXT,
    url             TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id   UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, object, type, ref_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_issue_reference_issue
    ON cerebro_issue_reference (issue_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_issue_reference_lookup
    ON cerebro_issue_reference (object, ref_id);
