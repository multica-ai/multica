-- Dropping the table loses only derived state: inbox_item remains the complete
-- v1 truth at every point in this rollout, and reconcile rebuilds groups from
-- it when the table comes back.
DROP TABLE IF EXISTS inbox_group;
