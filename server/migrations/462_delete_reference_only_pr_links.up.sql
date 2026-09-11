-- Retire reference-only PR links. A link row was written for an issue key that
-- appeared ONLY as a bare mention in a PR body (no closing keyword, nothing in
-- the title or branch), then flagged reference_only and filtered out of every
-- read path: the issue PR list, the close-intent aggregate, and the review
-- dedup head SHA. The row therefore had no reader at all, and once the PR went
-- terminal the link upsert preserved the flag, so adding "Closes KEY" to a
-- merged PR's body could never surface the PR on its issue — the one recovery
-- action a user can take was the one that could not work.
--
-- The webhook now links only a key it read from the PR title, the branch name,
-- or a body closing keyword, which is exactly the set that was visible before.
-- Deleting the hidden rows keeps the two in step: left behind, they would
-- become visible — and block auto-advance while in flight — as soon as the read
-- filters went away.
--
-- Old API instances still running during a rolling deploy can insert a few more
-- such rows; the column stays in place for now, and the follow-up migration
-- that drops it re-runs this delete as a mop-up.
DELETE FROM issue_pull_request WHERE reference_only;
DELETE FROM issue_vcs_pull_request WHERE reference_only;
