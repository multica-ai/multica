-- Partial index the reconciler tick uses to enumerate initiatives that still
-- need reconcile passes. Keep the status list in sync with the "active" set in
-- server/internal/service/initiative_transitions.go.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_initiative_active ON initiative (id) WHERE status IN ('planning', 'plan_review', 'executing', 'integrating', 'verifying', 'needs_human');
