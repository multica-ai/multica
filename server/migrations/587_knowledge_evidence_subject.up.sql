CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_evidence_subject_idx ON knowledge_evidence (knowledge_base_id, subject_type, subject_id);
