-- MUL-5483 review follow-up: an opt-out has to remember HOW FAR it reaches.
--
-- 249 gave every unsubscribe a tombstone, and HasAncestorOptOut treats any
-- ancestor tombstone as "do not re-subscribe below here". That made the two UI
-- choices — "unsubscribe from this issue" and "…and its sub-issues" — differ
-- only for descendants that already exist: both silently suppressed FUTURE
-- children. Recording the scope is what makes the narrow choice actually narrow.
--
-- NULL means "not opted out" (the row is an active subscription). Only a
-- tombstoned row carries a scope.
ALTER TABLE issue_subscriber ADD COLUMN opt_out_scope TEXT
    CHECK (opt_out_scope IN ('issue', 'subtree'));

-- Rows tombstoned by 249 predate the distinction. They were all written by a
-- path that suppressed future children, so 'subtree' preserves their observed
-- behavior rather than silently widening or narrowing it.
UPDATE issue_subscriber SET opt_out_scope = 'subtree' WHERE unsubscribed_at IS NOT NULL;
