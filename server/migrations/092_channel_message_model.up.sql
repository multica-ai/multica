CREATE TABLE channel_processing_lock (
    provider         TEXT         NOT NULL,
    connection_id    TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    processing_key   TEXT         NOT NULL,
    active_event_id  UUID         REFERENCES channel_inbound_event(id) ON DELETE SET NULL,
    active_since     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, processing_key)
);

INSERT INTO channel_processing_lock (
    provider,
    connection_id,
    processing_key,
    active_event_id,
    active_since,
    created_at,
    updated_at
)
SELECT
    c.provider,
    c.connection_id,
    c.conversation_key,
    CASE WHEN e.id IS NULL THEN NULL ELSE c.active_event_id END AS active_event_id,
    CASE WHEN e.id IS NULL THEN NULL ELSE c.active_since END AS active_since,
    c.created_at,
    c.updated_at
FROM channel_conversation c
LEFT JOIN channel_inbound_event e
    ON e.id = c.active_event_id
ON CONFLICT (connection_id, processing_key) DO UPDATE SET
    provider = EXCLUDED.provider,
    active_event_id = EXCLUDED.active_event_id,
    active_since = EXCLUDED.active_since,
    updated_at = EXCLUDED.updated_at;

ALTER TABLE channel_conversation RENAME TO channel_conversation_legacy;

CREATE TABLE channel_conversation (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    provider            TEXT         NOT NULL,
    connection_id       TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    conversation_key    TEXT         NOT NULL,
    chat_id             TEXT         NOT NULL,
    chat_type           TEXT         NOT NULL CHECK (chat_type IN ('group', 'direct')),
    conversation_type   TEXT         NOT NULL CHECK (conversation_type IN ('group', 'direct', 'thread')),
    external_thread_id  TEXT         NOT NULL DEFAULT '',
    workspace_id        UUID         REFERENCES workspace(id) ON DELETE SET NULL,
    title               TEXT         NOT NULL DEFAULT '',
    sender_external_id  TEXT         NOT NULL DEFAULT '',
    status              TEXT         NOT NULL DEFAULT 'active'
                                      CHECK (status IN ('active', 'archived')),
    last_message_at     TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (connection_id, conversation_key)
);

INSERT INTO channel_conversation (
    provider,
    connection_id,
    conversation_key,
    chat_id,
    chat_type,
    conversation_type,
    external_thread_id,
    sender_external_id,
    last_message_at,
    created_at,
    updated_at
)
SELECT
    provider,
    connection_id,
    conversation_key,
    chat_id,
    chat_type,
    conversation_type,
    '' AS external_thread_id,
    sender_external_id,
    updated_at AS last_message_at,
    created_at,
    updated_at
FROM (
    SELECT
        provider,
        connection_id,
        CASE
            WHEN chat_type = 'direct' THEN connection_id || ':direct:' || sender_external_id
            ELSE connection_id || ':group:' || chat_id
        END AS conversation_key,
        chat_id,
        chat_type,
        CASE
            WHEN chat_type = 'direct' THEN 'direct'
            ELSE 'group'
        END AS conversation_type,
        CASE
            WHEN chat_type = 'direct' THEN sender_external_id
            ELSE ''
        END AS sender_external_id,
        min(created_at) AS created_at,
        max(updated_at) AS updated_at
    FROM channel_conversation_legacy
    GROUP BY
        provider,
        connection_id,
        CASE
            WHEN chat_type = 'direct' THEN connection_id || ':direct:' || sender_external_id
            ELSE connection_id || ':group:' || chat_id
        END,
        chat_id,
        chat_type,
        CASE
            WHEN chat_type = 'direct' THEN 'direct'
            ELSE 'group'
        END,
        CASE
            WHEN chat_type = 'direct' THEN sender_external_id
            ELSE ''
        END
) normalized
ON CONFLICT (connection_id, conversation_key) DO UPDATE SET
    last_message_at = GREATEST(channel_conversation.last_message_at, EXCLUDED.last_message_at),
    updated_at = GREATEST(channel_conversation.updated_at, EXCLUDED.updated_at);

DROP TABLE channel_conversation_legacy;

CREATE INDEX idx_channel_processing_lock_active
    ON channel_processing_lock(active_event_id)
    WHERE active_event_id IS NOT NULL;

CREATE INDEX idx_channel_processing_lock_stale
    ON channel_processing_lock(active_since)
    WHERE active_event_id IS NOT NULL;

