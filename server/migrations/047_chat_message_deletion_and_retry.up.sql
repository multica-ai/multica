-- Chat message delete / retry: no DDL required — uses existing `chat_message`.
-- Queries live in pkg/db/queries/chat.sql (sqlc). Do not put sqlc statements here;
-- migrate runs this file as plain PostgreSQL and does not substitute $1.
SELECT 1;
