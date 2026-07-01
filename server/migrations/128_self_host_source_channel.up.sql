CREATE TABLE system_setting (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE self_host_source_channel (
    instance_hash TEXT NOT NULL CHECK (instance_hash ~ '^[0-9a-f]{64}$'),
    subject_hash TEXT NOT NULL CHECK (subject_hash ~ '^[0-9a-f]{64}$'),
    channel TEXT NOT NULL CHECK (channel IN (
        'friends_colleagues',
        'search',
        'social_x',
        'social_linkedin',
        'social_youtube',
        'social_github',
        'social_other',
        'blog_newsletter',
        'ai_assistant',
        'from_work',
        'event_conference',
        'dont_remember',
        'other'
    )),
    schema_version INTEGER NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    first_received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    report_count INTEGER NOT NULL DEFAULT 1 CHECK (report_count > 0),
    PRIMARY KEY (instance_hash, subject_hash)
);

CREATE INDEX idx_self_host_source_channel_channel
    ON self_host_source_channel(channel);

CREATE INDEX idx_self_host_source_channel_last_received_at
    ON self_host_source_channel(last_received_at DESC);
