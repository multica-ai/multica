-- Allow `archived` as a first-class issue status.
-- `archived` is closed (leaves default list/board/search, terminal for
-- stage-barrier) but NOT completed (excluded from done-counts).  DROP/re-ADD
-- the inline status CHECK with NOT VALID so pre-existing rows are not
-- re-validated under a lock.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;

ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN (
        'backlog',
        'todo',
        'in_progress',
        'in_review',
        'done',
        'blocked',
        'cancelled',
        'archived'
    )) NOT VALID;
