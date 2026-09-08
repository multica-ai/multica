ALTER TABLE comment
    ADD COLUMN suggested_follow_ups jsonb NOT NULL DEFAULT '[]'::jsonb;
