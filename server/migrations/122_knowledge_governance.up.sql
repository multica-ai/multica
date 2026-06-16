ALTER TABLE knowledge_item
    ADD COLUMN stale_score NUMERIC(5,2) NOT NULL DEFAULT 0 CHECK (stale_score >= 0 AND stale_score <= 100),
    ADD COLUMN effectiveness_score NUMERIC(5,2) NOT NULL DEFAULT 100 CHECK (effectiveness_score >= 0 AND effectiveness_score <= 100),
    ADD COLUMN conflict_group TEXT,
    ADD COLUMN review_reason TEXT,
    ADD COLUMN update_suggestion TEXT,
    ADD COLUMN review_needed_at TIMESTAMPTZ,
    ADD COLUMN governance_checked_at TIMESTAMPTZ;

CREATE INDEX idx_knowledge_item_governance_review
    ON knowledge_item(workspace_id, review_needed_at DESC)
    WHERE review_needed_at IS NOT NULL;

CREATE INDEX idx_knowledge_item_effectiveness
    ON knowledge_item(workspace_id, effectiveness_score, updated_at DESC)
    WHERE lifecycle_status = 'published';
