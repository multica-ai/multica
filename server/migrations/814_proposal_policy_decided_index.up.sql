-- The weekly cap of a policy and its auto-approval history read the proposals a
-- policy decided, newest first.
CREATE INDEX CONCURRENTLY IF NOT EXISTS proposal_policy_decided_idx ON proposal (decided_by_policy_id, decided_at) WHERE decided_by_policy_id IS NOT NULL;
