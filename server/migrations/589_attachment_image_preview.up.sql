-- MUL-8182: image attachments carry their pixel dimensions and a downscaled
-- preview. Bodies render the preview inline and reserve the image's box before
-- it loads; the viewer still opens the original.
--
-- Nullable, no backfill: rows written before this, and every non-image file,
-- keep NULLs and render the original exactly as before.
ALTER TABLE attachment
    ADD COLUMN IF NOT EXISTS width INTEGER,
    ADD COLUMN IF NOT EXISTS height INTEGER,
    ADD COLUMN IF NOT EXISTS preview_url TEXT;
