CREATE TABLE fixed_login_code (
    user_id UUID PRIMARY KEY REFERENCES "user"(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

