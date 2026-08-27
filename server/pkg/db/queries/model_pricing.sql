-- name: GetModelPricing :one
SELECT * FROM model_pricing WHERE concrete = $1;

-- name: ListModelPricing :many
SELECT * FROM model_pricing;

-- name: UpsertModelPricing :one
INSERT INTO model_pricing (concrete, input_usd_per_mtok, output_usd_per_mtok, threshold_input_usd_per_mtok, fetched_at)
VALUES ($1, sqlc.narg('input_usd_per_mtok'), sqlc.narg('output_usd_per_mtok'), sqlc.narg('threshold_input_usd_per_mtok'), now())
ON CONFLICT (concrete) DO UPDATE SET input_usd_per_mtok = EXCLUDED.input_usd_per_mtok, output_usd_per_mtok = EXCLUDED.output_usd_per_mtok, threshold_input_usd_per_mtok = COALESCE(EXCLUDED.threshold_input_usd_per_mtok, model_pricing.threshold_input_usd_per_mtok), fetched_at = now()
RETURNING *;

-- name: ListDistinctConcreteFromTierMap :many
SELECT DISTINCT concrete FROM model_tier_map
UNION
SELECT DISTINCT unnest(fallback_concrete) FROM model_tier_map WHERE fallback_concrete IS NOT NULL AND array_length(fallback_concrete,1) > 0;
