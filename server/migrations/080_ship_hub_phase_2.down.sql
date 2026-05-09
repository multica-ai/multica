-- Reverse of 080_ship_hub_phase_2.up.sql in dependency order.
DROP TABLE IF EXISTS workspace_secret;
DROP INDEX IF EXISTS idx_pull_request_check_pr_head;
DROP TABLE IF EXISTS pull_request_check;
DROP INDEX IF EXISTS idx_pull_request_review_pr;
DROP TABLE IF EXISTS pull_request_review;
DROP INDEX IF EXISTS idx_github_webhook_delivery_received_at;
DROP TABLE IF EXISTS github_webhook_delivery;
ALTER TABLE workspace DROP COLUMN IF EXISTS ship_hub_webhook_secret;
