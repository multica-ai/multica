CREATE TABLE hzt_external_identity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_subject TEXT NOT NULL,
    user_id UUID NOT NULL,
    username_snapshot TEXT NOT NULL DEFAULT '',
    email_snapshot TEXT,
    role_snapshot TEXT NOT NULL DEFAULT '',
    roles_snapshot JSONB NOT NULL DEFAULT '[]',
    last_verified_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
