-- issue.start_date pairs with issue.due_date so an issue can express a planned
-- start as well as a deadline. Backs Project Gantt charts (MUL-1881) and the
-- progressive-disclosure "Add property" surface in the issue sidebar.
ALTER TABLE issue
    ADD COLUMN start_date TIMESTAMPTZ;
