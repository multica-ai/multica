DROP TABLE IF EXISTS crm_email_draft;

ALTER TABLE crm_imap_setting
    DROP COLUMN IF EXISTS smtp_secret_ref,
    DROP COLUMN IF EXISTS smtp_username,
    DROP COLUMN IF EXISTS smtp_tls_mode,
    DROP COLUMN IF EXISTS smtp_port,
    DROP COLUMN IF EXISTS smtp_host,
    DROP COLUMN IF EXISTS owner_id,
    DROP COLUMN IF EXISTS owner_type;
