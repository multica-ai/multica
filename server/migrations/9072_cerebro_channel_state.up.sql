-- Per-(channel × user) inbox controls for channels/DMs: "remind me" (snooze)
-- and "mark as unread". Channels are issues with kind in ('channel', 'dm');
-- this table overlays the computed unread state with the user's own inbox
-- controls, mirroring what inbox_item.muted_until / read already give a single
-- notification. Absence of a row = default (not snoozed, not manually unread).
--
--   muted_until  — snooze target. While in the future the channel is hidden
--                  from the normal inbox views and shown only in "Muted",
--                  resurfacing on its own once the time passes (same contract
--                  as inbox_item.muted_until).
--   unread_at    — set when the user manually marks the conversation unread.
--                  Overlays unread_count so the row shows as unread again, and
--                  gives a stable sort time. Cleared when the channel is read.
CREATE TABLE IF NOT EXISTS cerebro_channel_state (
    channel_id  uuid        NOT NULL,
    user_id     uuid        NOT NULL,
    muted_until timestamptz,
    unread_at   timestamptz,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, user_id)
);

CREATE INDEX IF NOT EXISTS cerebro_channel_state_user_idx
    ON cerebro_channel_state (user_id);
