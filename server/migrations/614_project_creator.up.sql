-- Existing projects have unknown creators and remain deletable only by
-- workspace owners/admins. The mutable lead is not evidence of authorship.
ALTER TABLE project
    ADD COLUMN creator_type TEXT,
    ADD COLUMN creator_id UUID;
