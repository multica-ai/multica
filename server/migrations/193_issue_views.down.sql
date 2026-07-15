DELETE FROM pinned_item WHERE item_type = 'view';

ALTER TABLE pinned_item DROP CONSTRAINT IF EXISTS pinned_item_item_type_check;
ALTER TABLE pinned_item
    ADD CONSTRAINT pinned_item_item_type_check
    CHECK (item_type IN ('issue', 'project'));

DROP TABLE IF EXISTS issue_view_preference;
DROP TABLE IF EXISTS issue_view;
