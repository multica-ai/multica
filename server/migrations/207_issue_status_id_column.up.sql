-- Phase 1: authoritative issue.status_id, nullable during the migration
-- window. No FK (CLAUDE.md); resolved to issue_status in app code. Nothing
-- reads or writes it yet -- the legacy issue.status TEXT column stays the
-- source of truth until the Phase 2 double-write lands. Its index is created
-- CONCURRENTLY in migration 208.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS status_id UUID;
