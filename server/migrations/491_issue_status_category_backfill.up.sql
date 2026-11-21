-- Self-host upgrades converge automatically. SaaS must finish the bounded
-- maintenance job BEFORE deploying this revision, so this updates zero rows.
-- Keep data work separate from the ACCESS EXCLUSIVE constraint transaction.
UPDATE issue_status
SET category = issue_status_category(category)
WHERE category IN ('backlog', 'todo', 'in_progress', 'in_review', 'blocked', 'cancelled');
