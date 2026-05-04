ALTER TABLE project ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'none'
    CHECK (priority IN ('urgent', 'high', 'medium', 'low', 'none'));
