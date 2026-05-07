-- Rollback the two targeted renames from 056.up. The DO-block fallback
-- renames are not reversible in general (we don't record the prior slug),
-- but in practice only the two audited rows were touched in prod, and both
-- are identified by workspace_id so the down migration is deterministic.
UPDATE workspace SET slug = 'home'
  WHERE id = '68a982da-68a7-4e2e-ac8e-45a0323507f3' AND slug = 'home-1';
UPDATE workspace SET slug = 'dashboard'
  WHERE id = 'ea5a332f-06f9-480d-ab81-8f2324c92d80' AND slug = 'dashboard-1';
