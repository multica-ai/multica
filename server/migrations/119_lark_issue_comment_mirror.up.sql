CREATE TABLE IF NOT EXISTS lark_issue_comment_mirror (
    comment_id UUID PRIMARY KEY REFERENCES comment(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    chat_session_id UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    installation_id UUID NOT NULL REFERENCES lark_installation(id) ON DELETE CASCADE,
    lark_chat_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'claimed' CHECK (status IN ('claimed', 'sent', 'failed')),
    error TEXT,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lark_issue_comment_mirror_issue_id
    ON lark_issue_comment_mirror(issue_id);

CREATE INDEX IF NOT EXISTS idx_lark_issue_comment_mirror_chat_session_id
    ON lark_issue_comment_mirror(chat_session_id);

CREATE INDEX IF NOT EXISTS idx_lark_issue_comment_mirror_installation_status
    ON lark_issue_comment_mirror(installation_id, status);
