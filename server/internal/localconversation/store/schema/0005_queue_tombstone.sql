-- Messages the user removed from a queue, so a client retrying an enqueue whose
-- answer it lost cannot bring one back. Kept for a week.
CREATE TABLE prompt_queue_tombstone (
    conversation_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    removed_at TEXT NOT NULL,
    PRIMARY KEY (conversation_id, request_id)
);
