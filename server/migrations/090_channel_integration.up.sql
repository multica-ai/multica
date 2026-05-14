CREATE TABLE channel_connection (
    id            TEXT        PRIMARY KEY,
    provider      TEXT        NOT NULL,
    display_name  TEXT        NOT NULL,
    enabled       BOOLEAN     NOT NULL DEFAULT TRUE,
    is_default    BOOLEAN     NOT NULL DEFAULT FALSE,
    config        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    secret_config JSONB       NOT NULL DEFAULT '{}'::jsonb,
    status        TEXT        NOT NULL DEFAULT 'configured'
                                CHECK (status IN ('configured', 'connected', 'disabled', 'error')),
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_channel_connection_provider_default
    ON channel_connection(provider)
    WHERE is_default;

CREATE TABLE channel_user_binding (
    id                 UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    provider           TEXT         NOT NULL,
    connection_id      TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    external_user_id   TEXT         NOT NULL,
    user_id            UUID         NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    external_name      TEXT,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (connection_id, external_user_id),
    UNIQUE (connection_id, user_id)
);

CREATE TABLE channel_chat_binding (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    provider            TEXT         NOT NULL,
    connection_id       TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    external_chat_id    TEXT         NOT NULL,
    chat_type           TEXT         NOT NULL CHECK (chat_type IN ('group', 'dm')),
    workspace_id        UUID         NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    is_primary          BOOLEAN      NOT NULL DEFAULT TRUE,
    bound_by_user_id    UUID         REFERENCES "user"(id) ON DELETE SET NULL,
    external_chat_name  TEXT,
    default_project_id  UUID         REFERENCES project(id) ON DELETE SET NULL,
    listen_mode         TEXT         NOT NULL DEFAULT 'mentions'
                                          CHECK (listen_mode IN ('mentions', 'all')),
    agent_id            UUID         REFERENCES agent(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (connection_id, external_chat_id)
);

CREATE UNIQUE INDEX idx_channel_chat_binding_primary_per_connection_ws
    ON channel_chat_binding (connection_id, workspace_id)
    WHERE is_primary;

CREATE INDEX idx_channel_chat_binding_workspace_connection
    ON channel_chat_binding (workspace_id, connection_id);

CREATE INDEX idx_channel_chat_binding_default_project
    ON channel_chat_binding(default_project_id)
    WHERE default_project_id IS NOT NULL;

CREATE INDEX idx_channel_chat_binding_agent
    ON channel_chat_binding(agent_id)
    WHERE agent_id IS NOT NULL;

CREATE TABLE channel_bind_token (
    token_hash         BYTEA        PRIMARY KEY,
    purpose            TEXT         NOT NULL DEFAULT 'user_identity'
                                      CHECK (purpose IN ('user_identity', 'chat_workspace')),
    provider           TEXT         NOT NULL,
    connection_id      TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    external_user_id   TEXT         NOT NULL,
    external_chat_id   TEXT,
    external_chat_type TEXT,
    external_chat_name TEXT,
    expires_at         TIMESTAMPTZ  NOT NULL,
    consumed_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CHECK (
        purpose = 'user_identity'
        OR (
            external_chat_id IS NOT NULL
            AND external_chat_type IS NOT NULL
        )
    )
);

CREATE INDEX idx_channel_bind_token_unconsumed
    ON channel_bind_token (expires_at)
    WHERE consumed_at IS NULL;

CREATE INDEX idx_channel_bind_token_connection
    ON channel_bind_token (connection_id, external_user_id);

CREATE TABLE channel_inbound_event_dedup (
    provider       TEXT         NOT NULL,
    connection_id  TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    event_id       TEXT         NOT NULL,
    processed_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    status         TEXT         NOT NULL DEFAULT 'processed'
                                CHECK (status IN ('processing', 'processed', 'failed')),
    attempts       INTEGER      NOT NULL DEFAULT 1,
    last_error     TEXT,
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, event_id)
);

CREATE INDEX idx_channel_inbound_event_dedup_processed_at
    ON channel_inbound_event_dedup (processed_at);

CREATE INDEX idx_channel_inbound_event_dedup_retryable
    ON channel_inbound_event_dedup (status, updated_at)
    WHERE status IN ('processing', 'failed');

CREATE TABLE channel_conversation (
    provider            TEXT         NOT NULL,
    connection_id       TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    conversation_key    TEXT         NOT NULL,
    chat_id             TEXT         NOT NULL,
    chat_type           TEXT         NOT NULL CHECK (chat_type IN ('group', 'direct')),
    sender_external_id  TEXT         NOT NULL,
    active_event_id     UUID,
    active_since        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, conversation_key)
);

