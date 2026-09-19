CREATE UNIQUE INDEX CONCURRENTLY idx_comment_create_request
    ON comment (workspace_id, author_type, author_id, request_key)
    WHERE request_key IS NOT NULL;
