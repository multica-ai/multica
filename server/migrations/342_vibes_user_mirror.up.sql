CREATE TABLE vibes_user_mirror (
    vibes_user_id TEXT NOT NULL,
    multica_user_id UUID NOT NULL,
    profile_email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
