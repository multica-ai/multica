DROP TABLE IF EXISTS comment_create_request;

ALTER TABLE issue_create_request
    DROP CONSTRAINT issue_create_request_issue_id_fkey;

ALTER TABLE issue_create_request
    ADD CONSTRAINT issue_create_request_issue_id_fkey
    FOREIGN KEY (issue_id) REFERENCES issue(id) ON DELETE CASCADE;

ALTER TABLE issue_create_request
    DROP COLUMN deleted_at;
