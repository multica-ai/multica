ALTER TABLE feishu_project_integration
    ADD COLUMN IF NOT EXISTS assign_open_items_to_owner_agent BOOLEAN NOT NULL DEFAULT false;
