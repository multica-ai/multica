ALTER TABLE chat_session ADD COLUMN issue_id UUID REFERENCES issue(id) ON DELETE SET NULL;
CREATE INDEX idx_chat_session_issue ON chat_session(issue_id) WHERE issue_id IS NOT NULL;