CREATE TABLE channel_inbound_event (
    id                    UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    provider              TEXT         NOT NULL,
    connection_id         TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    event_id              TEXT         NOT NULL,
    event_type            TEXT         NOT NULL,
    conversation_key      TEXT         NOT NULL,
    chat_id               TEXT         NOT NULL,
    chat_type             TEXT         NOT NULL CHECK (chat_type IN ('group', 'direct')),
    sender_external_id    TEXT         NOT NULL,
    sender_name           TEXT         NOT NULL DEFAULT '',
    message_id            TEXT         NOT NULL DEFAULT '',
    text                  TEXT         NOT NULL DEFAULT '',
    canonical_event       JSONB        NOT NULL,
    raw_payload           JSONB        NOT NULL DEFAULT '{}'::jsonb,
    status                TEXT         NOT NULL DEFAULT 'queued'
                                        CHECK (status IN (
                                            'queued',
                                            'processing',
                                            'processed',
                                            'waiting_agent',
                                            'waiting_user',
                                            'failed',
                                            'dead',
                                            'rejected_backpressure'
                                        )),
    phase                 TEXT         NOT NULL DEFAULT 'pre'
                                        CHECK (phase IN ('pre', 'intent', 'post', 'done')),
    wait_kind             TEXT         CHECK (wait_kind IN ('intent', 'action', 'channel_turn', 'user_clarification')),
    wait_task_id          UUID         REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    wait_expires_at       TIMESTAMPTZ,
    workspace_id          UUID         REFERENCES workspace(id) ON DELETE SET NULL,
    default_project_id    UUID         REFERENCES project(id) ON DELETE SET NULL,
    intent_payload        JSONB,
    dispatch_completed_at TIMESTAMPTZ,
    dispatch_reply_text   TEXT         NOT NULL DEFAULT '',
    attempts              INTEGER      NOT NULL DEFAULT 0,
    max_attempts          INTEGER      NOT NULL DEFAULT 3,
    next_attempt_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    locked_at             TIMESTAMPTZ,
    locked_by             TEXT,
    started_at            TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,
    last_error            TEXT,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (connection_id, event_id)
);

CREATE INDEX idx_channel_inbound_event_claim
    ON channel_inbound_event(status, next_attempt_at, created_at)
    WHERE status = 'queued';

CREATE INDEX idx_channel_inbound_event_processing
    ON channel_inbound_event(status, updated_at)
    WHERE status = 'processing';

CREATE INDEX idx_channel_inbound_event_waiting_agent
    ON channel_inbound_event(status, wait_task_id, updated_at)
    WHERE status = 'waiting_agent';

CREATE INDEX idx_channel_inbound_event_waiting_user_expiry
    ON channel_inbound_event(status, wait_expires_at)
    WHERE status = 'waiting_user';

CREATE INDEX idx_channel_inbound_event_connection_conversation
    ON channel_inbound_event(connection_id, conversation_key, status, created_at);

CREATE TABLE channel_action_result (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    inbound_event_id    UUID        NOT NULL REFERENCES channel_inbound_event(id) ON DELETE CASCADE,
    action_kind         TEXT        NOT NULL CHECK (action_kind IN (
                                      'create_issue',
                                      'add_comment',
                                      'set_status',
                                      'set_assignee',
                                      'set_priority',
                                      'add_label',
                                      'remove_label'
                                    )),
    status              TEXT        NOT NULL DEFAULT 'processing'
                                      CHECK (status IN ('processing', 'completed')),
    result_payload      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    last_error          TEXT,
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (inbound_event_id, action_kind)
);

