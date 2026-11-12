ALTER TABLE comment
    ADD COLUMN pinned_at timestamptz,
    ADD COLUMN pinned_by_type text,
    ADD COLUMN pinned_by_id uuid;
