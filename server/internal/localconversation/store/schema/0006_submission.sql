CREATE TABLE prompt_submission (
    conversation_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    item TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'queued',
    revision INTEGER NOT NULL DEFAULT 1,
    attempt_id TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL DEFAULT '',
    blocked_reason TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (conversation_id, request_id)
);

CREATE TABLE prompt_attempt (
    conversation_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    submission_id TEXT NOT NULL,
    delivery TEXT NOT NULL DEFAULT 'not_sent',
    turn_ids TEXT NOT NULL DEFAULT '[]',
    settings TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (conversation_id, attempt_id)
);

CREATE TABLE conversation_execution (
    conversation_id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL
);

INSERT INTO prompt_submission (conversation_id, request_id, item, updated_at)
SELECT conversation_id, request_id, item, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM prompt_queue_item;

INSERT OR IGNORE INTO prompt_submission (conversation_id, request_id, item, state, updated_at)
SELECT conversation_id, request_id, '{}', 'removed', removed_at
FROM prompt_queue_tombstone;
