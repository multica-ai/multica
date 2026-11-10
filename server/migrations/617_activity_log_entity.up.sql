ALTER TABLE activity_log
    ADD COLUMN entity_type TEXT,
    ADD COLUMN entity_id UUID;

UPDATE activity_log
SET entity_type = 'issue', entity_id = issue_id
WHERE issue_id IS NOT NULL;

ALTER TABLE activity_log
    ADD CONSTRAINT activity_log_entity_type_check
        CHECK (entity_type IS NULL OR entity_type IN ('issue', 'project')),
    ADD CONSTRAINT activity_log_entity_pair_check
        CHECK ((entity_type IS NULL) = (entity_id IS NULL)),
    ADD CONSTRAINT activity_log_issue_entity_check
        CHECK (issue_id IS NULL OR (entity_type = 'issue' AND entity_id = issue_id));
