-- name: GetSiteCounter :one
SELECT value FROM site_counter WHERE name = $1;

-- name: AddSiteCounter :one
INSERT INTO site_counter (name, value)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE
SET value = site_counter.value + EXCLUDED.value, updated_at = now()
RETURNING value;
