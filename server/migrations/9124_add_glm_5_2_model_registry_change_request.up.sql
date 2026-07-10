-- FIR-2998: Propose adding GLM-5.2 to the model registry (pending change request).
--
-- GLM-5.2 was missing from the registry seed (migration 9120): only glm-5.1 and
-- below were curated, every GLM entry with context_window 0 ("not curated").
-- The model registry is governed (propose -> approve, FIR-2698) and the API is
-- member-gated, so this migration inserts a PENDING change request that an
-- approver (workspace owner/admin) reviews and merges from the Models page.
--
-- proposed_snapshot is built from the LIVE registry row via jsonb_set, so it
-- stays correct regardless of any changes merged since seed — no need to know
-- the current prod snapshot from here.
--
-- Values are the TensorX list price (TensorX is the first-party EU provider we
-- route GLM-5.2 through, migration 20260620093000). Source:
-- tensorx.ai/models/z-ai--glm-5.2 (fetched 2026-07-09):
--   input $1.50 / output $4.50 / cache-read $0.38 per 1M tokens,
--   context window 1M tokens.
-- cache_write is not published by TensorX; set = input, matching every other
-- GLM entry in the registry (glm-5.1, glm-5, glm-5-turbo, glm-4.x).
--
-- proposed_version is the next patch version from the live registry, so the
-- migration still produces an approvable request if another registry update
-- has already moved the singleton past 1.0.0.
--
-- proposed_by is the Rasmus agent id (FIR-2998 owner). The column is a
-- polymorphic UUID (no FK) by design, matching agent_change_request.
--
-- Idempotent: the NOT EXISTS guard makes a re-run a no-op once a pending
-- glm-5.2 proposal exists for this registry.

BEGIN;

INSERT INTO model_registry_change_request (
    registry_id, title, description, base_version, proposed_version,
    proposed_snapshot, status, proposed_by
)
SELECT
    r.id,
    'Add GLM-5.2 (TensorX pricing, 1M context window)',
    'GLM-5.2 was absent from the registry seed (only glm-5.1 and below existed, all with context_window 0). Adds glm-5.2 at TensorX list prices: input $1.50 / output $4.50 / cache-read $0.38 per 1M tokens, 1M context window. Source: tensorx.ai/models/z-ai--glm-5.2. cache_write not published by TensorX; set = input, matching the other GLM entries. Proposed by the Rasmus agent (FIR-2998).',
    r.current_version,
    concat(
        split_part(r.current_version, '.', 1),
        '.',
        split_part(r.current_version, '.', 2),
        '.',
        (split_part(r.current_version, '.', 3)::int + 1)::text
    ),
    jsonb_set(
        r.snapshot,
        '{models,glm-5.2}',
        '{"label":"GLM-5.2","provider":"zhipu","context_window":1000000,"input_usd_per_mtok":1.5,"output_usd_per_mtok":4.5,"cache_read_usd_per_mtok":0.38,"cache_write_usd_per_mtok":1.5}'::jsonb,
        true
    ),
    'pending',
    'cb01aff2-2305-480a-b18c-a450a0ec22de'
FROM model_registry r
WHERE r.registry_key = 'default'
  AND NOT EXISTS (
    SELECT 1
    FROM model_registry_change_request cr
    WHERE cr.registry_id = r.id
      AND cr.status = 'pending'
      AND cr.proposed_snapshot -> 'models' ? 'glm-5.2'
  );

COMMIT;
