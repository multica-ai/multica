DROP INDEX IF EXISTS idx_feishu_business_line_route_fallback_agent;

ALTER TABLE feishu_project_business_line_route
    DROP COLUMN IF EXISTS fallback_agent_id;
