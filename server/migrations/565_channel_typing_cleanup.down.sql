DROP TABLE IF EXISTS channel_typing_reaction;
ALTER TABLE chat_message DROP COLUMN IF EXISTS channel_typing_settled;
