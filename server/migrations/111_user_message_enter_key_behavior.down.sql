ALTER TABLE "user"
    DROP CONSTRAINT IF EXISTS user_message_enter_key_behavior_check,
    DROP COLUMN IF EXISTS message_enter_key_behavior;
