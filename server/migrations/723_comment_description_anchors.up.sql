-- Additive metadata; deletion follows the owning comment, with no new ACL or index.
ALTER TABLE comment ADD COLUMN IF NOT EXISTS description_anchors jsonb NOT NULL DEFAULT '[]'::jsonb;
