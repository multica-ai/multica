-- Durable recovery state for post-commit comment side effects. The comment
-- itself remains the source of truth; these request projections let a replay
-- or the background reconciler retry attachment linking and agent dispatch
-- without trusting a new client payload.
ALTER TABLE comment_idempotency
    ADD COLUMN attachment_ids UUID[] NOT NULL DEFAULT '{}',
    ADD COLUMN suppress_agent_ids UUID[] NOT NULL DEFAULT '{}',
    ADD COLUMN side_effects_completed_at TIMESTAMPTZ;
