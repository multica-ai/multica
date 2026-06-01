-- Contact-sales inquiries submitted from the public marketing site.
--
-- The endpoint is unauthenticated so the row has no user_id / workspace_id.
-- Spam mitigation is handled by per-IP rate limiting + business-email
-- validation at the handler layer. We store the IP only so the abuse signal
-- survives a process restart; we never expose it back through the API.

CREATE TABLE contact_sales_inquiry (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    first_name          TEXT NOT NULL,
    last_name           TEXT NOT NULL,
    business_email      TEXT NOT NULL,
    company_name        TEXT NOT NULL,
    company_size        TEXT NOT NULL,
    country_region      TEXT NOT NULL,
    use_case            TEXT NOT NULL,
    goals               TEXT NOT NULL DEFAULT '',
    consent_outreach    BOOLEAN NOT NULL DEFAULT false,
    consent_updates     BOOLEAN NOT NULL DEFAULT false,
    submitter_ip        INET,
    user_agent          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_contact_sales_inquiry_created ON contact_sales_inquiry(created_at DESC);
CREATE INDEX idx_contact_sales_inquiry_email_created ON contact_sales_inquiry(business_email, created_at DESC);
