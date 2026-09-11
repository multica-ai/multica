-- Bind an OAuth account to a stable provider subject instead of a mutable
-- profile field such as email. The issuer is part of the key because separate
-- self-hosted Gitea installations can reuse numeric user IDs.
CREATE TABLE external_auth_identity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    provider TEXT NOT NULL,
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
