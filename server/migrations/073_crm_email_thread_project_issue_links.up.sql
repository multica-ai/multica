ALTER TABLE crm_email_thread
    ADD COLUMN project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    ADD COLUMN issue_id UUID REFERENCES issue(id) ON DELETE SET NULL;

CREATE INDEX idx_crm_email_thread_project_time ON crm_email_thread(project_id, COALESCE(last_message_at, updated_at) DESC) WHERE project_id IS NOT NULL;
CREATE INDEX idx_crm_email_thread_issue_time ON crm_email_thread(issue_id, COALESCE(last_message_at, updated_at) DESC) WHERE issue_id IS NOT NULL;
