-- High-watermark sourced from Feishu's own updated_at, used by incremental
-- sync to set the `updated_at >= ?` filter without depending on the local
-- wall clock (avoids clock skew between Multica and Feishu Project's
-- gateway). Nullable so existing integrations fall back to the legacy
-- last_synced_at-based behavior on their next run.
ALTER TABLE feishu_project_integration
    ADD COLUMN last_seen_updated_at_ms BIGINT,
    ADD COLUMN last_reconciled_at TIMESTAMPTZ;