CREATE INDEX idx_channel_conversation_connection_chat
    ON channel_conversation(connection_id, chat_id, conversation_type);

CREATE INDEX idx_channel_conversation_workspace
    ON channel_conversation(workspace_id, updated_at DESC)
    WHERE workspace_id IS NOT NULL;

CREATE INDEX idx_channel_conversation_last_message
    ON channel_conversation(connection_id, last_message_at DESC)
    WHERE last_message_at IS NOT NULL;

ALTER TABLE channel_outbound_notification
    ADD COLUMN actor_type TEXT NOT NULL DEFAULT ''
        CHECK (actor_type IN ('', 'member', 'agent', 'system')),
    ADD COLUMN actor_id UUID,
    ADD COLUMN source_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL;

CREATE TABLE channel_message (
    id                            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    provider                      TEXT         NOT NULL,
    connection_id                 TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    conversation_id               UUID         NOT NULL REFERENCES channel_conversation(id) ON DELETE CASCADE,
    workspace_id                  UUID         REFERENCES workspace(id) ON DELETE SET NULL,
    chat_id                       TEXT         NOT NULL,
    chat_type                     TEXT         NOT NULL CHECK (chat_type IN ('group', 'direct')),
    thread_id                     TEXT         NOT NULL DEFAULT '',
    platform_message_id           TEXT         NOT NULL DEFAULT '',
    event_id                      TEXT         NOT NULL DEFAULT '',
    inbound_event_id              UUID         REFERENCES channel_inbound_event(id) ON DELETE SET NULL,
    outbound_notification_id      UUID         REFERENCES channel_outbound_notification(id) ON DELETE SET NULL,
    direction                     TEXT         NOT NULL CHECK (direction IN ('inbound', 'outbound', 'internal')),
    message_type                  TEXT         NOT NULL CHECK (message_type IN ('user', 'bot', 'agent', 'system', 'notification')),
    sender_type                   TEXT         NOT NULL CHECK (sender_type IN ('user', 'bot', 'agent', 'system', 'unknown')),
    sender_external_id            TEXT         NOT NULL DEFAULT '',
    sender_user_id                UUID         REFERENCES "user"(id) ON DELETE SET NULL,
    sender_agent_id               UUID         REFERENCES agent(id) ON DELETE SET NULL,
    represented_agent_id          UUID         REFERENCES agent(id) ON DELETE SET NULL,
    text                          TEXT         NOT NULL DEFAULT '',
    body                          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    content_format                TEXT         NOT NULL DEFAULT 'plain'
                                                  CHECK (content_format IN ('plain', 'markdown', 'card', 'json')),
    reply_to_platform_message_id  TEXT         NOT NULL DEFAULT '',
    quoted_platform_message_id    TEXT         NOT NULL DEFAULT '',
    reply_to_message_id           UUID         REFERENCES channel_message(id) ON DELETE SET NULL,
    quoted_message_id             UUID         REFERENCES channel_message(id) ON DELETE SET NULL,
    handoff_kind                  TEXT         NOT NULL DEFAULT 'none'
                                                  CHECK (handoff_kind IN (
                                                      'none',
                                                      'approval',
                                                      'retry',
                                                      'continue',
                                                      'need_input',
                                                      'review_pass',
                                                      'failure'
                                                  )),
    suggested_actions             JSONB        NOT NULL DEFAULT '[]'::jsonb
                                                  CHECK (jsonb_typeof(suggested_actions) = 'array'),
    metadata                      JSONB        NOT NULL DEFAULT '{}'::jsonb,
    occurred_at                   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at                    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_channel_message_platform_message
    ON channel_message(connection_id, platform_message_id)
    WHERE platform_message_id <> '';

CREATE UNIQUE INDEX idx_channel_message_event
    ON channel_message(connection_id, event_id)
    WHERE event_id <> '';

CREATE UNIQUE INDEX idx_channel_message_inbound_event
    ON channel_message(inbound_event_id)
    WHERE inbound_event_id IS NOT NULL;

CREATE UNIQUE INDEX idx_channel_message_outbound_notification
    ON channel_message(outbound_notification_id)
    WHERE outbound_notification_id IS NOT NULL;

CREATE INDEX idx_channel_message_conversation_occurred
    ON channel_message(conversation_id, occurred_at DESC);

CREATE INDEX idx_channel_message_thread_occurred
    ON channel_message(connection_id, chat_id, thread_id, occurred_at DESC)
    WHERE thread_id <> '';

CREATE INDEX idx_channel_message_handoff
    ON channel_message(conversation_id, handoff_kind, occurred_at DESC)
    WHERE handoff_kind <> 'none';

CREATE INDEX idx_channel_message_reply_platform
    ON channel_message(connection_id, reply_to_platform_message_id)
    WHERE reply_to_platform_message_id <> '';

CREATE TABLE channel_message_entity_ref (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id   UUID         NOT NULL REFERENCES channel_message(id) ON DELETE CASCADE,
    workspace_id UUID         REFERENCES workspace(id) ON DELETE SET NULL,
    entity_type  TEXT         NOT NULL CHECK (entity_type IN (
                                'issue',
                                'agent',
                                'agent_task',
                                'issue_comment',
                                'project',
                                'pull_request',
                                'inbox_item',
                                'workspace'
                              )),
    entity_id    UUID,
    entity_key   TEXT         NOT NULL DEFAULT '',
    display      TEXT         NOT NULL DEFAULT '',
    role         TEXT         NOT NULL DEFAULT 'mentioned'
                              CHECK (role IN ('primary', 'mentioned', 'handoff_target', 'source', 'result', 'context')),
    metadata     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_channel_message_entity_ref_unique_id
    ON channel_message_entity_ref(message_id, entity_type, role, entity_id)
    WHERE entity_id IS NOT NULL;

CREATE UNIQUE INDEX idx_channel_message_entity_ref_unique_key
    ON channel_message_entity_ref(message_id, entity_type, role, entity_key)
    WHERE entity_key <> '';

CREATE INDEX idx_channel_message_entity_ref_message
    ON channel_message_entity_ref(message_id);

CREATE INDEX idx_channel_message_entity_ref_entity_id
    ON channel_message_entity_ref(entity_type, entity_id, created_at DESC)
    WHERE entity_id IS NOT NULL;

CREATE INDEX idx_channel_message_entity_ref_workspace_key
    ON channel_message_entity_ref(workspace_id, entity_type, entity_key, created_at DESC)
    WHERE workspace_id IS NOT NULL AND entity_key <> '';

CREATE TABLE channel_turn (
    id                    UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    provider              TEXT         NOT NULL,
    connection_id         TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    conversation_id       UUID         NOT NULL REFERENCES channel_conversation(id) ON DELETE CASCADE,
    workspace_id          UUID         REFERENCES workspace(id) ON DELETE SET NULL,
    inbound_event_id      UUID         REFERENCES channel_inbound_event(id) ON DELETE SET NULL,
    inbound_message_id    UUID         REFERENCES channel_message(id) ON DELETE SET NULL,
    outbound_message_id   UUID         REFERENCES channel_message(id) ON DELETE SET NULL,
    sender_external_id    TEXT         NOT NULL DEFAULT '',
    intent_kind           TEXT         NOT NULL DEFAULT '',
    intent_source         TEXT         NOT NULL DEFAULT '',
    intent_payload        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    authz_status          TEXT         NOT NULL DEFAULT ''
                                      CHECK (authz_status IN ('', 'allowed', 'denied', 'skipped')),
    status                TEXT         NOT NULL DEFAULT 'processing'
                                      CHECK (status IN (
                                          'processing',
                                          'waiting_agent',
                                          'waiting_user',
                                          'completed',
                                          'failed',
                                          'dead',
                                          'skipped'
                                      )),
    wait_kind             TEXT,
    wait_task_id          UUID         REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    result_payload        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    last_error            TEXT,
    started_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    completed_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_channel_turn_inbound_event
    ON channel_turn(inbound_event_id)
    WHERE inbound_event_id IS NOT NULL;

CREATE UNIQUE INDEX idx_channel_turn_inbound_message
    ON channel_turn(inbound_message_id)
    WHERE inbound_message_id IS NOT NULL;

CREATE INDEX idx_channel_turn_conversation_created
    ON channel_turn(conversation_id, created_at DESC);

CREATE INDEX idx_channel_turn_status
    ON channel_turn(status, updated_at);

CREATE INDEX idx_channel_turn_waiting_task
    ON channel_turn(wait_task_id, updated_at)
    WHERE wait_task_id IS NOT NULL;
