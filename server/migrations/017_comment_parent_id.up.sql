-- CEREBRO-PATCH(migration-idempotent-017-comment-parent-id): cerebro modification of upstream file
ALTER TABLE comment ADD COLUMN IF NOT EXISTS parent_id UUID REFERENCES comment(id) ON DELETE SET NULL;
