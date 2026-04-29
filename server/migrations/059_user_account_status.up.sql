-- Account lifecycle: suspend login without deleting the user row (self-host ops).
ALTER TABLE "user"
    ADD COLUMN account_status TEXT NOT NULL DEFAULT 'active'
    CONSTRAINT user_account_status_check CHECK (account_status IN ('active', 'suspended'));
