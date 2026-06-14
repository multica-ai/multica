ALTER TABLE "user"
    ADD COLUMN message_enter_key_behavior TEXT NOT NULL DEFAULT 'newline';

ALTER TABLE "user"
    ADD CONSTRAINT user_message_enter_key_behavior_check
    CHECK (message_enter_key_behavior IN ('send', 'newline'));
