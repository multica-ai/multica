ALTER TABLE feishu_project_attachment_binding
    DROP CONSTRAINT IF EXISTS feishu_project_attachment_binding_int_issue_ext_key;

-- NOTE: this can fail if rows added under the wider key now share
-- (integration_id, external_attachment_id) across different issues. Dedup those
-- rows before rolling back.
ALTER TABLE feishu_project_attachment_binding
    ADD CONSTRAINT feishu_project_attachment_bin_integration_id_external_attac_key
    UNIQUE (integration_id, external_attachment_id);
