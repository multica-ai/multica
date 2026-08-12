CREATE UNIQUE INDEX CONCURRENTLY execution_evidence_record_review_task_uidx ON execution_evidence_record (review_task_id) WHERE review_task_id IS NOT NULL;
