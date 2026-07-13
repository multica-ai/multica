-- Migration 162 restores the legacy index after it removes non-issue labels.
-- Recreating it here could fail while those rows still exist during rollback.
SELECT 1;
