CREATE INDEX CONCURRENTLY idx_marketplace_template_catalog ON marketplace_template (visibility, featured_at DESC NULLS LAST, applied_count DESC, updated_at DESC);
