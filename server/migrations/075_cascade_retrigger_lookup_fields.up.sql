-- PUL-102 wiring follow-up: add the columns the worker needs to
-- resolve `issue_id` after the fact.
--
-- PR3's webhook handler persists events on receive (A6 async
-- decoupling) but only stored the fields that came verbatim from the
-- GitHub payload's top-level keys: event_id, pr_url, pr_number,
-- head_sha, event_type. The cascade.Worker then runs
-- LookupIssueIdentifier(prTitle, branch) to find the issue —
-- primary on the [PUL-N] regex against PR title, fallback on the
-- agent-<id>/pul-N branch convention (G4).
--
-- Without these two columns the worker has nothing to look against
-- and falls back to scope_filter_skip on every row (as the PR4
-- defensive gate guards). Adding them flips the worker from "always
-- skip" to "real lookup".
--
-- Both columns are nullable: legacy rows persisted before this
-- migration have neither (the adapter wasn't passing them in), so
-- the worker continues to scope-skip those. New rows post-migration
-- carry both whenever GitHub's payload exposed them (workflow_run
-- omits the title — the branch fallback handles that case).

BEGIN;

ALTER TABLE cascade_retrigger
    ADD COLUMN pr_title TEXT NULL,
    ADD COLUMN branch   TEXT NULL;

COMMIT;
