-- Remove the durable delivery receipts for the reverted comment-steering
-- feature. Fresh databases that never applied migrations 507/508 are unchanged;
-- staging databases that exercised the feature converge back to the old schema.
DROP TABLE IF EXISTS comment_agent_delivery;
