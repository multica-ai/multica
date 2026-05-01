CREATE TABLE notification_preference (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    ntfy_url       TEXT,
    ntfy_token     TEXT,
    disabled_types TEXT[] NOT NULL DEFAULT '{}',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id)
);
