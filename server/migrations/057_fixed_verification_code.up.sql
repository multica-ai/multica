CREATE TABLE fixed_verification_code (
    email TEXT PRIMARY KEY,
    code_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_fixed_verification_code_email
    ON fixed_verification_code(email);

INSERT INTO fixed_verification_code (email, code_hash)
VALUES (
    'tester@multica.com',
    '92925488b28ab12584ac8fcaa8a27a0f497b2c62940c8f4fbc8ef19ebc87c43e'
);
