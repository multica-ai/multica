-- Nullable metadata preserves compatibility with older servers and uploads.
ALTER TABLE attachment
    ADD COLUMN width integer,
    ADD COLUMN height integer;
