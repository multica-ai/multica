-- A null position preserves the existing per-member workspace order.
ALTER TABLE member ADD COLUMN IF NOT EXISTS workspace_position INTEGER;
