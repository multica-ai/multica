-- OPE-2984 introduced "parent" as an issue_subscriber.reason value: when a
-- sub-issue is created, subscribers of its parent issue are inherited onto
-- the child (subscriber_listeners.go: inheritParentSubscribers). The
-- inheritance writes reason='parent' so inherited rows are distinguishable
-- from directly-subscribed ones.
--
-- Migration 015's CHECK constraint never included 'parent', so every
-- inherited-subscriber insert violated the constraint and was silently
-- dropped (addSubscriber logs+swallows the error). The feature was
-- effectively dead. Extend the whitelist to legitimize the value the
-- production code already writes.
ALTER TABLE issue_subscriber
    DROP CONSTRAINT IF EXISTS issue_subscriber_reason_check,
    ADD CONSTRAINT issue_subscriber_reason_check
        CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual', 'parent'));
