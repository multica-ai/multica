-- Add render_mode preference per channel for users.
-- Values: 'auto' (default), 'compact', 'detail'.
ALTER TABLE notification_channel_preference
    ADD COLUMN render_mode TEXT NOT NULL DEFAULT 'auto'
    CHECK (render_mode IN ('auto', 'compact', 'detail'));
