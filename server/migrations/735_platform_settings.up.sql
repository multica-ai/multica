CREATE TABLE platform_settings (
    settings jsonb NOT NULL DEFAULT '{}'::jsonb
);
INSERT INTO platform_settings DEFAULT VALUES;
