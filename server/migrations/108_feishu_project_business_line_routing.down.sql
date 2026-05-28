DROP TABLE IF EXISTS feishu_project_business_line_route;

ALTER TABLE feishu_project_integration
    DROP COLUMN IF EXISTS business_line_field_key,
    DROP COLUMN IF EXISTS business_line_field_name;
