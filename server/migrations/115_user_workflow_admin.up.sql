-- 115_user_workflow_admin.up.sql
-- Fix: add can_manage_workflows to user table. Migration 114 may have failed
-- on this statement due to "user" being a PostgreSQL reserved word.
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS can_manage_workflows BOOLEAN NOT NULL DEFAULT FALSE;
