-- Saved issue views become pinnable to the sidebar alongside issues and
-- projects. The pin row stores only (item_type, item_id); display data is
-- resolved client-side from the view detail endpoint.
ALTER TABLE pinned_item DROP CONSTRAINT pinned_item_item_type_check;
ALTER TABLE pinned_item ADD CONSTRAINT pinned_item_item_type_check
    CHECK (item_type IN ('issue', 'project', 'view'));
