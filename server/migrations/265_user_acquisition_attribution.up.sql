ALTER TABLE "user"
    ADD COLUMN acquisition_attribution JSONB;

COMMENT ON COLUMN "user".acquisition_attribution IS
    'Sanitized first-touch acquisition dimensions: source, medium, campaign, and referrer_host only. Never contains a full URL, query string, email, IP, content, or source code.';
