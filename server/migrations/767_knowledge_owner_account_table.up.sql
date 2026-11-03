-- The WeKnora login of each owner who enabled knowledge (plan 10.14), so an
-- owner can open WeKnora's own UI. The password is sealed with
-- MULTICA_KNOWLEDGE_SECRET_KEY, and the ciphertext is cleared when it is shown.
-- No primary key: the key is (workspace_id, user_id) in 768.
CREATE TABLE knowledge_owner_account (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    weknora_user_id TEXT NOT NULL,
    email TEXT NOT NULL,
    password_ciphertext BYTEA,
    revealed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
