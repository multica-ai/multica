DROP INDEX IF EXISTS idx_knowledge_item_effectiveness;
DROP INDEX IF EXISTS idx_knowledge_item_governance_review;

ALTER TABLE knowledge_item
    DROP COLUMN IF EXISTS governance_checked_at,
    DROP COLUMN IF EXISTS review_needed_at,
    DROP COLUMN IF EXISTS update_suggestion,
    DROP COLUMN IF EXISTS review_reason,
    DROP COLUMN IF EXISTS conflict_group,
    DROP COLUMN IF EXISTS effectiveness_score,
    DROP COLUMN IF EXISTS stale_score;
