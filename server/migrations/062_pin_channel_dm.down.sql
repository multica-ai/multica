-- Drop any channel/dm pins so the narrower constraint can be reinstated.
DELETE FROM pinned_item WHERE item_type IN ('channel', 'dm');

ALTER TABLE pinned_item DROP CONSTRAINT IF EXISTS pinned_item_item_type_check;
ALTER TABLE pinned_item ADD CONSTRAINT pinned_item_item_type_check
    CHECK (item_type IN ('issue', 'project'));
