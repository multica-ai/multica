-- Provider-reported cost for a usage record, and the rollup columns needed to
-- mix it with rows that have none.
--
-- WHY: cost has always been derived client-side as tokens x a static rate.
-- That cannot express request-level pricing rules. xAI bills a Grok request at
-- 2x once its prompt reaches 200K tokens, and a `task_usage` row aggregates
-- every model call in a turn — so the stored token counts genuinely cannot say
-- which tier any individual request hit. Thresholding on the aggregate would
-- turn a bounded 50% under-estimate into an unbounded over-estimate. Grok
-- already returns its own price per turn (`_meta.usage.costUsdTicks`); store
-- it and the arithmetic stops being a guess.
--
-- UNIT: ticks of 1e-10 USD, as xAI reports them. An integer keeps sub-cent
-- turn costs exact end-to-end instead of drifting through float64. BIGINT
-- holds ~9.2e8 USD, which is not a reachable spend for one usage record.
--
-- NULL means "the provider reported no cost" — every pre-existing row, and
-- every provider that doesn't return one. Those rows keep being estimated from
-- the pricing table. No backfill: historical rows have no authoritative figure
-- to recover, and inventing one would be the same guess we're removing.
ALTER TABLE task_usage
    ADD COLUMN IF NOT EXISTS cost_usd_ticks BIGINT;

COMMENT ON COLUMN task_usage.cost_usd_ticks IS
    'Provider-reported cost in 1e-10 USD. NULL when the provider reports none; those rows are priced client-side from the static rate table.';

-- Rollup side. A single hourly bucket can mix rows that carry an authoritative
-- cost with rows that don't (two providers, or Grok before and after a CLI
-- upgrade). Summing only the authoritative side would silently under-report
-- the bucket, so the rollup carries both halves:
--
--   cost_usd_ticks        - sum over rows that HAVE a provider cost
--   uncosted_*_tokens     - tokens from rows that do NOT, i.e. exactly the
--                           tokens still needing a rate-table estimate
--
-- The consumer then reports `authoritative + estimate(uncosted tokens)`, which
-- degrades to today's behaviour when nothing in the bucket is authoritative.
-- The existing token columns keep their meaning (every row in the bucket) so
-- token displays are untouched.
--
-- The `uncosted_*` columns are NULLABLE WITH NO DEFAULT, and that is load-
-- bearing: NULL means "this bucket has never been recomputed since the split
-- existed", which is every pre-existing row. Readers COALESCE it to the
-- bucket's own token total, i.e. "estimate all of it" — exactly what those
-- rows did before. A `NOT NULL DEFAULT 0` would instead assert "nothing here
-- needs estimating" and collapse every historical bucket's cost to $0 until
-- the rollup happened to touch it again, which is why this migration does NOT
-- rewrite history to seed them: with NULL there is nothing to seed. A bare
-- ADD COLUMN is metadata-only, so this stays a fast DDL with no table rewrite,
-- no WAL churn, and no lock held proportional to table size.
--
-- The rollup writes concrete values for every bucket it recomputes, so rows
-- heal into the split naturally as they are touched.
--
-- `cost_usd_ticks` is NOT NULL DEFAULT 0 because 0 is the honest identity for
-- a sum of "no rows here were priced by their provider", which is true of
-- every historical bucket.
--
-- All additive, so the unique key, the dirty-queue key shape, and every
-- trigger in migration 102 are unaffected.
ALTER TABLE task_usage_hourly
    ADD COLUMN IF NOT EXISTS cost_usd_ticks              BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS uncosted_input_tokens       BIGINT,
    ADD COLUMN IF NOT EXISTS uncosted_output_tokens      BIGINT,
    ADD COLUMN IF NOT EXISTS uncosted_cache_read_tokens  BIGINT,
    ADD COLUMN IF NOT EXISTS uncosted_cache_write_tokens BIGINT;

COMMENT ON COLUMN task_usage_hourly.cost_usd_ticks IS
    'Sum of provider-reported cost (1e-10 USD) over the rows in this bucket that had one; 0 when none did.';
COMMENT ON COLUMN task_usage_hourly.uncosted_input_tokens IS
    'Input tokens from rows with no provider-reported cost — the portion still priced from the static rate table. NULL on buckets not yet recomputed since this column existed; readers COALESCE to input_tokens.';

