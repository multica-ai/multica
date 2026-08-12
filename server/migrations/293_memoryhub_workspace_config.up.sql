-- Workspace MemoryHub configuration: only a credential reference, a
-- user-key hash for verification, and a service id. No plaintext key and no
-- directly decryptable business ciphertext column lives here; the secret
-- envelope lives in memoryhub_secret.
CREATE TABLE memoryhub_workspace_config (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    credential_ref TEXT,
    user_key_hash TEXT,
    service_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
