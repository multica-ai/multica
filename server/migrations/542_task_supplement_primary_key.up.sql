-- Reuse the concurrently built unique index; do not build another index while
-- holding the table lock. Deploy only after the complete migration batch.
ALTER TABLE task_supplement
    ADD CONSTRAINT task_supplement_pkey PRIMARY KEY USING INDEX task_supplement_comment_uidx;
