CREATE TABLE fixed_verification_code (
    email TEXT PRIMARY KEY,
    code_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
