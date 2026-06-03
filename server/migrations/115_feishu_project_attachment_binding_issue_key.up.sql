-- The same Feishu attachment can be referenced by multiple work items in one
-- integration (e.g. the same screenshot pasted into two bugs). The original
-- UNIQUE (integration_id, external_attachment_id) let only the first issue bind
-- it; every other issue's copy failed the binding insert and re-downloaded on
-- every sync. Scope the uniqueness to the issue so each issue binds its own copy.
--
-- Widening the key (adding a column) cannot introduce violations on existing
-- rows that already satisfied the narrower key.
ALTER TABLE feishu_project_attachment_binding
    DROP CONSTRAINT IF EXISTS feishu_project_attachment_bin_integration_id_external_attac_key;

ALTER TABLE feishu_project_attachment_binding
    ADD CONSTRAINT feishu_project_attachment_binding_int_issue_ext_key
    UNIQUE (integration_id, issue_id, external_attachment_id);
