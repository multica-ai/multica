ALTER TABLE comment_idempotency
    DROP COLUMN IF EXISTS side_effects_completed_at,
    DROP COLUMN IF EXISTS suppress_agent_ids,
    DROP COLUMN IF EXISTS attachment_ids;
