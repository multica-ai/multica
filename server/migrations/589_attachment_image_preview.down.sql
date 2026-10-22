ALTER TABLE attachment
    DROP COLUMN IF EXISTS preview_url,
    DROP COLUMN IF EXISTS height,
    DROP COLUMN IF EXISTS width;
