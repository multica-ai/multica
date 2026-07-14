ALTER TABLE agent
ADD COLUMN kind TEXT NOT NULL DEFAULT 'user'
    CHECK (kind IN ('user', 'system')),
ADD COLUMN system_key TEXT;
