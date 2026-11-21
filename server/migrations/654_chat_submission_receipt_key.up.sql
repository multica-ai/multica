CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_chat_submission_receipt_key ON chat_submission_receipt (chat_session_id, workspace_id, initiator_user_id, actor_type, actor_id, submission_id);
