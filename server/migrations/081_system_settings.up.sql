CREATE TABLE system_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Seed initial settings
INSERT INTO system_settings (key, value) VALUES ('custom_logo_url', '');
INSERT INTO system_settings (key, value) VALUES ('login_page_text', '');

-- Add system admin flag to users
ALTER TABLE "user" ADD COLUMN is_system_admin BOOLEAN NOT NULL DEFAULT FALSE;
