CREATE TABLE IF NOT EXISTS crm_email_thread_issue_link (
    thread_id UUID NOT NULL REFERENCES crm_email_thread(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (thread_id, issue_id)
);

INSERT INTO crm_email_thread_issue_link (thread_id, issue_id)
SELECT id, issue_id FROM crm_email_thread WHERE issue_id IS NOT NULL
ON CONFLICT DO NOTHING;
