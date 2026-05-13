-- JEH-1173: audit table for persona-mask redactions.
--
-- Captures one row per redacted read: deny on a classified resource, or
-- allow-with-mask where one or more fields were stripped before render.
-- Allow-without-mask reads are NOT recorded — the table is a redaction
-- ledger, not a general access log (that's persona's logbog).
--
-- Used by ops to answer "did Mia see CPR data?" and by the post-mortem
-- flow when a wrong grant is suspected. Indexed for the two queries
-- callers actually run: by (workspace, time desc) and by (resource_id,
-- time desc).
CREATE TABLE IF NOT EXISTS cerebro_persona_mask_audit (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID        NOT NULL,
    actor_type      TEXT        NOT NULL,
    actor_id        TEXT        NOT NULL,
    action          TEXT        NOT NULL,
    resource_kind   TEXT        NOT NULL,
    resource_id     TEXT        NOT NULL,
    classification  TEXT        NOT NULL,
    decision        TEXT        NOT NULL CHECK (decision IN ('deny', 'mask')),
    masked_fields   JSONB       NOT NULL DEFAULT '[]'::jsonb,
    decision_id     TEXT        NOT NULL DEFAULT '',
    reason          TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_persona_mask_audit_ws_time
    ON cerebro_persona_mask_audit (workspace_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_cerebro_persona_mask_audit_resource
    ON cerebro_persona_mask_audit (resource_kind, resource_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_cerebro_persona_mask_audit_actor
    ON cerebro_persona_mask_audit (actor_id, created_at DESC);
