CREATE INDEX CONCURRENTLY idx_issue_source_context_object_intent_due ON issue_source_context_object_intent (next_attempt_at) WHERE state IN ('pending', 'deleting');
