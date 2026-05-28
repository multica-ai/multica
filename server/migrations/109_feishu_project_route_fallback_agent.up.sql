ALTER TABLE feishu_project_business_line_route
    ADD COLUMN IF NOT EXISTS fallback_agent_id UUID
        REFERENCES agent(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_feishu_business_line_route_fallback_agent
    ON feishu_project_business_line_route(fallback_agent_id)
    WHERE fallback_agent_id IS NOT NULL;
