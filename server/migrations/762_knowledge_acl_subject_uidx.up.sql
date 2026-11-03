-- The grant key: one row per base and subject. Changing a grant's access
-- updates this row in place.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_acl_subject_uidx
    ON knowledge_acl (knowledge_base_id, subject_type, subject_id);
