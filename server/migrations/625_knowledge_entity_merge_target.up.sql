-- Merge is a logical redirect. Raw evidence and relation rows keep their
-- original entity IDs; graph reads resolve this column to the survivor.
ALTER TABLE knowledge_entity ADD COLUMN IF NOT EXISTS merged_into_id UUID;
