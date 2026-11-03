-- NULL means reviews have not been fetched; [] means a complete empty result.
-- Additive and nullable so older servers can keep reading and writing PRs.
ALTER TABLE github_pull_request ADD COLUMN IF NOT EXISTS review_snapshot JSONB;