CREATE INDEX idx_channel_action_result_event
    ON channel_action_result(inbound_event_id);

CREATE TABLE channel_outbound_notification (
    id                        UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    provider                  TEXT         NOT NULL,
    connection_id             TEXT         NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    event_kind                TEXT         NOT NULL,
    target_user_id            UUID         REFERENCES "user"(id) ON DELETE CASCADE,
    target_external_user_id   TEXT,
    target_type               TEXT         NOT NULL DEFAULT 'user'
                                            CHECK (target_type IN ('user', 'chat')),
    target_chat_id            TEXT         NOT NULL DEFAULT '',
    mention_external_user_id  TEXT         NOT NULL DEFAULT '',
    title                     TEXT         NOT NULL,
    body                      TEXT         NOT NULL DEFAULT '',
    status                    TEXT         NOT NULL DEFAULT 'pending'
                                            CHECK (status IN ('pending', 'processing', 'sent', 'dead')),
    attempts                  INTEGER      NOT NULL DEFAULT 0,
    max_attempts              INTEGER      NOT NULL DEFAULT 3,
    aggregation_due_at        TIMESTAMPTZ  NOT NULL,
    next_attempt_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_error                TEXT,
    workspace_id              UUID         REFERENCES workspace(id) ON DELETE SET NULL,
    issue_id                  UUID         REFERENCES issue(id) ON DELETE SET NULL,
    issue_identifier          TEXT         NOT NULL DEFAULT '',
    issue_title               TEXT         NOT NULL DEFAULT '',
    inbox_item_id             UUID         REFERENCES inbox_item(id) ON DELETE SET NULL,
    replyable                 BOOLEAN      NOT NULL DEFAULT false,
    created_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_outbound_notification_due
    ON channel_outbound_notification (aggregation_due_at, next_attempt_at)
    WHERE status = 'pending';

CREATE INDEX idx_channel_outbound_notification_connection
    ON channel_outbound_notification (connection_id, aggregation_due_at, next_attempt_at)
    WHERE status = 'pending';

CREATE INDEX idx_channel_outbound_notification_processing
    ON channel_outbound_notification (updated_at)
    WHERE status = 'processing';

CREATE INDEX idx_channel_outbound_notification_cleanup
    ON channel_outbound_notification (updated_at)
    WHERE status IN ('sent', 'dead');

CREATE TABLE channel_action_proposal (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code                TEXT        NOT NULL,
    connection_id       TEXT        NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    chat_id             TEXT        NOT NULL,
    sender_external_id  TEXT        NOT NULL,
    workspace_id        UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    inbound_event_id    UUID        NOT NULL REFERENCES channel_inbound_event(id) ON DELETE CASCADE,
    action_kind         TEXT        NOT NULL CHECK (action_kind IN (
                                      'CreateIssue',
                                      'AddComment',
                                      'SetStatus',
                                      'SetAssignee',
                                      'SetPriority',
                                      'SetLabel'
                                    )),
    intent_payload      JSONB       NOT NULL,
    status              TEXT        NOT NULL DEFAULT 'pending'
                                      CHECK (status IN ('pending', 'confirmed', 'cancelled', 'expired')),
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (inbound_event_id, action_kind)
);

CREATE INDEX idx_channel_action_proposal_lookup
    ON channel_action_proposal(connection_id, chat_id, sender_external_id, (upper(code)), created_at DESC);

CREATE INDEX idx_channel_action_proposal_expiry
    ON channel_action_proposal(status, expires_at)
    WHERE status = 'pending';

CREATE TABLE channel_reply_context (
    connection_id       TEXT        NOT NULL REFERENCES channel_connection(id) ON DELETE CASCADE,
    external_user_id    TEXT        NOT NULL,
    workspace_id        UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id            UUID        NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    issue_identifier    TEXT        NOT NULL DEFAULT '',
    issue_title         TEXT        NOT NULL DEFAULT '',
    inbox_item_id       UUID        REFERENCES inbox_item(id) ON DELETE SET NULL,
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, external_user_id)
);

CREATE INDEX idx_channel_reply_context_expiry
    ON channel_reply_context (expires_at);
