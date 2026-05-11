DROP TABLE IF EXISTS crm_email_message;
DROP TABLE IF EXISTS crm_email_thread;

DROP INDEX IF EXISTS idx_crm_contact_wechat;
DROP INDEX IF EXISTS idx_crm_contact_mobile;
DROP INDEX IF EXISTS idx_crm_contact_decision_role;
DROP INDEX IF EXISTS idx_crm_contact_account_primary;

ALTER TABLE crm_contact
    DROP CONSTRAINT IF EXISTS crm_contact_decision_role_check,
    DROP COLUMN IF EXISTS salutation,
    DROP COLUMN IF EXISTS job_title,
    DROP COLUMN IF EXISTS department,
    DROP COLUMN IF EXISTS role,
    DROP COLUMN IF EXISTS mobile,
    DROP COLUMN IF EXISTS whatsapp,
    DROP COLUMN IF EXISTS wechat,
    DROP COLUMN IF EXISTS linkedin_url,
    DROP COLUMN IF EXISTS preferred_language,
    DROP COLUMN IF EXISTS is_primary,
    DROP COLUMN IF EXISTS decision_role,
    DROP COLUMN IF EXISTS last_contacted_at;

DROP INDEX IF EXISTS idx_crm_account_unique_code;
DROP INDEX IF EXISTS idx_crm_account_workspace_follow_up;
DROP INDEX IF EXISTS idx_crm_account_workspace_priority;
DROP INDEX IF EXISTS idx_crm_account_workspace_rating;
DROP INDEX IF EXISTS idx_crm_account_workspace_source;
DROP INDEX IF EXISTS idx_crm_account_workspace_type;

ALTER TABLE crm_account
    DROP CONSTRAINT IF EXISTS crm_account_priority_check,
    DROP CONSTRAINT IF EXISTS crm_account_rating_check,
    DROP CONSTRAINT IF EXISTS crm_account_source_check,
    DROP CONSTRAINT IF EXISTS crm_account_account_type_check,
    DROP COLUMN IF EXISTS account_code,
    DROP COLUMN IF EXISTS account_type,
    DROP COLUMN IF EXISTS country_code,
    DROP COLUMN IF EXISTS country_name,
    DROP COLUMN IF EXISTS city,
    DROP COLUMN IF EXISTS sub_industry,
    DROP COLUMN IF EXISTS owner_member_id,
    DROP COLUMN IF EXISTS rating,
    DROP COLUMN IF EXISTS priority,
    DROP COLUMN IF EXISTS annual_revenue,
    DROP COLUMN IF EXISTS employee_count,
    DROP COLUMN IF EXISTS tags,
    DROP COLUMN IF EXISTS last_contacted_at,
    DROP COLUMN IF EXISTS next_follow_up_at;
