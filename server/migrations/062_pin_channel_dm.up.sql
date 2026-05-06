-- Extend pinned_item.item_type to allow pinning channels and DMs alongside
-- issues and projects. Channels and DMs live in the issue table with
-- kind IN ('channel', 'dm'), so item_id still points at issue.id.

ALTER TABLE pinned_item DROP CONSTRAINT IF EXISTS pinned_item_item_type_check;
ALTER TABLE pinned_item ADD CONSTRAINT pinned_item_item_type_check
    CHECK (item_type IN ('issue', 'project', 'channel', 'dm'));
