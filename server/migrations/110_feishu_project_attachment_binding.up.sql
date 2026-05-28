-- Maps Feishu Project (Meego) attachment IDs to the local attachment rows
-- created from them. Mirrors the feishu_project_issue_binding pattern so the
-- sync flow doesn't have to dedup by filename — filename-based dedup creates
-- duplicate rows whenever Meego renames an attachment, and collapses two
-- attachments that happen to share a filename. Bindings keyed on the
-- external attachment ID fix both.

CREATE TABLE feishu_project_attachment_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    integration_id UUID NOT NULL REFERENCES feishu_project_integration(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    attachment_id UUID NOT NULL REFERENCES attachment(id) ON DELETE CASCADE,
    external_attachment_id TEXT NOT NULL,
    external_filename TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (integration_id, external_attachment_id)
);

CREATE INDEX idx_feishu_project_attachment_binding_issue
    ON feishu_project_attachment_binding(integration_id, issue_id);

CREATE INDEX idx_feishu_project_attachment_binding_attachment
    ON feishu_project_attachment_binding(attachment_id);
