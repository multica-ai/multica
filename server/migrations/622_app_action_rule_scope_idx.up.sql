-- One rule per class for the team account and per member for their personal
-- account. NULL owner (team) folds to the nil UUID so it is unique too.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_app_action_rule_scope
    ON app_action_rule (workspace_id, toolkit_slug, action_class, COALESCE(owner_user_id, '00000000-0000-0000-0000-000000000000'::uuid));
