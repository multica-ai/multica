-- Merge-queue state for the GitHub PR API snapshot.
--
-- A pull request sitting in a repository's merge queue is still an open PR
-- whose mergeStateStatus says nothing about the queue, so the PR card could
-- only render it as a plain open PR. This column carries GitHub's merge-queue
-- entry state so the card can say "waiting its turn to merge" instead.
--
-- Nullable, like every other snapshot column: NULL means "not in a merge
-- queue", which is also the state of every PR in a repository that has no
-- merge queue configured and of every row written before this migration.

ALTER TABLE github_pull_request
    -- GraphQL `mergeQueueEntry.state`: QUEUED / AWAITING_CHECKS / MERGEABLE /
    -- UNMERGEABLE / LOCKED. NULL when the PR is not queued.
    ADD COLUMN api_merge_queue_state TEXT;