-- Teach the rollup to populate them. Body is migration 102's, with the two
-- new expression groups in `recomputed` and the matching SET list on
-- conflict; everything else — dirty discovery, the empty-bucket delete, the
-- queue drain, the idempotency contract — is unchanged.
CREATE OR REPLACE FUNCTION rollup_task_usage_hourly_window(
    p_from TIMESTAMPTZ,
    p_to   TIMESTAMPTZ
)
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    v_rows BIGINT;
BEGIN
    IF p_from >= p_to THEN
        RETURN 0;
    END IF;

    WITH
    dirty_from_updates AS (
        SELECT DISTINCT
            task_usage_hour_bucket(tu.created_at) AS bucket_hour,
            a.workspace_id                        AS workspace_id,
            atq.runtime_id                        AS runtime_id,
            atq.agent_id                          AS agent_id,
            i.project_id                          AS project_id,
            tu.provider                           AS provider,
            tu.model                              AS model
          FROM task_usage tu
          JOIN agent_task_queue atq ON atq.id      = tu.task_id
          JOIN agent            a   ON a.id        = atq.agent_id
          LEFT JOIN issue       i   ON i.id        = atq.issue_id
         WHERE atq.runtime_id IS NOT NULL
           AND (
                (tu.updated_at >= p_from AND tu.updated_at < p_to)
                -- Legacy updated_at-NULL rows; partial index from 078.
                OR (tu.updated_at IS NULL
                    AND tu.created_at >= p_from
                    AND tu.created_at <  p_to)
           )
    ),
    dirty_from_queue AS (
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, provider, model
          FROM task_usage_hourly_dirty
         WHERE enqueued_at < p_to
    ),
    dirty_keys AS (
        SELECT * FROM dirty_from_updates
        UNION
        SELECT * FROM dirty_from_queue
    ),
    recomputed AS (
        SELECT
            dk.bucket_hour,
            dk.workspace_id,
            dk.runtime_id,
            dk.agent_id,
            dk.project_id,
            dk.provider,
            dk.model,
            SUM(tu.input_tokens)::bigint       AS input_tokens,
            SUM(tu.output_tokens)::bigint      AS output_tokens,
            SUM(tu.cache_read_tokens)::bigint  AS cache_read_tokens,
            SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
            -- Authoritative half: only rows the provider priced.
            COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
            -- Estimated half: tokens from rows the provider did not price.
            COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
            COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
            COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
            COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
            COUNT(DISTINCT tu.task_id)::bigint AS task_count,
            COUNT(*)::bigint                   AS event_count
          FROM dirty_keys dk
          JOIN agent_task_queue atq ON atq.runtime_id  = dk.runtime_id
                                    AND atq.agent_id    = dk.agent_id
          JOIN agent            a   ON a.id            = atq.agent_id
                                    AND a.workspace_id = dk.workspace_id
          LEFT JOIN issue       i   ON i.id            = atq.issue_id
          JOIN task_usage       tu  ON tu.task_id      = atq.id
                                    AND tu.provider    = dk.provider
                                    AND tu.model       = dk.model
                                    AND task_usage_hour_bucket(tu.created_at) = dk.bucket_hour
         WHERE (i.project_id IS NOT DISTINCT FROM dk.project_id)
         GROUP BY 1, 2, 3, 4, 5, 6, 7
    ),
    upserted AS (
        INSERT INTO task_usage_hourly AS d (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            task_count, event_count
        )
        SELECT
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            task_count, event_count
          FROM recomputed
        ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_key DO UPDATE
            SET input_tokens                = EXCLUDED.input_tokens,
                output_tokens               = EXCLUDED.output_tokens,
                cache_read_tokens           = EXCLUDED.cache_read_tokens,
                cache_write_tokens          = EXCLUDED.cache_write_tokens,
                cost_usd_ticks              = EXCLUDED.cost_usd_ticks,
                uncosted_input_tokens       = EXCLUDED.uncosted_input_tokens,
                uncosted_output_tokens      = EXCLUDED.uncosted_output_tokens,
                uncosted_cache_read_tokens  = EXCLUDED.uncosted_cache_read_tokens,
                uncosted_cache_write_tokens = EXCLUDED.uncosted_cache_write_tokens,
                task_count                  = EXCLUDED.task_count,
                event_count                 = EXCLUDED.event_count,
                updated_at                  = now()
        RETURNING 1
    ),
    deleted_empty AS (
        DELETE FROM task_usage_hourly d
         USING dirty_keys dk
         WHERE d.bucket_hour  = dk.bucket_hour
           AND d.workspace_id = dk.workspace_id
           AND d.runtime_id   = dk.runtime_id
           AND d.agent_id     = dk.agent_id
           AND d.project_id IS NOT DISTINCT FROM dk.project_id
           AND d.provider     = dk.provider
           AND d.model        = dk.model
           AND NOT EXISTS (
               SELECT 1 FROM recomputed r
                WHERE r.bucket_hour  = dk.bucket_hour
                  AND r.workspace_id = dk.workspace_id
                  AND r.runtime_id   = dk.runtime_id
                  AND r.agent_id     = dk.agent_id
                  AND r.project_id IS NOT DISTINCT FROM dk.project_id
                  AND r.provider     = dk.provider
                  AND r.model        = dk.model
           )
        RETURNING 1
    )
    SELECT (SELECT COUNT(*) FROM upserted) + (SELECT COUNT(*) FROM deleted_empty)
      INTO v_rows;

    DELETE FROM task_usage_hourly_dirty WHERE enqueued_at < p_to;

    RETURN v_rows;
END;
$$;
